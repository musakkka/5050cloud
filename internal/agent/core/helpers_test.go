// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/sample"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// ---------------------------------------------------------------- fake parts

type fakeNet struct {
	mu        sync.Mutex
	applied   []proto.DesiredState // Host, Mesh, Networks and DNS of each Apply
	egress    []string
	applyErr  error
	egressErr error
	pub       string
	hs        map[string]int64
	down      []string
}

func (f *fakeNet) Apply(_ context.Context, host proto.HostConfig, mesh proto.Mesh, nets []proto.Network, dns []proto.DNSEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, proto.DesiredState{Host: host, Mesh: mesh, Networks: nets, DNS: dns})
	return f.applyErr
}
func (f *fakeNet) PublicKey() (string, error) {
	if f.pub == "" {
		return "", errors.New("no key")
	}
	return f.pub, nil
}
func (f *fakeNet) DNSServer(env string) string { return "10.129.0.1" }
func (f *fakeNet) AllowEgress(_ context.Context, ip string, n proto.Network) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.egress = append(f.egress, ip)
	return f.egressErr
}
func (f *fakeNet) BuildNetwork(context.Context) (string, error) { return buildNetworkName, nil }
func (f *fakeNet) MeshHandshakes() map[string]int64             { return f.hs }
func (f *fakeNet) UnreachablePeers(time.Time, time.Duration) []string {
	return f.down
}
func (f *fakeNet) applies() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

type fakeIngress struct {
	mu        sync.Mutex
	ensured   []string
	applied   []proto.Ingress
	upstreams map[string][]string
	setCalls  int
	logs      chan proto.LogRecord
	certs     chan proto.CertBundle
}

func newFakeIngress() *fakeIngress {
	return &fakeIngress{upstreams: map[string][]string{}, logs: make(chan proto.LogRecord, 16), certs: make(chan proto.CertBundle, 4)}
}
func (f *fakeIngress) EnsureRunning(_ context.Context, img string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured = append(f.ensured, img)
	return nil
}
func (f *fakeIngress) Apply(_ context.Context, in proto.Ingress, _ []proto.TCPProxy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, in)
	return nil
}
func (f *fakeIngress) SetUpstreams(name string, ips []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if ips == nil {
		delete(f.upstreams, name)
	} else {
		f.upstreams[name] = append([]string(nil), ips...)
	}
	return nil
}
func (f *fakeIngress) CertStatus(context.Context) (map[string]agentapi.CertState, error) {
	return nil, nil
}
func (f *fakeIngress) IssuedCerts(ctx context.Context, out chan<- proto.CertBundle) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case c := <-f.certs:
			out <- c
		}
	}
}
func (f *fakeIngress) AccessLogs(ctx context.Context, out chan<- proto.LogRecord) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-f.logs:
			out <- r
		}
	}
}
func (f *fakeIngress) ups(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.upstreams[name]...)
}

type fakeBuilder struct {
	mu      sync.Mutex
	ensured []string
	pruned  int
	build   func(ctx context.Context, job proto.BuildJob, logf func(string)) proto.BuildResult
}

func (f *fakeBuilder) Build(ctx context.Context, job proto.BuildJob, _ proto.Registry, logf func(string)) proto.BuildResult {
	if f.build != nil {
		return f.build(ctx, job, logf)
	}
	logf("building " + job.BuildID)
	return proto.BuildResult{OK: true, Image: job.Image}
}
func (f *fakeBuilder) Prune(context.Context, int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruned++
	return nil
}
func (f *fakeBuilder) EnsureRunning(_ context.Context, img string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured = append(f.ensured, img)
	return nil
}

// ---------------------------------------------------------------- test agent

type testAgent struct {
	*Agent
	eng *engine.Fake
	net *fakeNet
	ing *fakeIngress
	bld *fakeBuilder
	dir string
	key []byte
}

func fastTuning() tuning {
	t := defaultTuning()
	t.unit = 2 * time.Millisecond
	t.reconcileEvery = time.Hour
	t.metricsEvery = time.Hour
	t.tailEvery = time.Hour
	t.janitorEvery = time.Hour
	t.retentionEvery = time.Hour
	t.probeEvery = time.Hour
	t.volumeEvery = time.Hour
	t.helloTimeout = 5 * time.Second
	t.pingEvery = time.Hour
	t.backoffMin = 5 * time.Millisecond
	t.backoffMax = 20 * time.Millisecond
	t.refusedWait = 20 * time.Millisecond
	t.revokedWait = 20 * time.Millisecond
	t.ballastBytes = 1 << 10
	t.logBatch = 5 * time.Millisecond
	t.execKeep = time.Second
	return t
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newTestAgent builds an agent on a fake engine in a temp data dir.
func newTestAgent(t *testing.T, mod ...func(*Options)) *testAgent {
	t.Helper()
	return newTestAgentIn(t, t.TempDir(), engine.NewFake(), []byte("0123456789abcdef0123456789abcdef"), mod...)
}

func newTestAgentIn(t *testing.T, dir string, eng *engine.Fake, key []byte, mod ...func(*Options)) *testAgent {
	t.Helper()
	ta := &testAgent{eng: eng, net: &fakeNet{pub: "wgpub="}, ing: newFakeIngress(), bld: &fakeBuilder{}, dir: dir, key: key}
	o := Options{
		ControlURL: "http://127.0.0.1:1", HostToken: "tok", DataDir: dir, Network: ta.net, Ingress: ta.ing, Builder: ta.bld,
		HostKey: key, HostID: "hst_1", MachineID: "m1", Engine: eng, Logger: quietLogger(), Version: "v0.1.0-test",
		Roots: sample.Roots{Proc: dir + "/noproc", Cgroup: dir + "/nocg"},
	}
	for _, m := range mod {
		m(&o)
	}
	a, err := newAgent(o)
	if err != nil {
		t.Fatal(err)
	}
	a.t = fastTuning()
	a.outbox = newOutbox(a.t.outboxTTL)
	a.disk.statfs = func(string) (int64, int64, error) { return 10, 100, nil }
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	t.Cleanup(func() {
		cancel()
		a.stopAllFollowers()
		a.regFront.close()
		time.Sleep(10 * time.Millisecond)
		a.logs.Close()
	})
	ta.Agent = a
	return ta
}

// attach gives the agent a fake connection whose outgoing frames land in
// the returned channel, and returns the session.
func (ta *testAgent) attach() *session {
	ta.mu.Lock()
	ta.sessSeq++
	s := &session{id: ta.sessSeq, ctl: make(chan []byte, 4096), data: make(chan []byte, 4096), done: make(chan struct{}), subs: map[string]context.CancelFunc{}}
	ta.sess = s
	ta.mu.Unlock()
	return s
}

// deliver hands a desired state to the agent as if it came on session s.
func (ta *testAgent) deliver(s *session, ds proto.DesiredState) {
	ta.handleDesired(context.Background(), desiredMsg{ds: &ds, sess: s.id})
}

// waitFor polls cond until it holds or the test times out.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// frames decodes every envelope queued on s so far.
func frames(s *session) []proto.Envelope {
	var out []proto.Envelope
	for {
		select {
		case b := <-s.ctl:
			var env proto.Envelope
			_ = json.Unmarshal(b, &env)
			out = append(out, env)
		default:
			return out
		}
	}
}

// collector keeps every frame a session sent, in order.
type collector struct {
	mu  sync.Mutex
	all []proto.Envelope
}

func collect(s *session) *collector {
	c := &collector{}
	go func() {
		for {
			select {
			case b := <-s.ctl:
				var env proto.Envelope
				_ = json.Unmarshal(b, &env)
				c.mu.Lock()
				c.all = append(c.all, env)
				c.mu.Unlock()
			case <-s.done:
				return
			}
		}
	}()
	return c
}

func (c *collector) of(typ string) []proto.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []proto.Envelope
	for _, e := range c.all {
		if e.T == typ {
			out = append(out, e)
		}
	}
	return out
}

func decodeAs[T any](t *testing.T, env proto.Envelope) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(env.D, &v); err != nil {
		t.Fatalf("decode %s: %v", env.T, err)
	}
	return v
}

// lastStatus returns the newest replica_status for a replica.
func (c *collector) lastStatus(t *testing.T, rep string) (proto.ReplicaStatus, bool) {
	var st proto.ReplicaStatus
	ok := false
	for _, e := range c.of(proto.TReplicaStatus) {
		s := decodeAs[proto.ReplicaStatus](t, e)
		if s.ReplicaID == rep {
			st, ok = s, true
		}
	}
	return st, ok
}

// ---------------------------------------------------------------- specs

const testImage = "127.0.0.1:5000/svc_a@sha256:aaaa"

func worker(id string) proto.ReplicaSpec {
	return proto.ReplicaSpec{
		ID: id, DeploymentID: "dep_1", ServiceID: "svc_a", EnvironmentID: "env_1", ProjectID: "prj_1", ServiceName: "api",
		Image: testImage, Env: map[string]string{"FOO": "bar"}, Network: "5050-env_1", IP: "10.129.4.7",
		Restart: proto.RestartPolicy{Policy: "on_failure", MaxRetries: 3}, Desired: "running", CgroupParent: "5050-apps.slice",
	}
}

func baseState(gen int64, reps ...proto.ReplicaSpec) proto.DesiredState {
	return proto.DesiredState{
		Generation: gen, Epoch: "ep1",
		Host:     proto.HostConfig{Roles: []string{"app"}, RegistryPull: proto.Registry{Address: "127.0.0.1:5000", Username: "pull", Password: "pw"}},
		Networks: []proto.Network{{EnvironmentID: "env_1", Name: "5050-env_1", Subnet: "10.129.4.0/24", Gateway: "10.129.4.1", Egress: "allowed"}},
		Replicas: reps,
	}
}

// fakeCP is a control plane speaking the agent protocol over a real WebSocket.
type fakeCP struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	hellos    []proto.Hello
	got       []proto.Envelope
	tokens    []string
	conns     []*websocket.Conn
	onHello   func(c *websocket.Conn, h proto.Hello)
	closeWith string // close every connection right after hello with this reason
}

func newFakeCP(t *testing.T) *fakeCP {
	cp := &fakeCP{t: t}
	cp.srv = httptest.NewServer(http.HandlerFunc(cp.serve))
	t.Cleanup(cp.srv.Close)
	return cp
}

func (cp *fakeCP) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/agent/v1/connect" {
		http.NotFound(w, r)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(proto.MaxMessageBytes)
	cp.mu.Lock()
	cp.tokens = append(cp.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	cp.conns = append(cp.conns, c)
	closeWith := cp.closeWith
	cp.mu.Unlock()
	ctx := r.Context()
	_, b, err := c.Read(ctx)
	if err != nil {
		return
	}
	var env proto.Envelope
	_ = json.Unmarshal(b, &env)
	var h proto.Hello
	_ = json.Unmarshal(env.D, &h)
	cp.mu.Lock()
	cp.hellos = append(cp.hellos, h)
	cp.mu.Unlock()
	if closeWith != "" {
		_ = c.Close(websocket.StatusPolicyViolation, closeWith)
		return
	}
	cp.send(c, proto.THelloAck, "", proto.HelloAck{HostID: h.HostID, ServerVersion: "v0.1.0", ProtocolVersion: proto.ProtocolVersion, Epoch: "ep1"})
	if cp.onHello != nil {
		cp.onHello(c, h)
	}
	for {
		typ, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var env proto.Envelope
		_ = json.Unmarshal(b, &env)
		cp.mu.Lock()
		cp.got = append(cp.got, env)
		cp.mu.Unlock()
	}
}

func (cp *fakeCP) send(c *websocket.Conn, typ, id string, v any) {
	b, _ := encodeEnvelope(typ, id, "", v)
	_ = c.Write(context.Background(), websocket.MessageText, b)
}

func (cp *fakeCP) of(typ string) []proto.Envelope {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	var out []proto.Envelope
	for _, e := range cp.got {
		if e.T == typ {
			out = append(out, e)
		}
	}
	return out
}

func (cp *fakeCP) helloCount() int {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return len(cp.hellos)
}

func (cp *fakeCP) hello(i int) proto.Hello {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return cp.hellos[i]
}

func (cp *fakeCP) lastConn() *websocket.Conn {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return cp.conns[len(cp.conns)-1]
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
