# internal/cp/core (WP1)

The control plane's domain model and REST API (docs/SPEC.md 6.1, 8.3, 9.3, 9.5,
9.11, 9.14, 9.15, 9.17, 9.20 to 9.24 and 11). `cmd/5050d` wires it to the
orchestrator (`internal/cp/orch`) and serves it.

## Layout

| Path | What it does |
|---|---|
| `core.go`, `support.go` | `Core`, settings rows (install, SMTP, backups, alerts, GitHub App), events, presence, activity, audit, positions and groups, background loops (public repo polling every 2 minutes, update check) |
| `users.go`, `access.go` | Setup, sessions, login with TOTP, tokens, CLI auth, invites, members, project grants and the permission levels |
| `projects.go`, `services.go`, `variables.go` | Projects, environments, services and variables as staged changes |
| `commit.go` | The changes list (diffs, rename rewrites), discard, the all-or-nothing commit and its `CommitPlan`, the canvas |
| `network.go` | Domains (generated and custom, DNS checks through 1.1.1.1 and 8.8.8.8, Cloudflare detection), TCP proxies, volumes (staged), monitors |
| `cpconfig.go` | The `cpapi.Config` side the orchestrator calls: snapshots, `ResolveVariables`, `SecretValues`, `MergeConfigFile`, registry credentials, restore clones, promote name swap, `CommitVariables`, `Notify`, webhooks |
| `tmpl.go`, `templates/` | Template catalog from `templates.FS` and template deploys in dependency order |
| `githubops.go` | GitHub App manifest flow, repos, branches, scan, push webhook with watch paths |
| `misc.go` | Recovery key format, search, 5050.yaml export, environment sync, Docker Compose import |
| `hooks.go` | Project webhook CRUD and test deliveries |
| `api/` | The HTTP layer: every route of `pkg/api.Routes` with its role and audit action, SSE streams, shell and tunnel websockets, uploads, `/readyz` (and the `e2e_broken_readyz` tag), the embedded console |
| `fakeorch/` | In-memory orchestrator for `5050d serve --dev` and tests; never used by a production serve |

## Rules

- Every write goes through the store's single writer. Secret columns are
  sealed with the master key; staged variable values are sealed inside the
  change JSON.
- A route that touches an environment, project, domain, monitor, commit,
  webhook or deployment is checked against the caller's project level, not
  only their install role. Environment-scoped tokens only reach their
  environment.
- Mutating requests with a session cookie must come from the console's own
  origin (SameSite=Lax cookie plus an Origin check).
- Read-only mode (6.6) refuses every write with one sentence.

## Tests

`api/e2e_test.go` runs the scripted flow of the WP1 acceptance: setup, 2FA,
login, project, image service, references, rename rewrite, staged commit plan
received by fakeorch, audited reveal, tokens, CLI auth, invite link, forgot
password with a fake mailer, a template deploy, domains, canvas and export. It
also checks that every route has a handler, a role and (when mutating) an
audit action.

## Later work (not in this build)

- **Import from Railway** (9.22): `POST /imports/railway` answers 503 with a
  sentence. Compose and Heroku imports work.
- **Litestream** and uploading state snapshots to the bucket (9.17). Hourly
  encrypted snapshots are written to `/var/lib/5050/backups` (24 hourly, 7
  daily) and `backup-state` / `restore-state --from FILE` work; restoring from
  `s3://` or `litestream` is refused with instructions.
- **rotate-master-key**: the subcommand exists and refuses with a sentence.
- Rebuild of a dead control host (`--replace` one-time token printed by
  `restore-state`, 9.17.2).
- `requireTotp` is stored and shown but not yet enforced at login.
- Backup and replica routes (`/backups/{id}`, `/replicas/{rid}`) are checked
  against the install role only, not the project level.
- Unit tests of the core package itself beyond the scripted HTTP flow.
