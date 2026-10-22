// SPDX-License-Identifier: AGPL-3.0-only

// Package hub is the control plane's side of the agent protocol
// (docs/SPEC.md 7, 11.6, pkg/proto): it serves /api/agent/v1/join,
// /connect (one WebSocket per agent) and /uploads/{id}, authenticates hosts
// with rotating tokens bound to their machine-id and WireGuard key, refuses
// cloned machines, numbers desired states (generation = max(stored,
// applied)+1, epoch and adopt), coalesces pushes to one per 250 ms per host,
// correlates requests with replies, and carries exec and TCP streams in
// binary frames.
//
// The hub never decides what runs: it asks its Handler (the engine) for a
// host's desired state and hands every other agent message to it.
package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Handler is what the engine gives the hub.
type Handler interface {
	// DesiredState builds the full desired state of a host. The hub fills
	// Generation, Epoch and Adopt.
	DesiredState(ctx context.Context, hostID string) (proto.DesiredState, error)
	// HostConnected is called after a valid hello, before the first desired state.
	HostConnected(ctx context.Context, hostID string, hello proto.Hello)
	// HostDisconnected is called when the current socket of a host closes.
	HostDisconnected(hostID string)
	// HostMessage receives every agent message that is not the reply to a
	// pending Call (state_applied, replica_status, metrics, events, build
	// logs and results, cert_issued, host_logs_tail, ...).
	HostMessage(ctx context.Context, hostID string, env proto.Envelope)
	// HostEvent reports hub-level events (Event* constants).
	HostEvent(ctx context.Context, kind, hostID, text string)
}

// Hub-level events passed to Handler.HostEvent.
const (
	EventJoined      = "host.joined"
	EventDuplicate   = "host.duplicate"
	EventUnknownHost = "host.unknown"
	EventTokenRotate = "host.token_rotated"
	EventVersionSkew = "agent.version_skew"
)

// Options configures a Hub.
type Options struct {
	DB      *sql.DB
	Write   WriteFunc
	Handler Handler
	Plan    ipam.Plan
	// ServerVersion is sent in hello_ack.
	ServerVersion string
	// UploadPath maps an upload id to a local tarball (GET /uploads/{id}).
	UploadPath func(ctx context.Context, id string) (string, error)
	Log        *slog.Logger
	Now        func() time.Time

	// Timings; zero values use the protocol defaults.
	PingInterval     time.Duration // 20 s
	DeadAfter        time.Duration // 60 s
	PushInterval     time.Duration // 250 ms
	HelloTimeout     time.Duration // 30 s
	TokenRotateEvery time.Duration // 30 days
	PrevTokenGrace   time.Duration // 10 minutes
	JoinPerMinute    int           // 10
}

// Hub serves agents.
type Hub struct {
	o         Options
	log       *slog.Logger
	joinLimit *rateLimiter

	mu      sync.Mutex
	conns   map[string]*conn
	unknown map[string]*conn // hosts not in the restored state, held open (9.17.1)

	epoch atomic.Value // string
	adopt atomic.Bool
	ready atomic.Bool
}

// New builds a hub. Call Load before serving.
func New(o Options) *Hub {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.PingInterval == 0 {
		o.PingInterval = 20 * time.Second
	}
	if o.DeadAfter == 0 {
		o.DeadAfter = 60 * time.Second
	}
	if o.PushInterval == 0 {
		o.PushInterval = 250 * time.Millisecond
	}
	if o.HelloTimeout == 0 {
		o.HelloTimeout = 30 * time.Second
	}
	if o.TokenRotateEvery == 0 {
		o.TokenRotateEvery = 30 * 24 * time.Hour
	}
	if o.PrevTokenGrace == 0 {
		o.PrevTokenGrace = 10 * time.Minute
	}
	if o.JoinPerMinute == 0 {
		o.JoinPerMinute = 10
	}
	if o.Plan.Pod.Bits() == 0 {
		o.Plan = ipam.MustDefault()
	}
	h := &Hub{
		o:         o,
		log:       o.Log.With("component", "hub"),
		joinLimit: newRateLimiter(o.JoinPerMinute, time.Minute),
		conns:     map[string]*conn{},
		unknown:   map[string]*conn{},
	}
	h.epoch.Store("")
	return h
}

func (h *Hub) now() time.Time { return h.o.Now() }

// Load reads the cluster epoch and adopt mode from o_state, creating an
// epoch on first start. The hub accepts agents after Load.
func (h *Hub) Load(ctx context.Context) error {
	var epoch string
	ok, err := GetState(ctx, h.o.DB, "epoch", &epoch)
	if err != nil {
		return err
	}
	if !ok || epoch == "" {
		epoch = NewID("epc")
		if err := h.o.Write(ctx, func(tx *sql.Tx) error { return PutState(ctx, tx, "epoch", epoch) }); err != nil {
			return err
		}
	}
	var adopt AdoptState
	if _, err := GetState(ctx, h.o.DB, "adopt_mode", &adopt); err != nil {
		return err
	}
	h.epoch.Store(epoch)
	h.adopt.Store(adopt.On)
	h.ready.Store(true)
	return nil
}

// AdoptState is the o_state "adopt_mode" value.
type AdoptState struct {
	On         bool  `json:"on"`
	SnapshotAt int64 `json:"snapshotAt"`
	StartedAt  int64 `json:"startedAt"`
}

// Epoch returns the cluster epoch.
func (h *Hub) Epoch() string { return h.epoch.Load().(string) }

// Adopting reports whether adopt mode is on.
func (h *Hub) Adopting() bool { return h.adopt.Load() }

// StartAdopt sets a new epoch and turns adopt mode on (9.17.1); every
// desired state then carries Adopt and the new epoch.
func (h *Hub) StartAdopt(ctx context.Context, snapshotAt int64) error {
	epoch := NewID("epc")
	st := AdoptState{On: true, SnapshotAt: snapshotAt, StartedAt: h.now().UnixMilli()}
	if err := h.o.Write(ctx, func(tx *sql.Tx) error {
		if err := PutState(ctx, tx, "epoch", epoch); err != nil {
			return err
		}
		return PutState(ctx, tx, "adopt_mode", st)
	}); err != nil {
		return err
	}
	h.epoch.Store(epoch)
	h.adopt.Store(true)
	h.MarkDirty()
	return nil
}

// FinishAdopt turns adopt mode off; removals are allowed again.
func (h *Hub) FinishAdopt(ctx context.Context) error {
	if err := h.o.Write(ctx, func(tx *sql.Tx) error {
		return PutState(ctx, tx, "adopt_mode", AdoptState{})
	}); err != nil {
		return err
	}
	h.adopt.Store(false)
	h.MarkDirty()
	return nil
}

// AdoptInfo returns the adopt state.
func (h *Hub) AdoptInfo(ctx context.Context) (AdoptState, error) {
	var st AdoptState
	_, err := GetState(ctx, h.o.DB, "adopt_mode", &st)
	return st, err
}

// Ready reports whether the hub accepts agents.
func (h *Hub) Ready() bool { return h.ready.Load() }

// ---------------------------------------------------------------- HTTP

// Handler serves /join, /connect and /uploads/{id}. Mount it with the
// /api/agent/v1 prefix stripped (or not: paths are matched by suffix).
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/join", h.serveJoin)
	mux.HandleFunc("/connect", h.serveConnect)
	mux.HandleFunc("/uploads/{id}", h.serveUpload)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if i := strings.Index(p, "/api/agent/v1"); i >= 0 {
			p = p[i+len("/api/agent/v1"):]
		}
		if p == "" {
			p = "/"
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = p
		if !h.Ready() {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "The control plane is starting. Try again in a few seconds.")
			return
		}
		mux.ServeHTTP(w, r2)
	})
}

func bearer(r *http.Request) string {
	a := r.Header.Get("Authorization")
	if len(a) > 7 && strings.EqualFold(a[:7], "bearer ") {
		return strings.TrimSpace(a[7:])
	}
	return ""
}

// authHost finds the host of a token: the current one, the previous one
// within its grace period, or a pending one (the agent wrote it but the
// ack was lost; it is promoted).
func (h *Hub) authHost(ctx context.Context, token string) (HostRow, bool, error) {
	if token == "" {
		return HostRow{}, false, nil
	}
	hash := HashToken(token)
	now := h.now().UnixMilli()
	var id string
	var pendingMatch bool
	err := h.o.DB.QueryRowContext(ctx, `SELECT id, CASE WHEN pending_token_hash = ? THEN 1 ELSE 0 END FROM o_hosts
		WHERE token_hash = ? OR (prev_token_hash = ? AND prev_valid_until > ?) OR pending_token_hash = ? LIMIT 1`,
		hash, hash, hash, now, hash).Scan(&id, &pendingMatch)
	if errors.Is(err, sql.ErrNoRows) {
		return HostRow{}, false, nil
	}
	if err != nil {
		return HostRow{}, false, err
	}
	if pendingMatch {
		if err := h.promotePending(ctx, id); err != nil {
			return HostRow{}, false, err
		}
	}
	row, err := LoadHost(ctx, h.o.DB, id)
	return row, err == nil, err
}

func (h *Hub) promotePending(ctx context.Context, hostID string) error {
	now := h.now()
	return h.o.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_hosts SET prev_token_hash = token_hash, prev_valid_until = ?, token_hash = pending_token_hash,
			pending_token_hash = NULL, token_rotated_at = ? WHERE id = ? AND pending_token_hash IS NOT NULL`,
			now.Add(h.o.PrevTokenGrace).UnixMilli(), now.UnixMilli(), hostID)
		return err
	})
}

func (h *Hub) serveUpload(w http.ResponseWriter, r *http.Request) {
	host, ok, err := h.authHost(r.Context(), bearer(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "The upload could not be read because of an internal error.")
		return
	}
	if !ok || host.State == "unknown" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "This host token is not valid.")
		return
	}
	if h.o.UploadPath == nil {
		writeError(w, http.StatusNotFound, "not_found", "This upload does not exist.")
		return
	}
	path, err := h.o.UploadPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "This upload does not exist or was already deleted.")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "This upload does not exist or was already deleted.")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "The upload could not be read.")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	http.ServeContent(w, r, "source.tar.gz", st.ModTime(), f)
}

// ---------------------------------------------------------------- connect

func (h *Hub) serveConnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := bearer(r)
	host, known, err := h.authHost(ctx, token)
	if err != nil {
		h.log.Error("agent auth", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "The connection could not be checked because of an internal error.")
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return
	}
	c.SetReadLimit(proto.MaxMessageBytes)
	if !known {
		if h.Adopting() && token != "" && h.joinLimit.allow("unknown:"+clientIP(r), h.now()) {
			h.serveUnknown(c, r, token)
			return
		}
		_ = c.Close(websocket.StatusPolicyViolation, proto.CloseRevoked)
		return
	}
	if host.State == "unknown" {
		// A host row created for an unknown connection that has not been re-adopted yet.
		h.serveUnknown(c, r, token)
		return
	}
	cn := h.newConn(c, host.ID, host.Name, clientIP(r), r.Header.Get("X-5050-Agent-Version"))
	hello, err := cn.readHello(ctx, h.o.HelloTimeout)
	if err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}
	if hello.HostID != "" && hello.HostID != host.ID {
		_ = c.Close(websocket.StatusPolicyViolation, proto.CloseRevoked)
		return
	}
	if (host.MachineID != "" && hello.MachineID != host.MachineID) || (host.WGPublicKey != "" && hello.WGPublicKey != host.WGPublicKey) {
		h.recordConflict(ctx, host, hello, cn.ip)
		_ = c.Close(websocket.StatusPolicyViolation, proto.CloseDuplicateHost)
		return
	}
	h.run(ctx, cn, host, hello)
}

func (h *Hub) recordConflict(ctx context.Context, host HostRow, hello proto.Hello, ip string) {
	now := h.now().UnixMilli()
	err := h.o.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO o_host_conflicts (host_id, machine_id, wg_pubkey, ip, seen_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(host_id, machine_id, wg_pubkey) DO UPDATE SET ip = excluded.ip, seen_at = excluded.seen_at`,
			host.ID, hello.MachineID, hello.WGPublicKey, ip, now)
		return err
	})
	if err != nil {
		h.log.Error("record conflict", "err", err)
	}
	h.event(ctx, EventDuplicate, host.ID, fmt.Sprintf("Duplicate host detected: a second machine is using the identity of host %s. Remove it or run `5050-agent rejoin` on it.", host.Name))
}

// run serves an authenticated agent until its socket closes.
func (h *Hub) run(ctx context.Context, cn *conn, host HostRow, hello proto.Hello) {
	connCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	cn.cancel = cancel
	if err := h.activate(connCtx, cn, host, hello); err != nil {
		h.log.Error("agent hello", "host", host.ID, "err", err)
		_ = cn.ws.Close(websocket.StatusInternalError, "hello failed")
		return
	}
	go cn.pingLoop(connCtx)
	cn.readLoop(connCtx)
	h.dropConn(cn)
}

// activate makes cn the host's current connection, answers the hello and
// starts pushing desired states.
func (h *Hub) activate(ctx context.Context, cn *conn, host HostRow, hello proto.Hello) error {
	cn.hostID, cn.hostName = host.ID, host.Name
	h.mu.Lock()
	old := h.conns[host.ID]
	h.conns[host.ID] = cn
	if h.unknown[host.ID] == cn {
		delete(h.unknown, host.ID)
	}
	h.mu.Unlock()
	if old != nil && old != cn {
		old.close(websocket.StatusNormalClosure, "replaced by a new connection")
	}
	cn.active.Store(true)
	if err := h.accept(ctx, cn, host, hello); err != nil {
		h.mu.Lock()
		if h.conns[host.ID] == cn {
			delete(h.conns, host.ID)
		}
		h.mu.Unlock()
		return err
	}
	go cn.pushLoop(ctx)
	return nil
}

// accept records the hello, answers hello_ack and pushes the first state.
func (h *Hub) accept(ctx context.Context, cn *conn, host HostRow, hello proto.Hello) error {
	now := h.now()
	factsJSON, _ := json.Marshal(hello.Facts)
	var gen int64
	var stored int64
	err := h.o.Write(ctx, func(tx *sql.Tx) error {
		if _, err := GetState(ctx, tx, "gen:"+host.ID, &stored); err != nil {
			return err
		}
		gen = stored
		if hello.Applied > gen {
			gen = hello.Applied
		}
		if err := PutState(ctx, tx, "gen:"+host.ID, gen); err != nil {
			return err
		}
		state := host.State
		switch state {
		case "maintenance", "lost":
		default:
			state = "online"
		}
		if host.State == "lost" {
			state = "online"
		}
		pub := hello.Facts.PublicIP
		if pub == "" {
			pub = host.PublicIP
		}
		_, err := tx.ExecContext(ctx, `UPDATE o_hosts SET facts = ?, version = ?, public_ip = ?, last_seen = ?, disconnected_at = 0,
			state = ?, machine_id = CASE WHEN machine_id = '' THEN ? ELSE machine_id END,
			mesh_pubkey = CASE WHEN mesh_pubkey = '' THEN ? ELSE mesh_pubkey END WHERE id = ?`,
			string(factsJSON), hello.Facts.Version, pub, now.UnixMilli(), state, hello.MachineID, hello.WGPublicKey, host.ID)
		return err
	})
	if err != nil {
		return err
	}
	cn.gen.Store(gen)
	cn.applied.Store(hello.Applied)
	cn.version = hello.Facts.Version
	ack := proto.HelloAck{HostID: host.ID, ServerVersion: h.o.ServerVersion, ProtocolVersion: proto.ProtocolVersion, Epoch: h.Epoch()}
	if err := cn.send(ctx, proto.THelloAck, "", "", ack); err != nil {
		return err
	}
	if skew := versionSkew(h.o.ServerVersion, hello.Facts.Version); skew != "" {
		h.event(ctx, EventVersionSkew, host.ID, skew)
	}
	h.o.Handler.HostConnected(ctx, host.ID, hello)
	cn.markSeq.Add(1)
	cn.trigger()
	if host.TokenRotatedAt > 0 && now.Sub(time.UnixMilli(host.TokenRotatedAt)) >= h.o.TokenRotateEvery {
		go func() {
			if err := h.RotateToken(ctx, host.ID); err != nil {
				h.log.Warn("token rotation", "host", host.ID, "err", err)
			}
		}()
	}
	return nil
}

func (h *Hub) dropConn(cn *conn) {
	h.mu.Lock()
	current := h.conns[cn.hostID] == cn
	if current {
		delete(h.conns, cn.hostID)
	}
	h.mu.Unlock()
	cn.failPending(errorf("Host %s disconnected before it answered.", cn.hostName))
	if !current {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := h.now().UnixMilli()
	_ = h.o.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_hosts SET disconnected_at = ?, last_seen = ? WHERE id = ?`, now, now, cn.hostID)
		return err
	})
	h.o.Handler.HostDisconnected(cn.hostID)
}

// serveUnknown holds a connection from a host the restored state does not
// know (9.17.1). Nothing is sent until an admin re-adopts it; the
// handshake then continues on the same socket.
func (h *Hub) serveUnknown(c *websocket.Conn, r *http.Request, token string) {
	ctx := r.Context()
	cn := h.newConn(c, "", "", clientIP(r), r.Header.Get("X-5050-Agent-Version"))
	hello, err := cn.readHello(ctx, h.o.HelloTimeout)
	if err != nil || hello.HostID == "" {
		_ = c.Close(websocket.StatusPolicyViolation, proto.CloseUnknownHost)
		return
	}
	if existing, err := LoadHost(ctx, h.o.DB, hello.HostID); err == nil && existing.State != "unknown" {
		// The id is known but the token is not: refuse rather than guess.
		_ = c.Close(websocket.StatusPolicyViolation, proto.CloseUnknownHost)
		return
	}
	cn.hostID, cn.hostName, cn.hello = hello.HostID, hostLabel(hello), hello
	factsJSON, _ := json.Marshal(hello.Facts)
	now := h.now().UnixMilli()
	err = h.o.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO o_hosts (id, name, roles, state, token_hash, token_rotated_at, machine_id, facts,
			mesh_index, mesh_pubkey, public_ip, last_seen, drain, reserve_mb, version, created_at)
			VALUES (?, ?, '["app"]', 'unknown', ?, ?, ?, ?, NULL, ?, ?, ?, 0, 0, ?, ?)
			ON CONFLICT(id) DO UPDATE SET facts = excluded.facts, last_seen = excluded.last_seen, token_hash = excluded.token_hash`,
			hello.HostID, hostLabel(hello), HashToken(token), now, hello.MachineID, string(factsJSON), hello.WGPublicKey,
			hello.Facts.PublicIP, now, hello.Facts.Version, now)
		return err
	})
	if err != nil {
		_ = c.Close(websocket.StatusInternalError, proto.CloseUnknownHost)
		return
	}
	h.mu.Lock()
	if old := h.unknown[hello.HostID]; old != nil {
		old.close(websocket.StatusNormalClosure, "replaced")
	}
	h.unknown[hello.HostID] = cn
	h.mu.Unlock()
	h.event(ctx, EventUnknownHost, hello.HostID, fmt.Sprintf("Host %s is not in the restored state. Re-adopt or remove it on the Hosts page.", hostLabel(hello)))
	connCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	cn.cancel = cancel
	cn.connCtx = connCtx
	go cn.pingLoop(connCtx)
	cn.readLoop(connCtx)
	if cn.active.Load() {
		h.dropConn(cn)
		return
	}
	h.mu.Lock()
	if h.unknown[hello.HostID] == cn {
		delete(h.unknown, hello.HostID)
	}
	h.mu.Unlock()
}

func hostLabel(hello proto.Hello) string {
	if hello.Facts.PublicIP != "" {
		return hello.Facts.PublicIP
	}
	return hello.HostID
}

// UnknownHostIDs lists hosts held open while not in the restored state.
func (h *Hub) UnknownHostIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.unknown))
	for id := range h.unknown {
		out = append(out, id)
	}
	return out
}

// Readopt turns an unknown host into a member (9.17.1): it gets a mesh
// index and the given roles, a new token over its open socket, then the
// normal hello_ack and desired state.
func (h *Hub) Readopt(ctx context.Context, hostID string, roles []string) error {
	h.mu.Lock()
	cn := h.unknown[hostID]
	h.mu.Unlock()
	if cn == nil {
		return errorf("Host %s is not connected. Start its agent, then re-adopt it.", hostID)
	}
	roles, err := NormalizeRoles(roles)
	if err != nil {
		return err
	}
	token := NewToken()
	rb, _ := json.Marshal(roles)
	if err := h.o.Write(ctx, func(tx *sql.Tx) error {
		idx, err := nextMeshIndex(ctx, tx, false)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE o_hosts SET state = 'joining', mesh_index = ?, roles = ?, pending_token_hash = ? WHERE id = ?`,
			idx, string(rb), HashToken(token), hostID)
		return err
	}); err != nil {
		return err
	}
	reply, err := cn.call(ctx, proto.TTokenRotate, NewID("req"), proto.TokenRotate{Token: token}, nil, 30*time.Second)
	if err != nil {
		return err
	}
	var ack proto.Ack
	_ = json.Unmarshal(reply.D, &ack)
	if !ack.OK && reply.T == proto.TAck {
		return errorf("Host %s did not accept its new token: %s", hostID, ack.Error)
	}
	if err := h.promotePending(ctx, hostID); err != nil {
		return err
	}
	host, err := LoadHost(ctx, h.o.DB, hostID)
	if err != nil {
		return err
	}
	return h.activate(cn.connCtx, cn, host, cn.hello)
}

// ---------------------------------------------------------------- push and requests

// MarkDirty schedules a desired-state push for hosts (none = every host).
// Pushes are coalesced to at most one per PushInterval per host.
func (h *Hub) MarkDirty(hostIDs ...string) {
	h.mu.Lock()
	var cs []*conn
	if len(hostIDs) == 0 {
		for _, c := range h.conns {
			cs = append(cs, c)
		}
	} else {
		for _, id := range hostIDs {
			if c := h.conns[id]; c != nil {
				cs = append(cs, c)
			}
		}
	}
	h.mu.Unlock()
	for _, c := range cs {
		c.markSeq.Add(1)
		c.trigger()
	}
}

// WaitApplied waits until every connected host in hostIDs (none = all)
// reported state_applied for a state built after this call started, or
// until timeout. It returns the hosts that did not converge.
func (h *Hub) WaitApplied(ctx context.Context, hostIDs []string, timeout time.Duration) []string {
	type target struct {
		c   *conn
		seq uint64
	}
	h.mu.Lock()
	var ts []target
	if len(hostIDs) == 0 {
		for _, c := range h.conns {
			ts = append(ts, target{c, c.markSeq.Load()})
		}
	} else {
		for _, id := range hostIDs {
			if c := h.conns[id]; c != nil {
				ts = append(ts, target{c, c.markSeq.Load()})
			}
		}
	}
	h.mu.Unlock()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var pending []string
		for _, t := range ts {
			if t.c.closed.Load() {
				continue
			}
			if !t.c.appliedSince(t.seq) {
				pending = append(pending, t.c.hostID)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return pending
		case <-deadline.C:
			return pending
		case <-tick.C:
		}
	}
}

func (h *Hub) get(hostID string) *conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[hostID]
}

// Connected reports whether a host has a live socket.
func (h *Hub) Connected(hostID string) bool { return h.get(hostID) != nil }

// ConnectedHosts lists hosts with a live socket.
func (h *Hub) ConnectedHosts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for id := range h.conns {
		out = append(out, id)
	}
	return out
}

// AgentVersion is the version a connected agent reported ("" when offline).
func (h *Hub) AgentVersion(hostID string) string {
	if c := h.get(hostID); c != nil {
		return c.version
	}
	return ""
}

// ErrOffline is returned when a host has no live socket.
type ErrOffline struct{ Host string }

func (e *ErrOffline) Error() string {
	return fmt.Sprintf("Host %s is offline.", e.Host)
}

// Send sends one message without waiting for a reply.
func (h *Hub) Send(ctx context.Context, hostID, t, id string, payload any) error {
	c := h.get(hostID)
	if c == nil {
		return &ErrOffline{Host: hostID}
	}
	return c.send(ctx, t, id, "", payload)
}

// Call sends a request and waits for its final reply (logs_result,
// job_result, build_result or ack), passing streamed messages (job_log,
// build_log, log_lines) to stream. id must be unique; jobs use the job id.
func (h *Hub) Call(ctx context.Context, hostID, t, id string, payload any, stream func(proto.Envelope)) (proto.Envelope, error) {
	c := h.get(hostID)
	if c == nil {
		return proto.Envelope{}, &ErrOffline{Host: hostID}
	}
	if id == "" {
		id = NewID("req")
	}
	return c.call(ctx, t, id, payload, stream, 0)
}

// Subscribe starts a live log tail on a host; fn receives every log_lines
// message until ctx is done, when logs_unsubscribe is sent.
func (h *Hub) Subscribe(ctx context.Context, hostID string, sub proto.LogsSubscribe, fn func(proto.LogLines)) error {
	c := h.get(hostID)
	if c == nil {
		return &ErrOffline{Host: hostID}
	}
	if sub.SubID == "" {
		sub.SubID = NewID("sub")
	}
	w := c.register(sub.SubID, func(e proto.Envelope) {
		var ll proto.LogLines
		if json.Unmarshal(e.D, &ll) == nil {
			fn(ll)
		}
	}, true)
	defer c.unregister(sub.SubID)
	if err := c.send(ctx, proto.TLogsSubscribe, sub.SubID, "", sub); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-w.done:
	}
	if !c.closed.Load() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = c.send(sctx, proto.TLogsUnsubscribe, NewID("req"), "", proto.LogsSubscribe{SubID: sub.SubID})
		cancel()
	}
	if w.err != nil && ctx.Err() == nil {
		return w.err
	}
	return nil
}

// Disconnect closes a host's socket with a reason (proto.Close* constants).
func (h *Hub) Disconnect(hostID, reason string) {
	h.mu.Lock()
	c := h.conns[hostID]
	u := h.unknown[hostID]
	h.mu.Unlock()
	if c != nil {
		c.close(websocket.StatusPolicyViolation, reason)
	}
	if u != nil {
		u.close(websocket.StatusPolicyViolation, reason)
	}
}

// RotateToken gives a connected host a new token over its socket (7.1).
// The old token stays valid for PrevTokenGrace after the agent acknowledges.
func (h *Hub) RotateToken(ctx context.Context, hostID string) error {
	c := h.get(hostID)
	if c == nil {
		return &ErrOffline{Host: hostID}
	}
	token := NewToken()
	if err := h.o.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_hosts SET pending_token_hash = ? WHERE id = ?`, HashToken(token), hostID)
		return err
	}); err != nil {
		return err
	}
	reply, err := c.call(ctx, proto.TTokenRotate, NewID("req"), proto.TokenRotate{Token: token}, nil, 60*time.Second)
	if err != nil {
		return err
	}
	var ack proto.Ack
	_ = json.Unmarshal(reply.D, &ack)
	if !ack.OK {
		return errorf("Host %s did not store its new token: %s", hostID, ack.Error)
	}
	if err := h.promotePending(ctx, hostID); err != nil {
		return err
	}
	h.event(ctx, EventTokenRotate, hostID, "The host token was rotated.")
	return nil
}

// CheckRotations rotates tokens older than TokenRotateEvery on connected hosts.
func (h *Hub) CheckRotations(ctx context.Context) {
	hosts, err := LoadHosts(ctx, h.o.DB)
	if err != nil {
		return
	}
	now := h.now()
	for _, host := range hosts {
		if host.TokenRotatedAt == 0 || now.Sub(time.UnixMilli(host.TokenRotatedAt)) < h.o.TokenRotateEvery || !h.Connected(host.ID) {
			continue
		}
		if err := h.RotateToken(ctx, host.ID); err != nil {
			h.log.Warn("token rotation", "host", host.ID, "err", err)
		}
	}
}

func (h *Hub) event(ctx context.Context, kind, hostID, text string) {
	if h.o.Handler != nil {
		h.o.Handler.HostEvent(ctx, kind, hostID, text)
	}
}

// versionSkew returns a sentence when agent and server are more than one
// minor version apart (9.16); dev builds never warn.
func versionSkew(server, agent string) string {
	sm, sn, ok1 := minor(server)
	am, an, ok2 := minor(agent)
	if !ok1 || !ok2 {
		return ""
	}
	d := (sm*1000 + sn) - (am*1000 + an)
	if d < 0 {
		d = -d
	}
	if sm != am || d > 1 {
		return fmt.Sprintf("The agent runs %s and the control plane %s, more than one minor version apart. Upgrade the agent.", agent, server)
	}
	return ""
}

func minor(v string) (int, int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	var a, b int
	if _, err := fmt.Sscanf(parts[0]+" "+parts[1], "%d %d", &a, &b); err != nil {
		return 0, 0, false
	}
	return a, b, true
}
