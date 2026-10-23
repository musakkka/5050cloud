// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// session is one WebSocket connection to the control plane.
type session struct {
	id   uint64
	c    *websocket.Conn
	ctl  chan []byte // JSON envelopes, written before binary data
	data chan []byte // binary frames
	done chan struct{}
	once sync.Once
	// subs are live log tails started on this connection.
	mu   sync.Mutex
	subs map[string]context.CancelFunc
}

func (s *session) close() {
	s.once.Do(func() { close(s.done) })
}

// errRefused is returned when the control plane closed with a reason that
// must not be retried quickly.
type errRefused struct {
	reason string
	wait   time.Duration
}

func (e errRefused) Error() string { return "the control plane refused this agent: " + e.reason }

// WebSocketURL turns the console URL into the agent connect URL.
func WebSocketURL(control string) (string, error) {
	u, err := url.Parse(strings.TrimRight(control, "/"))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("the control plane address %q is not a valid URL", control)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("the control plane address %q must start with https:// or http://", control)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/agent/v1/connect"
	return u.String(), nil
}

// HTTPClient returns a client that trusts the system roots plus caPEM, and
// when pin is set ("sha256:<hex>") accepts a server certificate with that
// SHA-256 fingerprint, or one signed by a certificate with it, even if no root
// signs it (see verifyPinned).
func HTTPClient(caPEM []byte, pin string) (*http.Client, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(caPEM) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("the CA certificate for the console is not valid PEM")
		}
		cfg.RootCAs = pool
	}
	if pin != "" {
		want := strings.ToLower(strings.TrimPrefix(pin, "sha256:"))
		want = strings.ReplaceAll(want, ":", "")
		if len(want) != 64 {
			return nil, fmt.Errorf("the certificate pin %q must look like sha256:<64 hex characters>", pin)
		}
		cfg.InsecureSkipVerify = true // replaced by the pin check below
		cfg.VerifyConnection = func(cs tls.ConnectionState) error { return verifyPinned(cs, want) }
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	return &http.Client{Transport: tr, Timeout: 0}, nil
}

// verifyPinned accepts a connection when the server's leaf certificate has the
// pinned fingerprint, or when a certificate with that fingerprint signs the
// chain: the pinned certificate becomes the only trust root and the leaf must
// verify against it for the requested host name. A pinned certificate that
// merely appears in the chain (it is public) is not enough.
func verifyPinned(cs tls.ConnectionState, want string) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("the console sent no certificate")
	}
	fp := func(c *x509.Certificate) string {
		sum := sha256.Sum256(c.Raw)
		return hex.EncodeToString(sum[:])
	}
	leaf := cs.PeerCertificates[0]
	if fp(leaf) == want {
		return nil
	}
	roots := x509.NewCertPool()
	inter := x509.NewCertPool()
	pinned := false
	for _, c := range cs.PeerCertificates[1:] {
		if fp(c) == want {
			roots.AddCert(c)
			pinned = true
		} else {
			inter.AddCert(c)
		}
	}
	if pinned {
		_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, DNSName: cs.ServerName})
		if err == nil {
			return nil
		}
		return fmt.Errorf("the console certificate is not signed by the pinned certificate: %w", err)
	}
	return errors.New("the console certificate does not match the pinned fingerprint")
}

// connectLoop keeps one connection up, reconnecting with exponential
// backoff from 1 s to 30 s with full jitter (7.1 step 4).
func (a *Agent) connectLoop(ctx context.Context) {
	backoff := a.t.backoffMin
	for ctx.Err() == nil {
		if brokenBuild {
			a.log.Error("This agent was built with the e2e_broken_agent tag and never connects to the control plane (test E29).")
			if !sleepCtx(ctx, a.t.backoffMax) {
				return
			}
			continue
		}
		started := time.Now()
		err := a.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		var ref errRefused
		wait := time.Duration(0)
		switch {
		case errors.As(err, &ref):
			a.log.Error(ref.Error(), "reason", ref.reason)
			wait = ref.wait
		case err != nil:
			a.log.Warn("lost the connection to the control plane", "err", err)
		}
		if time.Since(started) > time.Minute {
			backoff = a.t.backoffMin // the connection was healthy for a while
		}
		if wait == 0 {
			// Full jitter: a random wait between 0 and the current backoff.
			wait = time.Duration(rand.Int64N(int64(backoff)) + 1)
			backoff = min(backoff*2, a.t.backoffMax)
		}
		if !sleepCtx(ctx, wait) {
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (a *Agent) connectOnce(ctx context.Context) error {
	wsURL, err := WebSocketURL(a.o.ControlURL)
	if err != nil {
		return err
	}
	client, err := HTTPClient(a.o.CACertPEM, a.o.CAPin)
	if err != nil {
		return err
	}
	a.mu.Lock()
	token := a.token
	a.mu.Unlock()
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+token)
	hdr.Set("X-5050-Agent-Version", a.o.Version)
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	c, resp, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      hdr,
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	cancel()
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("the control plane did not accept the host token (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("could not connect to %s: %w", wsURL, err)
	}
	c.SetReadLimit(proto.MaxMessageBytes)
	defer c.CloseNow()

	a.mu.Lock()
	a.sessSeq++
	s := &session{id: a.sessSeq, c: c, ctl: make(chan []byte, 1024), data: make(chan []byte, 64), done: make(chan struct{}), subs: map[string]context.CancelFunc{}}
	a.mu.Unlock()
	sctx, scancel := context.WithCancel(ctx)
	defer scancel()

	// Hello first, written directly.
	hello := a.buildHello(sctx)
	if err := writeEnvelope(sctx, c, proto.THello, "", "", hello); err != nil {
		return fmt.Errorf("could not send hello: %w", err)
	}
	ack, err := readHelloAck(sctx, c, a.t.helloTimeout)
	if err != nil {
		return a.closeErr(err)
	}
	a.log.Info("connected to the control plane", "server", ack.ServerVersion, "protocol", ack.ProtocolVersion, "epoch", ack.Epoch)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.writeLoop(sctx, s) }()
	go func() { defer wg.Done(); a.pingLoop(sctx, s) }()

	a.mu.Lock()
	a.sess = s
	a.everAcked = true
	a.mu.Unlock()
	// Re-send every replica's status, then anything buffered while offline.
	a.sendAllStatuses()
	for _, env := range a.outbox.drain() {
		a.sendRaw(s, env)
	}

	err = a.readLoop(sctx, s)

	a.mu.Lock()
	if a.sess == s {
		a.sess = nil
	}
	a.mu.Unlock()
	s.close()
	scancel()
	a.endSession(s)
	wg.Wait()
	return a.closeErr(err)
}

// closeErr turns a close frame with a refusal reason into errRefused.
func (a *Agent) closeErr(err error) error {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Reason {
		case proto.CloseDuplicateHost:
			return errRefused{reason: "Duplicate host detected: another machine already uses this host's identity. Run 5050-agent rejoin on this machine, or remove it.", wait: a.t.refusedWait}
		case proto.CloseUnknownHost:
			return errRefused{reason: "The control plane was restored from a backup that does not know this host. Re-adopt it on the Hosts page.", wait: a.t.refusedWait}
		case proto.CloseRevoked:
			return errRefused{reason: "This host was removed from the install. Run 5050-agent rejoin with a new join token to add it again.", wait: a.t.revokedWait}
		}
	}
	return err
}

func readHelloAck(ctx context.Context, c *websocket.Conn, timeout time.Duration) (proto.HelloAck, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		typ, b, err := c.Read(rctx)
		if err != nil {
			return proto.HelloAck{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var env proto.Envelope
		if json.Unmarshal(b, &env) != nil || env.T != proto.THelloAck {
			continue
		}
		var ack proto.HelloAck
		if err := json.Unmarshal(env.D, &ack); err != nil {
			return proto.HelloAck{}, fmt.Errorf("the control plane sent an unreadable hello_ack: %w", err)
		}
		return ack, nil
	}
}

func encodeEnvelope(t, id, ref string, v any) ([]byte, error) {
	env := proto.Envelope{T: t, ID: id, Ref: ref}
	if v != nil {
		d, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		env.D = d
	}
	return json.Marshal(env)
}

func writeEnvelope(ctx context.Context, c *websocket.Conn, t, id, ref string, v any) error {
	b, err := encodeEnvelope(t, id, ref, v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b)
}

// writeLoop writes queued frames; control messages go before stream data.
func (a *Agent) writeLoop(ctx context.Context, s *session) {
	write := func(typ websocket.MessageType, b []byte) bool {
		wctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if err := s.c.Write(wctx, typ, b); err != nil {
			s.close()
			_ = s.c.Close(websocket.StatusGoingAway, "write failed")
			return false
		}
		return true
	}
	for {
		select {
		case b := <-s.ctl:
			if !write(websocket.MessageText, b) {
				return
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case b := <-s.ctl:
			if !write(websocket.MessageText, b) {
				return
			}
		case b := <-s.data:
			if !write(websocket.MessageBinary, b) {
				return
			}
		}
	}
}

// pingLoop pings every 20 s; a ping without a pong in time closes the socket.
func (a *Agent) pingLoop(ctx context.Context, s *session) {
	t := time.NewTicker(a.t.pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, a.t.pingTimeout)
			err := s.c.Ping(pctx)
			cancel()
			if err != nil && ctx.Err() == nil {
				a.log.Warn("the control plane did not answer a ping; reconnecting", "err", err)
				_ = s.c.Close(websocket.StatusGoingAway, "ping timeout")
				s.close()
				return
			}
		}
	}
}

func (a *Agent) readLoop(ctx context.Context, s *session) error {
	for {
		typ, b, err := s.c.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageBinary {
			if len(b) < 9 {
				continue
			}
			a.streams.input(b[0], binary.BigEndian.Uint64(b[1:9]), b[9:])
			continue
		}
		var env proto.Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			a.log.Warn("ignoring an unreadable message from the control plane", "err", err)
			continue
		}
		a.dispatch(ctx, s, env)
	}
}

// send queues a message on the current session; it is dropped when offline.
func (a *Agent) send(t, ref string, v any) bool {
	b, err := encodeEnvelope(t, "", ref, v)
	if err != nil {
		a.log.Error("could not encode a message", "type", t, "err", err)
		return false
	}
	a.mu.Lock()
	s := a.sess
	a.mu.Unlock()
	if s == nil {
		return false
	}
	return a.sendRaw(s, b)
}

func (a *Agent) sendRaw(s *session, b []byte) bool {
	select {
	case s.ctl <- b:
		return true
	case <-s.done:
		return false
	}
}

// sendDurable queues a message that must survive a disconnect (build and
// job results, kept for an hour, 7.5).
func (a *Agent) sendDurable(t, ref string, v any) {
	b, err := encodeEnvelope(t, "", ref, v)
	if err != nil {
		a.log.Error("could not encode a message", "type", t, "err", err)
		return
	}
	a.mu.Lock()
	s := a.sess
	a.mu.Unlock()
	if s != nil && a.sendRaw(s, b) {
		return
	}
	a.outbox.add(b, t != proto.TBuildLog && t != proto.TJobLog)
}

// sendBinary queues a stream frame on session s, blocking for backpressure.
func (a *Agent) sendBinary(s *session, kind byte, id uint64, payload []byte) bool {
	b := make([]byte, 9+len(payload))
	b[0] = kind
	binary.BigEndian.PutUint64(b[1:9], id)
	copy(b[9:], payload)
	select {
	case s.data <- b:
		return true
	case <-s.done:
		return false
	}
}

func (a *Agent) currentSession() *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sess
}

// dispatch handles one message from the control plane.
func (a *Agent) dispatch(ctx context.Context, s *session, env proto.Envelope) {
	decode := func(v any) bool {
		if err := json.Unmarshal(env.D, v); err != nil {
			a.log.Warn("ignoring a malformed message", "type", env.T, "err", err)
			if env.ID != "" {
				a.send(proto.TAck, env.ID, proto.Ack{Error: "The agent could not read this " + env.T + " message."})
			}
			return false
		}
		return true
	}
	switch env.T {
	case proto.TDesired:
		var ds proto.DesiredState
		if decode(&ds) {
			// Keep only the newest pending state.
			for {
				select {
				case a.applyCh <- desiredMsg{ds: &ds, sess: s.id}:
				default:
					select {
					case <-a.applyCh:
					default:
					}
					continue
				}
				break
			}
		}
	case proto.THelloAck:
		// A late duplicate; nothing to do.
	case proto.TBuild:
		var job proto.BuildJob
		if decode(&job) {
			a.startBuild(ctx, job)
		}
	case proto.TBuildCancel:
		var job proto.BuildJob
		if decode(&job) {
			a.cancelBuild(job.BuildID)
		}
	case proto.TJob:
		var j proto.Job
		if decode(&j) {
			a.startJob(ctx, j)
		}
	case proto.TJobCancel:
		var j proto.Job
		if decode(&j) {
			a.cancelJob(j.JobID)
		}
	case proto.TLogsQuery:
		var q proto.LogsQuery
		if decode(&q) {
			go a.answerLogsQuery(env.ID, q)
		}
	case proto.TLogsSubscribe:
		var sub proto.LogsSubscribe
		if decode(&sub) {
			a.subscribeLogs(ctx, s, env.ID, sub)
		}
	case proto.TLogsUnsubscribe:
		var sub proto.LogsSubscribe
		if decode(&sub) {
			a.unsubscribeLogs(s, sub.SubID)
		}
	case proto.TExecOpen:
		var o proto.ExecOpen
		if decode(&o) {
			go a.openExec(ctx, s, env.ID, o)
		}
	case proto.TExecResize:
		var r proto.ExecResize
		if decode(&r) {
			a.streams.resize(ctx, r)
		}
	case proto.TExecClose:
		var c proto.StreamClose
		if decode(&c) {
			a.streams.closeStream(proto.BinExec, c.StreamID)
		}
	case proto.TTCPOpen:
		var o proto.TCPOpen
		if decode(&o) {
			go a.openTCP(ctx, s, env.ID, o)
		}
	case proto.TTCPClose:
		var c proto.StreamClose
		if decode(&c) {
			a.streams.closeStream(proto.BinTCP, c.StreamID)
		}
	case proto.TTokenRotate:
		var tr proto.TokenRotate
		if decode(&tr) {
			a.rotateToken(env.ID, tr)
		}
	case proto.TMaintenance:
		var m proto.Maintenance
		if decode(&m) {
			go a.maintain(ctx, env.ID, m)
		}
	case proto.TUpgrade:
		var u proto.Upgrade
		if decode(&u) {
			go a.selfUpgrade(ctx, env.ID, u)
		}
	default:
		a.log.Debug("ignoring an unknown message type", "type", env.T)
	}
}

func (a *Agent) rotateToken(id string, tr proto.TokenRotate) {
	if tr.Token == "" {
		a.send(proto.TAck, id, proto.Ack{Error: "The new host token was empty and was not saved."})
		return
	}
	if a.o.OnTokenRotate != nil {
		if err := a.o.OnTokenRotate(tr.Token); err != nil {
			a.log.Error("could not save the rotated host token", "err", err)
			a.send(proto.TAck, id, proto.Ack{Error: "The agent could not save the new host token: " + err.Error() + "."})
			return
		}
	}
	a.mu.Lock()
	a.token = tr.Token
	a.mu.Unlock()
	a.o.Live.setToken(tr.Token)
	a.log.Info("host token rotated")
	a.send(proto.TAck, id, proto.Ack{OK: true})
}

// endSession stops what belonged to a connection: live tails, plain exec
// sessions and TCP streams. Named exec sessions are kept for reattach.
func (a *Agent) endSession(s *session) {
	s.mu.Lock()
	for id, cancel := range s.subs {
		cancel()
		delete(s.subs, id)
	}
	s.mu.Unlock()
	a.streams.detachSession(s, a.t.execKeep)
}
