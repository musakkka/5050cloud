// SPDX-License-Identifier: AGPL-3.0-only

// Package core is the agent runtime (docs/SPEC.md 7 and 8.1, WP3). It owns
// the connection to the control plane, the encrypted desired-state cache,
// the reconcile loop, health checks, Docker event folding, metrics, logs,
// jobs, builds (through agentapi.Builder), exec and TCP streams, tool
// downloads, image retention and disk guards, the ballast file, the
// registry container on the control host, maintenance drains and
// self-upgrade (through internal/upgrade).
//
// Two rules hold everywhere: containers without our labels are never
// touched, and no container is stopped or removed before a desired state
// has arrived after hello_ack on the current connection.
package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/internal/agent/assets"
	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/internal/agent/core/sample"
	"github.com/musakkka/5050cloud/internal/agent/core/statecache"
	"github.com/musakkka/5050cloud/internal/agent/core/tools"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/proto"
	"github.com/musakkka/5050cloud/pkg/version"
)

// Options configures the runtime. The first block is the constructor shape
// fixed by docs/SPEC.md 8.1; the rest is optional.
type Options struct {
	ControlURL string // "https://console.example.com" or "http://127.0.0.1:5050"
	HostToken  string
	DataDir    string // "/var/lib/5050"
	DockerHost string // "unix:///var/run/docker.sock"
	Network    agentapi.Network
	Ingress    agentapi.Ingress
	Builder    agentapi.Builder
	// CACertPEM is an extra trusted CA for the console certificate (Caddy's
	// internal CA when ACME could not issue one).
	CACertPEM []byte
	// HostKey is the content of /etc/5050/host.key; it encrypts the
	// desired-state cache.
	HostKey []byte

	// HostID and MachineID identify this host in hello (7.1).
	HostID    string
	MachineID string
	// CAPin pins the console certificate: "sha256:<hex of the DER>".
	CAPin string
	// Tools is the tool manager shared with the builder; nil creates one.
	Tools *tools.Manager
	// Engine overrides the Docker engine (tests).
	Engine engine.Engine
	Logger *slog.Logger
	// OnTokenRotate persists a rotated host token (agent.json) before it is
	// acknowledged (7.1 step 5).
	OnTokenRotate func(token string) error
	// Upgrade configures self-upgrade (9.16).
	Upgrade upgrade.Options
	// Version reported in facts; default pkg/version.Version.
	Version string
	// Roots points the samplers at procfs and cgroup; default the real ones.
	Roots sample.Roots
	// FactsRoot prefixes /etc, /sys and /run when facts are read (tests).
	FactsRoot string
	// PublicIP is reported in facts (from agent.json; the installer detects it).
	PublicIP string
	// Live receives the rotated host token and the image overrides of each
	// desired state, for parts built before the agent (the builder).
	Live *Live
}

// tuning holds intervals the tests shorten.
type tuning struct {
	unit           time.Duration // one "second" of HealthCheck fields
	reconcileEvery time.Duration
	metricsEvery   time.Duration
	tailEvery      time.Duration
	janitorEvery   time.Duration
	retentionEvery time.Duration
	probeEvery     time.Duration
	volumeEvery    time.Duration
	helloTimeout   time.Duration
	pingEvery      time.Duration
	pingTimeout    time.Duration
	backoffMin     time.Duration
	backoffMax     time.Duration
	refusedWait    time.Duration
	revokedWait    time.Duration
	ballastBytes   int64
	outboxTTL      time.Duration
	logBatch       time.Duration
	execKeep       time.Duration
}

func defaultTuning() tuning {
	return tuning{
		unit:           time.Second,
		reconcileEvery: 30 * time.Second,
		metricsEvery:   30 * time.Second,
		tailEvery:      10 * time.Second,
		janitorEvery:   time.Minute,
		retentionEvery: time.Hour,
		probeEvery:     60 * time.Second,
		volumeEvery:    5 * time.Minute,
		helloTimeout:   30 * time.Second,
		pingEvery:      20 * time.Second,
		pingTimeout:    40 * time.Second,
		backoffMin:     time.Second,
		backoffMax:     30 * time.Second,
		refusedWait:    time.Minute,
		revokedWait:    10 * time.Minute,
		ballastBytes:   1 << 30,
		outboxTTL:      time.Hour,
		logBatch:       250 * time.Millisecond,
		execKeep:       10 * time.Minute,
	}
}

// Agent is one running agent.
type Agent struct {
	// ctx is the run context; followers and checkers derive from it.
	ctx   context.Context
	o     Options
	t     tuning
	log   *slog.Logger
	eng   engine.Engine
	cache *statecache.Cache
	logs  *logstore.Store
	tools *tools.Manager
	roots sample.Roots

	assetPaths map[string]string
	bootTime   time.Time
	startedAt  time.Time

	applyCh chan desiredMsg
	kick    chan struct{}

	mu sync.Mutex
	// desired is the state being enforced (fresh, or from the cache).
	desired *proto.DesiredState
	applied int64
	epoch   string
	// cached is true while running from the cache without a fresh state.
	cached bool
	// freshSess is the session id that delivered the last desired state;
	// removals are allowed only while it is the current session.
	freshSess uint64
	sess      *session
	sessSeq   uint64
	token     string
	everAcked bool

	replicas    map[string]*replica
	maintenance map[string]bool
	// maintOn is set between a maintenance order with On and the one that ends it.
	maintOn    bool
	meshDown   map[string]bool
	fwWarnings []string
	maintMu    sync.Mutex
	upgradeMu  sync.Mutex
	infra      infraState
	upstreams  map[string][]string

	sem      chan struct{}
	outbox   *outbox
	builds   map[string]context.CancelFunc
	jobs     map[string]context.CancelFunc
	streams  *streamTable
	http     *httpAgg
	db       map[string]proto.DBSample
	volSize  map[string]int64
	cursors  *cursorFile
	disk     *diskGuard
	regFront *registryFront
	cpu      *sample.CPUTracker
	hostCPU  sample.CPUTimes
}

// infraState remembers which infrastructure images were last ensured.
type infraState struct {
	caddy    string
	buildkit string
	caddyOK  bool
	buildOK  bool
}

// Run starts the agent and blocks until ctx ends.
func Run(ctx context.Context, o Options) error {
	a, err := newAgent(o)
	if err != nil {
		return err
	}
	return a.run(ctx)
}

func newAgent(o Options) (*Agent, error) {
	if o.DataDir == "" {
		return nil, errors.New("the agent needs a data directory (Options.DataDir)")
	}
	if o.Network == nil || o.Ingress == nil || o.Builder == nil {
		return nil, errors.New("the agent needs its network, ingress and builder parts")
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if o.Version == "" {
		o.Version = version.Version
	}
	if o.Roots.Proc == "" {
		o.Roots = sample.DefaultRoots
	}
	a := &Agent{
		o: o, t: defaultTuning(), log: o.Logger, roots: o.Roots,
		applyCh: make(chan desiredMsg, 1), kick: make(chan struct{}, 1),
		token: o.HostToken, replicas: map[string]*replica{}, maintenance: map[string]bool{},
		upstreams: map[string][]string{}, builds: map[string]context.CancelFunc{}, jobs: map[string]context.CancelFunc{},
		db: map[string]proto.DBSample{}, volSize: map[string]int64{}, cpu: sample.NewCPUTracker(),
		startedAt: time.Now(), ctx: context.Background(),
	}
	if a.o.Live == nil {
		a.o.Live = NewLive(o.HostToken)
	}
	a.outbox = newOutbox(a.t.outboxTTL)
	a.streams = newStreamTable()
	a.http = newHTTPAgg()
	for _, d := range []string{"agent", "logs", "tools", "assets", "replicas", "jobs"} {
		if err := os.MkdirAll(filepath.Join(o.DataDir, d), 0o700); err != nil {
			return nil, fmt.Errorf("could not create %s: %w", filepath.Join(o.DataDir, d), err)
		}
	}
	// Assets are read by containers, so their directory is world-readable.
	_ = os.Chmod(filepath.Join(o.DataDir, "assets"), 0o755)
	_ = os.Chmod(filepath.Join(o.DataDir, "tools"), 0o755)
	var err error
	if a.assetPaths, err = assets.WriteAll(filepath.Join(o.DataDir, "assets")); err != nil {
		return nil, err
	}
	if o.Engine != nil {
		a.eng = o.Engine
	} else {
		d, err := engine.NewDocker(o.DockerHost)
		if err != nil {
			return nil, err
		}
		a.eng = d
	}
	if len(o.HostKey) > 0 {
		if a.cache, err = statecache.New(statecache.Path(o.DataDir), o.HostKey); err != nil {
			return nil, err
		}
	}
	if a.logs, err = logstore.Open(filepath.Join(o.DataDir, "logs")); err != nil {
		return nil, err
	}
	a.tools = o.Tools
	if a.tools == nil {
		a.tools = tools.New(filepath.Join(o.DataDir, "tools"), nil)
	}
	if bt, err := a.roots.BootTime(); err == nil {
		a.bootTime = bt
	}
	a.cursors = loadCursors(filepath.Join(o.DataDir, "agent", "log-cursors.json"))
	a.disk = newDiskGuard(a)
	a.regFront = newRegistryFront(a)
	return a, nil
}

// connectedOnce reports whether the agent ever completed hello on this run
// (the self-upgrade revert check, 9.16).
func (a *Agent) connectedOnce() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.everAcked
}

func (a *Agent) run(ctx context.Context) error {
	a.ctx = ctx
	defer func() { _ = a.logs.Close() }()
	if err := upgrade.CheckPendingAgentUpgrade(a.o.Upgrade, a.connectedOnce); err != nil {
		a.log.Warn("pending agent upgrade check failed", "err", err)
	}
	if _, err := a.eng.Version(ctx); err != nil {
		a.log.Warn("Docker is not reachable yet; the agent keeps trying", "err", err)
	}
	a.bootFromCache(ctx)

	var wg sync.WaitGroup
	loops := []func(context.Context){
		a.reconcileLoop, a.eventLoop, a.metricsLoop, a.tailLoop, a.janitorLoop,
		a.retentionLoop, a.probeLoop, a.volumeLoop, a.accessLogLoop, a.certLoop, a.certStatusLoop,
	}
	for _, l := range loops {
		wg.Add(1)
		go func(l func(context.Context)) {
			defer wg.Done()
			l(ctx)
		}(l)
	}
	a.connectLoop(ctx)
	wg.Wait()
	a.regFront.close()
	a.stopAllFollowers()
	for _, p := range []any{a.o.Ingress, a.o.Network} {
		if c, ok := p.(io.Closer); ok {
			_ = c.Close()
		}
	}
	a.cursors.save()
	return nil
}

// bootFromCache applies the cached desired state before connecting, so
// private DNS, isolation, quarantine, the mesh and public routes come back
// after a reboot or an agent restart while the control plane is down (7.2).
// Nothing is stopped or removed in this mode.
func (a *Agent) bootFromCache(ctx context.Context) {
	if a.cache == nil {
		return
	}
	e, err := a.cache.Load()
	if errors.Is(err, statecache.ErrNoCache) {
		a.log.Info("no desired-state cache yet; waiting for the control plane")
		return
	}
	if err != nil {
		a.log.Warn("could not use the desired-state cache", "err", err)
		return
	}
	ds := e.State
	a.mu.Lock()
	a.desired = &ds
	a.applied = ds.Generation
	a.epoch = ds.Epoch
	a.cached = true
	a.mu.Unlock()
	a.log.Info("applying the cached desired state", "generation", ds.Generation, "replicas", len(ds.Replicas))
	errs, _ := a.apply(ctx, &ds, false)
	for _, e := range errs {
		a.log.Warn("cached state item failed", "err", e)
	}
}

// trigger asks the reconcile loop for a level-triggered pass.
func (a *Agent) trigger() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// reconcileLoop serialises every apply: new desired states, the 30 s tick
// and Docker events.
func (a *Agent) reconcileLoop(ctx context.Context) {
	tick := time.NewTicker(a.t.reconcileEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-a.applyCh:
			a.handleDesired(ctx, m)
		case <-a.kick:
			a.reconcileCurrent(ctx)
		case <-tick.C:
			a.reconcileCurrent(ctx)
		}
	}
}

func (a *Agent) reconcileCurrent(ctx context.Context) {
	a.mu.Lock()
	ds := a.desired
	allow := a.removalAllowedLocked()
	a.mu.Unlock()
	if ds == nil {
		return
	}
	_, _ = a.apply(ctx, ds, allow)
}

// removalAllowedLocked reports whether the current connection delivered a
// desired state after hello_ack (7.2).
func (a *Agent) removalAllowedLocked() bool {
	return a.sess != nil && a.freshSess == a.sess.id && !a.cached
}

// desiredMsg is a desired state and the connection that delivered it.
type desiredMsg struct {
	ds   *proto.DesiredState
	sess uint64
}

// handleDesired applies a state received from the control plane.
func (a *Agent) handleDesired(ctx context.Context, m desiredMsg) {
	ds := m.ds
	a.mu.Lock()
	sess := a.sess
	if ds.Epoch == a.epoch && ds.Generation < a.applied {
		applied := a.applied
		a.mu.Unlock()
		msg := fmt.Sprintf("Refused desired state generation %d: generation %d is already applied and the epoch did not change.", ds.Generation, applied)
		a.log.Warn(msg)
		a.send(proto.TStateApplied, "", proto.StateApplied{Generation: ds.Generation, Epoch: ds.Epoch, Errors: []string{msg}})
		return
	}
	a.desired = ds
	a.cached = false
	// Removals are allowed only for a state that arrived on the connection
	// that is still current (7.2).
	if sess != nil && sess.id == m.sess {
		a.freshSess = sess.id
	}
	allow := a.removalAllowedLocked()
	a.mu.Unlock()

	errs, unknown := a.apply(ctx, ds, allow)

	a.mu.Lock()
	a.applied = ds.Generation
	a.epoch = ds.Epoch
	a.mu.Unlock()
	if a.cache != nil {
		if err := a.cache.Save(statecache.Entry{State: *ds, AppliedAt: time.Now().UnixMilli()}); err != nil {
			errs = append(errs, err.Error())
		}
	}
	a.send(proto.TStateApplied, "", proto.StateApplied{Generation: ds.Generation, Epoch: ds.Epoch, Errors: errs, Unknown: unknown})
}

// AppliedGeneration returns the last applied generation and epoch.
func (a *Agent) AppliedGeneration() (int64, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applied, a.epoch
}

func hasRole(h proto.HostConfig, role string) bool {
	for _, r := range h.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// every runs fn at the interval until ctx ends.
func every(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}
