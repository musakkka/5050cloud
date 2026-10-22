// SPDX-License-Identifier: MIT

// Package api is the REST contract between 5050d and its clients (the web
// console and the 5050 CLI). It is frozen for v0.1: a change here is a change
// to docs/SPEC.md first. JSON field names are camelCase. Timestamps are Unix
// milliseconds (int64). IDs are prefixed strings (see docs/SPEC.md section 6).
//
// Every endpoint lives under /api/v1 (except /healthz and /readyz). Routes
// lists every endpoint with its method, role and body types; it is the
// machine-readable form of docs/SPEC.md section 11. Errors use the Error
// shape with an HTTP status of 4xx or 5xx.
package api

// ---------------------------------------------------------------- common

// Error is the body of every non-2xx response.
type Error struct {
	Error   string `json:"error"`             // one sentence for humans
	Code    string `json:"code"`              // one of the Code* constants
	Field   string `json:"field,omitempty"`   // the offending field for "validation"
	Details any    `json:"details,omitempty"` // optional structured data
}

// Error codes (docs/SPEC.md 11.7).
const (
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeValidation   = "validation"
	CodeRateLimited  = "rate_limited"
	CodeUnavailable  = "unavailable" // also read-only mode
	CodeInternal     = "internal"
)

// Page wraps list responses that can be long.
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"` // opaque cursor; empty means the end
}

// ConfirmRequest is the body of destructive requests: the name typed by the user.
type ConfirmRequest struct {
	Confirm string `json:"confirm"`
}

// TestResult answers the "Test" buttons (bucket, SMTP).
type TestResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"` // one sentence
}

// Status is the one status vocabulary used on service cards, panels, the
// activity feed and the CLI. Deployment rows use DeploymentStatus and
// StatusLabel.
type Status string

const (
	StatusOnline    Status = "online"
	StatusDeploying Status = "deploying"
	StatusBuilding  Status = "building"
	StatusCrashed   Status = "crashed"
	StatusUnhealthy Status = "unhealthy"
	StatusFailed    Status = "failed"
	StatusPending   Status = "pending" // waiting for capacity or a build host
	StatusSleeping  Status = "sleeping"
	StatusNoDeploy  Status = "no_deployment"
	StatusRemoved   Status = "removed"
	StatusCompleted Status = "completed" // cron or job finished with exit 0
	StatusHostLost  Status = "host_lost" // its volume was on a host that is gone (9.17.2)
)

// ---------------------------------------------------------------- system

// Readyz is GET /readyz. Checks are only filled for requests from localhost.
type Readyz struct {
	Version  string       `json:"version"`
	Ready    bool         `json:"ready"`
	ReadOnly bool         `json:"readOnly"`
	Checks   *ReadyChecks `json:"checks,omitempty"`
}

type ReadyChecks struct {
	DB         bool `json:"db"`
	Migrations bool `json:"migrations"`
	Hub        bool `json:"hub"`
	Agents     int  `json:"agents"` // connected agents
}

type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// ---------------------------------------------------------------- auth, users, tokens

// Role is an install role. Project grants use ProjectRole.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// ProjectRole is a per-project grant that replaces the install role for
// Members and Viewers on that project (Owner and Admin keep full access).
type ProjectRole string

const (
	ProjectEditor ProjectRole = "editor"
	ProjectViewer ProjectRole = "viewer"
	ProjectNone   ProjectRole = "none"
)

type SetupStatus struct {
	NeedsSetup bool   `json:"needsSetup"`
	Version    string `json:"version"`
}

type SetupRequest struct {
	Token    string `json:"token"` // one-time token printed by the installer
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"` // >= 10 chars
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse is Me after a full login, or NeedTOTP with a Ticket when the
// user has 2FA; the client then calls /auth/login/totp.
type LoginResponse struct {
	User     *User    `json:"user,omitempty"`
	Install  *Install `json:"install,omitempty"`
	NeedTOTP bool     `json:"needTotp,omitempty"`
	Ticket   string   `json:"ticket,omitempty"` // valid 5 minutes
}

type TOTPLoginRequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"` // 6 digits or a recovery code
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

type User struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Role          Role   `json:"role"`
	EmailVerified bool   `json:"emailVerified"`
	TOTPEnabled   bool   `json:"totpEnabled"`
	Disabled      bool   `json:"disabled"`
	GitHub        string `json:"github,omitempty"` // GitHub login if linked
	CreatedAt     int64  `json:"createdAt"`
}

type Me struct {
	User    User    `json:"user"`
	Install Install `json:"install"`
}

type UpdateMeRequest struct {
	Name            *string `json:"name,omitempty"`
	Password        *string `json:"password,omitempty"`
	CurrentPassword *string `json:"currentPassword,omitempty"` // required with Password
}

type TOTPSetupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauthUrl"` // rendered as a QR code
}

type TOTPEnableRequest struct {
	Code string `json:"code"`
}

type TOTPEnableResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"` // 10 single-use codes, shown once
}

type TOTPDisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// Notification events a user can subscribe to (docs/SPEC.md 9.15).
const (
	EventDeploymentFailed  = "deployment.failed"
	EventDeploymentCrashed = "deployment.crashed"
	EventBackupFailed      = "backup.failed"
	EventDiskHigh          = "disk.high"
	EventVolumeHigh        = "volume.high"
	EventMonitorTriggered  = "monitor.triggered"
	EventHostOffline       = "host.offline"
)

type NotificationPref struct {
	Event string `json:"event"`
	Email bool   `json:"email"`
	InApp bool   `json:"inApp"`
}

// Notification is one item in the bell.
type Notification struct {
	ID     string `json:"id"`
	TS     int64  `json:"ts"`
	Kind   string `json:"kind"` // one of the Event* constants
	Text   string `json:"text"`
	Ref    string `json:"ref,omitempty"`  // deployment, backup, host or monitor id
	Link   string `json:"link,omitempty"` // console path to open
	ReadAt int64  `json:"readAt,omitempty"`
}

type MarkNotificationsReadRequest struct {
	IDs []string `json:"ids,omitempty"`
	All bool     `json:"all,omitempty"`
}

type APIToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"` // first 8 chars, for recognition
	CreatedAt int64  `json:"createdAt"`
	LastUsed  int64  `json:"lastUsed,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	// Scope "account" acts as the user; "environment" can deploy, read logs
	// and set variables in EnvironmentID only.
	Scope         string `json:"scope"`
	EnvironmentID string `json:"environmentId,omitempty"`
}

type CreateTokenRequest struct {
	Name          string `json:"name"`
	Scope         string `json:"scope"` // "account" | "environment"
	EnvironmentID string `json:"environmentId,omitempty"`
	ExpiresInDays int    `json:"expiresInDays,omitempty"` // 0 = never
}

type CreateTokenResponse struct {
	Token  APIToken `json:"token"`
	Secret string   `json:"secret"` // "5050_" + 40 random chars; shown once
}

// CLI browser login (device-code style).
type CLIAuthStart struct {
	Code      string `json:"code"`      // short code shown in the terminal and the browser, e.g. "WXYZ-1234"
	VerifyURL string `json:"verifyUrl"` // https://<console>/cli-auth?code=WXYZ-1234
	PollToken string `json:"pollToken"`
	ExpiresAt int64  `json:"expiresAt"`
	Interval  int    `json:"interval"` // seconds between polls
}

type CLIAuthApprove struct {
	Code string `json:"code"`
}

type CLIAuthPoll struct {
	PollToken string `json:"pollToken"`
}

type CLIAuthPollResponse struct {
	Status string `json:"status"` // "pending" | "approved" | "denied" | "expired"
	Token  string `json:"token,omitempty"`
	User   *User  `json:"user,omitempty"`
}

type Invite struct {
	ID        string `json:"id"`
	Role      Role   `json:"role"`
	Email     string `json:"email,omitempty"`     // empty = link invite
	ProjectID string `json:"projectId,omitempty"` // empty = install-wide
	MaxUses   int    `json:"maxUses"`
	Uses      int    `json:"uses"`
	URL       string `json:"url,omitempty"` // accept link; only in the create response
	ExpiresAt int64  `json:"expiresAt"`
	CreatedAt int64  `json:"createdAt"`
}

type CreateInviteRequest struct {
	Role           Role   `json:"role"`
	Email          string `json:"email,omitempty"` // sends an email when SMTP is set
	ProjectID      string `json:"projectId,omitempty"`
	MaxUses        int    `json:"maxUses,omitempty"`        // default 1
	ExpiresInHours int    `json:"expiresInHours,omitempty"` // default 168 (7 days)
}

// InvitePreview is what the public accept page shows before sign-up.
type InvitePreview struct {
	Valid       bool   `json:"valid"`
	Reason      string `json:"reason,omitempty"` // "This invite has expired."
	Role        Role   `json:"role"`
	InstallName string `json:"installName"`
	ProjectName string `json:"projectName,omitempty"`
	Email       string `json:"email,omitempty"`
	ExpiresAt   int64  `json:"expiresAt"`
}

type AcceptInviteRequest struct {
	Token    string `json:"token"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type UpdateMemberRequest struct {
	Role     *Role `json:"role,omitempty"`
	Disabled *bool `json:"disabled,omitempty"`
}

type TransferOwnershipRequest struct {
	Password string `json:"password"`
	Code     string `json:"code,omitempty"` // TOTP code when the owner has 2FA
}

// ---------------------------------------------------------------- install, settings

type Install struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	LatestVersion string `json:"latestVersion,omitempty"`
	ConsoleURL    string `json:"consoleUrl"`
	BaseDomain    string `json:"baseDomain"` // e.g. "203-0-113-7.sslip.io" or "apps.example.com"
	PublicIP      string `json:"publicIp"`
	ACMEEmail     string `json:"acmeEmail,omitempty"`
	// TrustedProxies are CIDRs whose X-Forwarded-For is trusted (9.11).
	TrustedProxies  []string `json:"trustedProxies"`
	RegistryCapPct  int      `json:"registryCapPct"`  // default 30
	BuildTimeoutMin int      `json:"buildTimeoutMin"` // default 30
	RailwayAliases  bool     `json:"railwayAliases"`  // RAILWAY_* provided variables, default true
	RequireTOTP     bool     `json:"requireTotp"`
	GitHubApp       bool     `json:"githubApp"` // a GitHub App is connected
	BackupsReady    bool     `json:"backupsReady"`
	SMTPReady       bool     `json:"smtpReady"`
	// ReadOnly is set while the control host disk is full (6.6).
	ReadOnly bool `json:"readOnly"`
	// Restore is set while adopt mode is active after a state restore (9.17.1).
	Restore *RestoreStatus `json:"restore,omitempty"`
}

type RestoreStatus struct {
	SnapshotAt      int64 `json:"snapshotAt"`
	UnknownReplicas int   `json:"unknownReplicas"`
	UnknownHosts    int   `json:"unknownHosts"`
}

type UpdateInstallRequest struct {
	Name            *string   `json:"name,omitempty"`
	BaseDomain      *string   `json:"baseDomain,omitempty"`
	ACMEEmail       *string   `json:"acmeEmail,omitempty"`
	ConsoleDomain   *string   `json:"consoleDomain,omitempty"` // the old one keeps working for 24 hours
	TrustedProxies  *[]string `json:"trustedProxies,omitempty"`
	RegistryCapPct  *int      `json:"registryCapPct,omitempty"`
	BuildTimeoutMin *int      `json:"buildTimeoutMin,omitempty"`
	RailwayAliases  *bool     `json:"railwayAliases,omitempty"`
	RequireTOTP     *bool     `json:"requireTotp,omitempty"`
}

// BackupSettings configures the S3-compatible bucket used for database,
// volume, image and control-plane backups. Secret keys are write-only: GET
// returns them empty, and an empty value in PUT keeps the stored one.
type BackupSettings struct {
	Endpoint string `json:"endpoint"` // https://s3.us-east-1.amazonaws.com, http://minio:9000
	Bucket   string `json:"bucket"`
	Region   string `json:"region"`
	Prefix   string `json:"prefix"`
	// The write key (put and get only) goes into data containers and backup jobs.
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	// The optional retention key (delete rights) is used only by retention
	// jobs run from the control host.
	RetentionAccessKeyID     string `json:"retentionAccessKeyId,omitempty"`
	RetentionSecretAccessKey string `json:"retentionSecretAccessKey,omitempty"`
	PathStyle                bool   `json:"pathStyle"`
	StateBackups             bool   `json:"stateBackups"` // hourly control-plane snapshot to the bucket
	StoreImages              bool   `json:"storeImages"`  // registry uses the bucket (9.6)
	// Read-only status fields.
	LastStateBackup    int64  `json:"lastStateBackup,omitempty"`
	LitestreamLagMs    int64  `json:"litestreamLagMs,omitempty"`
	LitestreamPosition string `json:"litestreamPosition,omitempty"`
}

// SMTPSettings is Settings > General > Email. Password is write-only.
type SMTPSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLSMode  string `json:"tlsMode"` // "starttls" | "tls" | "none"
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
}

type SMTPTestRequest struct {
	To string `json:"to"`
}

// Install-wide alert events (docs/SPEC.md 9.15).
const (
	AlertHostOffline       = "host.offline"
	AlertDiskHigh          = "disk.high"
	AlertDiskCritical      = "disk.critical"
	AlertBackupStale       = "backup.stale"
	AlertUpgradeRolledBack = "upgrade.rolled_back"
	AlertCertFailing       = "certificate.failing"
	AlertVersionSkew       = "agent.version_skew"
	AlertHostDuplicate     = "host.duplicate"
	AlertReadOnly          = "controlplane.readonly"
)

// InstallNotifications is Settings > General > Alerts.
type InstallNotifications struct {
	Emails     []string `json:"emails"`
	WebhookURL string   `json:"webhookUrl,omitempty"`
	Events     []string `json:"events"` // Alert* constants
}

type RecoveryKeyRequest struct {
	Password string `json:"password"`
}

type RecoveryKeyResponse struct {
	RecoveryKey string `json:"recoveryKey"` // "5050R-..."
}

type UpgradeRequest struct {
	Version string `json:"version,omitempty"` // empty = latest stable
}

type UpgradeStatus struct {
	State       string `json:"state"` // "idle" | "downloading" | "switching" | "verifying" | "done" | "rolled_back" | "failed"
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion"`
	Message     string `json:"message,omitempty"`
	At          int64  `json:"at"`
	// Agents reports the per-host agent upgrade after the control plane switched.
	Agents []AgentUpgrade `json:"agents,omitempty"`
}

type AgentUpgrade struct {
	HostID  string `json:"hostId"`
	State   string `json:"state"` // "pending" | "upgrading" | "done" | "reverted"
	Version string `json:"version"`
	Message string `json:"message,omitempty"`
}

// ---------------------------------------------------------------- hosts

type Host struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"` // control, build, ingress, app, data
	// State: "online" | "offline" | "draining" | "maintenance" | "joining" | "lost" | "unknown" (not in the restored state).
	State       string  `json:"state"`
	Version     string  `json:"version"`
	Arch        string  `json:"arch"` // amd64 | arm64
	OS          string  `json:"os"`
	CPUs        int     `json:"cpus"`
	MemBytes    int64   `json:"memBytes"`
	ReserveMB   int     `json:"reserveMb"`
	ReservedMB  int     `json:"reservedMb"` // memory reservations placed here
	PublicIP    string  `json:"publicIp"`
	EgressIP    string  `json:"egressIp"`
	MeshIP      string  `json:"meshIp"`
	ACMEPrimary bool    `json:"acmePrimary"`
	Disks       []Disk  `json:"disks"`
	Replicas    int     `json:"replicas"`
	LastSeen    int64   `json:"lastSeen"`
	CPUUsed     float64 `json:"cpuUsed"` // vCPU in use (not percent)
	MemUsed     int64   `json:"memUsed"`
	// RebootRequired: a kernel update waits for a reboot (never automatic).
	RebootRequired bool `json:"rebootRequired"`
	// Conflicts lists machines that presented this host's identity (7.1).
	Conflicts []HostConflict `json:"conflicts,omitempty"`
	// Unreachable lists peer host ids with no WireGuard handshake for 3 minutes.
	Unreachable []string `json:"unreachable,omitempty"`
	// Warnings are one-sentence notes shown on the host row.
	Warnings []string `json:"warnings,omitempty"`
}

type Disk struct {
	Mount string `json:"mount"` // "/", "/data"
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

type HostConflict struct {
	MachineID   string `json:"machineId"`
	WGPublicKey string `json:"wgPublicKey"`
	IP          string `json:"ip"`
	SeenAt      int64  `json:"seenAt"`
}

type CreateJoinTokenRequest struct {
	Roles []string `json:"roles"` // default ["app"]
	Name  string   `json:"name,omitempty"`
}

type JoinToken struct {
	Command   string `json:"command"` // curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --join <url> --token <t> --roles app
	Token     string `json:"token"`   // single use
	ExpiresAt int64  `json:"expiresAt"`
}

type UpdateHostRequest struct {
	Name      *string   `json:"name,omitempty"`
	Roles     *[]string `json:"roles,omitempty"`
	Drain     *bool     `json:"drain,omitempty"` // move replicas off and stop scheduling here
	ReserveMB *int      `json:"reserveMb,omitempty"`
}

type MaintenanceRequest struct {
	On bool `json:"on"`
}

// UnknownReplica is a container an agent runs that the restored state does not know (9.17.1).
type UnknownReplica struct {
	ID          string            `json:"id"`
	HostID      string            `json:"hostId"`
	HostName    string            `json:"hostName"`
	ContainerID string            `json:"containerId"`
	Image       string            `json:"image"`
	ServiceID   string            `json:"serviceId,omitempty"` // from labels, when the service exists
	ServiceName string            `json:"serviceName,omitempty"`
	Labels      map[string]string `json:"labels"`
	ReportedAt  int64             `json:"reportedAt"`
}

type RegistryCredential struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Server     string   `json:"server"` // "ghcr.io"
	Username   string   `json:"username"`
	CreatedAt  int64    `json:"createdAt"`
	LastUsedAt int64    `json:"lastUsedAt,omitempty"`
	UsedBy     []string `json:"usedBy"` // service ids
}

type CreateRegistryCredentialRequest struct {
	Name     string `json:"name"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ---------------------------------------------------------------- projects, environments

type Project struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	PublicRead     bool          `json:"publicRead"`
	CreatedAt      int64         `json:"createdAt"`
	LastActivityAt int64         `json:"lastActivityAt"`
	Environments   []Environment `json:"environments"`
	// Summary per environment for dashboard cards.
	Summary []EnvSummary `json:"summary,omitempty"`
}

type EnvSummary struct {
	EnvironmentID string     `json:"environmentId"`
	Total         int        `json:"total"`
	Online        int        `json:"online"`
	Icons         []IconChip `json:"icons"`
	// Mini is the mini canvas: service positions and statuses.
	Mini []MiniNode `json:"mini,omitempty"`
}

type IconChip struct {
	Icon   string `json:"icon"`
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
}

type MiniNode struct {
	ServiceID string     `json:"serviceId"`
	Icon      string     `json:"icon"`
	Status    Status     `json:"status"`
	Position  [2]float64 `json:"position"`
}

type Environment struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	Name        string   `json:"name"`
	Egress      string   `json:"egress"` // "allowed" | "blocked" (quarantine)
	EgressAllow []string `json:"egressAllow,omitempty"`
	CreatedAt   int64    `json:"createdAt"`
}

type CreateProjectRequest struct {
	Name        string `json:"name,omitempty"` // empty = generated two-word name
	Description string `json:"description,omitempty"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	PublicRead  *bool   `json:"publicRead,omitempty"` // Admin only
}

type ProjectGrant struct {
	UserID string      `json:"userId"`
	Email  string      `json:"email"`
	Name   string      `json:"name"`
	Role   ProjectRole `json:"role"`
}

type CreateEnvironmentRequest struct {
	Name string `json:"name"`
	// Mode "empty" creates no services; "duplicate" copies every service,
	// non-sealed variable and setting from From as staged changes.
	Mode string `json:"mode"`
	From string `json:"from,omitempty"`
}

type UpdateEnvironmentRequest struct {
	Name        *string   `json:"name,omitempty"`
	Egress      *string   `json:"egress,omitempty"` // Admin only
	EgressAllow *[]string `json:"egressAllow,omitempty"`
}

type SyncRequest struct {
	From string `json:"from"` // source environment id
}

// ---------------------------------------------------------------- usage

type UsageResponse struct {
	From     int64      `json:"from"`
	Until    int64      `json:"until"`
	Services []UsageRow `json:"services"`
	Total    UsageRow   `json:"total"`
	// Railway is the same usage at Railway's list prices (9.23).
	Railway RailwayEstimate `json:"railway"`
}

type UsageRow struct {
	ServiceID      string  `json:"serviceId,omitempty"`
	Name           string  `json:"name"`
	VCPUHours      float64 `json:"vcpuHours"`
	MemGBHours     float64 `json:"memGbHours"`
	EgressGB       float64 `json:"egressGb"`
	VolumeGBMonths float64 `json:"volumeGbMonths"`
	BackupBytes    int64   `json:"backupBytes"`
}

type RailwayEstimate struct {
	USD        float64 `json:"usd"`
	PricesDate string  `json:"pricesDate"` // "2026-10-01"
	SourceURL  string  `json:"sourceUrl"`
}

// ---------------------------------------------------------------- service config

// ServiceKind is derived from config: a cron schedule makes a cron, no port
// and no domain makes a worker, otherwise web. It can be pinned explicitly.
type ServiceKind string

const (
	KindWeb      ServiceKind = "web"
	KindWorker   ServiceKind = "worker"
	KindCron     ServiceKind = "cron"
	KindDatabase ServiceKind = "database" // from a database template
)

// ServiceConfig is everything a deployment of a service is built and run
// from. It is versioned per environment; staged edits are JSON merge patches
// (RFC 7396) against it. Zero values mean "default" as documented per field.
type ServiceConfig struct {
	Name   string       `json:"name"` // 1-32 chars, [a-zA-Z0-9-_ ], unique per environment
	Icon   string       `json:"icon"` // icon key or URL path under /icons/
	Kind   ServiceKind  `json:"kind,omitempty"`
	Source SourceConfig `json:"source"`
	Build  BuildConfig  `json:"build"`
	Deploy DeployConfig `json:"deploy"`
	Net    NetConfig    `json:"networking"`
	// Template marks a service created from a template ("postgres", "redis", ...).
	Template string `json:"template,omitempty"`
	// TemplateVersion is the chosen major for database templates ("17").
	TemplateVersion string `json:"templateVersion,omitempty"`
}

type SourceConfig struct {
	Type string `json:"type"` // "github" | "image" | "upload" | "empty"
	// github
	Repo       string `json:"repo,omitempty"`       // "owner/name" (GitHub App) or https URL of a public repo
	Branch     string `json:"branch,omitempty"`     // default: repo default branch
	RootDir    string `json:"rootDir,omitempty"`    // default "/"
	AutoDeploy *bool  `json:"autoDeploy,omitempty"` // default true (webhook with an App, polling for public repos)
	WaitForCI  bool   `json:"waitForCi,omitempty"`  // not in v0.1; refused with a sentence
	// image
	Image              string `json:"image,omitempty"`              // "nginx:1.27", "ghcr.io/o/r:tag"
	RegistryCredential string `json:"registryCredential,omitempty"` // id of a stored credential
}

type BuildConfig struct {
	// Builder: "auto" (default: Dockerfile if present, else Railpack) |
	// "railpack" | "dockerfile" | "nixpacks" (built with Railpack, with a log line).
	Builder        string `json:"builder,omitempty"`
	DockerfilePath string `json:"dockerfilePath,omitempty"` // relative to rootDir; default "Dockerfile"
	BuildCommand   string `json:"buildCommand,omitempty"`
	// RailpackVersion is one of the pinned versions (current and the two before); empty = current.
	RailpackVersion string `json:"railpackVersion,omitempty"`
	// NixpacksCompat maps NIXPACKS_* variables and nixpacks.toml hints to
	// Railpack (9.20.2). Default true; set false to ignore them.
	NixpacksCompat *bool    `json:"nixpacksCompat,omitempty"`
	WatchPaths     []string `json:"watchPaths,omitempty"` // gitignore-style, relative to repo root
	NoCache        bool     `json:"noCache,omitempty"`
	TimeoutMin     int      `json:"timeoutMin,omitempty"` // default: the install's build timeout (30)
	// ConfigPath is the config-as-code file ("railway.json", "infra/5050.yaml");
	// empty = look for 5050.yaml, 5050.json, railway.json, railway.toml in rootDir.
	ConfigPath string `json:"configPath,omitempty"`
}

type DeployConfig struct {
	StartCommand          string `json:"startCommand,omitempty"`
	PreDeployCommand      string `json:"preDeployCommand,omitempty"`
	PreDeployTimeoutSec   int    `json:"preDeployTimeoutSec,omitempty"`   // default 300, 1..3600
	CronSchedule          string `json:"cronSchedule,omitempty"`          // 5-field crontab or @hourly/@daily/@weekly/@monthly
	CronTimezone          string `json:"cronTimezone,omitempty"`          // IANA, default "UTC"
	CronOverlap           string `json:"cronOverlap,omitempty"`           // "skip" (default) | "queue"
	CronTimeoutSec        int    `json:"cronTimeoutSec,omitempty"`        // 0 = none
	HealthcheckPath       string `json:"healthcheckPath,omitempty"`       // empty = TCP check on the target port
	HealthcheckTimeoutSec int    `json:"healthcheckTimeoutSec,omitempty"` // default 300
	RestartPolicy         string `json:"restartPolicy,omitempty"`         // "on_failure" (default) | "always" | "never"
	RestartMaxRetries     int    `json:"restartMaxRetries,omitempty"`     // default 10
	OverlapSec            int    `json:"overlapSec,omitempty"`            // keep old serving after cutover, default 0
	DrainingSec           int    `json:"drainingSec,omitempty"`           // SIGTERM to SIGKILL, default 30
	Replicas              int    `json:"replicas,omitempty"`              // default 1; must be 1 with a volume
	// MaxSurge is how many new replicas start per wave: a number ("1",
	// default) or a percentage of Replicas ("25%").
	MaxSurge string `json:"maxSurge,omitempty"`
	// Singleton replicas are never rescheduled automatically while their
	// host is offline. Default: on for workers, off for web services (7.1).
	Singleton *bool `json:"singleton,omitempty"`
	// Processes per replica. "auto" = one per vCPU of the replica's CPU limit
	// (or of the host when unlimited), 1..32. For Node start commands of the
	// form "node <file>" the agent wraps the process with cluster.cjs; for
	// every runtime it sets WEB_CONCURRENCY and FIFTY_PROCESSES. Default "1".
	Processes     string  `json:"processes,omitempty"`
	CPULimit      float64 `json:"cpuLimit,omitempty"`      // vCPU, 0 = unlimited
	MemoryLimitMB int     `json:"memoryLimitMb,omitempty"` // 0 = unlimited
	// MemoryReservationMB is what placement reserves per replica. Default:
	// the memory limit when set, else 256 (database templates 512).
	MemoryReservationMB int       `json:"memoryReservationMb,omitempty"`
	RunAsUser           string    `json:"runAsUser,omitempty"` // "0", "1000:1000"
	ShmSizeMB           int       `json:"shmSizeMb,omitempty"` // default 64
	Placement           Placement `json:"placement"`
}

type Placement struct {
	Roles []string `json:"roles,omitempty"` // default ["app"]; database templates use ["data"]
	Hosts []string `json:"hosts,omitempty"` // pin to host ids; overrides roles
}

type NetConfig struct {
	Port        int    `json:"port,omitempty"`        // target port; default from PORT detection, else 8080 injected as PORT
	PrivateName string `json:"privateName,omitempty"` // default slug of service name; <privateName>.internal
	// AllowMetadata lets the service reach 169.254.169.254 and the mesh
	// (Admin only, audited). Default false (8.4).
	AllowMetadata bool `json:"allowMetadata,omitempty"`
	// PublicAccess creates a TCP proxy for database templates (9.11).
	PublicAccess bool `json:"publicAccess,omitempty"`
}

// ---------------------------------------------------------------- services

type CreateServiceRequest struct {
	Name   string       `json:"name,omitempty"` // default: from repo/image/template
	Source SourceConfig `json:"source"`         // Type "empty" for an empty service
	// RootDir and StartCommand come from the scan dialog (monorepo packages).
	RootDir    string   `json:"rootDir,omitempty"`
	WatchPaths []string `json:"watchPaths,omitempty"`
	// Template creates the service(s) from a database template instead of Source.
	Template          string            `json:"template,omitempty"`
	TemplateVersion   string            `json:"templateVersion,omitempty"`
	TemplateVariables map[string]string `json:"templateVariables,omitempty"`
	Variables         map[string]string `json:"variables,omitempty"`
	Position          *[2]float64       `json:"position,omitempty"`
	// DeployNow commits the creation and starts the first deployment at once
	// ("Deploy Now"). False stages it ("Add Variables" then Deploy).
	// Templates are always created and deployed at once.
	DeployNow bool `json:"deployNow"`
}

type CreateServiceResponse struct {
	Services     []Service    `json:"services"`
	Deployments  []Deployment `json:"deployments,omitempty"`
	ChangesCount int          `json:"changesCount"`
}

// Service is a service as seen in one environment.
type Service struct {
	ID            string        `json:"id"`
	ProjectID     string        `json:"projectId"`
	EnvironmentID string        `json:"environmentId"`
	Config        ServiceConfig `json:"config"` // committed config
	// Staged is the config with staged changes applied, present only when
	// there are staged changes for this service.
	Staged       *ServiceConfig `json:"staged,omitempty"`
	StagedAction string         `json:"stagedAction,omitempty"` // "create" | "update" | "delete" | "rename"
	// Source maps a config field path ("deploy.startCommand") to where a
	// config file set it ("railway.json:12"). Those fields are read-only in the UI.
	Source     map[string]string `json:"source,omitempty"`
	ConfigFile *ConfigFileInfo   `json:"configFile,omitempty"`
	Kind       ServiceKind       `json:"kind"`
	Status     Status            `json:"status"`
	// StatusText is the card's sub-line: "Deployed 3m ago via GitHub",
	// "Build failed", "Pending: no capacity", "Next run in 4m".
	StatusText       string     `json:"statusText"`
	ActiveDeployment string     `json:"activeDeployment,omitempty"`
	Domains          []Domain   `json:"domains"`
	PrivateDomain    string     `json:"privateDomain"` // "<name>.internal"
	Volume           *Volume    `json:"volume,omitempty"`
	TCPProxies       []TCPProxy `json:"tcpProxies"`
	Replicas         []Replica  `json:"replicas"`
	ListeningPorts   []int      `json:"listeningPorts"` // detected inside the active replicas
	// OutboundIPs are the public IPs of the hosts this service runs on.
	OutboundIPs []string `json:"outboundIps"`
	// Processes is the resolved process count and mode text (9.7).
	Processes     int    `json:"processes"`
	ProcessesText string `json:"processesText,omitempty"`
	// MonitorFiring is true while any monitor of this service is in alarm.
	MonitorFiring bool  `json:"monitorFiring"`
	CreatedAt     int64 `json:"createdAt"`
}

type ConfigFileInfo struct {
	Path      string `json:"path"`   // "railway.json"
	Format    string `json:"format"` // "json" | "toml" | "yaml"
	CommitSHA string `json:"commitSha,omitempty"`
	URL       string `json:"url,omitempty"` // link to the file on GitHub
}

type Replica struct {
	ID           string `json:"id"`
	DeploymentID string `json:"deploymentId"`
	HostID       string `json:"hostId"`
	HostName     string `json:"hostName"`
	State        string `json:"state"` // "pending" | "starting" | "healthy" | "unhealthy" | "running" | "exited" | "crashed"
	Restarts     int    `json:"restarts"`
	StartedAt    int64  `json:"startedAt"`
	Singleton    bool   `json:"singleton"`
	// Note is a one-line warning: "Host offline: singleton not moved."
	Note string `json:"note,omitempty"`
}

// UpdateServiceConfigRequest stages a JSON merge patch (RFC 7396) of
// ServiceConfig. Replicas-only patches still stage, but committing them
// scales without a rebuild.
type UpdateServiceConfigRequest struct {
	Patch map[string]any `json:"patch"`
}

// UpdateServiceInfoRequest: a rename is staged, an icon change is immediate.
type UpdateServiceInfoRequest struct {
	Name *string `json:"name,omitempty"`
	Icon *string `json:"icon,omitempty"`
}

type MoveReplicaRequest struct {
	HostID string `json:"hostId,omitempty"` // empty = best eligible host
}

// ---------------------------------------------------------------- variables

type Variable struct {
	Name string `json:"name"`
	// Reference is the raw template when the value contains ${{...}} (it is
	// not secret and always returned, e.g. "${{Postgres.DATABASE_URL}}").
	Reference string `json:"reference,omitempty"`
	// Value is only present in reveal responses.
	Value  string `json:"value,omitempty"`
	Sealed bool   `json:"sealed"`
	Staged string `json:"staged,omitempty"` // "add" | "update" | "delete"
	// Unresolved lists references that do not resolve (unknown service or variable).
	Unresolved []string `json:"unresolved,omitempty"`
	// NeedsValue is set for variables staged by sync or import without a value ("Set a value").
	NeedsValue bool  `json:"needsValue,omitempty"`
	UpdatedAt  int64 `json:"updatedAt"`
}

type VariablesResponse struct {
	Variables []Variable `json:"variables"`
	// Provided lists the names 5050cloud injects (PORT, FIFTY_*, RAILWAY_* aliases).
	Provided []string `json:"provided"`
	// Suggested lists names found in .env.example and similar files in the repo.
	Suggested []string `json:"suggested,omitempty"`
}

type RevealRequest struct {
	Names []string `json:"names,omitempty"` // empty = all non-sealed
	// Resolved returns the final values with references substituted.
	Resolved bool `json:"resolved,omitempty"`
}

type RevealResponse struct {
	Values map[string]string `json:"values"`
}

// SetVariablesRequest stages variable edits. Values may be multi-line and
// may contain references. Names match ^[A-Za-z_][A-Za-z0-9_]{0,255}$.
type SetVariablesRequest struct {
	Set   map[string]string `json:"set,omitempty"`
	Unset []string          `json:"unset,omitempty"`
	Seal  []string          `json:"seal,omitempty"`
	// Raw replaces the whole set from .env or JSON text (Raw Editor).
	Raw *string `json:"raw,omitempty"`
}

type ShareVariableRequest struct {
	Name     string   `json:"name"`
	Services []string `json:"services"` // service ids that get NAME=${{shared.NAME}}
}

// ---------------------------------------------------------------- staged changes

type Change struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId,omitempty"` // empty for shared-variable changes
	Service   string `json:"service,omitempty"`   // name for display
	// Kind: "service.create" | "service.update" | "service.delete" |
	// "service.rename" | "variables" | "shared_variables" | "volume.create" |
	// "volume.update" | "volume.delete".
	Kind string `json:"kind"`
	// Diff lists the leaf paths that change, with old and new values.
	// Secret values are shown as "•••" (never the value).
	Diff []DiffEntry `json:"diff"`
	// Rewrites lists reference rewrites a rename causes in other variables.
	Rewrites []DiffEntry `json:"rewrites,omitempty"`
	// Tag is set by environment sync: "new" | "edited" | "removed".
	Tag string `json:"tag,omitempty"`
	// Notes are one-line notes, e.g. "Set a value for API_KEY".
	Notes     []string `json:"notes,omitempty"`
	Author    string   `json:"author"`
	CreatedAt int64    `json:"createdAt"`
}

type DiffEntry struct {
	Path string `json:"path"` // "deploy.startCommand", "variables.API_KEY"
	Old  any    `json:"old"`
	New  any    `json:"new"`
}

type ChangesResponse struct {
	Changes []Change `json:"changes"`
	// Candidates are sync results that are not staged until ticked (services
	// only in this environment, tag "removed"); stage one with DELETE on the service.
	Candidates []Change `json:"candidates,omitempty"`
}

type CommitRequest struct {
	Message string `json:"message,omitempty"`
	// Deploy redeploys every affected service in reference order. False
	// commits without deploying (Alt+Deploy).
	Deploy bool `json:"deploy"`
}

type CommitResponse struct {
	CommitID    string       `json:"commitId"`
	Deployments []Deployment `json:"deployments"`
}

// CommitDetail is GET /commits/{id}: the commit as the Details modal showed it.
type CommitDetail struct {
	ID            string   `json:"id"`
	EnvironmentID string   `json:"environmentId"`
	Message       string   `json:"message"`
	Author        string   `json:"author"`
	CreatedAt     int64    `json:"createdAt"`
	Changes       []Change `json:"changes"`     // secrets masked
	Deployments   []string `json:"deployments"` // deployment ids it started
}

// ---------------------------------------------------------------- deployments

type DeploymentStatus string

const (
	DepQueued    DeploymentStatus = "QUEUED"
	DepWaiting   DeploymentStatus = "WAITING" // reserved for Wait for CI (v0.2)
	DepBuilding  DeploymentStatus = "BUILDING"
	DepDeploying DeploymentStatus = "DEPLOYING"
	DepSuccess   DeploymentStatus = "SUCCESS" // shown as Active while it is the live one
	DepFailed    DeploymentStatus = "FAILED"
	DepCrashed   DeploymentStatus = "CRASHED"
	DepCompleted DeploymentStatus = "COMPLETED"
	DepSkipped   DeploymentStatus = "SKIPPED"
	DepRemoving  DeploymentStatus = "REMOVING"
	DepRemoved   DeploymentStatus = "REMOVED"
	DepCancelled DeploymentStatus = "CANCELLED" // shown as Removed with the reason
)

type Deployment struct {
	ID            string           `json:"id"`
	ServiceID     string           `json:"serviceId"`
	EnvironmentID string           `json:"environmentId"`
	Status        DeploymentStatus `json:"status"`
	// Label is StatusLabel(Status, Active): the word every client shows.
	Label  string `json:"label"`
	Active bool   `json:"active"`
	// Diagnosis is one sentence explaining a FAILED or CRASHED deployment,
	// e.g. "Your app listens on 3000 but the target port is 8080. ..."
	Diagnosis string `json:"diagnosis,omitempty"`
	// Reason explains CANCELLED and SKIPPED: "Aborted by Ada",
	// "Superseded by 3f2a1c9", "No changed files matched the watch paths".
	Reason      string       `json:"reason,omitempty"`
	Trigger     string       `json:"trigger"` // "github" | "ui" | "cli" | "rollback" | "redeploy" | "commit" | "template" | "cron" | "restore"
	Actor       string       `json:"actor,omitempty"`
	Commit      *Commit      `json:"commit,omitempty"`
	Image       string       `json:"image,omitempty"`  // registry ref
	Digest      string       `json:"digest,omitempty"` // sha256:...
	RollbackOf  string       `json:"rollbackOf,omitempty"`
	Message     string       `json:"message,omitempty"` // commit message from staged changes or CLI -m
	Steps       []DeployStep `json:"steps"`
	CanRollback bool         `json:"canRollback"` // image still retained
	Restarts    int          `json:"restarts"`
	CreatedAt   int64        `json:"createdAt"`
	StartedAt   int64        `json:"startedAt,omitempty"`
	FinishedAt  int64        `json:"finishedAt,omitempty"`
}

// DeployStep is one line in the expandable progress list:
// "queued", "fetch", "build", "pull image", "push", "pre_deploy", "start",
// "healthcheck", "cutover", "drain".
type DeployStep struct {
	Name       string `json:"name"`
	State      string `json:"state"` // "pending" | "running" | "ok" | "failed" | "skipped"
	StartedAt  int64  `json:"startedAt,omitempty"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type Commit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Message string `json:"message"`
	Branch  string `json:"branch,omitempty"`
	URL     string `json:"url,omitempty"`
	Time    int64  `json:"time"`
}

// DeploymentDetail adds what the deployment ran with.
type DeploymentDetail struct {
	Deployment    `tstype:",extends"`
	Config        ServiceConfig `json:"config"`
	VariableNames []string      `json:"variableNames"`
	// Source maps config fields that came from a config file to "file:line".
	Source     map[string]string `json:"source,omitempty"`
	ConfigFile *ConfigFileInfo   `json:"configFile,omitempty"`
	Notices    []string          `json:"notices,omitempty"`
	Replicas   []Replica         `json:"replicas"`
}

type DeployRequest struct {
	// Ref "latest" (default) builds the head of the branch; a commit SHA builds that commit.
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message,omitempty"`
}

type LogLine struct {
	ID           string         `json:"id"` // stable line id, used by ?around= (View in Context)
	TS           int64          `json:"ts"`
	Msg          string         `json:"msg"`
	Stream       string         `json:"stream"` // "stdout" | "stderr" | "build" | "http" | "system"
	Level        string         `json:"level"`  // "debug" | "info" | "warn" | "error"
	ServiceID    string         `json:"serviceId,omitempty"`
	Replica      string         `json:"replica,omitempty"`
	DeploymentID string         `json:"deploymentId,omitempty"`
	Attrs        map[string]any `json:"attrs,omitempty"` // parsed JSON fields; for http: method, path, httpStatus, durationMs, host, srcIp, requestId, userAgent, bytes
}

type LogsResponse struct {
	Lines []LogLine `json:"lines"`
	// Truncated is true when the limit cut the result.
	Truncated bool `json:"truncated"`
	// Notices explain gaps, e.g. "Host h2 is offline. Showing its last error
	// and deploy lines; full logs return when it is back."
	Notices []string `json:"notices,omitempty"`
}

// ---------------------------------------------------------------- metrics, monitors

type MetricsResponse struct {
	Step int64 `json:"step"` // ms between points
	// Series: "cpu" (vCPU), "mem" (bytes), "rx", "tx" (bytes/s), "disk"
	// (bytes), "req_2xx".."req_5xx" (per second), "error_rate" (0..1),
	// "p50", "p90", "p95", "p99" (ms). Points are [ts, value].
	Series      map[string][][2]float64 `json:"series"`
	MemLimit    int64                   `json:"memLimit,omitempty"`
	CPULimit    float64                 `json:"cpuLimit,omitempty"`
	Deployments []DeployMarker          `json:"deployments"`
	// PerReplica is set when ?view=replica.
	PerReplica map[string]map[string][][2]float64 `json:"perReplica,omitempty"`
}

type DeployMarker struct {
	TS           int64   `json:"ts"`
	DeploymentID string  `json:"deploymentId"`
	Label        string  `json:"label"`
	Commit       *Commit `json:"commit,omitempty"`
}

// DBMetricsResponse is the Postgres panel's Metrics tab (9.10).
type DBMetricsResponse struct {
	Step int64 `json:"step"`
	// Series: "connections_active", "connections_idle", ...,
	// "cache_hit_ratio", "wal_bytes".
	Series     map[string][][2]float64 `json:"series"`
	TopQueries []QueryStat             `json:"topQueries"`
}

type QueryStat struct {
	Query   string  `json:"query"`
	Calls   int64   `json:"calls"`
	TotalMs float64 `json:"totalMs"`
	MeanMs  float64 `json:"meanMs"`
}

// Monitor is a per-service threshold alert (9.21).
type Monitor struct {
	ID            string `json:"id"`
	ServiceID     string `json:"serviceId"`
	EnvironmentID string `json:"environmentId"`
	// Metric: "cpu" (% of limit or host) | "memory" (% of limit) | "disk"
	// (% of host filesystem, or GB with Unit "gb") | "http_5xx_rate" (% of requests).
	Metric     string          `json:"metric"`
	Threshold  float64         `json:"threshold"`
	Unit       string          `json:"unit,omitempty"` // "pct" (default) | "gb"
	ForMinutes int             `json:"forMinutes"`     // 1..60
	Channels   MonitorChannels `json:"channels"`
	State      string          `json:"state"` // "ok" | "firing"
	LastFired  int64           `json:"lastFired,omitempty"`
}

type MonitorChannels struct {
	Users    []string `json:"users,omitempty"`    // user ids that get email
	Webhooks []string `json:"webhooks,omitempty"` // project webhook ids
}

type CreateMonitorRequest struct {
	Metric     string          `json:"metric"`
	Threshold  float64         `json:"threshold"`
	Unit       string          `json:"unit,omitempty"`
	ForMinutes int             `json:"forMinutes"`
	Channels   MonitorChannels `json:"channels"`
}

type UpdateMonitorRequest struct {
	Threshold  *float64         `json:"threshold,omitempty"`
	Unit       *string          `json:"unit,omitempty"`
	ForMinutes *int             `json:"forMinutes,omitempty"`
	Channels   *MonitorChannels `json:"channels,omitempty"`
}

// ---------------------------------------------------------------- networking

type Domain struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId"`
	Domain    string `json:"domain"`
	Kind      string `json:"kind"` // "generated" | "custom"
	Port      int    `json:"port"`
	// DNS is the records the user can add for a custom domain (A to every
	// ingress host, or a CNAME to the generated domain).
	DNS    []DNSRecord `json:"dns,omitempty"`
	DNSOK  bool        `json:"dnsOk"`
	CertOK bool        `json:"certOk"`
	// CertExpiresAt is shown as "Certificate valid until <date>".
	CertExpiresAt int64  `json:"certExpiresAt,omitempty"`
	Status        string `json:"status"` // "pending_dns" | "issuing" | "active" | "error"
	Message       string `json:"message,omitempty"`
	// Cloudflare is set when the record points at Cloudflare's proxy.
	Cloudflare bool `json:"cloudflare,omitempty"`
}

type DNSRecord struct {
	Type  string `json:"type"` // "A" | "CNAME"
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CreateDomainRequest struct {
	Kind   string `json:"kind"`             // "generated" | "custom"
	Domain string `json:"domain,omitempty"` // custom only; "*" is refused (wildcards are v0.2)
	Port   int    `json:"port,omitempty"`   // default: service target port
}

type UpdateDomainRequest struct {
	Port int `json:"port"`
}

type TCPProxy struct {
	ID         string `json:"id"`
	TargetPort int    `json:"targetPort"`
	HostPort   int    `json:"hostPort"` // 20000-29999 on every ingress host
	Address    string `json:"address"`  // "<base domain or public ip>:<hostPort>"
}

type CreateTCPProxyRequest struct {
	TargetPort int `json:"targetPort"`
}

type Volume struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId"`
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	HostID    string `json:"hostId"`
	HostName  string `json:"hostName"`
	UsedBytes int64  `json:"usedBytes"`
	// AlertAtPct fires volume.high at this share of the host filesystem (default 80).
	AlertAtPct int    `json:"alertAtPct"`
	Staged     string `json:"staged,omitempty"` // "create" | "update" | "delete"
	// HostLost is set when the volume's host is gone (9.17.2).
	HostLost bool `json:"hostLost,omitempty"`
}

// VolumeDetail is the volume panel.
type VolumeDetail struct {
	Volume         `tstype:",extends"`
	HostFreeBytes  int64         `json:"hostFreeBytes"`
	HostTotalBytes int64         `json:"hostTotalBytes"`
	Backups        *BackupHealth `json:"backups,omitempty"`
}

type CreateVolumeRequest struct {
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath"`
}

type UpdateVolumeRequest struct {
	Name       *string `json:"name,omitempty"`
	MountPath  *string `json:"mountPath,omitempty"` // staged
	AlertAtPct *int    `json:"alertAtPct,omitempty"`
}

// ---------------------------------------------------------------- backups, data services

type Backup struct {
	ID         string `json:"id"`
	ServiceID  string `json:"serviceId"`
	Kind       string `json:"kind"`   // "postgres_wal" | "redis_rdb" | "mongo_dump" | "volume"
	Status     string `json:"status"` // "running" | "ok" | "failed"
	SizeBytes  int64  `json:"sizeBytes"`
	Trigger    string `json:"trigger"` // "schedule" | "manual" | "pre_upgrade" | "restore_test"
	Locked     bool   `json:"locked"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	VerifiedAt int64  `json:"verifiedAt,omitempty"` // last monthly restore test that used it
}

type BackupSchedule struct {
	Enabled       bool   `json:"enabled"`
	Cron          string `json:"cron"`          // default "0 3 * * *"
	Timezone      string `json:"timezone"`      // default "UTC"
	RetainDays    int    `json:"retainDays"`    // default 7
	ContinuousWAL bool   `json:"continuousWal"` // Postgres only, default true
}

type BackupHealth struct {
	Configured bool `json:"configured"` // a bucket is set
	// Supported is false for templates without backups (MySQL in v0.1).
	Supported     bool   `json:"supported"`
	LastOK        int64  `json:"lastOk,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	WALArchiveOK  *bool  `json:"walArchiveOk,omitempty"`
	WALBytes      int64  `json:"walBytes,omitempty"`
	RecoverableTo int64  `json:"recoverableTo,omitempty"` // latest point-in-time restorable
	Stale         bool   `json:"stale"`                   // last good base backup older than 26h, or archiving failing
	Message       string `json:"message,omitempty"`       // "WAL archiving failing for 12 min, pg_wal is 1.4 GB. ..."
}

type BackupsResponse struct {
	Backups  []Backup       `json:"backups"`
	Schedule BackupSchedule `json:"schedule"`
	Health   BackupHealth   `json:"health"`
}

type LockBackupRequest struct {
	Locked bool `json:"locked"`
}

type RestoreRequest struct {
	BackupID    string `json:"backupId,omitempty"`    // a specific backup
	PointInTime int64  `json:"pointInTime,omitempty"` // Postgres PITR, ms
	// Restores always go to a new service "<name>-restored-YYYYMMDD-HHMM"
	// in TargetEnvironment (default: same environment) on TargetHost
	// (default: placement). The source is never touched.
	TargetEnvironment string `json:"targetEnvironment,omitempty"`
	TargetHost        string `json:"targetHost,omitempty"`
}

// RestoreResponse answers restore and major-upgrade.
type RestoreResponse struct {
	ServiceID  string     `json:"serviceId"`
	Deployment Deployment `json:"deployment"`
	// RecoveredTo is the time the copy recovered to, when known.
	RecoveredTo int64 `json:"recoveredTo,omitempty"`
}

type PromoteRequest struct {
	// RestoredServiceID takes over the original's name (so ${{Name.VAR}}
	// references resolve to it); the original is renamed "<name>-old-<stamp>"
	// and stopped. Dependents redeploy.
	RestoredServiceID string `json:"restoredServiceId"`
}

type MajorUpgradeRequest struct {
	Version string `json:"version"` // one of the template's MajorVersions
}

// ConnectInfo is the database Connect modal. URLs are masked
// ("postgresql://postgres:•••@postgres.internal:5432/railway") until revealed
// through the variables reveal route.
type ConnectInfo struct {
	Client     string `json:"client"` // "psql" | "mysql" | "redis-cli" | "mongosh"
	PrivateURL string `json:"privateUrl"`
	PublicURL  string `json:"publicUrl,omitempty"` // when Public access is on
	// Variables names the variables holding the URLs (for reveal).
	PrivateVariable string `json:"privateVariable"`
	PublicVariable  string `json:"publicVariable,omitempty"`
	CLICommand      string `json:"cliCommand"` // "5050 connect Postgres"
}

type DataTable struct {
	Name   string `json:"name"`
	Schema string `json:"schema,omitempty"`
	Kind   string `json:"kind"` // "table" | "collection"
	Rows   int64  `json:"rows"` // estimate
	Bytes  int64  `json:"bytes"`
}

// DataResponse is the Data tab. There is no query box in v0.1.
type DataResponse struct {
	Tables      []DataTable `json:"tables"`
	SizeBytes   int64       `json:"sizeBytes"`
	Connections int         `json:"connections"`
	Version     string      `json:"version"` // server version, e.g. "17.6"
}

type Extension struct {
	Name      string `json:"name"`
	Version   string `json:"version"` // default version
	Installed bool   `json:"installed"`
	Comment   string `json:"comment,omitempty"`
}

type EnableExtensionRequest struct {
	Name string `json:"name"`
}

type RedisKey struct {
	Key       string `json:"key"`
	Type      string `json:"type"` // "string" | "hash" | "list" | "set" | "zset" | "stream"
	TTLMs     int64  `json:"ttlMs"`
	SizeBytes int64  `json:"sizeBytes"`
}

type RedisKeysPage struct {
	Keys   []RedisKey `json:"keys"`
	Cursor string     `json:"cursor,omitempty"` // empty = done
}

type RedisRevealRequest struct {
	Key string `json:"key"`
}

type RedisRevealResponse struct {
	Value     string `json:"value"` // first 1 KB
	Truncated bool   `json:"truncated"`
}

type CronRun struct {
	ID          string `json:"id"`
	ServiceID   string `json:"serviceId"`
	Status      string `json:"status"`  // "running" | "completed" | "failed" | "skipped" | "timeout" | "missed"
	Trigger     string `json:"trigger"` // "schedule" | "manual"
	ExitCode    *int   `json:"exitCode,omitempty"`
	Message     string `json:"message,omitempty"` // "Missed while the control plane was down"
	ScheduledAt int64  `json:"scheduledAt"`
	StartedAt   int64  `json:"startedAt,omitempty"`
	FinishedAt  int64  `json:"finishedAt,omitempty"`
}

// ---------------------------------------------------------------- templates, github

type TemplateSummary struct {
	ID          string          `json:"id"` // "postgres"
	Name        string          `json:"name"`
	Tagline     string          `json:"tagline"`
	Description string          `json:"description"`
	Docs        string          `json:"docs,omitempty"`
	Icon        string          `json:"icon"`
	Category    string          `json:"category"` // "database" | "storage" | "app"
	Services    []string        `json:"services"`
	Inputs      []TemplateInput `json:"inputs"`
}

type TemplateInput struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	// Generate is a template function evaluated for the default:
	// "secret(32)", "secret(16, \"abc\")", "randomInt(1000,9999)".
	Generate string `json:"generate,omitempty"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

// TemplateDetail is the template page (/new/template/:id). Inputs carry
// freshly generated defaults.
type TemplateDetail struct {
	TemplateSummary `tstype:",extends"`
	MajorVersions   []string              `json:"majorVersions,omitempty"`
	ServiceInfo     []TemplateServiceInfo `json:"serviceInfo"`
}

type TemplateServiceInfo struct {
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Volume    string   `json:"volume,omitempty"` // mount path
	Domain    bool     `json:"domain"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

type TemplateDeployRequest struct {
	Inputs       map[string]string `json:"inputs"`
	MajorVersion string            `json:"majorVersion,omitempty"`
}

type TemplateDeployResponse struct {
	Services    []Service    `json:"services"`
	Deployments []Deployment `json:"deployments"`
}

type GitHubStatus struct {
	AppConfigured bool     `json:"appConfigured"`
	AppSlug       string   `json:"appSlug,omitempty"`
	InstallURL    string   `json:"installUrl,omitempty"`
	ConfigureURL  string   `json:"configureUrl,omitempty"` // installation settings on GitHub
	Installations []string `json:"installations"`          // account logins
}

type GitHubAppRequest struct {
	Owner    string `json:"owner,omitempty"`    // organization; empty = the user's account
	ReturnTo string `json:"returnTo,omitempty"` // console path to return to
}

// GitHubAppResponse: the console POSTs Manifest as a form to FormURL (GitHub's manifest flow).
type GitHubAppResponse struct {
	FormURL  string         `json:"formUrl"`
	Manifest map[string]any `json:"manifest"`
}

type GitHubRepo struct {
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
	UpdatedAt     int64  `json:"updatedAt"`
}

type ScanRequest struct {
	Repo    string `json:"repo"` // "owner/name" or public https URL
	Branch  string `json:"branch,omitempty"`
	RootDir string `json:"rootDir,omitempty"`
}

type ScanResult struct {
	Builder       string   `json:"builder"`  // "dockerfile" | "railpack"
	Provider      string   `json:"provider"` // railpack provider: "node", "python", "go", ...
	Dockerfile    bool     `json:"dockerfile"`
	StartCommand  string   `json:"startCommand,omitempty"`
	SuggestedPort int      `json:"suggestedPort,omitempty"`
	EnvNames      []string `json:"envNames"` // from .env.example etc.
	// EnvExamples holds values from example files only (.env.example, .env.sample, .env.template).
	EnvExamples map[string]string `json:"envExamples,omitempty"`
	ConfigFiles []ScanConfigFile  `json:"configFiles,omitempty"`
	// Packages lists deployable packages for JS monorepos (one service each).
	Packages []ScanPackage `json:"packages,omitempty"`
}

type ScanConfigFile struct {
	Path string   `json:"path"` // "railway.json"
	Sets []string `json:"sets"` // field paths it sets
}

type ScanPackage struct {
	Name         string   `json:"name"`
	RootDir      string   `json:"rootDir"`
	StartCommand string   `json:"startCommand,omitempty"`
	WatchPaths   []string `json:"watchPaths,omitempty"`
}

// ---------------------------------------------------------------- imports, search

// Import is an import job (Railway project, Docker Compose file, Heroku variables).
type Import struct {
	ID            string       `json:"id"`
	Kind          string       `json:"kind"` // "railway" | "compose" | "heroku_env"
	ProjectID     string       `json:"projectId"`
	EnvironmentID string       `json:"environmentId,omitempty"`
	Status        string       `json:"status"` // "running" | "done" | "failed"
	Error         string       `json:"error,omitempty"`
	Report        ImportReport `json:"report"`
	CreatedAt     int64        `json:"createdAt"`
}

type ImportReport struct {
	Landed  []ImportItem `json:"landed"`
	Skipped []ImportItem `json:"skipped"`
}

type ImportItem struct {
	Kind   string `json:"kind"` // "service" | "variable" | "domain" | "volume" | "database" | "tcp_proxy" | "setting" | "key"
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"` // why it was skipped, or what it became
}

type RailwayImportRequest struct {
	Token       string `json:"token"` // Railway project token; used once, never stored
	ProjectName string `json:"projectName,omitempty"`
}

type SearchResult struct {
	Projects     []SearchItem `json:"projects"`
	Services     []SearchItem `json:"services"`
	Environments []SearchItem `json:"environments"`
	Settings     []SearchItem `json:"settings"`
	Deployments  []SearchItem `json:"deployments"` // matched by commit message
}

type SearchItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Path     string `json:"path"` // console route to open
}

// ---------------------------------------------------------------- canvas, activity, events

type Canvas struct {
	Project      Project               `json:"project"`
	Environment  Environment           `json:"environment"`
	Services     []Service             `json:"services"`
	Edges        []Edge                `json:"edges"`
	Groups       []Group               `json:"groups"`
	Positions    map[string][2]float64 `json:"positions"`
	ChangesCount int                   `json:"changesCount"`
	Presence     []Presence            `json:"presence"`
}

// Edge connects a service to a service whose variables it references.
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type Group struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Icon     string   `json:"icon,omitempty"`
	Services []string `json:"services"`
	Parent   string   `json:"parent,omitempty"`
}

type SavePositionsRequest struct {
	Positions map[string][2]float64 `json:"positions"` // at most 500 keys; empty = automatic layout
}

type PresenceRequest struct {
	Cursor *[2]float64 `json:"cursor,omitempty"`
}

type Presence struct {
	UserID   string      `json:"userId"`
	Name     string      `json:"name"`
	Cursor   *[2]float64 `json:"cursor,omitempty"`
	LastSeen int64       `json:"lastSeen"`
}

type Activity struct {
	ID        string `json:"id"`
	TS        int64  `json:"ts"`
	ServiceID string `json:"serviceId,omitempty"`
	// Kind: "commit" | "deployment" | "domain" | "backup" | "host" | "alert" | "member".
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Actor string `json:"actor,omitempty"`
	Ref   string `json:"ref,omitempty"` // deployment or commit id
	// Status is the deployment label for deployment rows.
	Status string `json:"status,omitempty"`
}

// Event is one Server-Sent Event on GET /api/v1/events?environment=<id>.
// The SSE "event:" field equals Type; "data:" is this JSON.
type Event struct {
	// Type: "deployment" | "service" | "changes" | "activity" | "host" |
	// "positions" | "presence" | "groups" | "notification" | "install".
	Type          string                `json:"type"`
	EnvironmentID string                `json:"environmentId,omitempty"`
	ServiceID     string                `json:"serviceId,omitempty"`
	Deployment    *Deployment           `json:"deployment,omitempty"`
	Service       *Service              `json:"service,omitempty"`
	ChangesCount  *int                  `json:"changesCount,omitempty"`
	Activity      *Activity             `json:"activity,omitempty"`
	Host          *Host                 `json:"host,omitempty"`
	Positions     map[string][2]float64 `json:"positions,omitempty"`
	Presence      []Presence            `json:"presence,omitempty"`
	Groups        []Group               `json:"groups,omitempty"`
	Notification  *Notification         `json:"notification,omitempty"`
	Install       *Install              `json:"install,omitempty"`
	// Origin is the session that caused the event, so it can skip its own echo.
	Origin string `json:"origin,omitempty"`
}

type AuditEntry struct {
	ID     string `json:"id"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`  // user email, "token:<name>", "github:<login>", "agent:<host>", "system"
	Action string `json:"action"` // "variables.reveal", "deployment.rollback", ...
	Target string `json:"target"`
	IP     string `json:"ip,omitempty"`
	Detail string `json:"detail,omitempty"` // never contains secret values
}

// Webhook events (docs/SPEC.md 9.15).
const (
	HookDeploymentSuccess = "deployment.success"
	HookDeploymentFailed  = "deployment.failed"
	HookDeploymentCrashed = "deployment.crashed"
	HookReplicaOOM        = "replica.oom_killed"
	HookServiceUnhealthy  = "service.unhealthy"
	HookVolumeHigh        = "volume.high"
	HookDiskHigh          = "disk.high"
	HookBackupFailed      = "backup.failed"
	HookMonitorTriggered  = "monitor.triggered"
	HookMonitorResolved   = "monitor.resolved"
	HookHostOffline       = "host.offline"
)

type Webhook struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	URL       string `json:"url"`
	// Events lists Hook* constants; Preset "critical" means deployment.crashed,
	// replica.oom_killed, volume.high, disk.high, backup.failed, monitor.triggered.
	Events  []string `json:"events"`
	Preset  string   `json:"preset,omitempty"`
	Secret  string   `json:"secret,omitempty"` // shown once at create; deliveries carry X-5050-Signature: sha256=<hmac>
	Enabled bool     `json:"enabled"`
	// Deliveries are the latest 20 deliveries (GET only).
	Deliveries []WebhookDelivery `json:"deliveries,omitempty"`
}

type WebhookDelivery struct {
	ID           string `json:"id"`
	Event        string `json:"event"`
	Status       string `json:"status"` // "ok" | "failed" | "pending"
	Attempts     int    `json:"attempts"`
	ResponseCode int    `json:"responseCode,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

type WebhookTestResponse struct {
	Status       string `json:"status"` // "ok" | "failed"
	ResponseCode int    `json:"responseCode,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ShellControl is a text frame on the shell WebSocket.
type ShellControl struct {
	Resize *[2]int `json:"resize,omitempty"` // [cols, rows], client to server
	Exit   *int    `json:"exit,omitempty"`   // exit code, server to client
}

type UploadResponse struct {
	DeploymentID string `json:"deploymentId"`
	LogsURL      string `json:"logsUrl"`
}
