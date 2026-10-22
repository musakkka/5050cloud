// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxPending is how much output a detached exec session keeps for reattach.
const maxPending = 1 << 20

// inQueue is how many inbound frames a stream buffers before the socket
// read loop waits (backpressure, 7.5).
const inQueue = 64

// defaultShell prefers bash and falls back to sh.
var defaultShell = []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"}

type streamKey struct {
	kind byte
	id   uint64
}

type stream struct {
	kind byte
	in   chan []byte
	done chan struct{}
	once sync.Once

	mu       sync.Mutex
	id       uint64
	sess     *session
	name     string // exec session name, kept for reattach
	ex       engine.ExecSession
	conn     net.Conn
	pending  []byte
	exited   bool
	exitCode int
	reaper   *time.Timer
}

func (st *stream) finish() {
	st.once.Do(func() {
		close(st.done)
		if st.ex != nil {
			_ = st.ex.Close()
		}
		if st.conn != nil {
			_ = st.conn.Close()
		}
	})
}

// streamTable holds the open exec sessions and TCP tunnels.
type streamTable struct {
	mu      sync.Mutex
	streams map[streamKey]*stream
	named   map[string]*stream
}

func newStreamTable() *streamTable {
	return &streamTable{streams: map[streamKey]*stream{}, named: map[string]*stream{}}
}

func (t *streamTable) add(st *stream) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streams[streamKey{st.kind, st.id}] = st
	if st.name != "" {
		t.named[st.name] = st
	}
}

func (t *streamTable) get(kind byte, id uint64) *stream {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.streams[streamKey{kind, id}]
}

func (t *streamTable) remove(st *stream) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st.mu.Lock()
	key := streamKey{st.kind, st.id}
	name := st.name
	st.mu.Unlock()
	if t.streams[key] == st {
		delete(t.streams, key)
	}
	if name != "" && t.named[name] == st {
		delete(t.named, name)
	}
}

// count returns the number of open streams (tests).
func (t *streamTable) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.streams)
}

// input hands an inbound binary frame to its stream. It blocks while the
// stream's queue is full, which pauses the socket read loop.
func (t *streamTable) input(kind byte, id uint64, payload []byte) {
	st := t.get(kind, id)
	if st == nil {
		return
	}
	p := append([]byte(nil), payload...)
	select {
	case st.in <- p:
	case <-st.done:
	}
}

func (t *streamTable) resize(ctx context.Context, r proto.ExecResize) {
	st := t.get(proto.BinExec, r.StreamID)
	if st == nil || st.ex == nil {
		return
	}
	_ = st.ex.Resize(ctx, r.Cols, r.Rows)
}

func (t *streamTable) closeStream(kind byte, id uint64) {
	st := t.get(kind, id)
	if st == nil {
		return
	}
	t.remove(st)
	st.finish()
}

// detachSession runs when a socket closes: named exec sessions are kept
// for keep (10 minutes) so the console can reattach; everything else of
// that socket ends.
func (t *streamTable) detachSession(s *session, keep time.Duration) {
	t.mu.Lock()
	var mine []*stream
	for _, st := range t.streams {
		st.mu.Lock()
		if st.sess == s {
			mine = append(mine, st)
		}
		st.mu.Unlock()
	}
	t.mu.Unlock()
	for _, st := range mine {
		st.mu.Lock()
		if st.kind == proto.BinExec && st.name != "" && !st.exited {
			st.sess = nil
			st.reaper = time.AfterFunc(keep, func() {
				t.remove(st)
				st.finish()
			})
			st.mu.Unlock()
			continue
		}
		st.mu.Unlock()
		t.remove(st)
		st.finish()
	}
}

// reattach binds a detached named session to a new socket and stream id.
func (t *streamTable) reattach(name string, s *session, newID uint64) *stream {
	t.mu.Lock()
	st := t.named[name]
	if st == nil {
		t.mu.Unlock()
		return nil
	}
	st.mu.Lock()
	if st.sess != nil {
		st.mu.Unlock()
		t.mu.Unlock()
		return nil // attached elsewhere
	}
	delete(t.streams, streamKey{st.kind, st.id})
	st.id = newID
	st.sess = s
	if st.reaper != nil {
		st.reaper.Stop()
		st.reaper = nil
	}
	t.streams[streamKey{st.kind, newID}] = st
	st.mu.Unlock()
	t.mu.Unlock()
	return st
}

// deliver sends output to the attached socket, or keeps it while detached.
func (a *Agent) deliver(st *stream, p []byte) {
	st.mu.Lock()
	s, id := st.sess, st.id
	if s == nil {
		st.pending = append(st.pending, p...)
		if len(st.pending) > maxPending {
			st.pending = append(st.pending[:0], st.pending[len(st.pending)-maxPending:]...)
		}
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()
	if !a.sendBinary(s, st.kind, id, p) {
		st.mu.Lock()
		st.pending = append(st.pending, p...)
		if len(st.pending) > maxPending {
			st.pending = append(st.pending[:0], st.pending[len(st.pending)-maxPending:]...)
		}
		st.mu.Unlock()
	}
}

func (a *Agent) openExec(ctx context.Context, s *session, reqID string, o proto.ExecOpen) {
	if o.Session != "" {
		if st := a.streams.reattach(o.Session, s, o.StreamID); st != nil {
			a.send(proto.TAck, reqID, proto.Ack{OK: true})
			st.mu.Lock()
			pending := st.pending
			st.pending = nil
			exited, code := st.exited, st.exitCode
			st.mu.Unlock()
			if len(pending) > 0 {
				a.deliver(st, pending)
			}
			if exited {
				a.send(proto.TExecExit, "", proto.ExecExit{StreamID: o.StreamID, Code: code})
				a.streams.remove(st)
				st.finish()
			}
			return
		}
	}
	cmd := o.Cmd
	if len(cmd) == 0 {
		cmd = defaultShell
	}
	var env []string
	if o.TTY {
		env = []string{"TERM=xterm-256color"}
	}
	ex, err := a.eng.Exec(ctx, ContainerName(o.ReplicaID), engine.ExecSpec{Cmd: cmd, Env: env, TTY: o.TTY, Stdin: true, Cols: o.Cols, Rows: o.Rows})
	if err != nil {
		msg := fmt.Sprintf("Could not open a shell in replica %s: %s", o.ReplicaID, sentence(err))
		if errors.Is(err, engine.ErrNotFound) {
			msg = fmt.Sprintf("Replica %s is not running on this host.", o.ReplicaID)
		}
		a.send(proto.TAck, reqID, proto.Ack{Error: msg})
		a.send(proto.TExecExit, "", proto.ExecExit{StreamID: o.StreamID, Code: -1})
		return
	}
	st := &stream{kind: proto.BinExec, id: o.StreamID, sess: s, name: o.Session, ex: ex, in: make(chan []byte, inQueue), done: make(chan struct{})}
	a.streams.add(st)
	a.send(proto.TAck, reqID, proto.Ack{OK: true})
	go func() {
		for {
			select {
			case p := <-st.in:
				if _, err := ex.Input().Write(p); err != nil {
					return
				}
			case <-st.done:
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 32<<10)
		out := ex.Output()
		for {
			n, err := out.Read(buf)
			if n > 0 {
				a.deliver(st, append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				break
			}
		}
		code, err := ex.Wait(context.WithoutCancel(ctx))
		if err != nil {
			code = -1
		}
		st.mu.Lock()
		st.exited, st.exitCode = true, code
		sess, id := st.sess, st.id
		st.mu.Unlock()
		if sess != nil {
			a.send(proto.TExecExit, "", proto.ExecExit{StreamID: id, Code: code})
			a.streams.remove(st)
			st.finish()
		}
		// A detached session keeps its exit code until reattach or the reaper.
	}()
}

// openTCP opens a tunnel to a replica port (5050 connect). Only addresses
// inside the pod range are allowed, so the agent never becomes a proxy to
// the host or the internet.
func (a *Agent) openTCP(ctx context.Context, s *session, reqID string, o proto.TCPOpen) {
	fail := func(msg string) {
		a.send(proto.TTCPClose, "", proto.StreamClose{StreamID: o.StreamID, Error: msg})
		if reqID != "" {
			a.send(proto.TAck, reqID, proto.Ack{Error: msg})
		}
	}
	host, port, err := net.SplitHostPort(o.Target)
	ip := net.ParseIP(host)
	if err != nil || ip == nil {
		fail(fmt.Sprintf("The tunnel target %q must be an IP address and a port.", o.Target))
		return
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		fail(fmt.Sprintf("The tunnel target %q has no valid port.", o.Target))
		return
	}
	podCIDR := "10.128.0.0/9"
	a.mu.Lock()
	if a.desired != nil && a.desired.Host.PodCIDR != "" {
		podCIDR = a.desired.Host.PodCIDR
	}
	a.mu.Unlock()
	if _, n, err := net.ParseCIDR(podCIDR); err != nil || !n.Contains(ip) {
		fail(fmt.Sprintf("The tunnel target %s is not a replica address.", o.Target))
		return
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", o.Target)
	if err != nil {
		fail(fmt.Sprintf("Could not connect to %s: %s", o.Target, sentence(err)))
		return
	}
	a.serveTCP(ctx, s, reqID, o.StreamID, conn)
}

func (a *Agent) serveTCP(ctx context.Context, s *session, reqID string, id uint64, conn net.Conn) {
	st := &stream{kind: proto.BinTCP, id: id, sess: s, conn: conn, in: make(chan []byte, inQueue), done: make(chan struct{})}
	a.streams.add(st)
	if reqID != "" {
		a.send(proto.TAck, reqID, proto.Ack{OK: true})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	errc := make(chan error, 2)
	go func() { // control plane -> replica
		defer wg.Done()
		for {
			select {
			case p := <-st.in:
				if len(p) == 0 {
					if cw, ok := conn.(interface{ CloseWrite() error }); ok {
						_ = cw.CloseWrite()
					}
					return
				}
				if _, err := conn.Write(p); err != nil {
					errc <- err
					return
				}
			case <-st.done:
				return
			}
		}
	}()
	go func() { // replica -> control plane
		defer wg.Done()
		buf := make([]byte, 32<<10)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if !a.sendBinary(s, proto.BinTCP, id, append([]byte(nil), buf[:n]...)) {
					errc <- errors.New("the control plane connection closed")
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					errc <- err
					return
				}
				a.sendBinary(s, proto.BinTCP, id, nil) // half-close
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(errc)
	}()
	var firstErr error
	for e := range errc {
		if firstErr == nil {
			firstErr = e
			st.finish() // unblock the other direction
		}
	}
	a.streams.remove(st)
	st.finish()
	msg := ""
	if firstErr != nil && !errors.Is(firstErr, net.ErrClosed) {
		msg = sentence(firstErr)
	}
	a.send(proto.TTCPClose, "", proto.StreamClose{StreamID: id, Error: msg})
}
