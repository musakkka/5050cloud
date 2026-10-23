// SPDX-License-Identifier: MIT

package api

// Route is one REST endpoint of docs/SPEC.md section 11. The table below is
// the machine-readable form of that section: the core API router (WP1)
// mounts exactly these routes, its audit test walks them, the MSW mocks in
// web/src/api/mocks are generated from them, and a test in this package
// checks the table against the spec text.
type Route struct {
	Method string
	// Path is relative to /api/v1 (or to the root when NoPrefix is set) and
	// uses net/http ServeMux patterns: {eid}, {sid}, {id}.
	Path     string
	NoPrefix bool
	// Role is the minimum role: "-" public, "V" viewer, "M" member or
	// project editor, "A" admin, "O" owner.
	Role string
	// Req and Resp are zero values of the body types (nil = no body,
	// 204 for responses). Text marks a non-JSON body by content type.
	Req  any
	Resp any
	// Stream is "" for plain JSON, "sse" for Server-Sent Events of Resp,
	// "ws" for a WebSocket, "redirect" for a 302.
	Stream string
	// Query documents accepted query parameters.
	Query string
	// Audit is the audit action name ("<resource>.<verb>"). Every route
	// that is not a GET has one, as do reveals, shells and tunnels, unless
	// NoAudit is set (high-frequency heartbeats and polls).
	Audit   string
	NoAudit bool
}

// Text is a non-JSON body; the value is its content type.
type Text string

// Path prefixes used in the table.
const (
	pEnv = "/environments/{eid}"
	pSvc = "/environments/{eid}/services/{sid}"
)

// Roles.
const (
	Public = "-"
	Viewer = "V"
	Member = "M"
	Admin  = "A"
	Owner  = "O"
)

// Routes lists every endpoint under /api/v1 plus /healthz and /readyz.
var Routes = []Route{
	// 11.1 System, auth, users
	{Method: "GET", Path: "/healthz", NoPrefix: true, Role: Public, Resp: Text("text/plain")},
	{Method: "GET", Path: "/readyz", NoPrefix: true, Role: Public, Resp: Readyz{}},
	{Method: "GET", Path: "/version", Role: Public, Resp: VersionInfo{}},
	{Method: "GET", Path: "/setup", Role: Public, Resp: SetupStatus{}},
	{Method: "POST", Path: "/setup", Role: Public, Req: SetupRequest{}, Resp: Me{}, Audit: "setup.complete"},
	{Method: "POST", Path: "/auth/login", Role: Public, Req: LoginRequest{}, Resp: LoginResponse{}, Audit: "auth.login"},
	{Method: "POST", Path: "/auth/login/totp", Role: Public, Req: TOTPLoginRequest{}, Resp: Me{}, Audit: "auth.login_totp"},
	{Method: "POST", Path: "/auth/logout", Role: Viewer, Audit: "auth.logout"},
	{Method: "POST", Path: "/auth/forgot", Role: Public, Req: ForgotPasswordRequest{}, Audit: "auth.forgot"},
	{Method: "POST", Path: "/auth/reset", Role: Public, Req: ResetPasswordRequest{}, Resp: LoginResponse{}, Audit: "auth.reset"},
	{Method: "POST", Path: "/auth/verify-email", Role: Public, Req: VerifyEmailRequest{}, Audit: "auth.verify_email"},
	{Method: "GET", Path: "/me", Role: Viewer, Resp: Me{}},
	{Method: "PATCH", Path: "/me", Role: Viewer, Req: UpdateMeRequest{}, Resp: User{}, Audit: "me.update"},
	{Method: "POST", Path: "/me/2fa/setup", Role: Viewer, Resp: TOTPSetupResponse{}, Audit: "me.2fa_setup"},
	{Method: "POST", Path: "/me/2fa/enable", Role: Viewer, Req: TOTPEnableRequest{}, Resp: TOTPEnableResponse{}, Audit: "me.2fa_enable"},
	{Method: "POST", Path: "/me/2fa/disable", Role: Viewer, Req: TOTPDisableRequest{}, Audit: "me.2fa_disable"},
	{Method: "GET", Path: "/me/notification-prefs", Role: Viewer, Resp: []NotificationPref{}},
	{Method: "PUT", Path: "/me/notification-prefs", Role: Viewer, Req: []NotificationPref{}, Resp: []NotificationPref{}, Audit: "me.notification_prefs"},
	{Method: "GET", Path: "/me/notifications", Role: Viewer, Resp: Page[Notification]{}, Query: "before="},
	{Method: "POST", Path: "/me/notifications/read", Role: Viewer, Req: MarkNotificationsReadRequest{}, NoAudit: true},
	{Method: "POST", Path: "/cli/auth/start", Role: Public, Resp: CLIAuthStart{}, Audit: "cli_auth.start"},
	{Method: "POST", Path: "/cli/auth/approve", Role: Viewer, Req: CLIAuthApprove{}, Audit: "cli_auth.approve"},
	{Method: "POST", Path: "/cli/auth/poll", Role: Public, Req: CLIAuthPoll{}, Resp: CLIAuthPollResponse{}, NoAudit: true},
	{Method: "GET", Path: "/tokens", Role: Viewer, Resp: []APIToken{}},
	{Method: "POST", Path: "/tokens", Role: Viewer, Req: CreateTokenRequest{}, Resp: CreateTokenResponse{}, Audit: "token.create"},
	{Method: "DELETE", Path: "/tokens/{id}", Role: Viewer, Audit: "token.delete"},
	{Method: "GET", Path: "/members", Role: Viewer, Resp: []User{}},
	{Method: "GET", Path: "/invites", Role: Admin, Resp: []Invite{}},
	{Method: "POST", Path: "/invites", Role: Admin, Req: CreateInviteRequest{}, Resp: Invite{}, Audit: "invite.create"},
	{Method: "DELETE", Path: "/invites/{id}", Role: Admin, Audit: "invite.delete"},
	{Method: "GET", Path: "/invites/{id}", Role: Public, Resp: InvitePreview{}},
	{Method: "POST", Path: "/invites/accept", Role: Public, Req: AcceptInviteRequest{}, Resp: Me{}, Audit: "invite.accept"},
	{Method: "PATCH", Path: "/members/{id}", Role: Admin, Req: UpdateMemberRequest{}, Resp: User{}, Audit: "member.update"},
	{Method: "DELETE", Path: "/members/{id}", Role: Admin, Audit: "member.delete"},
	{Method: "POST", Path: "/members/{id}/transfer-ownership", Role: Owner, Req: TransferOwnershipRequest{}, Audit: "member.transfer_ownership"},
	{Method: "GET", Path: "/audit", Role: Admin, Resp: Page[AuditEntry]{}, Query: "before=&limit="},

	// 11.2 Install, settings, hosts, upgrades
	{Method: "GET", Path: "/install", Role: Viewer, Resp: Install{}},
	{Method: "PATCH", Path: "/install", Role: Admin, Req: UpdateInstallRequest{}, Resp: Install{}, Audit: "install.update"},
	{Method: "GET", Path: "/settings/backups", Role: Admin, Resp: BackupSettings{}},
	{Method: "PUT", Path: "/settings/backups", Role: Admin, Req: BackupSettings{}, Resp: BackupSettings{}, Audit: "settings.backups_update"},
	{Method: "POST", Path: "/settings/backups/test", Role: Admin, Req: BackupSettings{}, Resp: TestResult{}, Audit: "settings.backups_test"},
	{Method: "GET", Path: "/settings/smtp", Role: Admin, Resp: SMTPSettings{}},
	{Method: "PUT", Path: "/settings/smtp", Role: Admin, Req: SMTPSettings{}, Resp: SMTPSettings{}, Audit: "settings.smtp_update"},
	{Method: "POST", Path: "/settings/smtp/test", Role: Admin, Req: SMTPTestRequest{}, Resp: TestResult{}, Audit: "settings.smtp_test"},
	{Method: "GET", Path: "/settings/alerts", Role: Admin, Resp: InstallNotifications{}},
	{Method: "PUT", Path: "/settings/alerts", Role: Admin, Req: InstallNotifications{}, Resp: InstallNotifications{}, Audit: "settings.alerts_update"},
	{Method: "POST", Path: "/settings/recovery-key", Role: Owner, Req: RecoveryKeyRequest{}, Resp: RecoveryKeyResponse{}, Audit: "settings.recovery_key_reveal"},
	{Method: "GET", Path: "/hosts", Role: Viewer, Resp: []Host{}},
	{Method: "POST", Path: "/hosts/join-tokens", Role: Admin, Req: CreateJoinTokenRequest{}, Resp: JoinToken{}, Audit: "host.join_token"},
	{Method: "PATCH", Path: "/hosts/{id}", Role: Admin, Req: UpdateHostRequest{}, Resp: Host{}, Audit: "host.update"},
	{Method: "POST", Path: "/hosts/{id}/maintenance", Role: Admin, Req: MaintenanceRequest{}, Resp: Host{}, Audit: "host.maintenance"},
	{Method: "POST", Path: "/hosts/{id}/readopt", Role: Admin, Resp: Host{}, Audit: "host.readopt"},
	{Method: "DELETE", Path: "/hosts/{id}/conflicts", Role: Admin, Resp: Host{}, Audit: "host.clear_conflicts"},
	{Method: "DELETE", Path: "/hosts/{id}", Role: Admin, Query: "force=1", Audit: "host.delete"},
	{Method: "GET", Path: "/hosts/{id}/metrics", Role: Viewer, Resp: MetricsResponse{}, Query: "range=&from=&until="},
	{Method: "POST", Path: "/upgrade", Role: Admin, Req: UpgradeRequest{}, Resp: UpgradeStatus{}, Audit: "install.upgrade"},
	{Method: "GET", Path: "/upgrade", Role: Viewer, Resp: UpgradeStatus{}},
	{Method: "GET", Path: "/registry-credentials", Role: Member, Resp: []RegistryCredential{}},
	{Method: "POST", Path: "/registry-credentials", Role: Member, Req: CreateRegistryCredentialRequest{}, Resp: RegistryCredential{}, Audit: "registry_credential.create"},
	{Method: "DELETE", Path: "/registry-credentials/{id}", Role: Member, Audit: "registry_credential.delete"},
	{Method: "GET", Path: "/restore/unknown-replicas", Role: Admin, Resp: []UnknownReplica{}},
	{Method: "POST", Path: "/restore/unknown-replicas/{id}/adopt", Role: Admin, Audit: "restore.adopt"},
	{Method: "POST", Path: "/restore/unknown-replicas/{id}/remove", Role: Admin, Audit: "restore.remove"},
	{Method: "POST", Path: "/restore/finish", Role: Admin, Audit: "restore.finish"},

	// 11.3 Projects, environments, canvas, changes
	{Method: "GET", Path: "/projects", Role: Viewer, Resp: []Project{}, Query: "sort=activity|created|name"},
	{Method: "POST", Path: "/projects", Role: Member, Req: CreateProjectRequest{}, Resp: Project{}, Audit: "project.create"},
	{Method: "GET", Path: "/projects/{pid}", Role: Viewer, Resp: Project{}},
	{Method: "PATCH", Path: "/projects/{pid}", Role: Member, Req: UpdateProjectRequest{}, Resp: Project{}, Audit: "project.update"},
	{Method: "DELETE", Path: "/projects/{pid}", Role: Admin, Req: ConfirmRequest{}, Audit: "project.delete"},
	{Method: "GET", Path: "/projects/{pid}/members", Role: Viewer, Resp: []ProjectGrant{}},
	{Method: "PUT", Path: "/projects/{pid}/members", Role: Admin, Req: []ProjectGrant{}, Resp: []ProjectGrant{}, Audit: "project.grants_update"},
	{Method: "GET", Path: "/projects/{pid}/usage", Role: Viewer, Resp: UsageResponse{}, Query: "from=&until="},
	{Method: "POST", Path: "/projects/{pid}/environments", Role: Member, Req: CreateEnvironmentRequest{}, Resp: Environment{}, Audit: "environment.create"},
	{Method: "PATCH", Path: pEnv, Role: Member, Req: UpdateEnvironmentRequest{}, Resp: Environment{}, Audit: "environment.update"},
	{Method: "DELETE", Path: pEnv, Role: Admin, Req: ConfirmRequest{}, Audit: "environment.delete"},
	{Method: "POST", Path: pEnv + "/sync", Role: Member, Req: SyncRequest{}, Resp: ChangesResponse{}, Audit: "environment.sync"},
	{Method: "GET", Path: pEnv + "/canvas", Role: Viewer, Resp: Canvas{}},
	{Method: "PUT", Path: pEnv + "/positions", Role: Member, Req: SavePositionsRequest{}, NoAudit: true},
	{Method: "PUT", Path: pEnv + "/groups", Role: Member, Req: []Group{}, Resp: []Group{}, Audit: "canvas.groups_update"},
	{Method: "POST", Path: pEnv + "/presence", Role: Viewer, Req: PresenceRequest{}, NoAudit: true},
	{Method: "GET", Path: pEnv + "/changes", Role: Viewer, Resp: ChangesResponse{}},
	{Method: "DELETE", Path: pEnv + "/changes", Role: Member, Audit: "changes.discard_all"},
	{Method: "DELETE", Path: pEnv + "/changes/{cid}", Role: Member, Audit: "changes.discard"},
	{Method: "POST", Path: pEnv + "/changes/commit", Role: Member, Req: CommitRequest{}, Resp: CommitResponse{}, Audit: "changes.commit"},
	{Method: "GET", Path: pEnv + "/activity", Role: Viewer, Resp: Page[Activity]{}, Query: "service=&kind=&before="},
	{Method: "GET", Path: "/commits/{id}", Role: Viewer, Resp: CommitDetail{}},
	{Method: "GET", Path: "/events", Role: Viewer, Resp: Event{}, Stream: "sse", Query: "environment="},
	{Method: "GET", Path: pEnv + "/shared-variables", Role: Viewer, Resp: VariablesResponse{}},
	{Method: "PUT", Path: pEnv + "/shared-variables", Role: Member, Req: SetVariablesRequest{}, Resp: VariablesResponse{}, Audit: "shared_variables.stage"},
	{Method: "POST", Path: pEnv + "/shared-variables/reveal", Role: Member, Req: RevealRequest{}, Resp: RevealResponse{}, Audit: "shared_variables.reveal"},
	{Method: "POST", Path: pEnv + "/shared-variables/share", Role: Member, Req: ShareVariableRequest{}, Audit: "shared_variables.share"},
	{Method: "GET", Path: pEnv + "/export", Role: Viewer, Resp: Text("application/yaml")},
	{Method: "POST", Path: "/imports/railway", Role: Member, Req: RailwayImportRequest{}, Resp: Import{}, Audit: "import.railway"},
	{Method: "POST", Path: pEnv + "/imports/compose", Role: Member, Req: Text("application/yaml"), Resp: Import{}, Audit: "import.compose"},
	{Method: "POST", Path: pSvc + "/imports/heroku-env", Role: Member, Req: Text("text/plain"), Resp: Import{}, Audit: "import.heroku_env"},
	{Method: "GET", Path: "/imports/{id}", Role: Member, Resp: Import{}},
	{Method: "GET", Path: "/search", Role: Viewer, Resp: SearchResult{}, Query: "q="},

	// 11.4 Services
	{Method: "POST", Path: pEnv + "/services", Role: Member, Req: CreateServiceRequest{}, Resp: CreateServiceResponse{}, Audit: "service.create"},
	{Method: "GET", Path: pSvc, Role: Viewer, Resp: Service{}},
	{Method: "PATCH", Path: pSvc + "/config", Role: Member, Req: UpdateServiceConfigRequest{}, Resp: Service{}, Audit: "service.config_stage"},
	{Method: "PATCH", Path: pSvc + "/info", Role: Member, Req: UpdateServiceInfoRequest{}, Resp: Service{}, Audit: "service.info_update"},
	{Method: "POST", Path: pSvc + "/duplicate", Role: Member, Resp: Service{}, Audit: "service.duplicate"},
	{Method: "DELETE", Path: pSvc, Role: Admin, Req: ConfirmRequest{}, Audit: "service.delete"},
	{Method: "GET", Path: pSvc + "/variables", Role: Viewer, Resp: VariablesResponse{}},
	{Method: "PUT", Path: pSvc + "/variables", Role: Member, Req: SetVariablesRequest{}, Resp: VariablesResponse{}, Audit: "variables.stage"},
	{Method: "POST", Path: pSvc + "/variables/reveal", Role: Member, Req: RevealRequest{}, Resp: RevealResponse{}, Audit: "variables.reveal"},
	{Method: "GET", Path: pSvc + "/deployments", Role: Viewer, Resp: []Deployment{}, Query: "limit=&q="},
	{Method: "POST", Path: pSvc + "/deploy", Role: Member, Req: DeployRequest{}, Resp: Deployment{}, Audit: "deployment.deploy"},
	{Method: "POST", Path: pSvc + "/uploads", Role: Member, Req: Text("application/gzip"), Resp: UploadResponse{}, Query: "message=", Audit: "deployment.upload"},
	{Method: "GET", Path: "/deployments/{did}", Role: Viewer, Resp: DeploymentDetail{}},
	{Method: "POST", Path: "/deployments/{did}/redeploy", Role: Member, Resp: Deployment{}, Audit: "deployment.redeploy"},
	{Method: "POST", Path: "/deployments/{did}/rollback", Role: Member, Resp: Deployment{}, Audit: "deployment.rollback"},
	{Method: "POST", Path: "/deployments/{did}/restart", Role: Member, Resp: Deployment{}, Audit: "deployment.restart"},
	{Method: "POST", Path: "/deployments/{did}/abort", Role: Member, Resp: Deployment{}, Audit: "deployment.abort"},
	{Method: "POST", Path: "/deployments/{did}/remove", Role: Member, Resp: Deployment{}, Audit: "deployment.remove"},
	{Method: "POST", Path: "/replicas/{rid}/move", Role: Admin, Req: MoveReplicaRequest{}, Audit: "replica.move"},
	{Method: "GET", Path: "/deployments/{did}/build-logs", Role: Viewer, Resp: LogsResponse{}, Query: "follow=1 (SSE of []LogLine)"},
	{Method: "GET", Path: "/logs", Role: Viewer, Resp: LogsResponse{}, Query: "environment=&services=&deployment=&type=deploy,build,http&q=&since=&until=&limit=&around="},
	{Method: "GET", Path: "/logs/stream", Role: Viewer, Resp: []LogLine{}, Stream: "sse", Query: "same as /logs"},
	{Method: "GET", Path: pSvc + "/metrics", Role: Viewer, Resp: MetricsResponse{}, Query: "range=&from=&until=&view=sum|replica"},
	{Method: "GET", Path: pSvc + "/db-metrics", Role: Viewer, Resp: DBMetricsResponse{}, Query: "range="},
	{Method: "GET", Path: pSvc + "/monitors", Role: Viewer, Resp: []Monitor{}},
	{Method: "POST", Path: pSvc + "/monitors", Role: Member, Req: CreateMonitorRequest{}, Resp: Monitor{}, Audit: "monitor.create"},
	{Method: "PATCH", Path: "/monitors/{id}", Role: Member, Req: UpdateMonitorRequest{}, Resp: Monitor{}, Audit: "monitor.update"},
	{Method: "DELETE", Path: "/monitors/{id}", Role: Member, Audit: "monitor.delete"},
	{Method: "GET", Path: pSvc + "/domains", Role: Viewer, Resp: []Domain{}},
	{Method: "POST", Path: pSvc + "/domains", Role: Member, Req: CreateDomainRequest{}, Resp: Domain{}, Audit: "domain.create"},
	{Method: "PATCH", Path: "/domains/{id}", Role: Member, Req: UpdateDomainRequest{}, Resp: Domain{}, Audit: "domain.update"},
	{Method: "DELETE", Path: "/domains/{id}", Role: Member, Audit: "domain.delete"},
	{Method: "POST", Path: "/domains/{id}/check", Role: Member, Resp: Domain{}, Audit: "domain.check"},
	{Method: "POST", Path: "/domains/{id}/retry-certificate", Role: Member, Resp: Domain{}, Audit: "domain.retry_certificate"},
	{Method: "POST", Path: pSvc + "/tcp-proxies", Role: Member, Req: CreateTCPProxyRequest{}, Resp: TCPProxy{}, Audit: "tcp_proxy.create"},
	{Method: "DELETE", Path: pSvc + "/tcp-proxies/{id}", Role: Member, Audit: "tcp_proxy.delete"},
	{Method: "POST", Path: pSvc + "/volume", Role: Member, Req: CreateVolumeRequest{}, Resp: Volume{}, Audit: "volume.create"},
	{Method: "GET", Path: pSvc + "/volume", Role: Viewer, Resp: VolumeDetail{}},
	{Method: "PATCH", Path: pSvc + "/volume", Role: Member, Req: UpdateVolumeRequest{}, Resp: Volume{}, Audit: "volume.update"},
	{Method: "DELETE", Path: pSvc + "/volume", Role: Admin, Req: ConfirmRequest{}, Audit: "volume.delete"},
	{Method: "POST", Path: pSvc + "/volume/wipe", Role: Admin, Req: ConfirmRequest{}, Audit: "volume.wipe"},
	{Method: "GET", Path: pSvc + "/backups", Role: Viewer, Resp: BackupsResponse{}},
	{Method: "POST", Path: pSvc + "/backups", Role: Member, Resp: Backup{}, Audit: "backup.create"},
	{Method: "PUT", Path: pSvc + "/backups/schedule", Role: Member, Req: BackupSchedule{}, Resp: BackupSchedule{}, Audit: "backup.schedule_update"},
	{Method: "POST", Path: "/backups/{id}/lock", Role: Member, Req: LockBackupRequest{}, Resp: Backup{}, Audit: "backup.lock"},
	{Method: "DELETE", Path: "/backups/{id}", Role: Admin, Audit: "backup.delete"},
	{Method: "POST", Path: pSvc + "/restore", Role: Admin, Req: RestoreRequest{}, Resp: RestoreResponse{}, Audit: "backup.restore"},
	{Method: "POST", Path: pSvc + "/promote", Role: Admin, Req: PromoteRequest{}, Audit: "backup.promote"},
	{Method: "POST", Path: pSvc + "/major-upgrade", Role: Admin, Req: MajorUpgradeRequest{}, Resp: RestoreResponse{}, Audit: "database.major_upgrade"},
	{Method: "GET", Path: pSvc + "/connect-info", Role: Member, Resp: ConnectInfo{}},
	{Method: "POST", Path: pSvc + "/regenerate-credentials", Role: Admin, Resp: CommitResponse{}, Audit: "database.regenerate_credentials"},
	{Method: "GET", Path: pSvc + "/data", Role: Member, Resp: DataResponse{}},
	{Method: "GET", Path: pSvc + "/extensions", Role: Member, Resp: []Extension{}},
	{Method: "POST", Path: pSvc + "/extensions", Role: Admin, Req: EnableExtensionRequest{}, Resp: Extension{}, Audit: "database.extension_enable"},
	{Method: "GET", Path: pSvc + "/redis/keys", Role: Member, Resp: RedisKeysPage{}, Query: "match=&cursor="},
	{Method: "POST", Path: pSvc + "/redis/keys/reveal", Role: Member, Req: RedisRevealRequest{}, Resp: RedisRevealResponse{}, Audit: "database.redis_reveal"},
	{Method: "GET", Path: pSvc + "/cron-runs", Role: Viewer, Resp: []CronRun{}},
	{Method: "POST", Path: pSvc + "/cron-runs", Role: Member, Resp: CronRun{}, Audit: "cron.run_now"},
	{Method: "GET", Path: pSvc + "/shell", Role: Member, Stream: "ws", Query: "replica=&cols=&rows=&session=", Audit: "service.shell"},
	{Method: "GET", Path: pSvc + "/tunnel", Role: Member, Stream: "ws", Query: "port=", Audit: "service.tunnel"},

	// 11.5 Templates, GitHub, webhooks
	{Method: "GET", Path: "/templates", Role: Viewer, Resp: []TemplateSummary{}, Query: "q="},
	{Method: "GET", Path: "/templates/{id}", Role: Viewer, Resp: TemplateDetail{}},
	{Method: "POST", Path: pEnv + "/templates/{id}/deploy", Role: Member, Req: TemplateDeployRequest{}, Resp: TemplateDeployResponse{}, Audit: "template.deploy"},
	{Method: "GET", Path: "/icons/{name}", Role: Public, Resp: Text("image/svg+xml")},
	{Method: "GET", Path: "/github", Role: Viewer, Resp: GitHubStatus{}},
	{Method: "POST", Path: "/github/app", Role: Admin, Req: GitHubAppRequest{}, Resp: GitHubAppResponse{}, Audit: "github.app_create"},
	{Method: "GET", Path: "/github/app/callback", Role: Admin, Stream: "redirect", Query: "code=", Audit: "github.app_callback"},
	{Method: "GET", Path: "/github/repos", Role: Member, Resp: []GitHubRepo{}, Query: "q=&refresh=1"},
	{Method: "GET", Path: "/github/branches", Role: Member, Resp: []string{}, Query: "repo="},
	{Method: "POST", Path: "/github/scan", Role: Member, Req: ScanRequest{}, Resp: ScanResult{}, Audit: "github.scan"},
	{Method: "POST", Path: "/github/webhook", Role: Public, Req: Text("application/json"), Audit: "github.webhook"},
	{Method: "GET", Path: "/projects/{pid}/webhooks", Role: Admin, Resp: []Webhook{}},
	{Method: "POST", Path: "/projects/{pid}/webhooks", Role: Admin, Req: Webhook{}, Resp: Webhook{}, Audit: "webhook.create"},
	{Method: "PATCH", Path: "/webhooks/{id}", Role: Admin, Req: Webhook{}, Resp: Webhook{}, Audit: "webhook.update"},
	{Method: "DELETE", Path: "/webhooks/{id}", Role: Admin, Audit: "webhook.delete"},
	{Method: "POST", Path: "/webhooks/{id}/test", Role: Admin, Resp: WebhookTestResponse{}, Audit: "webhook.test"},
}

// AgentRoutes are served by the orchestrator under /api/agent/v1/ (11.6).
// Their bodies are pkg/proto types.
var AgentRoutes = []Route{
	{Method: "POST", Path: "/join", Role: Public, Audit: "agent.join"},
	{Method: "GET", Path: "/connect", Role: Public, Stream: "ws", Audit: "agent.connect"},
	{Method: "GET", Path: "/uploads/{id}", Role: Public, Resp: Text("application/gzip")},
}
