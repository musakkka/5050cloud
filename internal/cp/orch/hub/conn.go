// SPDX-License-Identifier: AGPL-3.0-only

package hub

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// conn is one agent socket.
type conn struct {
	h        *Hub
	ws       *websocket.Conn
	hostID   string
	hostName string
	ip       string
	version  string
	hello    proto.Hello
	cancel   context.CancelFunc
	connCtx  context.Context

	active atomic.Bool
	closed atomic.Bool

	gen      atomic.Int64 // last generation sent
	applied  atomic.Int64 // last generation the agent applied
	markSeq  atomic.Uint64
	lastSeen atomic.Int64
	dirty    chan struct{}

	pmu    sync.Mutex
	pushes []pushRec

	wmu     sync.Mutex
	waiters map[string]*waiter

	smu     sync.Mutex
	streams map[uint64]*Stream
	nextSID atomic.Uint64
}

type pushRec struct {
	seq uint64
	gen int64
}

type waiter struct {
	stream     func(proto.Envelope)
	persistent bool
	done       chan struct{}
	once       sync.Once
	reply      proto.Envelope
	err        error
}

func (w *waiter) finish(e proto.Envelope, err error) {
	w.once.Do(func() {
		w.reply, w.err = e, err
		close(w.done)
	})
}

func (h *Hub) newConn(ws *websocket.Conn, hostID, name, ip, version string) *conn {
	c := &conn{
		h: h, ws: ws, hostID: hostID, hostName: name, ip: ip, version: version,
		dirty:   make(chan struct{}, 1),
		waiters: map[string]*waiter{},
		streams: map[uint64]*Stream{},
	}
	c.lastSeen.Store(h.now().UnixMilli())
	return c
}

// final reply types end a Call.
func isFinal(t string) bool {
	switch t {
	case proto.TLogsResult, proto.TJobResult, proto.TBuildResult, proto.TAck:
		return true
	}
	return false
}

func (c *conn) readHello(ctx context.Context, timeout time.Duration) (proto.Hello, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	typ, data, err := c.ws.Read(rctx)
	if err != nil {
		return proto.Hello{}, err
	}
	if typ != websocket.MessageText {
		return proto.Hello{}, errors.New("first frame is not text")
	}
	var env proto.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.T != proto.THello {
		return proto.Hello{}, errors.New("first message is not hello")
	}
	var hello proto.Hello
	if err := json.Unmarshal(env.D, &hello); err != nil {
		return proto.Hello{}, err
	}
	return hello, nil
}

func (c *conn) send(ctx context.Context, t, id, ref string, payload any) error {
	var d json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		d = b
	}
	b, err := json.Marshal(proto.Envelope{T: t, ID: id, Ref: ref, D: d})
	if err != nil {
		return err
	}
	if len(b) > proto.MaxMessageBytes {
		return errorf("The message for host %s is %d bytes, above the %d byte limit.", c.hostName, len(b), proto.MaxMessageBytes)
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.ws.Write(wctx, websocket.MessageText, b); err != nil {
		c.close(websocket.StatusGoingAway, "write failed")
		return err
	}
	return nil
}

func (c *conn) sendBinary(ctx context.Context, kind byte, sid uint64, payload []byte) error {
	b := make([]byte, 9+len(payload))
	b[0] = kind
	binary.BigEndian.PutUint64(b[1:9], sid)
	copy(b[9:], payload)
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.ws.Write(wctx, websocket.MessageBinary, b); err != nil {
		c.close(websocket.StatusGoingAway, "write failed")
		return err
	}
	return nil
}

func (c *conn) register(id string, stream func(proto.Envelope), persistent bool) *waiter {
	w := &waiter{stream: stream, persistent: persistent, done: make(chan struct{})}
	c.wmu.Lock()
	c.waiters[id] = w
	c.wmu.Unlock()
	if c.closed.Load() {
		w.finish(proto.Envelope{}, errorf("Host %s disconnected before it answered.", c.hostName))
	}
	return w
}

func (c *conn) unregister(id string) {
	c.wmu.Lock()
	delete(c.waiters, id)
	c.wmu.Unlock()
}

func (c *conn) call(ctx context.Context, t, id string, payload any, stream func(proto.Envelope), timeout time.Duration) (proto.Envelope, error) {
	w := c.register(id, stream, false)
	defer c.unregister(id)
	if err := c.send(ctx, t, id, "", payload); err != nil {
		return proto.Envelope{}, err
	}
	var tc <-chan time.Time
	if timeout > 0 {
		tm := time.NewTimer(timeout)
		defer tm.Stop()
		tc = tm.C
	}
	select {
	case <-w.done:
		return w.reply, w.err
	case <-ctx.Done():
		return proto.Envelope{}, ctx.Err()
	case <-tc:
		return proto.Envelope{}, errorf("Host %s did not answer within %s.", c.hostName, timeout)
	}
}

func (c *conn) failPending(err error) {
	c.wmu.Lock()
	ws := make([]*waiter, 0, len(c.waiters))
	for _, w := range c.waiters {
		ws = append(ws, w)
	}
	c.wmu.Unlock()
	for _, w := range ws {
		w.finish(proto.Envelope{}, err)
	}
	c.smu.Lock()
	ss := make([]*Stream, 0, len(c.streams))
	for _, s := range c.streams {
		ss = append(ss, s)
	}
	c.smu.Unlock()
	for _, s := range ss {
		s.remoteClosed(-1, err.Error())
	}
}

func (c *conn) close(code websocket.StatusCode, reason string) {
	if c.closed.Swap(true) {
		return
	}
	_ = c.ws.Close(code, reason)
	if c.cancel != nil {
		c.cancel()
	}
}

// routeKey finds which pending request a message belongs to: Ref, or the
// build, job or subscription id in the payload (they equal the request id).
func routeKey(e proto.Envelope) string {
	if e.Ref != "" {
		return e.Ref
	}
	switch e.T {
	case proto.TBuildLog, proto.TBuildResult:
		var v struct {
			BuildID string `json:"buildId"`
		}
		_ = json.Unmarshal(e.D, &v)
		return v.BuildID
	case proto.TJobLog, proto.TJobResult:
		var v struct {
			JobID string `json:"jobId"`
		}
		_ = json.Unmarshal(e.D, &v)
		return v.JobID
	case proto.TLogLines:
		var v struct {
			SubID string `json:"subId"`
		}
		_ = json.Unmarshal(e.D, &v)
		return v.SubID
	}
	return ""
}

func (c *conn) readLoop(ctx context.Context) {
	defer c.close(websocket.StatusNormalClosure, "")
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		c.lastSeen.Store(c.h.now().UnixMilli())
		if typ == websocket.MessageBinary {
			c.onBinary(data)
			continue
		}
		var env proto.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.h.log.Warn("agent sent invalid JSON", "host", c.hostID)
			continue
		}
		c.dispatch(ctx, env)
	}
}

func (c *conn) dispatch(ctx context.Context, env proto.Envelope) {
	switch env.T {
	case proto.TExecExit, proto.TExecClose, proto.TTCPClose:
		c.onStreamEnd(env)
		return
	}
	if key := routeKey(env); key != "" {
		c.wmu.Lock()
		w := c.waiters[key]
		c.wmu.Unlock()
		if w != nil {
			if isFinal(env.T) && !w.persistent {
				w.finish(env, nil)
				return
			}
			if !isFinal(env.T) {
				if w.stream != nil {
					w.stream(env)
				}
				return
			}
		}
	}
	if env.T == proto.TStateApplied {
		var sa proto.StateApplied
		if json.Unmarshal(env.D, &sa) == nil && (sa.Epoch == "" || sa.Epoch == c.h.Epoch()) {
			for {
				cur := c.applied.Load()
				if sa.Generation <= cur || c.applied.CompareAndSwap(cur, sa.Generation) {
					break
				}
			}
		}
	}
	if !c.active.Load() {
		return
	}
	c.h.o.Handler.HostMessage(ctx, c.hostID, env)
}

func (c *conn) trigger() {
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

// pushLoop sends the host's desired state whenever it is marked dirty, at
// most once per PushInterval.
func (c *conn) pushLoop(ctx context.Context) {
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.dirty:
		}
		if wait := c.h.o.PushInterval - time.Since(last); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
		last = time.Now()
		if err := c.push(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.h.log.Error("desired state push", "host", c.hostID, "err", err)
			// retry after the next interval
			go func() {
				time.Sleep(time.Second)
				c.trigger()
			}()
		}
	}
}

func (c *conn) push(ctx context.Context) error {
	seq := c.markSeq.Load()
	ds, err := c.h.o.Handler.DesiredState(ctx, c.hostID)
	if err != nil {
		return err
	}
	gen := c.gen.Load() + 1
	if err := c.h.o.Write(ctx, func(tx *sql.Tx) error { return PutState(ctx, tx, "gen:"+c.hostID, gen) }); err != nil {
		return err
	}
	c.gen.Store(gen)
	ds.Generation = gen
	ds.Epoch = c.h.Epoch()
	ds.Adopt = c.h.Adopting()
	if err := c.send(ctx, proto.TDesired, "", "", ds); err != nil {
		return err
	}
	c.pmu.Lock()
	c.pushes = append(c.pushes, pushRec{seq: seq, gen: gen})
	if len(c.pushes) > 32 {
		c.pushes = c.pushes[len(c.pushes)-32:]
	}
	c.pmu.Unlock()
	return nil
}

// appliedSince reports whether a state built at or after mark seq has been applied.
func (c *conn) appliedSince(seq uint64) bool {
	c.pmu.Lock()
	defer c.pmu.Unlock()
	for _, p := range c.pushes {
		if p.seq >= seq {
			return c.applied.Load() >= p.gen
		}
	}
	return false
}

func (c *conn) pingLoop(ctx context.Context) {
	t := time.NewTicker(c.h.o.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, c.h.o.DeadAfter-c.h.o.PingInterval)
		err := c.ws.Ping(pctx)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				c.close(websocket.StatusGoingAway, "no answer to ping")
			}
			return
		}
		c.lastSeen.Store(c.h.now().UnixMilli())
	}
}

// LastSeen returns when a connected host last sent anything (Unix ms, 0 when offline).
func (h *Hub) LastSeen(hostID string) int64 {
	if c := h.get(hostID); c != nil {
		return c.lastSeen.Load()
	}
	return 0
}
