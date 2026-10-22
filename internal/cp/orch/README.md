# internal/cp/orch

The orchestrator half of `5050d` (docs/SPEC.md 8.3, 9). It implements
`cpapi.Orchestrator` and owns hosts, the agent hub, IP allocation, placement,
the deploy engine, desired-state computation, builds, routes, logs and
metrics fan-out, monitor evaluation and usage.

Two work packages share this tree:

| Directory | Owner | What |
|---|---|---|
| `internal/cp/orch/` (except `data/`) | WP2a | hub, engine, sched, ipam, routes, logs, metrics, monitors, usage, registry client |
| `internal/cp/orch/data/` | WP2b | cron, backups, restore, promote, major upgrades, credential regeneration, template rendering at deploy, registry retention, data inspection |

WP2a's `Orchestrator` struct holds a `data.Service` and delegates every data
route to it. WP2b never imports WP2a: everything it needs from the hub and the
engine comes through the `data.Runner` interface, which WP2a implements and
passes in `data.Options`. Both halves call back into core only through
`cpapi.Config`, and publish events with `Config.Publish`.

## Rules both packages follow

- Tables use the `o_` prefix and the orchestrator migration set
  (`orch_migrations`, embedded `0001_*.sql`, forward only). WP2b's tables are
  `o_cron_runs`, `o_backups`, `o_backup_schedules` and its own `o_data_*`
  tables; WP2a owns every other `o_` table.
- SQLite rules from docs/SPEC.md 6.5: one writer goroutine per database,
  short read transactions, nothing held open while streaming.
- Every error a person can see is one full sentence (docs/SPEC.md 9.18).
- Unit tests use `internal/cp/orch/fakeconfig` (WP2a ships it) for
  `cpapi.Config` and a fake `data.Runner` (WP2b ships it).

## data.Service interface

This section is frozen (docs/SPEC.md 5.3); its hash is in
`docs/contracts.sha256`. The declarations below are copied verbatim from
`internal/cp/orch/data/stub.go`. WP2b replaces the stub implementation and
keeps these declarations identical; a change goes through the planner first.

```go
// Runner is what WP2a gives WP2b: placement and execution on agents.
type Runner interface {
	// RunJob dispatches a job to a host and waits for its result, streaming log lines.
	RunJob(ctx context.Context, hostID string, job proto.Job, logf func(line string)) (proto.JobResult, error)
	// PlaceJob picks an online host with every role in roles (empty = "app")
	// that has a free network slot for envID. Jobs that need a volume use
	// VolumeHost instead.
	PlaceJob(ctx context.Context, envID string, roles []string) (hostID string, err error)
	// ActiveReplica returns the host and replica running the service's active deployment.
	ActiveReplica(ctx context.Context, envID, serviceID string) (hostID, replicaID string, err error)
	// VolumeHost returns the host a service's volume lives on ("" before first deploy).
	VolumeHost(ctx context.Context, envID, serviceID string) (string, error)
	// ControlHost returns the control host id (retention jobs run there).
	ControlHost(ctx context.Context) (string, error)
	// JobNetwork returns the docker network name and a free IP for a job in envID on hostID.
	JobNetwork(ctx context.Context, envID, hostID string) (network, ip string, release func(), err error)
	// Deploy starts a deployment (restore copies, promote, scheduled cron images).
	Deploy(ctx context.Context, in cpapi.DeployInput) (api.Deployment, error)
	// StopService stops a service's replicas without a new deployment (promote, wipe).
	StopService(ctx context.Context, envID, serviceID, actor string) error
	// PauseBuilds pauses the build queue and waits for running pushes to end;
	// resume restarts it (registry garbage collection, 9.6).
	PauseBuilds(ctx context.Context) (resume func(), err error)
	// RegistryJob runs a command in the registry container on the control host.
	RegistryJob(ctx context.Context, cmd []string, env map[string]string) (proto.JobResult, error)
	// ActiveImage returns the image, config snapshot and variable snapshot of the active deployment.
	ActiveImage(ctx context.Context, envID, serviceID string) (image string, cfg api.ServiceConfig, env map[string]string, err error)
}

// Options builds a Service.
type Options struct {
	DB *sql.DB // state.db; WP2b tables use the "o_" prefix and orch migrations
	// Config is core; Runner is WP2a.
	Config cpapi.Config
	Runner Runner
	// Templates is templates.FS.
	Templates fs.FS
	DataDir   string
}

// Rendered is a template service's runtime material for one deploy on one host.
type Rendered struct {
	Files        []proto.FileSpec
	Mounts       []proto.Mount
	Tools        []string
	Env          map[string]string // extra variables (backup credentials, memory settings)
	Health       []string          // exec health probe
	Probe        []string          // metrics probe (9.10)
	StopGraceSec int
	ShmSizeMB    int
	// MemoryReservationMB overrides the default reservation (512 for databases).
	MemoryReservationMB int
}

// Service is the data services API WP2a calls.
type Service interface {
	// Start runs the cron scheduler, backup schedules, retention, registry
	// GC and the monthly restore test. Blocks until ctx is done. On start it
	// records missed cron slots of the last 24 hours (9.9).
	Start(ctx context.Context) error

	// Render returns the files, mounts, tools and env of a template service
	// for a deploy on a host with the given facts. Services without a template return a zero Rendered.
	Render(ctx context.Context, snap cpapi.ServiceSnapshot, host proto.Facts) (Rendered, error)
	// ServiceChanged is called after a deploy, removal or config change so
	// schedules follow (cron next runs, backup schedules).
	ServiceChanged(ctx context.Context, envID, serviceID string)
	// DBSample feeds probe results for the WAL guard and backup health (9.13).
	DBSample(ctx context.Context, envID, serviceID string, s proto.DBSample)

	// cron
	CronRuns(ctx context.Context, envID, serviceID string, limit int) ([]api.CronRun, error)
	RunCronNow(ctx context.Context, envID, serviceID, actor string) (api.CronRun, error)

	// backups and restore
	Backups(ctx context.Context, envID, serviceID string) (api.BackupsResponse, error)
	BackupNow(ctx context.Context, envID, serviceID, actor string) (api.Backup, error)
	SetBackupSchedule(ctx context.Context, envID, serviceID string, s api.BackupSchedule) (api.BackupSchedule, error)
	LockBackup(ctx context.Context, backupID string, locked bool) (api.Backup, error)
	DeleteBackup(ctx context.Context, backupID string) error
	Restore(ctx context.Context, envID, serviceID string, req api.RestoreRequest, actor string) (api.RestoreResponse, error)
	Promote(ctx context.Context, envID, serviceID string, req api.PromoteRequest, actor string) error
	MajorUpgrade(ctx context.Context, envID, serviceID string, req api.MajorUpgradeRequest, actor string) (api.RestoreResponse, error)
	RegenerateCredentials(ctx context.Context, envID, serviceID, actor string) (api.CommitResponse, error)
	TestBucket(ctx context.Context, s api.BackupSettings) api.TestResult
	BackupSettingsChanged(ctx context.Context) error

	// volumes
	WipeVolume(ctx context.Context, envID, serviceID, actor string) error

	// data inspection
	DataSummary(ctx context.Context, envID, serviceID string) (api.DataResponse, error)
	Extensions(ctx context.Context, envID, serviceID string) ([]api.Extension, error)
	EnableExtension(ctx context.Context, envID, serviceID, name, actor string) (api.Extension, error)
	RedisKeys(ctx context.Context, envID, serviceID, match, cursor string) (api.RedisKeysPage, error)
	RevealRedisKey(ctx context.Context, envID, serviceID, key string) (api.RedisRevealResponse, error)
}

```

## Call flow

- **Deploy of a template service**: the engine calls `Service.Render` with the
  snapshot and the facts of the host chosen by placement, then merges the
  result into the `proto.ReplicaSpec` (files, mounts, tools, env, health exec,
  probe, stop grace, shm size).
- **After every deploy, removal or committed config change**: the engine calls
  `Service.ServiceChanged` so cron schedules and backup schedules follow.
- **Metrics**: when an agent's `proto.Metrics` carries `DB` samples, the hub
  passes each one to `Service.DBSample` (WAL guard, backup health).
- **Data routes**: `Orchestrator.Backups`, `BackupNow`, `SetBackupSchedule`,
  `LockBackup`, `DeleteBackup`, `Restore`, `Promote`, `MajorUpgrade`,
  `RegenerateCredentials`, `TestBucket`, `BackupSettingsChanged`,
  `WipeVolume`, `CronRuns`, `RunCronNow`, `DataSummary`, `Extensions`,
  `EnableExtension`, `RedisKeys` and `RevealRedisKey` forward to the method of
  the same name.
- **Registry GC**: WP2b schedules it (weekly and above the size cap), calls
  `Runner.PauseBuilds`, switches the registry to read-only through
  `Runner.RegistryJob`, runs `registry garbage-collect --delete-untagged`,
  switches it back and calls `resume` (docs/SPEC.md 9.6).
