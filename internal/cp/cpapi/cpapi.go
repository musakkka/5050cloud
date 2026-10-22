// SPDX-License-Identifier: AGPL-3.0-only

// Package cpapi is the in-process contract between the two halves of 5050d,
// so they can be built in parallel. Frozen for v0.1; see docs/SPEC.md 8.3.
//
//   - Config is implemented by cp-core (WP1: internal/cp/core/...). It owns
//     users, projects, environments, services and their committed config,
//     variables, staged changes, config files, domains, TCP proxies, volumes,
//     monitors (definitions), GitHub, settings, email, notifications, audit,
//     activity and presence.
//   - Orchestrator is implemented by cp-orchestrator (WP2a:
//     internal/cp/orch/...), which delegates data services to WP2b
//     (internal/cp/orch/data, interface in internal/cp/orch/README.md). It
//     owns hosts, the agent hub, IP allocation, deployments, replicas,
//     builds, routes, logs/metrics fan-out, monitor evaluation, usage, cron
//     runs, backups, restore and upgrades.
//
// Both use the same SQLite file (state.db) through *sql.DB, each with its own
// migration set and table prefix (core tables have no prefix, orchestrator
// tables start with "o_"). Neither touches the other's tables directly.
//
// Templates: both halves read templates.FS by ServiceConfig.Template. Core
// renders template variables once at creation; the orchestrator (WP2b)
// renders files, mounts, tools and backup recipes at each deploy. Neither
// calls the other for template data.
//
// Events: the orchestrator publishes through Config.Publish. There is no
// separate event bus in orch.Options.
//
// Wiring in cmd/5050d (WP1):
//
//	db := store.Open(cfg.DataDir)
//	c := core.New(db, cfg)                      // implements cpapi.Config
//	o, err := orch.New(orch.Options{DB: db, Config: c, DataDir: cfg.DataDir, PublicURL: ..., LocalURL: ...})
//	c.SetOrchestrator(o)
//	go o.Start(ctx)
//	http handler = core API router, which mounts o.AgentHandler() at /api/agent/v1/
package cpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// ---------------------------------------------------------------- shared types

// ServiceSnapshot is a committed service instance, as the orchestrator needs it.
type ServiceSnapshot struct {
	ProjectID   string
	ProjectName string
	EnvID       string
	EnvName     string
	ServiceID   string
	Rev         int64 // committed config revision
	Config      api.ServiceConfig
	PrivateName string // "api" -> api.internal
	// PrivateAliases are old private names kept for 24 hours after a rename.
	PrivateAliases []string
	Domains        []api.Domain
	TCPProxies     []api.TCPProxy
	Volume         *api.Volume
	// References are the names of services in the same environment whose
	// variables this service references (deploy ordering, canvas edges).
	References  []string
	Egress      string   // environment egress: "allowed" | "blocked"
	EgressAllow []string // environment allowlist when Egress is "blocked"
	Deleted     bool
}

// Runtime is the orchestrator's view of a service instance for cards and panels.
type Runtime struct {
	Status           api.Status
	StatusText       string
	ActiveDeployment string
	Replicas         []api.Replica
	ListeningPorts   []int
	OutboundIPs      []string
	Processes        int
	ProcessesText    string
	MonitorFiring    bool
	VolumeUsedBytes  int64
	VolumeHostLost   bool
}

// PlanItem is one service affected by a commit.
type PlanItem struct {
	ServiceID string
	// Action: "create" | "update" | "delete" | "scale".
	Action string
	// Rebuild is true when source or build settings changed (or the
	// service has no image yet); false means redeploy the last image with the
	// new config and variables.
	Rebuild bool
}

type CommitPlan struct {
	CommitID string
	EnvID    string
	Items    []PlanItem
	Deploy   bool // false = committed without deploying (only deletes and scale are applied)
	Actor    string
	Message  string
}

type DeployInput struct {
	EnvID     string
	ServiceID string
	Trigger   string // see api.Deployment.Trigger
	Actor     string
	Message   string
	// Exactly one source selector: Commit (git), UploadPath (5050 up tarball
	// on local disk, moved into the orchestrator's data dir), or neither
	// (image services, or "latest" which the orchestrator resolves with
	// git ls-remote / the GitHub API).
	Commit     *api.Commit
	Ref        string
	UploadPath string
	Rebuild    bool
	// Skipped records a SKIPPED deployment (watch paths matched nothing) instead of deploying.
	Skipped bool
}

type LogsQuery struct {
	EnvID        string
	ServiceIDs   []string // empty = whole environment
	DeploymentID string
	Types        []string // "deploy" | "build" | "http"
	Filter       string
	Since, Until int64
	Limit        int
	Around       string // line id for View in Context
}

// MergedConfig is the effective config of a deployment after a config file overlay (9.20).
type MergedConfig struct {
	Config api.ServiceConfig
	// Source maps config field paths to "file:line".
	Source map[string]string
	// WatchPaths from the file apply to the next push.
	WatchPaths []string
	Notices    []string
}

// Notice is something core delivers to people: email, the bell, project
// webhooks and install alerts, according to preferences (9.15, 9.21).
type Notice struct {
	Event     string // api.Event*, api.Hook* or api.Alert* constant
	ProjectID string // empty for install-wide alerts
	EnvID     string
	ServiceID string
	Ref       string // deployment, backup, host or monitor id
	Text      string // one sentence
	// Users limits email to these user ids (monitor channels); empty = by preferences.
	Users []string
	// Webhooks limits delivery to these webhook ids; empty = every subscribed webhook.
	Webhooks []string
	Details  map[string]any
}

// VolumeRuntime is what the volume panel needs from the orchestrator.
type VolumeRuntime struct {
	HostID         string
	HostName       string
	UsedBytes      int64
	HostFreeBytes  int64
	HostTotalBytes int64
	HostLost       bool
}

// ---------------------------------------------------------------- Config (WP1 implements)

type Config interface {
	Install(ctx context.Context) (api.Install, error)
	BackupSettings(ctx context.Context) (api.BackupSettings, error) // includes both secret keys

	Environment(ctx context.Context, envID string) (api.Environment, api.Project, error)
	ServiceInstance(ctx context.Context, envID, serviceID string) (ServiceSnapshot, error)
	ServiceInstances(ctx context.Context, envID string) ([]ServiceSnapshot, error)
	// AllInstances lists every committed, non-deleted instance (used on boot to rebuild desired state).
	AllInstances(ctx context.Context) ([]ServiceSnapshot, error)

	// ResolveVariables returns the final environment for one service
	// instance: user variables with ${{...}} references resolved against the
	// environment, plus the provided variables passed in (PORT, FIFTY_*,
	// RAILWAY_* aliases are computed by the orchestrator, which knows replica
	// and deployment ids). Unresolved lists references that resolved to "".
	ResolveVariables(ctx context.Context, envID, serviceID string, provided map[string]string) (env map[string]string, unresolved []string, err error)
	// SecretValues returns values that must be masked in logs for this service.
	SecretValues(ctx context.Context, envID, serviceID string) ([]string, error)

	// MergeConfigFile records the config-as-code file a build found and
	// returns the effective config for this deployment (9.20). file is nil
	// when the source has no config file.
	MergeConfigFile(ctx context.Context, envID, serviceID, commitSHA string, file *proto.ConfigOverlay) (MergedConfig, error)

	GitHubCloneToken(ctx context.Context, repo string) (string, error) // "" for public repos
	RegistryCredential(ctx context.Context, id string) (username, password, server string, err error)

	// Services created by the orchestrator (restore to a copy, major
	// upgrade) and the rename swap used by Promote. They commit immediately
	// and record activity.
	CloneServiceForRestore(ctx context.Context, envID, sourceServiceID, newName, targetEnvID string) (newServiceID string, err error)
	SwapServiceNames(ctx context.Context, envID, keepNameServiceID, otherServiceID, otherNewName string) error
	// CommitVariables sets variables of one service and commits at once
	// (Regenerate credentials); dependents redeploy through ApplyCommit.
	CommitVariables(ctx context.Context, envID, serviceID string, set map[string]string, actor, message string) (api.CommitResponse, error)

	// Monitors returns every monitor definition (the orchestrator evaluates them).
	Monitors(ctx context.Context) ([]api.Monitor, error)
	// SetMonitorState records "ok" or "firing" after an evaluation.
	SetMonitorState(ctx context.Context, monitorID, state string, at int64) error

	RecordActivity(ctx context.Context, envID string, a api.Activity) error
	Audit(ctx context.Context, e api.AuditEntry) error
	Publish(e api.Event)
	// Notify delivers a notice by email, the bell, webhooks and install alerts.
	Notify(ctx context.Context, n Notice) error
	// FireWebhook queues a signed delivery for every webhook subscribed to event.
	FireWebhook(ctx context.Context, projectID, event string, payload any) error
}

// ---------------------------------------------------------------- Orchestrator (WP2a implements)

type Orchestrator interface {
	// Start loads state, resumes in-flight work and starts schedulers. Blocks until ctx is done.
	Start(ctx context.Context) error
	// Ready reports whether the hub accepts agents and how many are connected (/readyz).
	Ready(ctx context.Context) (hubOK bool, agents int)

	// AgentHandler serves /api/agent/v1/ (join, connect, uploads for builds).
	AgentHandler() http.Handler

	// --- hosts
	Hosts(ctx context.Context) ([]api.Host, error)
	CreateJoinToken(ctx context.Context, req api.CreateJoinTokenRequest) (api.JoinToken, error)
	UpdateHost(ctx context.Context, hostID string, req api.UpdateHostRequest) (api.Host, error)
	SetMaintenance(ctx context.Context, hostID string, on bool, actor string) (api.Host, error)
	// RemoveHost is refused while the host runs replicas unless force is set.
	RemoveHost(ctx context.Context, hostID string, force bool) error
	ClearHostConflicts(ctx context.Context, hostID string) (api.Host, error)
	// HostLost marks a host permanently gone: its volumes show Host lost
	// and its replicas are rescheduled where possible (9.17.2).
	HostLost(ctx context.Context, hostID, actor string) error
	HostMetrics(ctx context.Context, hostID string, fromMs, untilMs int64) (api.MetricsResponse, error)
	// Sync re-pushes the full desired state to one host ("" = every host).
	Sync(ctx context.Context, hostID string) error

	// --- adopt mode after a state restore (9.17.1)
	// Adopt switches adopt mode on with the snapshot time and a new epoch.
	Adopt(ctx context.Context, snapshotAt int64) error
	AdoptStatus(ctx context.Context) (*api.RestoreStatus, error) // nil when not in adopt mode
	UnknownReplicas(ctx context.Context) ([]api.UnknownReplica, error)
	AdoptReplica(ctx context.Context, unknownID, actor string) error
	RemoveUnknownReplica(ctx context.Context, unknownID, actor string) error
	ReadoptHost(ctx context.Context, hostID, actor string) (api.Host, error)
	FinishRestore(ctx context.Context, actor string) error

	// --- deployments
	ApplyCommit(ctx context.Context, plan CommitPlan) ([]api.Deployment, error)
	Deploy(ctx context.Context, in DeployInput) (api.Deployment, error)
	Redeploy(ctx context.Context, deploymentID, actor string) (api.Deployment, error)
	Rollback(ctx context.Context, deploymentID, actor string) (api.Deployment, error)
	Restart(ctx context.Context, deploymentID, actor string) (api.Deployment, error)
	Abort(ctx context.Context, deploymentID, actor string) (api.Deployment, error)
	Remove(ctx context.Context, deploymentID, actor string) (api.Deployment, error)
	MoveReplica(ctx context.Context, replicaID, hostID, actor string) error
	Deployments(ctx context.Context, envID, serviceID string, limit int, query string) ([]api.Deployment, error)
	Deployment(ctx context.Context, deploymentID string) (api.DeploymentDetail, error)
	// BuildLogs writes stored lines then, if follow, live lines until the build ends or ctx is done.
	BuildLogs(ctx context.Context, deploymentID string, follow bool, emit func([]api.LogLine) error) error
	Runtime(ctx context.Context, envID string) (map[string]Runtime, error) // by service id

	// NetworkChanged recomputes routes, TCP proxies, DNS and egress after
	// domains, proxies or environment egress changed. Cheap; may be coalesced.
	NetworkChanged(ctx context.Context, envID string)
	// DomainCertStatus returns "issuing" | "active" | "error" and the expiry (Unix ms).
	DomainCertStatus(ctx context.Context, domain string) (status string, notAfter int64, err error)
	RetryCertificate(ctx context.Context, domain string) error

	// --- logs, metrics, monitors, usage
	Logs(ctx context.Context, q LogsQuery) (api.LogsResponse, error)
	TailLogs(ctx context.Context, q LogsQuery, emit func([]api.LogLine) error) error
	Metrics(ctx context.Context, envID, serviceID string, fromMs, untilMs int64, perReplica bool) (api.MetricsResponse, error)
	DBMetrics(ctx context.Context, envID, serviceID string, fromMs, untilMs int64) (api.DBMetricsResponse, error)
	// Monitors returns the live state of every monitor of a service, by monitor id.
	Monitors(ctx context.Context, envID, serviceID string) (map[string]string, error)
	Usage(ctx context.Context, projectID string, fromMs, untilMs int64) (api.UsageResponse, error)

	// --- interactive (the API layer upgrades the HTTP connection and passes it down)
	// Shell: rw carries terminal bytes; resize events arrive on the channel.
	Shell(ctx context.Context, envID, serviceID, replicaID, session string, cmd []string, cols, rows int, rw io.ReadWriter, resize <-chan [2]int) (exitCode int, err error)
	Tunnel(ctx context.Context, envID, serviceID string, port int, rw io.ReadWriteCloser) error

	// --- volumes
	VolumeRuntime(ctx context.Context, envID, serviceID string) (VolumeRuntime, error)
	WipeVolume(ctx context.Context, envID, serviceID, actor string) error

	// --- cron (delegated to data.Service)
	CronRuns(ctx context.Context, envID, serviceID string, limit int) ([]api.CronRun, error)
	RunCronNow(ctx context.Context, envID, serviceID, actor string) (api.CronRun, error)

	// --- data services (delegated to data.Service)
	Backups(ctx context.Context, envID, serviceID string) (api.BackupsResponse, error)
	BackupNow(ctx context.Context, envID, serviceID, actor string) (api.Backup, error)
	SetBackupSchedule(ctx context.Context, envID, serviceID string, s api.BackupSchedule) (api.BackupSchedule, error)
	LockBackup(ctx context.Context, backupID string, locked bool) (api.Backup, error)
	DeleteBackup(ctx context.Context, backupID string) error
	Restore(ctx context.Context, envID, serviceID string, req api.RestoreRequest, actor string) (api.RestoreResponse, error)
	Promote(ctx context.Context, envID, serviceID string, req api.PromoteRequest, actor string) error
	MajorUpgrade(ctx context.Context, envID, serviceID string, req api.MajorUpgradeRequest, actor string) (api.RestoreResponse, error)
	RegenerateCredentials(ctx context.Context, envID, serviceID, actor string) (api.CommitResponse, error)
	// TestBucket puts, gets and deletes a small object with each key.
	TestBucket(ctx context.Context, s api.BackupSettings) api.TestResult
	// BackupSettingsChanged redeploys data services so archiving picks up new credentials.
	BackupSettingsChanged(ctx context.Context) error
	DataSummary(ctx context.Context, envID, serviceID string) (api.DataResponse, error)
	Extensions(ctx context.Context, envID, serviceID string) ([]api.Extension, error)
	EnableExtension(ctx context.Context, envID, serviceID, name, actor string) (api.Extension, error)
	RedisKeys(ctx context.Context, envID, serviceID, match, cursor string) (api.RedisKeysPage, error)
	RevealRedisKey(ctx context.Context, envID, serviceID, key string) (api.RedisRevealResponse, error)

	// --- control plane lifecycle
	Upgrade(ctx context.Context, req api.UpgradeRequest, actor string) (api.UpgradeStatus, error)
	UpgradeStatus(ctx context.Context) (api.UpgradeStatus, error)
}
