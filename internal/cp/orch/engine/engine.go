// SPDX-License-Identifier: AGPL-3.0-only

// Package engine is the orchestrator: it implements cpapi.Orchestrator on
// top of the agent hub, the scheduler, IPAM, routes, logs, metrics,
// monitors and usage, and delegates data services to WP2b's data.Service
// (docs/SPEC.md 8.3, 9).
//
// State lives in state.db (o_* tables, written by one writer goroutine)
// and metrics.db. Agents only reconcile: the engine computes each host's
// full desired state, the hub numbers and pushes it, and replica reports
// flow back into o_replicas, which drives rollouts, routes and status.
package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/data"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/logs"
	"github.com/musakkka/5050cloud/internal/cp/orch/metrics"
	"github.com/musakkka/5050cloud/internal/cp/orch/monitors"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/version"
)

// Timing holds every delay of the protocol and the pipeline. Zero values
// use the documented defaults; tests shorten them.
type Timing struct {
	OfflineAfter    time.Duration // 60 s (7.1)
	RescheduleAfter time.Duration // 120 s after offline (7.1)
	Liveness        time.Duration // how often host liveness is checked, 2 s
	PushInterval    time.Duration // 250 ms (7.1)
	PingInterval    time.Duration // 20 s
	DeadAfter       time.Duration // 60 s
	QueueTimeout    time.Duration // 15 min (9.1)
	CapacityWait    time.Duration // 10 min (7.3)
	SwitchWait      time.Duration // 15 s (7.3)
	StopWait        time.Duration // extra wait for a stop to be confirmed, 20 s
	HealthSlack     time.Duration // pull and start time added to the health deadline, 5 min
	MonitorEvery    time.Duration // 1 min (9.21)
	BuildGrace      time.Duration // a build result may arrive this long after its timeout, 5 min
	RetryEvery      time.Duration // placement and build-host retries, 5 s
	CertEvery       time.Duration // certificate expiry checks, 1 h
	// DrainUnit is the length of one "second" of OverlapSec and DrainingSec
	// (1 s; tests shrink it).
	DrainUnit time.Duration
}

func (t Timing) withDefaults() Timing {
	def := func(v *time.Duration, d time.Duration) {
		if *v == 0 {
			*v = d
		}
	}
	def(&t.OfflineAfter, 60*time.Second)
	def(&t.RescheduleAfter, 120*time.Second)
	def(&t.Liveness, 2*time.Second)
	def(&t.PushInterval, 250*time.Millisecond)
	def(&t.PingInterval, 20*time.Second)
	def(&t.DeadAfter, 60*time.Second)
	def(&t.QueueTimeout, 15*time.Minute)
	def(&t.CapacityWait, 10*time.Minute)
	def(&t.SwitchWait, 15*time.Second)
	def(&t.StopWait, 20*time.Second)
	def(&t.HealthSlack, 5*time.Minute)
	def(&t.MonitorEvery, time.Minute)
	def(&t.BuildGrace, 5*time.Minute)
	def(&t.RetryEvery, 5*time.Second)
	def(&t.CertEvery, time.Hour)
	def(&t.DrainUnit, time.Second)
	return t
}

// Options builds an Orchestrator.
type Options struct {
	DB     *sql.DB
	Config cpapi.Config
	// DataDir holds metrics.db, builds/, uploads/ (default /var/lib/5050).
	DataDir   string
	PublicURL string
	LocalURL  string
	PodCIDR   string
	MeshCIDR  string
	// Box encrypts secret columns (variable snapshots, certificates,
	// registry credentials). Required.
	Box *secretbox.Box
	// Templates is templates.FS, passed to data services and read for tools.json.
	Templates fs.FS
	// NewData builds the data services (default data.New).
	NewData func(data.Options) (data.Service, error)
	// Upgrade configures release downloads (9.16).
	Upgrade upgrade.Options
	// ServerVersion defaults to pkg/version.Version.
	ServerVersion string
	// ACMECA is the ACME directory ingress hosts use: "" (production),
	// "staging" or a directory URL (--acme-ca at init).
	ACMECA string
	// Git resolves branch heads and commit details (default: smart HTTP and the GitHub API).
	Git    GitResolver
	Log    *slog.Logger
	Timing Timing
	Now    func() time.Time
}

// Orchestrator implements cpapi.Orchestrator.
type Orchestrator struct {
	o      Options
	t      Timing
	cfg    cpapi.Config
	db     *sql.DB
	st     *Store
	hub    *hub.Hub
	data   data.Service
	met    *metrics.Store
	builds *logs.Builds
	fan    *logs.Fanout
	mon    *monitors.Evaluator
	box    *secretbox.Box
	plan   ipam.Plan
	log    *slog.Logger
	now    func() time.Time

	ctx    context.Context // set by Start
	cancel context.CancelFunc

	mu        sync.Mutex
	lanes     map[string]*lane
	buildWait map[string]chan buildOutcome
	building  map[string]int // host -> running builds
	paused    int
	pauseCond *sync.Cond
	envCache  map[string]map[string]string // deployment -> decrypted variable snapshot
	render    map[string]data.Rendered     // deployment|host -> rendered template material
	changed   chan struct{}
	alerted   map[string]bool
	placeMu   sync.Mutex
	net       *netCache
	agentUp   sync.Mutex
	certRetry map[string]bool
	jobs      *jobTracker
	git       GitResolver
	startedAt time.Time
}

var _ cpapi.Orchestrator = (*Orchestrator)(nil)

// New opens metrics.db, migrates the orchestrator tables and builds the
// hub and the data services. Start runs it.
func New(o Options) (*Orchestrator, error) {
	if o.DB == nil || o.Config == nil {
		return nil, errorf("The orchestrator needs the state database and the core config.")
	}
	if o.Box == nil {
		return nil, errorf("The orchestrator needs the master key to encrypt variable snapshots and certificates.")
	}
	if o.DataDir == "" {
		o.DataDir = "/var/lib/5050"
	}
	if o.ServerVersion == "" {
		o.ServerVersion = version.Version
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.NewData == nil {
		o.NewData = data.New
	}
	plan, err := ipam.NewPlan(o.PodCIDR, o.MeshCIDR)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if err := Migrate(ctx, o.DB); err != nil {
		return nil, err
	}
	for _, d := range []string{"builds", "uploads"} {
		if err := os.MkdirAll(filepath.Join(o.DataDir, d), 0o750); err != nil {
			return nil, errorf("Could not create %s: %w", filepath.Join(o.DataDir, d), err)
		}
	}
	met, err := metrics.Open(filepath.Join(o.DataDir, "metrics.db"), metrics.Options{Log: o.Log, Now: o.Now})
	if err != nil {
		return nil, err
	}
	builds, err := logs.NewBuilds(filepath.Join(o.DataDir, "builds"))
	if err != nil {
		_ = met.Close()
		return nil, err
	}
	e := &Orchestrator{
		o: o, t: o.Timing.withDefaults(), cfg: o.Config, db: o.DB, st: NewStore(o.DB), met: met, builds: builds,
		box: o.Box, plan: plan, log: o.Log.With("component", "orchestrator"), now: o.Now,
		lanes: map[string]*lane{}, buildWait: map[string]chan buildOutcome{}, building: map[string]int{},
		envCache: map[string]map[string]string{}, render: map[string]data.Rendered{}, changed: make(chan struct{}),
		alerted: map[string]bool{}, net: &netCache{}, certRetry: map[string]bool{}, jobs: newJobTracker(), git: o.Git,
		startedAt: o.Now(),
	}
	if e.git == nil {
		e.git = newHTTPGit()
	}
	e.pauseCond = sync.NewCond(&e.mu)
	e.hub = hub.New(hub.Options{
		DB: o.DB, Write: e.st.Write, Handler: e, Plan: plan, ServerVersion: o.ServerVersion,
		UploadPath: e.uploadPath, Log: o.Log, Now: o.Now,
		PingInterval: e.t.PingInterval, DeadAfter: e.t.DeadAfter, PushInterval: e.t.PushInterval,
	})
	e.fan = &logs.Fanout{Src: logSource{e}, Builds: builds, HostTimeout: 20 * time.Second}
	e.mon = monitors.New(o.Config, monitorSource{e}, o.Log)
	e.mon.Now = o.Now
	ds, err := o.NewData(data.Options{DB: o.DB, Config: o.Config, Runner: runner{e}, Templates: o.Templates, DataDir: o.DataDir})
	if err != nil {
		_ = met.Close()
		e.st.Close()
		return nil, err
	}
	e.data = ds
	return e, nil
}

// Hub exposes the agent hub (tests, the integrator's tools).
func (o *Orchestrator) Hub() *hub.Hub { return o.hub }

// Start loads state, resumes in-flight deployments and runs the
// background loops until ctx is done.
func (o *Orchestrator) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	o.mu.Lock()
	o.ctx, o.cancel = ctx, cancel
	o.startedAt = o.now()
	o.mu.Unlock()
	defer cancel()
	if err := o.hub.Load(ctx); err != nil {
		return err
	}
	if err := o.ensureRegistryCreds(ctx); err != nil {
		return err
	}
	if err := o.resume(ctx); err != nil {
		o.log.Error("resume deployments", "err", err)
	}
	var wg sync.WaitGroup
	run := func(f func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); f(ctx) }()
	}
	run(o.livenessLoop)
	run(o.met.Run)
	run(func(ctx context.Context) { o.mon.Run(ctx, o.t.MonitorEvery) })
	run(o.janitorLoop)
	run(o.agentUpgradeLoop)
	run(func(ctx context.Context) {
		if err := o.data.Start(ctx); err != nil && ctx.Err() == nil {
			o.log.Error("data services stopped", "err", err)
		}
	})
	<-ctx.Done()
	wg.Wait()
	o.waitLanes()
	_ = o.met.Close()
	o.st.Close()
	return nil
}

// Ready reports whether the hub accepts agents and how many are connected.
func (o *Orchestrator) Ready(context.Context) (bool, int) {
	return o.hub.Ready(), len(o.hub.ConnectedHosts())
}

// AgentHandler serves /api/agent/v1/.
func (o *Orchestrator) AgentHandler() http.Handler { return o.hub.Handler() }

func (o *Orchestrator) bg() context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ctx != nil {
		return o.ctx
	}
	return context.Background()
}

// notify wakes everything waiting for replica or deployment changes.
func (o *Orchestrator) notify() {
	o.mu.Lock()
	close(o.changed)
	o.changed = make(chan struct{})
	o.mu.Unlock()
}

func (o *Orchestrator) changes() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.changed
}

// ---------------------------------------------------------------- secrets

func (o *Orchestrator) sealEnv(depID string, env map[string]string) ([]byte, error) {
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return o.box.Seal(b, "o_deployments|"+depID+"|env")
}

func (o *Orchestrator) openEnv(depID string, enc []byte) (map[string]string, error) {
	o.mu.Lock()
	if m, ok := o.envCache[depID]; ok {
		o.mu.Unlock()
		return m, nil
	}
	o.mu.Unlock()
	env := map[string]string{}
	if len(enc) > 0 {
		b, err := o.box.Open(enc, "o_deployments|"+depID+"|env")
		if err != nil {
			return nil, errorf("The variable snapshot of deployment %s cannot be decrypted with this master key.", depID)
		}
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, err
		}
	}
	o.mu.Lock()
	if len(o.envCache) > 4096 {
		o.envCache = map[string]map[string]string{}
	}
	o.envCache[depID] = env
	o.mu.Unlock()
	return env, nil
}

// registryCreds are the internal registry's push and pull users.
type registryCreds struct {
	PushUser string `json:"pushUser"`
	PushPass string `json:"pushPass"`
	PullUser string `json:"pullUser"`
	PullPass string `json:"pullPass"`
}

func (o *Orchestrator) ensureRegistryCreds(ctx context.Context) error {
	var enc []byte
	ok, err := hub.GetState(ctx, o.db, "registry_credentials", &enc)
	if err != nil {
		return err
	}
	if ok && len(enc) > 0 {
		if _, err := o.box.Open(enc, "o_state|registry_credentials"); err == nil {
			return nil
		}
		return errorf("The registry credentials in state.db cannot be decrypted with this master key.")
	}
	c := registryCreds{PushUser: "push", PushPass: hub.NewToken(), PullUser: "pull", PullPass: hub.NewToken()}
	b, _ := json.Marshal(c)
	sealed, err := o.box.Seal(b, "o_state|registry_credentials")
	if err != nil {
		return err
	}
	return o.st.Write(ctx, func(tx *sql.Tx) error { return hub.PutState(ctx, tx, "registry_credentials", sealed) })
}

func (o *Orchestrator) registryCreds(ctx context.Context) (registryCreds, error) {
	var enc []byte
	var c registryCreds
	ok, err := hub.GetState(ctx, o.db, "registry_credentials", &enc)
	if err != nil || !ok {
		return c, errors.Join(err, errorf("The registry credentials are missing."))
	}
	b, err := o.box.Open(enc, "o_state|registry_credentials")
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}

func (o *Orchestrator) uploadPath(_ context.Context, id string) (string, error) {
	if !validFileID(id) {
		return "", errorf("This upload does not exist.")
	}
	p := filepath.Join(o.o.DataDir, "uploads", id+".tar.gz")
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func validFileID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		ok := r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- core callbacks

func (o *Orchestrator) publishDep(d depRow) {
	ad := o.apiDeployment(d)
	o.cfg.Publish(apiEvent("deployment", d.EnvID, d.ServiceID, &ad))
}

func (o *Orchestrator) activity(ctx context.Context, envID, svcID, kind, text, actor, ref, status string) {
	if err := o.cfg.RecordActivity(ctx, envID, apiActivity(svcID, kind, text, actor, ref, status, o.now())); err != nil {
		o.log.Warn("record activity", "err", err)
	}
}

func (o *Orchestrator) notice(ctx context.Context, n cpapi.Notice) {
	if err := o.cfg.Notify(ctx, n); err != nil {
		o.log.Warn("notify", "event", n.Event, "err", err)
	}
}
