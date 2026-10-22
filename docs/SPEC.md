# 5050cloud v0.1 build spec

Status: revision 2, 2026-10-22, after the DX, scale and build critiques; M0 landed the same day
(results in 19.0). Audience: the builder and
verifier agents, and anyone reviewing the release. This file is the source of truth. If code and
this file disagree, either the code is wrong or this file gets a reviewed change first. Section 23
("Decisions") records which critique items were accepted, changed or rejected, and why.

**No builder starts until milestone M0 (section 19.0) is merged.** M0 lands the contract changes
this revision makes (section 5.4), a pinned `go.mod`, a working `Makefile`, a generated
`web/src/api/types.gen.ts` and new hashes in `docs/contracts.sha256`.

Inputs this spec is based on: `reference/5050cloud-architecture.md` (draft architecture),
`docs/research/railway-docs.md`, `docs/research/railway-ui.md`, `docs/research/competitors.md`,
`docs/research/reuse.md`, and the console in `reference/console-web`.

Writing rules for everything in the public repo: plain English, second person in docs, no em
dashes, none of the words in `e2e/banned-words.txt` (a wagering term our UI never uses), and no private hostnames, IPs, account names, company or app names
from our own fleet. `make check-public` enforces this (section 17).

---

## 0. How to use this spec

- **Builders** own one work package (section 19). Only edit files inside your package's
  directories. The contract files in section 5.3 are frozen: if you need a change, stop and report
  it to the lead planner instead of editing them.
- **Contract stubs.** Some packages import code another package is still writing. For those, the
  planner has committed stub files with the exact constructor signatures (each contains the marker
  `CONTRACT-STUB`). The owning package replaces its stub. A release with any `CONTRACT-STUB` left
  fails the code-quality verifier.
- **go.mod.** The planner commits, in M0, a `go.mod` that already requires every dependency named
  in 3.2 at an exact version. Builders should not need `go get`. If one truly needs a new module,
  they ask the integrator (19.2), who adds it on `main`; builders never edit `go.mod` or `go.sum`
  in their worktree. WP6 runs `go mod tidy` at integration.
- **Worktrees.** Each builder works in its own git worktree on its own branch
  (`wp/<id>`), rebases on `main` before each hand-off, and the integrator merges in the fixed
  order of 19.1. Shared files (`Makefile`, `.golangci.yml`, CI, `go.mod`) exist and work from M0,
  so `make lint` and `make test` are usable from hour 0.
- **Linux tests on a Mac.** `make test-linux` runs the `integration` build tag inside a
  privileged `golang:<pinned>` container on the local Docker (Docker Desktop is enough for
  Docker, iptables, netns and WireGuard tests). Builders use it before asking for a VM. The test
  VMs are driven through `tools/test-vm.sh`, which takes an exclusive `flock` per VM, so only one
  agent uses a VM at a time.
- **Versions.** Every dependency and image is pinned to an exact version that is at least two weeks
  old on the day you pin it. No `latest` tags anywhere, including Docker images.
- **Verifiers** (section 20) judge the integrated build against sections 1, 2, 16 and 18. The
  release ships only when every verifier passes.

---

## 1. Goals and the release bar

5050cloud gives you Railway's developer experience on Linux servers you own. v0.1 is the first
public release. It must be something a stranger can install, use for a real app, scale to a second
server and trust with a database.

### 1.1 Release bar (all must hold, each proven by a test in section 18)

| # | Requirement | Proven by |
|---|---|---|
| R1 | One command on a fresh Ubuntu 24.04 VM (amd64 or arm64) installs everything and prints a login URL. Ubuntu 22.04 and Debian 12 install with a "not tested in this release" warning and are not claimed | E1 |
| R2 | Under 5 minutes from running that command to a public HTTPS URL serving an app built from a public GitHub repo, with no DNS setup (sslip.io) | E1 + E3 timing |
| R3 | Automatic HTTPS with a valid certificate for generated sslip.io domains and for custom domains | E3, E4, E25 |
| R4 | Everything Railway's UI lets you do for the v0.1 scope (section 2) works in our UI with no file editing: create, configure, deploy, redeploy, roll back, delete, import | UI suite U1-U42 |
| R5 | Variables with `${{Service.VAR}}` and `${{shared.VAR}}` references, staged changes with a diff, environment sync, and rollback that restores the variables a deployment ran with | E5, E6, E8, U5-U7, U28 |
| R6 | A second host joins with one command, needs no inbound management port, and replicas spread across hosts with zero-downtime deploys for public traffic | E18, E7 |
| R7 | "Use all cores": a single-threaded Node service gets at least 1.6x the throughput on a 2 vCPU non-burstable VM with Processes set to auto | E20 |
| R8 | Health checks, restart on crash, failure diagnosis in one sentence, old version keeps serving when a deploy fails | E9, E10 |
| R9 | Postgres, Redis, MySQL and MongoDB in one click; scheduled backups to any S3-compatible bucket for Postgres, Redis, MongoDB and volumes; restore to a copy; promote | E5, E15 |
| R10 | Control-plane state backup, continuous replication to a bucket, restore on the same or a new VM; `5050 upgrade` with automatic rollback when the new version is unhealthy | E16, E17, E28, E29 |
| R11 | CLI `5050` (and the `railway` alias) with `login`, `init`, `link`, `up`, `logs`, `run`, `shell`, `ssh`, `variables`, `domain`, `connect`, `add` | E13 |
| R12 | Docs site with Quick start, Guides, Reference and CLI sections, and a landing page with an honest price comparison | docs verifier |
| R13 | Apps keep serving when the `5050d` process is down, including across an agent restart and a worker host reboot | E22, E22b, E22c |
| R14 | Re-running the installer is safe and does not restart healthy containers | E21 |
| R15 | A Railway project moves over with `5050 import railway` or **New > Import from Railway**: services, sources, settings, variables, domains, volumes and databases land as staged changes | E30, U35 |
| R16 | Repos that carry `railway.json`, `railway.toml`, `nixpacks.toml`, `5050.yaml` or `5050.json` build and run as their file says | E31 |
| R17 | The console feels fast: the perceived-latency budgets in 16.1 hold | P1-P10 |

### 1.2 Non-goals for v0.1 (see section 22 for the v0.2 list)

Kubernetes, autoscaling, a highly available control plane, PR environments, Git mode for config
(`5050 config plan|apply`), serverless sleeping, functions, buckets, a template marketplace,
SSO/OIDC, Login with GitHub, agent mTLS, a free SQL query box in the Data tab, MySQL backups,
wildcard custom domains, Windows hosts, and a hosted 5050cloud service. Every one of these that a
Railway user meets in the first week has a "Coming from Railway" row (14.3) and, where Railway shows
it in the UI, a disabled control with a "Not in v0.1" tooltip (13.6).

---

## 2. Railway DX parity map (v0.1 scope)

Names, labels and keyboard shortcuts match Railway's so a Railway user feels at home. The
**Railway label** column holds the exact string Railway shows (from `railway-ui.md`; rows whose
label there is tagged [O] are re-checked against the live Railway dashboard in M0, see 19.0, before
the label is frozen). The V1 verifier compares our UI strings to this column mechanically
(`web/e2e/labels.json` is generated from this table). Where we do better, the 5050cloud column says
how. "WP" is the owning work package for the server side; the WP8 sub-packages own every UI row.

| # | Railway feature | Railway label | 5050cloud v0.1 | Server WP |
|---|---|---|---|---|
| D1 | Dashboard, projects with mini canvas and online summary, sort, grid/list, **New** (`Cmd+/`) | `New`, `Recent Activity`, `Creation Date`, `A-Z` | Same, plus a Hosts card with disk bars | WP1 |
| D2 | New project menu | `GitHub Repository`, `Database`, `Template`, `Docker Image`, `Empty Project` | Same list and order, plus `Import from Railway` and `Docker Compose` | WP1 |
| D3 | GitHub repo flow | `Deploy Now`, `Add Variables`, `Configure GitHub App` | Same; works with a GitHub App (private repos, push deploys) or a pasted public repo URL; inline `Connect GitHub` when no app exists | WP1 |
| D4 | Canvas: cards with status line, reference edges, hover highlight, drag with grid snap, groups, undo/redo, hide connections | `Hide Connections`, `New Group` | Same (reused console), plus presence avatars and live position sync | WP1 |
| D5 | Create menu | `+ Create`, `GitHub Repo`, `Database`, `Template`, `Docker Image`, `Empty Service`, `Volume` | Same items; one shared scan dialog for every GitHub entry point | WP1 |
| D6 | Card right-click menu | `Open`, `Duplicate`, `Attach Volume`, `Update Info`, `Add to Group`, `Delete` | Same; Duplicate never copies sealed values | WP1 |
| D7 | Staged changes | `N changes`, `Details`, `Discard`, `Deploy`, commit message | Same; we also stage service deletes, renames and volumes. Domains apply immediately, as on Railway | WP1 |
| D8 | Environments | `+ New Environment`, `Duplicate Environment`, `Empty Environment`, `Sync` | Same, plus an egress "quarantine" toggle per environment | WP1 |
| D9 | Service panel tabs and shortcuts | `Deployments`, `Variables`, `Metrics`, `Settings`; `G D`, `G V`, `G M`, `G S` | Same, plus **Backups**, **Data**, **Cron Runs** where they apply; `Escape` closes | WP1, WP2a |
| D10 | Deployment status badges | `Initializing`, `Building`, `Deploying`, `Active`, `Success`, `Completed`, `Crashed`, `Failed`, `Removing`, `Removed`, `Skipped` | Same strings and colours everywhere (9.1.1) | WP2a |
| D11 | Deployment menu | `View logs`, `Restart`, `Redeploy`, `Rollback`, `Remove`, `Abort` | Same; Rollback restores image and variables and does not expire after 24 to 120 hours (keeps last 10 images) | WP2a |
| D12 | Deployment view | `Details`, `Build Logs`, `Deploy Logs`, `Network` (sub-view `HTTP`) | Same tabs; auto-opens Build Logs on a failed build and Deploy Logs on a crash | WP2a |
| D13 | Diagnose a failed deploy | AI `Diagnose` | One sentence above the logs, rule based (9.18), no AI needed | WP2a, WP3, WP4 |
| D14 | Variables | `New Variable`, `Raw Editor`, `Shared Variable`, `Seal`, `Add Reference` | Same; every reveal is audited; suggested variables from `.env*` files | WP1 |
| D15 | Provided variables | `PORT`, `RAILWAY_PUBLIC_DOMAIN`, `RAILWAY_PRIVATE_DOMAIN`, ... | `FIFTY_*` names plus every `RAILWAY_*` alias (on by default) so apps move with zero edits (9.3) | WP2a |
| D16 | Settings sections | `Source`, `Networking`, `Build`, `Deploy`, `Scale`, `Danger`; right-hand index; `/` filter | Same sections with Railway's field placement (13.3), plus **Processes** and **Placement** under Scale | WP1 |
| D17 | Config as code | `railway.json`, `railway.toml`; file icon next to fields set by the file | Same files and priority, plus `5050.yaml`/`5050.json` and `nixpacks.toml` hints (9.20) | WP1, WP4 |
| D18 | Domains | `Generate Domain`, `+ Custom Domain`, `TCP Proxy`, DNS records, `Retry certificate` | Same; generated domains use sslip.io or your wildcard domain; outbound IP shown free | WP1, WP2a, WP5 |
| D19 | Private networking | `<name>.railway.internal` | `<name>.internal`, with `<name>.railway.internal` as an alias | WP5 |
| D20 | Health and restarts | `Healthcheck Path`, `Healthcheck Timeout`, `Restart Policy` `On Failure`/`Always`/`Never` | Same, plus TCP check when no path is set, 2xx and 3xx accepted, continuous checks after deploy | WP3 |
| D21 | Pre-deploy | `Pre-deploy Command` | Same, with a timeout | WP2a |
| D22 | Cron | `Cron Schedule`, presets `Every hour`, `Every day`, `Every week`, `Every month` | Same, plus time zones, no 5 minute minimum, run history, **Run now**, timeout | WP2b, WP3 |
| D23 | Replicas | `Replicas` | Same; spread across hosts, several may share a host, max surge setting | WP2a |
| D24 | Resource limits | `Resource Limits` (under Deploy) | Same place; vCPU and memory per replica | WP2a |
| D25 | Volumes | `Volume`, `Mount Path`, `Wipe Volume`, `Backups` | Same rules and a volume panel; volumes use host disk (no Live Resize needed) | WP2b |
| D26 | Databases | `Postgres`, `MySQL`, `Redis`, `MongoDB`; `Connect`; `Data` | Same variable names; Postgres continuous WAL archiving with point-in-time restore to a copy | WP2b |
| D27 | Templates | `Template`, `Deploy`, `Deploy on Railway` button | 12 built-in templates (10.2), `/new/template/<id>` deep link, `Deploy on 5050cloud` button | WP2b |
| D28 | Logs | `Log Explorer`, filter `@level:error`, `"phrase"`, `-word`, `AND`/`OR`, `@httpStatus:>=400` | Same syntax (9.4), side panel with attributes, View in Context | WP3, WP5 |
| D29 | Metrics | `CPU`, `Memory`, `Disk`, `Network`, `Requests`, `Error Rate`, `Response Time`; `Sum`/`Replica` | Same, plus p90; 30 days kept; Postgres database metrics | WP2a |
| D30 | Monitors | threshold alerts | CPU, memory, disk, 5xx rate monitors by email or webhook (9.21) | WP2a |
| D31 | Activity and notifications | activity panel, bell | Same: commits with diff, deployments, filters; in-app bell plus email | WP1 |
| D32 | Project settings | `General`, `Usage`, `Environments`, `Shared Variables`, `Webhooks`, `Members`, `Tokens`, `Danger` | Same pages; Usage shows what the same use would cost on Railway | WP1 |
| D33 | Members | `Owner`, `Editor`, `Viewer` per project; invite links; `Transfer Ownership` | Install roles plus per-project grants (9.15), invite links, 2FA, no seat fees | WP1 |
| D34 | Tokens | account, workspace and project tokens | Account tokens and environment tokens | WP1 |
| D35 | Command palette | `Cmd+K`, sub-menus | Same commands and adaptive labels (13.4) | WP1 |
| D36 | CLI | `railway login/init/link/up/logs/run/shell/ssh/variables/domain/connect/add/redeploy/restart/down/status/open/...` | `5050` with the same verbs and flags, and a `railway` alias (section 12) | WP7 |
| D37 | Regions | Railway Metal regions | Your hosts, grouped by role (`app`, `data`, `build`, `ingress`) | WP2a |
| D38 | Migration | none | `Import from Railway`, `Docker Compose` import, `Import variables from Heroku` | WP1 |
| D39 | Account | email login, magic link, 2FA | Email and password, `Forgot password` by SMTP, TOTP 2FA, email on failed and crashed deploys | WP1 |
| D40 | Railway features not in v0.1 | PR environments, Wait for CI, approvals, image auto updates, serverless, buckets, functions, observability board, AI fix PR, edge rules/CDN/WAF | Shown disabled with a tooltip where Railway shows them; each has a "Coming from Railway" row (14.3) | n/a |

---

## 3. Architecture

```
          browser (console)        5050 CLI          GitHub (webhooks)
                 \                   |                  /
                  \        HTTPS 443 (Caddy on the control host)
                   v                 v                v
     +--------------------------------------------------------------+
     | 5050d: control plane, one Go binary, listens on 127.0.0.1:5050|
     |  core (WP1): API, auth, projects, services, variables,        |
     |              staged changes, GitHub, audit, events, settings  |
     |  orch (WP2): hosts, agent hub, scheduler, IPAM, deploy engine,|
     |              builds queue, routes, logs/metrics fan-out, cron,|
     |              backups, upgrades                                 |
     |  SQLite: state.db (WAL), metrics.db                            |
     +--------------------------^-----------------------------------+
           WebSocket (wss), agent dials out, rotating host token bound to host keys
            ______________________|_____________________
           |                                            |
  +--------+-----------------+               +----------+---------------+
  | host 1 (control host)    |   WireGuard   | host 2..N                |
  | 5050-agent (WP3)         |<------------->| 5050-agent               |
  |  build (WP4): buildx,    |  UDP 51820    |  runtime: Docker Engine  |
  |   Railpack, image import |               |  netw: networks, DNS,    |
  |  netw (WP5): Caddy, DNS, |               |   firewall, (Caddy if    |
  |   firewall, mesh, TCP    |               |   ingress role)          |
  |  registry:2 (images)     |               |                          |
  |  Docker Engine           |               |  Docker Engine           |
  +--------------------------+               +--------------------------+
```

### 3.1 Processes and ports per host

| Process | Where | Runs as | Listens on |
|---|---|---|---|
| `5050d` | control host | systemd unit `5050d.service`, root, `MemoryMax=768M`, `OOMScoreAdjust=-900` | `127.0.0.1:5050` only |
| `5050-agent` | every host | systemd unit `5050-agent.service`, root, `OOMScoreAdjust=-900` | nothing public; DNS on each environment gateway IP:53; upstream resolver on `127.0.0.1:5354` (ingress hosts) |
| `5050-net-restore` | every host | oneshot unit, `Before=docker.service` | nothing; restores firewall chains and `wg5050` from the cached desired state at boot (8.4) |
| Caddy (`caddy:2.10.x`, container, host network, started with `--resume`) | hosts with role `ingress` | managed by the agent, `OOMScoreAdjust=-800` | `:80`, `:443`; admin API on the unix socket `/var/lib/5050/caddy/admin.sock` (root, 0600), never on TCP |
| Registry (`registry:2.8.x` or `3.0.x`, container) | control host | managed by the agent | `127.0.0.1:5000` and `<mesh ip>:5000`, htpasswd auth with a push user (build hosts) and a pull-only user (all hosts) |
| BuildKit (buildx `docker-container` driver, `moby/buildkit:v0.2x.y`) | hosts with role `build` | managed by the agent, capped at 50% of host memory and `cpus-1` CPUs | nothing; RUN steps use a bridge network, never the host network (8.2) |
| WireGuard `wg5050` | every host when there are 2+ hosts | kernel module via netlink | UDP 51820 (only peers) |
| TCP proxy (in the agent) | ingress hosts | the agent | 20000-29999, only allocated ports |
| Litestream (pinned binary, child of `5050d`) | control host, when a bucket is set | `5050d` | nothing; streams `state.db` WAL to the bucket (9.17) |
| App containers | per placement, in the systemd slice `5050-apps.slice` (`--cgroup-parent`) | Docker | nothing on the host (reached through Caddy or the mesh) |

Inbound firewall needs: 80 and 443 on ingress hosts, UDP 51820 between hosts, allocated TCP proxy
ports. No management port is ever opened. The control plane never connects to an agent.

**What the control host carries, said plainly.** By default the control host runs the control
plane, the only registry, the console route and every role, so with one host it is a single point
of failure, and with several hosts it is still the place where the control plane, registry and (by
default) the console live. v0.1 limits the damage: joins may take the `ingress` role so apps keep a
public entry point without the control host (9.2.1); the `data` role defaults to a non-control host
once one exists; the registry can store images in the bucket (9.6); `state.db` streams to the bucket
(9.17); and the rebuild procedure in 9.17.2 replaces a dead control host. A highly available
control plane is v0.2. Honest limits (14.1) and the **Add a host** guide say this.

### 3.2 Why these pieces (decision record)

| Decision | Choice | Why |
|---|---|---|
| Language | Go 1.26+ for 5050d, agent and CLI (`go.mod` says 1.26.0 because the pinned `golang.org/x/crypto` needs it; CI and `make test-linux` use `golang:1.27.1-bookworm`) | Static binaries, first-class Docker, WireGuard, DNS libraries; one toolchain |
| State | SQLite in WAL mode via `modernc.org/sqlite` (pure Go, no CGO) | One file, easy backup, cross-compiles; plenty for 50 hosts |
| Agent transport | One WebSocket per agent (`github.com/coder/websocket`), JSON text frames, binary frames for streams | Works through every proxy and NAT, simple to debug, no codegen. gRPC was considered and rejected for v0.1 to avoid codegen and HTTP/2 proxy issues |
| Desired state | Control plane pushes a full `DesiredState` per host; the agent reconciles idempotently | Survives disconnects and restarts on either side with no replay logic |
| Container runtime | Docker Engine API through `github.com/docker/docker/client` (pinned at v28.5.2, the last release of that module; it negotiates the API version, so it works with Docker 24 to 29. Moving to its successor `github.com/moby/moby/client` is a v0.2 task) | Already on every VM, proven, `live-restore` keeps apps running during engine restarts |
| Builds | Railpack (pinned binary) and Dockerfiles through `docker buildx` with the docker-container driver and a registry cache | Exactly what our production stack runs today; Railway's builder, so repos build the same |
| Image imports | `github.com/google/go-containerregistry` copy by digest into the internal registry | No double storage in the engine, same bytes on every host |
| Ingress and TLS | Caddy, configured with its admin API `/load` over a unix socket; upstreams come from Caddy's `dynamic` `a` upstream source pointed at the agent's local resolver | Automatic HTTPS with Let's Encrypt and ZeroSSL fallback, websockets, HTTP/2. Any admin API change reloads the whole Caddy config, so replica changes go through DNS and never reload Caddy; only route set changes do, with `stream_close_delay` so open websockets survive (8.4) |
| YAML | `sigs.k8s.io/yaml` (YAML to JSON, then the `json` tags) for every YAML file we read or write | One set of field names (`startCommand`, `networking`) for JSON and YAML; `yaml.v3` would silently lower-case untagged fields |
| State replication | Litestream (pinned) to the configured bucket, plus hourly encrypted snapshots | RPO of seconds instead of an hour when the control host disk dies |
| Private DNS | `github.com/miekg/dns` server in the agent | Tiny, no extra container |
| Mesh | Kernel WireGuard via `golang.zx2c4.com/wireguard/wgctrl` and `github.com/vishvananda/netlink` | Encrypted, in every supported kernel |
| Firewall | `iptables-restore --noflush` on our own chains, hooked into `DOCKER-USER` and `nat POSTROUTING` | Atomic swaps, works next to Docker's rules on iptables-nft hosts |
| Backups | WAL-G for Postgres, restic for MySQL dumps, Redis and volumes, both pinned binaries mounted into containers | Proven in our production; no custom database images needed |
| Web | React 18 + Vite 6 + Tailwind 3, reused from `reference/console-web` | The shell, kit, canvas and read-only views exist (about 2,000 lines). Most settings, forms and the deployment view are new work (split into WP8a/b/c) |
| Docs and landing | Astro + Starlight, static | Railway-like sidebar, search (Pagefind), Markdown per page |

---

## 4. Install and hosts

### 4.1 The command

```
curl -fsSL https://5050cloud.com/install.sh | sudo sh
```

The same script lives at `install/install.sh` in the repo and at
`https://raw.githubusercontent.com/5050cloud/5050cloud/main/install/install.sh` (fallback when the
site is not deployed yet). It is POSIX `sh`, `set -eu`, and every step is idempotent.

Flags (each also readable from an environment variable `FIFTY_<FLAG>` in upper snake case):

| Flag | Default | Meaning |
|---|---|---|
| `--version vX.Y.Z` | latest stable | Release to install |
| `--archive PATH` | none | Install from a local release tarball. The script looks for `checksums.txt` and `checksums.txt.minisig` next to the tarball and verifies both exactly as for a download; without them it refuses unless `--insecure-skip-verify` is also given (only the e2e harness for builder runs uses that) |
| `--release-base URL` | GitHub Releases | Download releases from another base URL (an e2e HTTP server serving a snapshot). Must be given together with `--release-pubkey` |
| `--release-pubkey KEY` | the embedded key | minisign public key for `--release-base`; refused without it |
| `--acme-ca staging\|production\|URL` | `production` | ACME directory. Builder e2e runs use `staging` (Let's Encrypt staging) to stay off production rate limits; verifier runs use `production` |
| `--public-ip IP` | detected | Public IP used for sslip.io names and DNS instructions |
| `--domain example.com` | `<dashed-ip>.sslip.io` | Base domain for generated service domains (needs a wildcard A record) |
| `--console-domain console.example.com` | `console-<4 random [a-z0-9]>.<base domain>` | Where the console is served. The random label gives every install its own certificate name, so reinstalling on the same IP never hits Let's Encrypt's 5 duplicate certificates per week limit |
| `--acme-email you@example.com` | none | Email for Let's Encrypt notices |
| `--data-disk auto\|none\|/dev/X` | `auto` | Use a blank data disk for all state (4.3) |
| `--no-swap` | swap on when RAM < 8 GB and no swap exists | Skip the 2 GB swap file |
| `--no-auto-updates` | unattended security updates on | Skip OS security updates |
| `--firewall` | off | Add nftables rules that drop inbound except SSH, 80, 443, WireGuard from peers |
| `--join URL --token T` | none | Join an existing install as a worker host |
| `--roles app,ingress,data,build` | `app` for joins | Roles for a joining host. `ingress` on a join makes it a second public entry point (9.2.1) |
| `--ca-pin sha256:...` | none | Pin the console certificate when it uses Caddy's internal CA (the fallback in 4.2 step 9). Unrelated to agent mTLS, which v0.1 does not have |
| `--recovery-key-file PATH` | `/etc/5050/recovery-key.txt` when stdout is not a TTY | Where the recovery key is written (0600). On a TTY it is also printed; otherwise only the path is printed, so the key never lands in cloud-init or CI logs |
| `--restore s3://bucket/prefix --recovery-key K` | none | Rebuild a dead control host on this VM from the bucket (9.17.2); takes `--s3-endpoint`, `--s3-region`, `--s3-access-key`, `--s3-secret-key` |
| `--uninstall [--purge]` | | Remove units and binaries; `--purge` also deletes `/var/lib/5050`, containers, volumes |

### 4.2 Steps (control host)

1. **Preflight.** Root or sudo; OS is Ubuntu 24.04 (Ubuntu 22.04 and Debian 12 continue with a
   "not tested in this release" warning; others exit with a clear message unless `--force`); arch
   amd64 or arm64; systemd present; at least 1 GB RAM (warn under 2 GB) and 10 GB free; ports 80
   and 443 free (if not, print the process holding them and exit). Also:
   - **Time sync**: `chronyd` or `systemd-timesyncd` active and the clock within 2 s of an HTTPS
     `Date` header (clock skew breaks log merging, cron and certificate checks); otherwise enable
     timesyncd, or exit with a message.
   - **IPv4**: an IPv6-only host exits with "5050cloud v0.1 needs a public IPv4 address."
   - **Firewalls**: when `ufw` or `firewalld` is active, print the ports to open (80, 443, UDP
     51820, the TCP proxy range) and check that their rules sit next to our `iptables-nft` chains
     without flushing them; `--firewall` refuses to run alongside an active ufw or firewalld.
   - **Docker Hub quota**: print the remaining anonymous pull quota (a `HEAD` on
     `ratelimitpreview/test`) and warn under 20.
2. **Packages.** `ca-certificates curl git iptables` and Docker Engine from Docker's official apt
   repository (`docker-ce docker-ce-cli containerd.io docker-buildx-plugin`), skipped when Docker
   24 or newer is already installed.
3. **Data disk** (4.3), before Docker starts for the first time.
4. **Docker config** in `/etc/docker/daemon.json`, merged with any existing file: `live-restore:
   true`, `log-driver: json-file` with `max-size 20m, max-file 3`, `builder.gc` keep 20 GB,
   `data-root` on the data disk when there is one, `insecure-registries` for the mesh registry on
   joined hosts. Restart Docker only when the file changed.
5. **sysctl** (`/etc/sysctl.d/90-5050.conf`): `vm.overcommit_memory=1`, `vm.swappiness=10`,
   `net.core.somaxconn=4096`, `net.ipv4.ip_forward=1`, `fs.inotify.max_user_watches=524288`,
   `net.netfilter.nf_conntrack_max` = 32768 per GB of RAM (min 131072), `net.ipv4.ip_local_port_range
   = 10240 65000` (below our TCP proxy range would collide, so the proxy range 20000-29999 is added
   to `net.ipv4.ip_local_reserved_ports`). Units get `LimitNOFILE=1048576`. Swap file unless
   `--no-swap`. unattended-upgrades for security updates unless disabled; kernel updates that need
   a reboot are reported on the Hosts page, never rebooted automatically.
6. **Binaries.** Download `5050_<ver>_linux_<arch>.tar.gz` and `checksums.txt` plus
   `checksums.txt.minisig` from GitHub Releases, verify the signature with the public key embedded
   in the script and the SHA-256, unpack to `/opt/5050/releases/<ver>/`, point
   `/opt/5050/current` at it, link `/usr/local/bin/5050d`, `5050-agent`, `5050`.
7. **Initialise.** `5050d init --public-ip ... --base-domain ... --console-domain ...` creates
   `/etc/5050/config.json` (0600), the master key `/etc/5050/master.key` (0600, 32 random bytes),
   the host key `/etc/5050/host.key` (0600, encrypts the agent's local desired-state cache),
   `state.db`, the ballast file `/var/lib/5050/ballast` (1 GB, 6.4), a one-time setup token (24 h),
   and a local join token for this host's agent.
8. **Start.** Install and start `5050d.service` and `5050-agent.service`. The agent joins over
   `http://127.0.0.1:5050`, gets roles `control,build,ingress,app,data`, starts the registry,
   Caddy and the BuildKit builder.
9. **Wait and print.** Poll `https://<console domain>/readyz` with real certificate verification
   for up to 120 s. Then print, and exit 0:

```
5050cloud v0.1.0 is ready.

  Open this link to create your account (valid 24 hours, works once):
  https://console.203-0-113-7.sslip.io/setup?token=Q2X7-KL9P-...

  Recovery key (decrypts your backups; store it somewhere safe, it is shown only here
  and in Settings > Backups to the owner; also saved to /etc/5050/recovery-key.txt, 0600;
  move it off this machine and delete the file):
  5050R-AB12-CD34-...

  Next: deploy something from the console, or install the CLI on your laptop:
  curl -fsSL https://5050cloud.com/install.sh | sh -s -- --cli
```

If the certificate cannot be issued in 120 s (no inbound 80/443, for example), the installer
switches the console route to Caddy's internal CA, prints the URL with a warning that the browser
will show a certificate warning, and prints how to fix it (open ports 80 and 443, or use
`--domain`). It still exits 0. The e2e suite treats this fallback as a failure (with `--acme-ca
staging` a staging certificate is expected and the suite checks it against the staging root).

**sslip.io and rate limits.** M0 checked whether `sslip.io` is on the Public Suffix List:
**it is not** (checked 2026-10-22 against `https://publicsuffix.org/list/public_suffix_list.dat`;
neither `sslip.io` nor `nip.io` appears). So Let's Encrypt counts every `<dashed-ip>.sslip.io`
name against the single registered domain `sslip.io`, and its 50 certificates per week limit is
shared with every sslip.io user on the internet. The docs therefore present sslip.io as the
zero-setup trial path and recommend `--domain` for anything beyond a trial, and the installer
falls back to Caddy's internal CA as described in step 9 when issuance fails. Either way the random
console label and `--acme-ca staging` keep repeated e2e runs off the duplicate limit, and the e2e
harness preserves `/var/lib/5050/caddy` across VM resets when it can.

Public IP detection: first the address of the interface holding the default route if it is public;
otherwise `https://checkip.amazonaws.com` then `https://api.ipify.org`; otherwise ask for
`--public-ip`.

### 4.3 Data disk

`auto` picks a disk only when there is exactly one disk that has no partition table, no filesystem
signature (`wipefs -n` empty), is not mounted and is at least 20 GB. It is formatted ext4 with label
`5050data`, mounted at `/data` by UUID with `noatime,nofail`, and used for `/data/5050` (bind
mounted at `/var/lib/5050`) and `/data/docker` (Docker `data-root`). The systemd units carry
`RequiresMountsFor=/data` so Docker never starts on an empty directory. When Docker already had
containers before the install, the installer does not move its data and prints a warning.
Anything else (two blank disks, a disk with data) is left alone with a message naming
`--data-disk /dev/X`.

### 4.4 Joining a host

The console's **Hosts > Add host** (or `5050 host add`) creates a single-use join token valid for
1 hour and shows:

```
curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --join https://console.example.com --token J-... --roles app
```

The dialog has role checkboxes (`app` ticked; `ingress`, `data`, `build` optional) and explains
each one. Ticking `ingress` shows the two-ingress setup (9.2.1).

The joining host runs steps 1 to 6, then `5050-agent join`, which `POST`s `/api/agent/v1/join`
with the token and host facts, receives a host id, a long-lived host token (stored in
`/etc/5050/agent.json`, 0600, server keeps only a SHA-256), its mesh address and the registry
address, writes `insecure-registries`, and starts `5050-agent.service`. The join request carries
the host's `/etc/machine-id` and WireGuard public key; the host identity is bound to both (7.1).
Target: online in the console in under 2 minutes on a fresh VM. `/api/agent/v1/join` and the
pre-auth part of `/connect` are rate limited to 10 per minute per IP.

### 4.5 Uninstall

`--uninstall` stops and removes units, binaries and the Caddy, registry and BuildKit containers.
App containers and `/var/lib/5050` are kept unless `--purge`. A purge asks for confirmation unless
`--yes`.

---

## 5. Repository layout, ownership and licenses

### 5.1 Layout

```
5050cloud/
  cmd/5050d/              control plane main                          WP1
  cmd/5050-agent/         agent main                                  WP3
  cmd/5050/               CLI main (MIT)                              WP7
  pkg/api/                REST contract types (MIT)                   FROZEN (planner)
  pkg/proto/              agent protocol types (MIT)                  FROZEN (planner)
  pkg/manifest/           template manifest and 5050.yaml types (MIT) FROZEN (planner)
  pkg/version/            build metadata (MIT)                        FROZEN (planner)
  pkg/logfilter/          log filter parser and matcher (MIT)         WP3
  pkg/configfile/         railway.json/.toml, nixpacks.toml, 5050.yaml WP1
                          parser with source line map (MIT)
  pkg/compose/            docker-compose.yml to staged services (MIT) WP1
  internal/cp/cpapi/      core <-> orchestrator interfaces            FROZEN (planner)
  internal/cp/secretbox/  secret column encryption (done, tested)     FROZEN (planner)
  internal/cp/core/...    control plane core                          WP1
  internal/cp/orch/       hub, engine, sched, ipam, routes, logs,     WP2a
                          metrics, monitors, registry client
  internal/cp/orch/data/  backups, restore, promote, cron, templates  WP2b
                          rendering, registry retention, data inspect
                          (interface pinned in internal/cp/orch/README.md by M0)
  internal/agent/agentapi agent part interfaces                       FROZEN (planner)
  internal/agent/core/... agent runtime                               WP3
  internal/agent/assets/  cluster.cjs (MIT header)                    WP3
  internal/agent/build/   builder                                     WP4
  internal/agent/netw/    networking, DNS, firewall, mesh, ingress    WP5
  internal/upgrade/       release download, verify, switch, rollback  WP6
  internal/cli/           CLI implementation (MIT)                    WP7
  templates/              built-in templates + tools.json (MIT)       WP2b
  web/src/app, api, kit,  shell, router, api client, auth, dashboard, WP8a
   features/{auth,dashboard,new,templates,import,canvas,palette,activity}
  web/src/features/{service,variables,service-settings,deployment,  WP8b
   logs,metrics,shell,data,volume,backups-tab,cron}
  web/src/features/{project-settings,settings,hosts,members,        WP8c
   account,usage,backups-settings}
  web/e2e/                Playwright suite (each WP8x owns its specs) WP8a (harness)
  site/                   landing page and docs (Astro Starlight)     WP9
  install/                install.sh, systemd units                   WP6
  e2e/                    VM e2e suite, fake agents, load tools       WP6
  examples/               sample apps used by docs and e2e (MIT)      WP6
  .github/                CI, release, site deploy                    WP6
  Makefile .goreleaser.yaml .golangci.yml LICENSE* go.mod             WP6
  README.md CONTRIBUTING.md SECURITY.md CODE_OF_CONDUCT.md            WP9
  docs/SPEC.md                                                        planner
```

`docs/research/` is internal research. It moves to `reference/` (gitignored) before the first
public push; only this spec stays in `docs/`.

### 5.2 Licenses

- **AGPL-3.0-only**: `cmd/5050d`, `cmd/5050-agent`, `internal/` except `internal/cli`, `web/`.
- **MIT**: `cmd/5050`, `internal/cli`, `pkg/`, `templates/`, `examples/`, `install/`, `site/`,
  `internal/agent/assets/cluster.cjs`.
- Root `LICENSE` is AGPL-3.0 with a `LICENSING.md` that lists the MIT directories; each MIT
  directory carries a `LICENSE` file. Every source file starts with an SPDX line.
- The CLI must not import anything AGPL: `go list -deps ./cmd/5050` may not contain
  `/internal/cp`, `/internal/agent`, `/internal/upgrade` or `/web` (checked in CI).

### 5.3 Frozen contracts (committed by the planner)

| File | What it fixes | Consumers |
|---|---|---|
| `pkg/api/types.go` | Every REST request and response body | WP1, WP2, WP7, WP8 (via generated TypeScript) |
| `pkg/proto/proto.go` | Agent wire protocol | WP2, WP3, WP4, WP5, WP6 (fake agents) |
| `pkg/manifest/manifest.go` | Template manifest, `5050.yaml` | WP1, WP2, WP7 |
| `internal/cp/cpapi/cpapi.go` | `Config` (WP1 implements) and `Orchestrator` (WP2 implements) | WP1, WP2 |
| `internal/agent/agentapi/agentapi.go` | `Builder`, `Network`, `Ingress`, `Tools` | WP3, WP4, WP5 |
| `internal/cp/secretbox/secretbox.go` | Encryption of secret columns (implemented and tested) | WP1, WP2 |
| `internal/cp/orch/README.md` (interface section) | The `data.Service` interface WP2a calls and WP2b implements | WP2a, WP2b |
| `pkg/manifest/golden_test.go` | Round trip of `template.yaml` and `5050.yaml` through `sigs.k8s.io/yaml` with camelCase names | all |
| Stubs: `internal/agent/build/stub.go`, `internal/agent/netw/stub.go`, `internal/cp/orch/stub.go`, `internal/cp/orch/data/stub.go`, `internal/upgrade/stub.go`, `templates/embed.go`, `web/embed.go` | Constructor signatures | replaced by WP4, WP5, WP2a, WP2b, WP6, WP2b, WP8a |
| Section 11 of this file | REST paths, methods, roles | WP1, WP7, WP8 |
| Section 12 of this file | CLI commands and flags | WP7, WP9 |

`docs/contracts.sha256` records the SHA-256 of every frozen file above (stubs excluded, since
their owners replace them), plus the files M0 added to the same contracts: `pkg/api/routes.go`
(section 11 as a Go table: method, path, role, body types, audit action), `pkg/api/status.go`
(`StatusLabel`) and the golden fixtures in `pkg/manifest/testdata/`. `scripts/contracts.sh check`
(part of `make check-public`) verifies it; `make contracts` rewrites it and only the planner runs
it. A changed hash without a matching spec change fails V6. Tests guard the contracts too:
`pkg/api` checks its route table against the tables of section 11 and requires an audit action on
every mutating route; `pkg/api` and `pkg/proto` require a camelCase JSON tag on every field.

### 5.4 Contract changes landed by M0 (revision 2)

These change frozen files, so they land in M0, before any builder starts, with new hashes.

| File | Change |
|---|---|
| `pkg/manifest/manifest.go` | Package comment pins that every YAML file is decoded and encoded with `sigs.k8s.io/yaml` (landed: the `yaml:` tags were removed altogether, so nothing suggests yaml.v3 works on these types). Add `golden_test.go`. `Template` gains `Tagline`, `Docs` (URL), `Order` per service (`DependsOn []string`), `Inputs` with `Required`, `Generate` (`secret(32)`, `randomInt(1000,9999)`) and `Description`, and `MajorVersions []string` for database templates. New `RailwayConfig` type mirroring Railway's config-as-code schema (`build`, `deploy`, `environments.<name>`) |
| `pkg/proto/proto.go` | `DesiredState` gains `Epoch string` (cluster epoch, changes on state restore) and `Adopt bool` (9.17.1). `Hello` gains `Epoch`, `MachineID`, `WGPublicKey`, `Cached bool` (agent runs from its cache). `HostConfig.Registry` becomes `RegistryPull` (all hosts) plus `RegistryPush` (build hosts only). `ReplicaSpec` gains `MemoryReservation int64`, `CgroupParent string`, `MemorySwap int64` (= limit), `Labels map[string]string`. `Route` drops `Upstreams` for `UpstreamName string` (a name the agent's upstream resolver answers) and gains `Panic bool`. `Ingress` gains `Console` on any ingress host, `ACMEPrimary bool`, `Certificates []CertBundle` (encrypted PEM pushed to non-primary ingress hosts) and `StreamCloseDelaySec`. `DNSEntry` gains `Draining []string` (kept until drain ends, 7.3). `BuildResult` gains `ConfigFile *ConfigOverlay` (path, format, the parsed overlay and a field-to-line map) and `Notices []string`. `BuildJob` drops env from argv concerns by adding `EnvFile bool`. New messages `cert_issued` (primary ingress to control plane), `token_rotate` (control plane to agent, 9.15), `host_logs_tail` (agent to control plane, 9.4), `maintenance` (drain order, 9.16) |
| `pkg/api/types.go` | New types for every route added in section 11 (SMTP settings, notification preferences, 2FA, invite links, project grants, monitors, usage, imports, environment sync, template detail, volume panel, database connect info, extensions, Redis keys, registry credentials UI, activity filters, presence, install notifications). `DeploymentStatus` keeps its internal values and gains `StatusLabel(status, isLatestSuccess)` documented in 9.1.1. `ServiceConfig` gains `deploy.singleton`, `deploy.maxSurge`, `build.railpackVersion`, `build.nixpacksCompat`, `build.timeoutMin`, `build.configPath`, `networking.allowMetadata`, `deploy.memoryReservationMb`, `volume.alertAtPct`, and every field gains a `source` map in `Service` responses (`"railway.json:12"`) for the file icon |
| `internal/cp/cpapi/cpapi.go` | Fix the wiring comment: events go through `Config.Publish`, there is no `Bus` in `orch.Options`. No template accessor is added: both halves read `templates.FS` by `ServiceConfig.Template` (8.3). Add `Orchestrator.Sync`, `Orchestrator.Adopt`, `Orchestrator.Usage`, `Orchestrator.Monitors`, `Orchestrator.HostLost` |
| `internal/agent/agentapi/agentapi.go` | `Ingress.Apply` takes the route set and certificates; new `Ingress.SetUpstreams(name, ips)` served by the local resolver (no Caddy reload) |

M0 also commits: `go.mod` requiring every module in 3.2 at exact versions (Docker client,
`coder/websocket`, `miekg/dns`, `wgctrl`, `netlink`, `modernc.org/sqlite`, `go-containerregistry`,
`robfig/cron/v3`, `filippo.io/age`, `sigs.k8s.io/yaml`, `BurntSushi/toml`, `spf13/cobra` for the
CLI, `pquerna/otp` for TOTP); a working `Makefile` with `lint`, `test`, `test-linux`, `gen-ts`,
`check-public` and `dev` targets; `.golangci.yml`; a generated `web/src/api/types.gen.ts`; and
`web/src/api/mocks/` (MSW handlers generated from `pkg/api`) so WP8 has something to run against
before WP1's dev server exists.

**As landed (M0, 2026-10-22).** Everything in the table above, plus what the routes and flows of
sections 7 to 11 need, so builders do not have to ask for it:

- `pkg/proto`: `JoinRequest` carries `MachineID`, `WGPublicKey` and `ReplaceHostID` (9.17.2) and
  drops the mTLS CSR; `JoinResponse` carries `RegistryAddress` and `Epoch`; `HelloAck` carries
  `ProtocolVersion` and `Epoch`; close reasons `duplicate_host`, `unknown_host`, `revoked`;
  `Ingress` gains `ACMECA`, `ACMEPrimaryAddr`, `Upstreams []UpstreamSet` (the resolver answers),
  `TrustedProxies`, and `Route` gains `Port`; `Network` gains `AllowMetadata` (replica IPs allowed
  to reach the metadata address) and `MTU`; `ReplicaSpec` gains `Probe`; `HealthCheck` gains `Exec`;
  `StateApplied` gains `Epoch` and `Unknown` (adopt mode); `Metrics` gains `DB []DBSample` (Postgres
  probe and WAL guard) and `HTTPSample.P90Ms`; `LogRecord` gains `ID` and `LogsQuery` gains `Around`
  (View in Context); `BuildJob` gains `ConfigPath` and `RailpackVersion`; `ConfigOverlay` holds a
  typed `manifest.RailwayConfig` and optional `NixpacksHints`. `ReplicaSpec.Labels` already existed.
- `pkg/api`: the `Query`/`QueryResponse` types are removed (the Data tab has no query box in v0.1).
  `Deployment` gains `Label` (= `StatusLabel`) and `Reason`; `Routes` and `AgentRoutes` list every
  endpoint (`routes.go`). `ServiceConfig` also gains `templateVersion` and `networking.publicAccess`;
  `volume.alertAtPct` lives on `Volume` and `UpdateVolumeRequest`.
- `internal/cp/cpapi`: `Config` gains `MergeConfigFile` (9.20), `CommitVariables` (Regenerate
  credentials), `Monitors` and `SetMonitorState` (definitions live in core, evaluation in the
  orchestrator) and `Notify` (email, bell, webhooks, install alerts). `Orchestrator` gains `Ready`,
  `SetMaintenance`, `ClearHostConflicts`, `HostLost`, `Sync` (re-push desired state), the adopt
  family (`Adopt`, `AdoptStatus`, `UnknownReplicas`, `AdoptReplica`, `RemoveUnknownReplica`,
  `ReadoptHost`, `FinishRestore`), `MoveReplica`, `RetryCertificate`, `DBMetrics`, `Monitors`
  (live state by monitor id), `Usage`, `VolumeRuntime`, `WipeVolume`, `MajorUpgrade`,
  `RegenerateCredentials`, `TestBucket`, `Extensions`, `EnableExtension`, `RedisKeys` and
  `RevealRedisKey`; `Query` is removed; time ranges are `fromMs, untilMs`.
- `internal/agent/agentapi`: also `Network.BuildNetwork` (the `5050-build` bridge, 8.2),
  `Ingress.IssuedCerts` (feeds `cert_issued`) and `CertStatus` returning `CertState` with expiry.
- `deps.go` (build tag `deps`, never compiled into a binary) imports every pinned module so
  `go mod tidy` keeps them until real code does; remove a line once a package imports it.

---

## 6. Data model

IDs are `<prefix>_<26 char ULID, lowercase>`: `usr_`, `ses_`, `tok_`, `inv_`, `prj_`, `env_`,
`svc_`, `chg_`, `cmt_`, `dep_`, `rep_`, `hst_`, `dom_`, `tcp_`, `vol_`, `bak_`, `crn_`, `job_`,
`whk_`, `aud_`, `act_`, `grp_`, `jtk_`, `upl_`. Timestamps are Unix milliseconds. Booleans are
INTEGER 0/1. JSON columns are TEXT holding JSON. Secret columns end in `_enc` and hold
XChaCha20-Poly1305 ciphertext (nonce prefixed) under the master key with the row identity as
additional data.

`state.db` holds both sets below with separate migration tables (`core_migrations`,
`orch_migrations`), embedded SQL files numbered `0001_*.sql`, applied at start, forward only.
`metrics.db` is separate so `state.db` stays small for backups.

### 6.1 Core tables (WP1)

| Table | Columns (key ones) |
|---|---|
| `settings` | `key` PK, `value` JSON. Keys: `install` (name, base domain, console domain, ACME email, public IP, trusted proxies), `backups` (S3 config, write key and optional retention key in `secret_enc`), `github_app`, `railway_aliases`, `smtp` (host, port, TLS mode, user, `password_enc`, from), `install_notifications` (emails, webhook URL, events) |
| `users` | `id`, `email` UNIQUE, `name`, `password_hash` (argon2id), `role`, `email_verified_at`, `totp_secret_enc`, `totp_enabled`, `recovery_codes_hash` JSON, `created_at`, `disabled` |
| `password_resets` | `token_hash` PK, `user_id`, `expires_at` (1 h), `used_at` |
| `notification_prefs` | `user_id`, `event` (`deployment.failed`, `deployment.crashed`, `backup.failed`, `disk.high`, `monitor.triggered`), `email` 0/1, `in_app` 0/1; PK(user_id, event) |
| `notifications` | `id`, `user_id`, `ts`, `kind`, `text`, `ref`, `read_at` (the bell) |
| `project_grants` | `project_id`, `user_id`, `role` (`editor`, `viewer`, `none`); PK(project_id, user_id) |
| `sessions` | `id_hash` PK (SHA-256 of the cookie value), `user_id`, `created_at`, `expires_at`, `ip`, `user_agent` |
| `api_tokens` | `id`, `user_id`, `name`, `prefix`, `secret_hash`, `scope`, `environment_id`, `created_at`, `last_used`, `expires_at` |
| `invites` | `id`, `role`, `project_id` (NULL = install-wide), `email` (NULL = link invite), `token_hash`, `max_uses`, `uses`, `expires_at`, `accepted_by` JSON |
| `setup_tokens` | `token_hash`, `expires_at`, `used_at` |
| `cli_auth` | `code`, `poll_hash`, `user_id`, `status`, `expires_at` |
| `projects` | `id`, `name` (default: generated two-word name, for example `sturdy-otter`), `description`, `public_read` 0/1, `created_at`, `last_opened_at`, `last_activity_at` |
| `environments` | `id`, `project_id`, `name`, `egress`, `egress_allow` JSON, `created_at`; UNIQUE(project_id, name) |
| `services` | `id`, `project_id`, `created_at` (a service exists in every environment of its project; per-environment data lives below) |
| `service_instances` | `service_id`, `environment_id`, `rev`, `config` JSON (`api.ServiceConfig`), `deleted_at`; PK(service_id, environment_id); UNIQUE(environment_id, name) via generated column |
| `config_revisions` | `service_id`, `environment_id`, `rev`, `config` JSON, `commit_id`, `created_at` |
| `config_files` | `service_id`, `environment_id`, `commit_sha`, `path`, `format`, `overlay` JSON, `lines` JSON (field to source line), `seen_at`; the latest parsed config-as-code file (9.20) |
| `name_aliases` | `environment_id`, `old_name`, `service_id`, `expires_at` (private DNS alias after a rename, 24 h) |
| `presence` | in memory only: `environment_id`, `user_id`, `last_seen`, `cursor` (not a table; listed for completeness) |
| `variables` | `id`, `environment_id`, `service_id` (NULL = shared), `name`, `value_enc`, `is_reference`, `reference_raw` (the `${{...}}` text when the whole value is not secret), `sealed`, `updated_at`; UNIQUE(environment_id, service_id, name) |
| `changes` | `id`, `environment_id`, `service_id`, `kind`, `patch` JSON (merge patch or variable ops; secret values encrypted inside as `{"$enc": "..."}`), `author`, `created_at`, `updated_at`; one row per (environment, service, kind), later edits merge |
| `commits` | `id`, `environment_id`, `message`, `author`, `created_at`, `summary` JSON |
| `domains` | `id`, `environment_id`, `service_id`, `domain` UNIQUE, `kind`, `port`, `dns_ok`, `checked_at`, `created_at` |
| `tcp_proxies` | `id`, `environment_id`, `service_id`, `target_port`, `host_port` UNIQUE |
| `volumes` | `id`, `environment_id`, `service_id` UNIQUE, `name`, `mount_path`, `host_id`, `alert_at_pct` (advisory, default 80; v0.1 does not cap size, 9.12), `created_at`, `deleted_at` |
| `registry_credentials` | `id`, `name`, `server`, `username`, `password_enc`, `created_at`, `last_used_at` |
| `monitors` | `id`, `environment_id`, `service_id`, `metric` (`cpu`, `memory`, `disk`, `http_5xx_rate`), `threshold`, `for_minutes`, `channels` JSON (`email`, webhook ids), `state`, `last_fired_at` |
| `imports` | `id`, `kind` (`railway`, `compose`, `heroku_env`), `project_id`, `status`, `report` JSON (what landed, what was skipped and why), `created_at` |
| `github_installations` | `id`, `account`, `installation_id`, `created_at` |
| `webhooks` | `id`, `project_id`, `url`, `events` JSON (or the preset `critical`), `secret_enc`, `enabled` |
| `webhook_deliveries` | `id`, `webhook_id`, `event`, `status`, `attempts`, `next_at`, `response_code`, `created_at` |
| `audit_log` | `id`, `ts`, `actor`, `action`, `target`, `ip`, `detail` |
| `activity` | `id`, `environment_id`, `service_id`, `ts`, `kind` (`commit`, `deployment`, `domain`, `backup`, `host`, `alert`, `member`), `text`, `actor`, `ref` |
| `canvas_positions` | `environment_id`, `positions` JSON |
| `groups` | `id`, `environment_id`, `name`, `icon`, `parent`, `services` JSON |

### 6.2 Orchestrator tables (WP2, prefix `o_`)

| Table | Columns (key ones) |
|---|---|
| `o_hosts` | `id`, `name`, `roles` JSON, `state` (`online`, `offline`, `draining`, `maintenance`, `lost`), `token_hash`, `prev_token_hash` (valid 10 min after a rotation), `token_rotated_at`, `machine_id`, `facts` JSON, `mesh_index` UNIQUE (1..127), `mesh_pubkey`, `public_ip`, `last_seen`, `drain`, `reserve_mb`, `created_at` |
| `o_host_conflicts` | `host_id`, `machine_id`, `wg_pubkey`, `ip`, `seen_at` ("Duplicate host detected") |
| `o_unknown_replicas` | `host_id`, `container_id`, `labels` JSON, `reported_at` (adopt mode, 9.17.1) |
| `o_join_tokens` | `id`, `token_hash`, `roles` JSON, `name`, `expires_at`, `used_at` |
| `o_subnets` | `host_id`, `environment_id`, `slot` (1..254), PK(host_id, environment_id) |
| `o_ips` | `ip` PK, `host_id`, `environment_id`, `replica_id` |
| `o_deployments` | `id`, `environment_id`, `service_id`, `status`, `trigger`, `actor`, `message`, `config_rev`, `config` JSON (snapshot), `commit` JSON, `image`, `digest`, `rollback_of`, `diagnosis`, `steps` JSON, `env_enc` (variable snapshot), `is_node`, `start_cmd_detected`, `upload_path`, `created_at`, `started_at`, `finished_at`, `active` |
| `o_replicas` | `id`, `deployment_id`, `service_id`, `environment_id`, `host_id`, `ip`, `state`, `container_id`, `restarts`, `exit_code`, `diagnosis`, `listening_ports` JSON, `started_at`, `updated_at`, `desired` |
| `o_images` | `service_id`, `deployment_id`, `ref`, `digest`, `created_at`, `deleted_at` (registry retention) |
| `o_cron_runs` | `id`, `service_id`, `environment_id`, `deployment_id`, `status` (`running`, `completed`, `failed`, `skipped`, `missed`), `exit_code`, `scheduled_at`, `started_at`, `finished_at`, `job_id`; UNIQUE(service_id, environment_id, scheduled_at) so a slot never runs twice |
| `o_backups` | `id`, `service_id`, `environment_id`, `kind`, `status`, `size_bytes`, `trigger` (`schedule`, `manual`, `restore_test`), `locked`, `store_id` (WAL-G backup name or restic snapshot id), `error`, `started_at`, `finished_at`, `verified_at` (monthly restore test) |
| `o_backup_schedules` | `service_id`, `environment_id`, `schedule` JSON (`api.BackupSchedule`), `last_run_at` |
| `o_state` | `key` PK, `value` JSON: `generation` per host, `epoch`, `adopt_mode`, `upgrade`, `last_state_backup`, `litestream` (last replicated position) |
| `o_host_logs` | `host_id`, `ts`, `service_id`, `level`, `line` (error lines and deploy logs of the last 15 min per host, max 5 MB per host, 9.4) |
| `o_tcp_ports` | `host_port` PK, `tcp_proxy_id` |

### 6.3 metrics.db (WP2)

`series(id INTEGER PK, kind TEXT, key TEXT)` where kind is `replica`, `service`, `host`, `http` or
`pg`, and `points(series_id, ts, cpu, mem, rx, tx, disk, PRIMARY KEY(series_id, ts)) WITHOUT ROWID`.
Raw 30 s points are kept 48 hours, 5 minute rollups 14 days and 1 hour rollups 30 days (Railway
keeps 30 days; so do we). HTTP series hold request counts per status class and p50/p90/p95/p99.
`pg` series hold connections by state and the cache hit ratio. A janitor deletes expired rows every
10 minutes in batches of 5,000 in their own short transactions. Usage (13.1, Usage page) is
computed from the 1 hour rollups.

### 6.4 Files on disk

| Path | Content | Backed up |
|---|---|---|
| `/etc/5050/config.json`, `master.key`, `agent.json` | Config, secret key, agent credentials | master key = recovery key |
| `/var/lib/5050/state.db` | Control plane state | yes, hourly (9.17) |
| `/var/lib/5050/metrics.db` | Metrics | no |
| `/var/lib/5050/builds/<dep>.log.gz` | Build logs | no |
| `/var/lib/5050/uploads/<upl>.tar.gz` | `5050 up` sources, deleted 1 hour after the build | no |
| `/var/lib/5050/logs/<env>/<svc>/<YYYYMMDDHH>.jsonl(.gz)` | Runtime and HTTP logs on each agent | no |
| `/var/lib/5050/tools/<name>-<version>` | Verified helper binaries | no |
| `/var/lib/5050/caddy/` | Caddy data (certificates) | no (re-issued) |
| `/var/lib/5050/registry/` | Images | no (rebuildable), excluded from state backups |
| `/var/lib/5050/backups/state-<ts>.db.age` | Local state snapshots (24 hourly, 7 daily) | copied to S3 when configured |
| `/var/lib/5050/upgrade.json` | Upgrade status | n/a |
| `/var/lib/5050/agent/desired.json.enc` | Last applied desired state, encrypted with `/etc/5050/host.key` (0600; it holds resolved env and the pull credential) | no (rebuilt by the control plane) |
| `/var/lib/5050/ballast` | 1 GB file the agent deletes when the disk reaches 95% (6.6) | no |
| `/etc/5050/recovery-key.txt` | Recovery key when the installer ran without a TTY (0600); the docs say to move it off the host | no |
| `s3://<bucket>/<prefix>/_state/litestream/` | Continuous `state.db` replica (9.17) | it is the backup |

### 6.5 SQLite operating rules (WP1 owns `internal/cp/core/store`, WP2a follows them)

- Pragmas at open: `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`,
  `foreign_keys=ON`, `wal_autocheckpoint=1000`.
- One writer: every write goes through a single writer goroutine per database (a channel of
  closures); reads use a pool of up to 8 connections.
- Read transactions are short. SSE handlers, log queries and metrics reads never hold a
  transaction open while they wait or stream; they read, release, then send.
- A checkpointer runs `PRAGMA wal_checkpoint(TRUNCATE)` every 5 minutes and when the `-wal` file
  passes 64 MB; Litestream coordinates through its own checkpoint lock as documented upstream.
- On `SQLITE_FULL` the control plane switches to read-only (6.6) instead of exiting.
- At start, deployments in `QUEUED` resume (they re-enter the queue in creation order) as well as
  `DEPLOYING` ones (9.1).

### 6.6 Disk full on any host

`state.db`, build logs, uploads, the registry and agent logs share one filesystem on the control
host. The rules:

- **Ballast**: the agent deletes `/var/lib/5050/ballast` at 95% use and raises `disk.critical`;
  it re-creates it once use drops below 85%.
- **Registry cap**: the registry lives in `/var/lib/5050/registry` with a size cap (default 30% of
  the filesystem, setting in Settings > General); above it retention drops to 3 images per service
  and GC runs (9.6).
- **Log budgets**: 9.19.
- **Read-only mode**: on `SQLITE_FULL` or under 200 MB free, `5050d` keeps serving reads, refuses
  writes with `unavailable` and the message "The control host disk is full. Free space to continue.",
  and the console shows a red banner. It returns to normal by itself when space comes back.
- The E26 test fills the disk to 100% on the control host and on a data host.

---

## 7. Agent protocol

The wire types are in `pkg/proto/proto.go`; this section explains the flows.

### 7.1 Join and connect

1. `POST /api/agent/v1/join` with `proto.JoinRequest` (facts, `machine-id`, WireGuard public key).
   The token is single use. The response carries the host id and host token. v0.1 has no agent
   mTLS: the channel is TLS to the console certificate (or a pinned internal CA certificate) plus a
   host token that is bound to the host and rotated.
2. `GET /api/agent/v1/connect` upgraded to a WebSocket, header `Authorization: Bearer <host token>`.
   The server checks that `Hello.MachineID` and `Hello.WGPublicKey` match the host record.
   - Same token, different machine-id or WireGuard key (a VM cloned after join): the newer
     connection is refused with `duplicate_host`, the first one stays, and the Hosts page shows
     "Duplicate host detected: a second machine is using the identity of host X. Remove it or run
     `5050-agent rejoin` on it." Nothing fights.
   - Same identity reconnecting (a network blip): the new connection replaces the old one.
3. Agent sends `hello` (facts, epoch, last applied generation, whether it is running from its local
   cache, every replica it runs). Server replies `hello_ack`, then a `desired_state` with
   `generation = max(stored generation for the host, Hello.Applied) + 1`, so a control plane
   restored from an older snapshot never sends a generation the agent would treat as stale. The
   server also re-sends `desired_state` whenever anything for that host changes, coalesced to at
   most one push per 250 ms per host. Messages are capped at 16 MiB and use per-message deflate
   (a 2,000 replica state is about 3 MB uncompressed).
4. Pings every 20 s in both directions; 60 s of silence closes the socket. The agent reconnects with
   exponential backoff from 1 s to 30 s with full jitter.
5. **Token rotation**: every 30 days, over the open socket, the server sends `token_rotate` with a
   new token; the agent writes it to `agent.json` and acknowledges; the old token stays valid for
   10 minutes. Removing a host revokes both.
6. **Offline**: a host with no connection for 60 s is `offline`. At that moment the control plane
   withdraws its replicas from every route and DNS answer (Caddy's passive health checks and
   `lb_try_duration` cover the first 60 s). After another 120 s, replicas without volumes and not
   marked **Singleton** are rescheduled to other eligible hosts (9.2). Their IPs stay reserved in
   `o_ips` until the old host confirms removal. When the host comes back, its next desired state no
   longer lists them and the agent removes them.
7. **Singleton** (Settings > Scale, default on for workers and off for web services): replicas of
   a singleton service are never rescheduled automatically while their host is offline, because a
   partitioned host keeps running them (R13) and a second copy would run the work twice. The UI
   shows "Host offline: singleton not moved. Move now" with a button that does it after a
   confirmation.

### 7.2 Reconcile rules (agent)

- **Local cache**: after applying a desired state the agent writes it to
  `<DataDir>/agent/desired.json.enc` (encrypted with `/etc/5050/host.key`, 0600). At boot, before
  connecting, it applies networks, firewall, mesh, DNS, TCP proxies and the Caddy config from the
  cache, so a host reboot or an agent restart during a control plane outage keeps private DNS,
  isolation, egress quarantine, the mesh and public routes. The oneshot `5050-net-restore.service`
  (`Before=docker.service`) restores the firewall chains and `wg5050` from the same cache before
  Docker restarts any container, so a quarantined environment never gets a window of open egress.
- **No removal before a fresh state**: the agent never stops or removes a container until it has
  received a `desired_state` after `hello_ack` in the current connection.
- **Generation and epoch guard**: the agent refuses a `desired_state` whose generation is lower than
  its last applied one unless the epoch changed. With `Adopt: true` it applies additions and
  changes but removes nothing and reports unknown containers instead (9.17.1).
- A replica in `desired_state` with `desired: running` and no container: pull the image (registry
  auth from `HostConfig.RegistryPull`), create the container named `5050-<replica id>` on its
  network with its static IP, aliases, env, mounts, files, limits (`--memory`, `--memory-swap`
  equal to it, `--memory-reservation`, `--cpus`, `--cgroup-parent 5050-apps.slice`), Docker
  restart policy (`on-failure:N`, `unless-stopped`, or `no`), labels `5050.replica`,
  `5050.deployment`, `5050.service`, `5050.environment`, `5050.epoch`; apply egress rules; start;
  then health check (7.4) and report.
- A container labelled `5050.replica` that is not in the desired list is stopped with SIGTERM, then
  SIGKILL after `StopGraceSec`, then removed (subject to the two rules above). Containers without
  our labels are never touched.
- Apply order per desired state: networks and firewall, volumes, files, replicas, ingress, DNS.
  Report `state_applied` with the generation and any per-item errors.
- Reconcile is level-triggered: it also runs every 30 s and on every Docker event.

### 7.3 Zero-downtime rollout (control plane drives, agents only reconcile)

For a deployment D replacing active deployment A of a web service with N replicas:

1. Allocate replicas for D (placement 9.2) in waves of `maxSurge` (default 1, or a percentage;
   Settings > Scale). A wave that cannot be placed because no host has the memory reservation free
   waits as **Pending: no capacity** for up to 10 minutes, then fails with that diagnosis.
2. Wait until each D replica reports `healthy`. A replica reporting `crashed`, `unhealthy` past the
   health deadline, or `exited` fails D: remove D's replicas, keep A untouched, record the replica's
   diagnosis on D, status `FAILED`.
3. Switch: push the new upstream set (the upstream name's answers, TCP proxy upstreams, private DNS
   answers) listing D's replicas to every ingress host and every host's resolver. Wait for
   `state_applied` from the hosts that are online, for at most 15 s; hosts that are offline or slow
   converge when they reconnect and never block a deploy.
4. Overlap: keep A running `OverlapSec` (default 0). Drain: A's replicas leave public routes at
   once, stay in private DNS under `DNSEntry.Draining` (answered only to clients that already
   resolved them, that is, not returned for new queries; they are kept alive for in-flight
   connections) and get `desired: stopped` after `DrainingSec` (default 30), which sends SIGTERM and
   waits `StopGraceSec` before SIGKILL. Internal callers with pooled keep-alive connections keep
   working for that window; the docs ask for idle keep-alive timeouts under 30 s, and Go and Node
   clients retry idempotent requests on a reused connection that closes. A stable per-service
   virtual IP is v0.2 (section 22).
5. Mark D `SUCCESS` and active, A `REMOVED`.

**Services with a volume** cannot run two containers on one volume. Their rollout stops A (the
container is stopped, not removed), then starts D, with a visible "Short gap on every deploy" note
in the UI. If D fails its health check, D is removed and A's container is started again, and D
is `FAILED` with "The new version failed its health check; the previous version was restarted."
Database templates refuse a change of major version on an existing volume (for example Postgres 17
to 18) with "Major version changes need a dump and restore. Use Upgrade to a new major version in
the database panel." (9.13).

Workers follow the same flow without routes. Cron services have no long-running replicas (9.9).

### 7.4 Health checks (agent)

The agent probes from the host to `<replica ip>:<port>` every `IntervalSec` (3 s). With a path:
HTTP GET with `Host` set to the service's first domain (or `<name>.internal`),
`X-Forwarded-Proto: https`, `User-Agent: 5050cloud-healthcheck`, no redirect following; 2xx or 3xx
passes. Without a path: TCP connect. After the first pass, wait `GraceSec` (5 s) and probe once
more, then report `healthy`. Past `DeadlineSec` (300 s) report `unhealthy` with a diagnosis.
After a deployment is active, the agent keeps probing every 30 s; three failures in a row report
`unhealthy` (the control plane pulls that replica from routes and records activity), and the first
pass after that reports `healthy` again. Workers without a port are healthy once running 10 s.

**Panic threshold (fail open)**: when every running replica of an active deployment is unhealthy
(for example a shared database is slow), the route keeps all running replicas and sets
`Route.Panic`, the card shows "All replicas failing health checks; serving anyway", and a
`service.unhealthy` alert fires. A degradation never becomes a guaranteed 502.

### 7.5 Requests and streams

- `build` -> many `build_log` -> `build_result`. The agent keeps building if the socket drops and
  sends the result after reconnecting (results are buffered up to 1 hour).
- `job` -> `job_log` -> `job_result` for pre-deploy commands, cron runs, backups, restores.
- `logs_query` -> `logs_result`; `logs_subscribe` -> `log_lines` until `logs_unsubscribe` or
  disconnect.
- `exec_open` and `tcp_open` start streams whose data travels in binary frames
  (`[kind][stream id uint64 BE][payload]`); `exec_close`, `tcp_close`, `exec_exit` end them.
  Flow control: each side keeps at most 1 MiB unacknowledged per stream and pauses reads beyond
  it (WebSocket backpressure is enough because frames are written in order).
- `upgrade` -> the agent upgrades itself (9.16) and reconnects with the new version.

---

## 8. Component internals

### 8.1 Agent runtime (`internal/agent/core`, WP3)

- `core.Run(ctx, core.Options{ControlURL, HostToken, DataDir, DockerHost, Network, Ingress,
  Builder, CACertPEM, HostKey})` owns the connection, the local desired-state cache (7.2), the
  reconcile loop, health checks, Docker event folding, metrics sampling, logs, jobs, exec, TCP
  streams, tool downloads (`agentapi.Tools`), image retention and disk guards, the ballast file
  (6.6), the registry container on the control host, cron execution, maintenance drains (9.16) and
  agent self-upgrade (calling `internal/upgrade`).
- **Metrics**: every 30 s read cgroup v2 files for each container (`cpu.stat` usage deltas ->
  vCPU, `memory.current`, `memory.max`), network counters from `/proc/<pid>/net/dev`, host CPU from
  `/proc/stat` deltas, memory from `/proc/meminfo`, disks from `statfs` on `/` and `/data`.
  Volume usage by walking the volume directory every 5 minutes with a 30 s budget. Listening ports
  from `/proc/<pid>/net/tcp` and `tcp6` (state `0A`). For Postgres replicas the agent also runs the
  template's metrics probe (10.1) every 60 s through Docker exec.
- **Logs**: follow each replica's Docker log stream (with timestamps), strip ANSI, cap lines at
  8 KiB, detect level (explicit JSON `level`/`severity`, `level=`, a leading `[ERROR]`-style
  token, then error words; stderr alone means nothing), parse JSON lines into `Attrs`, mask secret
  values (9.15) and append to hourly JSONL files per service; gzip files older than 2 hours.
  Retention is the smaller of `LogRetentionDays` (7) and the byte budgets in 9.19 (oldest files go
  first). More than 500 lines per second per replica are dropped with one warning line per second
  ("N log lines dropped: more than 500 lines per second"). Queries scan files in the window newest
  first and stop at the limit. Filters use `pkg/logfilter` (9.4). HTTP access logs come from
  `Ingress.AccessLogs` and are stored with stream `http` under the service they belong to.
- **Log tail to the control plane**: the agent sends `host_logs_tail` every 10 s with every
  error-level line and every deploy-log line of the last batch (capped at 200 lines per batch), so
  the control plane holds the last 15 minutes of them per host (`o_host_logs`, 9.4).
- **Use all cores**: when `ReplicaSpec.ClusterWrap` is true, the entrypoint becomes
  `["node", "/opt/5050/cluster.cjs", "<N>", <script>, <args>...]` with
  `internal/agent/assets/cluster.cjs` (embedded, written to `<DataDir>/assets/` and mounted
  read-only). cluster.cjs is the proven 52-line design from `reuse.md` 1.3: N workers sharing the
  port, `CLUSTER_WORKER=1..N`, restart a dead worker after 500 ms (5 s if it died within 10 s of
  start), forward SIGTERM/SIGINT, hard exit at 28 s. Every replica also gets
  `FIFTY_PROCESSES=N` and, unless the user set it, `WEB_CONCURRENCY=N`.
- **Isolation of system processes**: app containers run under `--cgroup-parent 5050-apps.slice`
  (a systemd slice created by the installer with `MemoryMax` = host RAM minus the host reserve,
  9.2). `5050d`, the agent and Caddy carry negative `OOMScoreAdjust` so the kernel kills an app
  first. Every app container gets `--memory-swap` equal to `--memory`, so an out-of-memory app is
  killed fast and diagnosed instead of swapping the host.
- **Image retention and disk**: keep the newest 3 images per service on a host (`ImageRetention`)
  plus any image a container uses; prune the rest hourly. When `/` or `/data` is above
  `DiskPruneAtPct` (80), prune unused images and build cache (`Builder.Prune`) before starting a
  build or pull, and report `disk.high` events at 75% and 85% and `disk.critical` at 95% (the
  ballast goes at 95%, 6.6).
- **Pins** for infrastructure images the runtime manages (registry) live in
  `internal/agent/core/pins.go`; `HostConfig.Images` may override any pinned image (mirrors).
- **Jobs** run as containers named `5050-job-<id>` with the given image, argv, env, network,
  mounts and files, a memory limit, and are killed at `TimeoutSec`. Kind `exec` with a
  `ReplicaID` runs inside that replica through Docker exec.

### 8.2 Builder (`internal/agent/build`, WP4)

Implements `agentapi.Builder`.

1. **Fetch**: Git sources: `git init`, `git fetch --depth 1 origin <sha>`, `git checkout`, with
   the token passed through `GIT_CONFIG_COUNT`/`http.extraHeader`, never in the URL or the log.
   Tarballs: download with the host token, reject archives over 500 MB, paths with `..` or
   absolute paths, and symlinks pointing outside. Work dir `<DataDir>/work/<build id>`, deleted
   after the build.
2. **Config file** (9.20): before choosing a builder, read the config-as-code file from the root
   directory (or the path set in **Config file path**), parse it with `pkg/configfile`, and return
   the overlay and its field-to-line map in `BuildResult.ConfigFile`. The control plane merges it
   (9.20). A parse error fails the build with "railway.json line 7: unknown field buildComand. Did
   you mean buildCommand?".
3. **Builder choice** (after the overlay is applied):
   - `auto` uses `Dockerfile` (or `DockerfilePath`, or the `RAILWAY_DOCKERFILE_PATH` /
     `FIFTY_DOCKERFILE_PATH` variable) when present in the root directory, else Railpack. The log
     prints `Using detected Dockerfile`.
   - `RAILPACK` and `DOCKERFILE` force one.
   - `NIXPACKS` (from a config file, an import or the API) builds with Railpack and logs
     `Nixpacks config detected, building with Railpack.` Nixpacks itself is not shipped (9.20.2
     lists the hints we honour and what we ignore).
   - `railpackVersion` (Settings > Build, or `build.railpackVersion` in the file) picks one of the
     Railpack versions in `pins.go` (the current pin and the two before it); another value fails
     with "Railpack X is not available on this install. Available: A, B, C."
4. **Railpack**: `railpack prepare <ctx> --plan-out <outside ctx>/plan.json --info-out info.json`
   with `--build-cmd` and `--start-cmd` when set. **Variables never go on argv**: the child process
   gets them in its environment (`cmd.Env`) and argv carries only `--env NAME` (names, which
   Railpack resolves from its environment). M0 checked this (2026-10-22) against Railpack v0.40.1,
   the newest release at least two weeks old: `core/app/environment.go` `FromEnvs` treats a bare
   `NAME` (no `=`) as "inherit from the process environment" (`os.LookupEnv`), and `NAME=` with an
   empty value is skipped. So no env file is needed and `BuildJob.EnvFile` stays false; WP4 keeps a
   unit test that asserts no value appears in the argv it builds.
   Then `docker buildx build --builder 5050 -f plan.json --build-arg BUILDKIT_SYNTAX=<pinned
   railpack frontend image, mirrored into the internal registry> --secret id=X,env=X` for every
   secret the plan lists, `--platform`, `--cache-from/--cache-to
   type=registry,ref=<CacheRef>,mode=max`, `--push -t <Image>`, `--output type=image,push=true`,
   `--provenance=false`. Delete the plan files afterwards. `info.json` gives the provider (sets
   `IsNode` for node) and the detected start command. V4 checks `ps auxww` during a build and finds
   no variable value.
5. **Dockerfile**: same buildx flags; only variables named by an `ARG` line become build arguments,
   passed with `--build-arg NAME` (value from the child environment, never on argv). The log lists
   which variable names are visible at build time.
6. **Image import** (`SourceImage`): `crane.Copy` from the source (with `SourceImageAuth`) to
   `Image`, resolving the digest for the target platform. Docker Hub short names are normalized.
   Image names are validated before the copy (lowercase repository, any case tag).
7. Every command is an argv array (`exec.CommandContext`), never a shell string. Build output
   streams line by line through `logf`. Secret values are masked in log lines.
8. **Sandbox**: `EnsureRunning` creates the buildx builder `5050` (driver `docker-container`,
   pinned BuildKit image, `--driver-opt network=5050-build`, buildkitd flags
   `--oci-worker-net=bridge` (M0 checked BuildKit v0.33.1, the newest release at least two weeks
   old: buildkitd's `--oci-worker-net` accepts `auto, bridge, cni or host`, and the
   `moby/buildkit` image ships the `buildkit-cni-bridge`, `-loopback` and `-host-local` plugins the
   bridge provider looks up, so bridge mode works inside the docker-container driver, which runs
   buildkitd privileged) and no `network.host` entitlement, GC keep 20 GB, container capped at
   50% of host memory and `cpus-1` CPUs) if missing or on a different image. `5050-build` is a
   Docker bridge the agent creates; the firewall (8.4) drops traffic from it to every host address
   (including the Caddy admin socket's absent TCP port, the registry's loopback port and the mesh)
   and to `169.254.169.254`, except the registry on the build network gateway port 5000 with the
   push credential. So a `RUN` step in an untrusted repo can reach the internet and nothing of
   ours. E38 runs a Dockerfile that tries `127.0.0.1:2019`, the gateway, the mesh and the metadata
   address, and expects all four to fail.
9. **Timeouts**: the build timeout is `build.timeoutMin` (default 30, Settings > Build).
10. **Diagnosis** (table-driven, `internal/agent/build/diagnose.go`, unit-tested on captured log
    fixtures): see 9.18.
11. Pins live in `internal/agent/build/pins.go`: Railpack versions + SHA-256 per arch, frontend
    image digest, BuildKit image digest. The installer mirrors the frontend and BuildKit images
    into the internal registry, so builds do not depend on ghcr.io or Docker Hub being reachable.

### 8.3 Control plane split (`internal/cp/cpapi`)

The control plane has two halves joined by `cpapi.Config` and `cpapi.Orchestrator`.

- **Core (WP1)** owns the HTTP API (section 11), auth, the domain model, variables, staged
  changes, config files, imports, email, notifications, activity and presence. On commit it builds
  a `cpapi.CommitPlan` and calls `Orchestrator.ApplyCommit`. On a GitHub push it matches services
  and calls `Orchestrator.Deploy`. API handlers for deployments, logs, metrics, hosts, backups,
  cron, monitors, usage and upgrades call the orchestrator.
- **Orchestrator (WP2a + WP2b)** owns hosts and the agent hub, IPAM, placement, the deploy engine,
  desired-state computation, builds, routes, logs and metrics fan-out, monitors and usage (WP2a),
  and the data services package `internal/cp/orch/data` (WP2b): cron, backups, restore, promote,
  template rendering at deploy, registry retention and data inspection. WP2a's `Orchestrator`
  struct holds a `data.Service` (interface pinned in `internal/cp/orch/README.md` by M0) and
  delegates every data route to it. It calls back into `cpapi.Config` for committed config,
  resolved variables, domains and settings, and publishes events through `Config.Publish` (there
  is no event bus in `orch.Options`).
- **Templates, who does what**: both halves read `templates.FS` by `ServiceConfig.Template`. WP1
  renders a template's variables once at creation (`${{secret(32)}}`, `${{randomInt(a,b)}}`,
  inputs from the form) and stages or creates the services in dependency order. WP2b renders the
  template's `Files`, mounts, tools and backup recipe at each deploy, when host facts such as
  `.MemBytes` are known. Neither calls the other for template data.
- **Dev server first**: WP1's milestone 1 (in M1, 19.1) is `5050d serve --dev` where every route
  in section 11 answers with typed fixture data from `fakeorch`. Until then WP8 and WP7 use the
  MSW mocks committed by M0.
- Each half has its own migrations and tables (section 6). Unit tests use fakes of the other
  interface: WP1 ships `internal/cp/core/fakeorch`, WP2a ships `internal/cp/orch/fakeconfig`.

### 8.4 Agent networking and ingress (`internal/agent/netw`, WP5)

**Address plan** (defaults; `--pod-cidr` and `--mesh-cidr` at init):

| Thing | Range | Example |
|---|---|---|
| All containers | `10.128.0.0/9` | |
| Host k (mesh index 1..127) | `10.(128+k).0.0/16` | host 1: `10.129.0.0/16` |
| Environment slot n on host k | `10.(128+k).n.0/24`, gateway `.1` | `10.129.4.0/24` |
| Replica IPs | `.2` to `.254` of that /24, assigned by the control plane | `10.129.4.7` |
| Build network | `10.128.255.0/24` per host (not routed over the mesh) | |
| Mesh tunnel addresses | `100.96.0.k/32` | `100.96.0.1` (control host) |

Limits that follow (stated in Reference > Limits): at most 127 hosts, at most 254 environments
with replicas on any one host (slot 255 is the build network), 253 replicas per environment per
host. When a host has no slot left, placement fails with "No network slot left on host X: it
already runs 254 environments." S1 creates 300 environments on 3 hosts to prove the message and
the spread to other hosts.

- **Docker networks**: one bridge per environment per host, named `5050-<env id>`, with the
  subnet and gateway above, `com.docker.network.bridge.name=f5-<short env id>`, ICC on, and
  `com.docker.network.driver.mtu` = the mesh MTU (1420, or the discovered path MTU minus 80) once
  the install has 2 or more hosts. The agent also clamps TCP MSS to path MTU on `wg5050`
  (`-j TCPMSS --clamp-mss-to-pmtu`), so clouds that drop ICMP do not hang large cross-host
  responses. Containers use the agent's DNS at the gateway IP.
- **Mesh**: with two or more hosts, interface `wg5050`, private key generated on the host
  (`<DataDir>/wg.key`, 0600, never sent anywhere), peers and allowed IPs from `proto.Mesh` (each
  peer: its `/32` plus its host `/16`), routes for each peer `/16` via `wg5050`,
  `PersistentKeepalive 25`. Peers behind NAT work when one side of each pair is reachable. The
  control host must be reachable on UDP 51820. When two hosts have had no handshake for 3 minutes
  while both are online, the Hosts page shows "Hosts X and Y cannot reach each other over
  WireGuard (both are behind NAT, or UDP 51820 is blocked)" and placement stops putting replicas of
  one environment on both. Relaying through the control host is v0.2.
- **Firewall** (one atomic `iptables-restore --noflush` per apply, chains `5050-ISOLATE`,
  `5050-EGRESS`, `5050-NAT`, `5050-HOSTIN`):
  - `DOCKER-USER` jumps to `5050-ISOLATE`: accept established/related; for each environment
    accept traffic from its subnets to its subnets (ipset `5050-env-<id>` of every host's /24 for
    that environment); drop other traffic between addresses in the pod CIDR (environments and
    projects cannot talk to each other, as on Railway); drop pod CIDR and build network traffic to
    `169.254.169.254` and the mesh `/24` unless the service has `networking.allowMetadata`
    (Settings > Networking, Admin only, audited).
  - `INPUT` jumps to `5050-HOSTIN` for traffic from the pod CIDR and the build network: allow
    UDP and TCP 53 to the container's own environment gateway, allow the build network to the
    registry port on its gateway, drop everything else addressed to the host (other gateways'
    DNS, the registry, Caddy, the mesh address, SSH).
  - `5050-EGRESS`: for environments with egress `blocked`, accept destinations in the
    environment's allowlist ipset and in pod and mesh ranges, reject everything else. Rules for a
    new container are in place before it joins any route (`AllowEgress`).
  - `nat POSTROUTING` jumps to `5050-NAT`: `RETURN` for pod CIDR to pod CIDR, so cross-host
    traffic keeps its source address (Docker's masquerade still applies to internet traffic).
  - Rules are rebuilt from the desired state (or its local cache, 7.2) only; nothing outside our
    chains is changed. `5050-net-restore.service` loads the same rules from the cache before
    Docker starts.
- **DNS**: one `miekg/dns` server bound on each environment gateway IP, port 53, UDP and TCP. It
  answers `<name>.internal` and `<name>.railway.internal` (and, for 24 hours after a rename, the
  old name, 9.5) with `DNSEntry.IPs` (healthy replicas, shuffled, TTL 5). `DNSEntry.Draining` IPs
  are never returned for new queries but stay routable until the drain ends (7.3). Everything else
  is forwarded to the host's resolvers from `/etc/resolv.conf` (or
  `/run/systemd/resolve/resolv.conf` when the stub is in use). The DNS server answers from the
  cache when the control plane is down and keeps answering across an agent restart from it (E22c).
- **Upstream resolver** (ingress hosts): a second DNS server on `127.0.0.1:5354` answers
  `<UpstreamName>.upstream.5050` with the current upstream IPs of each route. M0 checked Caddy
  v2.11.7 (`modules/caddyhttp/reverseproxy/upstreams.go`): the `http.reverse_proxy.upstreams.a`
  source takes `"resolver": {"addresses": ["udp/127.0.0.1:5354"]}`, parsed as a network address
  with default network `udp` and port 53, so a custom port works; `refresh` sets the cache time. `SetUpstreams`
  changes its answers; Caddy's `dynamic a` upstreams (refresh 1 s) read it. So replica changes,
  deploys and failovers never reload Caddy.
- **Ingress (Caddy)**: `EnsureRunning` starts `caddy:<pinned>` with host networking, `--resume`,
  data in `<DataDir>/caddy`, admin on `unix//var/lib/5050/caddy/admin.sock` (root, 0600, the agent
  is the only client). `Apply` renders one JSON config (HTTP app with a server on :443 and :80, one
  route per domain with a `reverse_proxy` using the dynamic upstream, `lb_policy random`,
  `lb_try_duration 5s`, passive health checks, `stream_close_delay 5m`, `header_up X-Real-IP`,
  access logs as JSON to a file the agent tails) and calls `POST /load` only when the route set or
  certificates changed. `stream_close_delay` keeps open websockets and streams alive across that
  reload (E32). TLS automation: Let's Encrypt then ZeroSSL issuers (or the `--acme-ca` directory),
  `email` = ACME email; for domains marked `internal` the internal issuer. HTTP redirects to HTTPS.
  A catch-all returns a styled 404 page ("No service is attached to this domain"). When `Route.Panic`
  is set the route keeps every running replica (7.4).
- **Several ingress hosts**: exactly one ingress host is `ACMEPrimary` (the control host while it
  is online, else the oldest online ingress host). Only it runs ACME. Other ingress hosts proxy
  `/.well-known/acme-challenge/*` to the primary over the mesh, so the CA's request can land on any
  of them. The primary reports `cert_issued` (certificate and key, encrypted to the control plane);
  the control plane stores them encrypted and pushes them in `Ingress.Certificates` to the other
  ingress hosts, which load them as static certificates. Rate limits are spent once per name.
- **TCP proxy**: a Go listener per allocated host port, round robin over upstreams, idle timeout
  1 hour, applied atomically (new listeners first, then close removed ones).
- Pins in `internal/agent/netw/pins.go` (Caddy image digest).

---

## 9. Control plane behavior

### 9.1 Deploy pipeline (WP2a)

```
QUEUED -> BUILDING -> DEPLOYING -> SUCCESS (active) -> REMOVING -> REMOVED
   |          |            |             \-> CRASHED (restarts exhausted)
   |          |            \-> FAILED (pre-deploy, start or health check failed; old stays live)
   |          \-> FAILED (build failed)
   \-> CANCELLED (aborted, superseded, or queue timeout)
SKIPPED (watch paths matched nothing), COMPLETED (cron or job exited 0)
```

- One in-flight deployment per service instance. A new deployment while one is queued supersedes
  the queued one (`CANCELLED`, message "Superseded by <short id>"). A new deployment while one is
  building or deploying waits in `QUEUED`.
- **Queue timeout**: a deployment that cannot start its build or placement for 15 minutes is
  cancelled with a diagnosis ("No online host has the role build." or "Waiting for capacity for
  15 minutes: no host has 512 MB free."). The card shows the reason while it waits.
- Steps (`api.DeployStep`) in order: `queued`, `fetch`, `build` (or `pull image`), `push`,
  `pre_deploy`, `start`, `healthcheck`, `cutover`, `drain`. Each records start, finish and state, and
  is published as an `Event` so the UI updates live.
- **Build placement**: an online host with role `build`, least busy first, with at most
  `max(1, cpus/4)` concurrent builds per host. Platform is the architecture of the hosts the
  service will run on (v0.1 keeps one architecture per service: the first eligible host's).
- **Rebuild vs redeploy**: a commit that changes `source` or `build` rebuilds. Changes to
  `deploy`, `networking` or variables redeploy the last image. A replicas-only change scales the
  active deployment in place (new replicas join routes when healthy, removed ones drain) without a
  new deployment record; it records activity "Scaled to N replicas".
- **Config file overlay**: when a build returns `ConfigFile`, the orchestrator asks
  `Config.MergeConfigFile` (WP1, 9.20) for the effective config and snapshots that.
- **Pre-deploy**: a `job` with the new image, the new variables, the private network, no volumes,
  timeout `PreDeployTimeoutSec`. Non-zero exit fails the deployment with
  "Pre-deploy command exited with code N: <last log line>".
- **Ordering on commit**: services in a commit plan deploy in topological order of their
  references and template `DependsOn` (a service waits for the services it references to be
  `SUCCESS`); cycles deploy in parallel. Single redeploys and pushes are not ordered.
- **Restart of the control plane** mid-pipeline: `QUEUED` deployments re-enter the queue in
  creation order; `DEPLOYING` ones resume from replica states. Builds still running on an agent are
  re-attached when its result arrives; a build no agent knows about is marked `FAILED` with
  "Interrupted by a control plane restart. Redeploy to retry."
- **Variable snapshot**: when a deployment starts, the orchestrator calls
  `Config.ResolveVariables` once and stores the result encrypted in `o_deployments.env_enc`, next
  to the config snapshot. Every replica, restart, redeploy and rollback of that deployment uses
  this snapshot. Provided variables that differ per replica (`FIFTY_REPLICA_ID`, `FIFTY_HOST`,
  `RAILWAY_REPLICA_ID`, `RAILWAY_REPLICA_REGION`) are added per replica.

#### 9.1.1 Status labels (one table for the card, panel, activity feed, CLI and webhooks)

`api.StatusLabel(status, isLatestSuccess)` returns the label; `web/src/kit/StatusGlyph` and the
CLI both use it, so the glyph and words are identical everywhere.

| Internal status | Label shown | Colour | Sub-line |
|---|---|---|---|
| `QUEUED` | `Initializing` | grey, pulsing | "Waiting for a build host" / "Waiting for capacity" when that applies |
| `BUILDING` | `Building` | blue, pulsing | current step |
| `DEPLOYING` | `Deploying` | blue, pulsing | current step |
| `SUCCESS`, the active one | `Active` | green | "Deployed 3m ago via GitHub" |
| `SUCCESS`, older | `Success` | green outline | |
| `COMPLETED` | `Completed` | green outline | exit code 0 |
| `CRASHED` | `Crashed` | red | restarts used; inline **Restart** button |
| `FAILED` | `Failed` | red | the diagnosis sentence |
| `REMOVING` | `Removing` | grey | |
| `REMOVED` | `Removed` | grey | |
| `CANCELLED` | `Removed` | grey | "Aborted by <name>", "Superseded by <short id>" or the queue timeout reason |
| `SKIPPED` | `Skipped` | grey outline | "No changed files matched the watch paths" |

Railway has no visible "Cancelled", so an aborted or superseded deployment shows `Removed` with the
reason. M0 result: Railway's public GraphQL `DeploymentStatus` enum (introspected 2026-10-22) is
`BUILDING, CRASHED, DEPLOYING, FAILED, INITIALIZING, NEEDS_APPROVAL, QUEUED, REMOVED, REMOVING,
SKIPPED, SLEEPING, SUCCESS, WAITING`: there is no cancelled state, so aborted and superseded
deployments are `REMOVED` on Railway too. The dashboard label itself was not checked live (see
19.0 item 4).

### 9.2 Scheduler and placement (WP2a)

- Eligible hosts: online, not draining or in maintenance, matching `Placement.Hosts` if set, else
  carrying every role in `Placement.Roles` (default `app`; database templates `data`), same
  architecture as the service's image, with a free network slot.
- **Capacity**: every replica has a memory reservation (`deploy.memoryReservationMb`, default the
  memory limit when one is set, else 256 MB; database templates 512 MB). A host's capacity is its
  RAM minus the host reserve (`reserve_mb`, default 10% of RAM plus 512 MB for Docker, 5050d, the
  agent, Caddy and BuildKit). A host whose reservations would exceed capacity is not eligible.
  Placement never overcommits reservations; actual limits may still overcommit.
- Score, lower is better: (replicas of this service on the host, reserved memory / capacity, CPU in
  use / CPUs). Ties break by host id. Several replicas of one service may share a host.
- **Pending: no capacity**: when no host is eligible because of reservations, the wave waits (7.3)
  and the card says "Pending: no capacity. Needs 512 MB; the largest free host has 300 MB."
- **Rescheduling after host loss** places one replica at a time with the same rules, so the
  survivors are never filled past capacity; what does not fit stays Pending and an alert fires.
- **Volumes**: a service with a volume is pinned to the volume's host, chosen at first deploy with
  the same scoring. Volume moves between hosts are v0.2.
- **Data role default**: the first non-control host that joins with `data` becomes the default for
  new databases. While every database runs on the control host and the install has 2 or more
  hosts, the Hosts page and the database panel show "All databases are on the control host. If it
  fails, they stop until it is rebuilt. Give another host the data role."
- **Draining** reschedules everything without a volume and refuses new placements; volume services
  on it show "Pinned to a draining host" and stay. **Maintenance** (9.16) also stops them in order.
- **IPAM**: the orchestrator allocates each (host, environment) a /24 slot and each replica a
  static IP, released when the agent confirms the replica is removed (never earlier, so a
  partitioned host's IPs are not handed out twice). Allocation is a transaction in `o_ips`.

#### 9.2.1 Two entry points (second ingress host)

Joining with `--roles app,ingress` (or ticking **ingress** in Add host) makes the host serve 80
and 443 for every route, with certificates shared as in 8.4. To survive the loss of the control
host you then put both IPs behind one of:

- **DNS failover**: two A records for your wildcard or custom domain (clients retry the other
  address), or a DNS provider health check that removes a dead address.
- **An external load balancer** (cloud LB, or a small VM running HAProxy) with both hosts as
  backends and a TCP health check on 443.

The console (`Ingress.Console`) is served by every ingress host, so it stays reachable while the
control plane runs elsewhere; when the control plane itself is down the console shows "The control
plane is not responding" and apps keep serving. Sslip.io names point at one IP, so a two-ingress
setup needs `--domain`. The **Add a host** guide covers both setups.

### 9.3 Variables and references (WP1)

- Names match `^[A-Za-z_][A-Za-z0-9_]{0,255}$`. Values are any UTF-8 up to 64 KiB, multi-line
  allowed. Stored encrypted. The API never returns plain values except through reveal.
- **Reference syntax** (same as Railway): `${{NAME}}` (same service), `${{shared.NAME}}` (shared
  variable of the environment), `${{Service.NAME}}` (another service in the same environment, by
  service name, case-sensitive, names with spaces allowed), including provided variables such as
  `${{api.RAILWAY_PUBLIC_DOMAIN}}`. Several references and plain text may mix in one value.
- **Template functions**: `${{secret(len)}}`, `${{secret(len, "alphabet")}}` (characters to draw
  from, default `A-Za-z0-9`), `${{randomInt(min, max)}}`. They are evaluated once, when the value
  is first saved (template creation or the variable editor), and the stored value is the result.
- **Resolution**: recursive with a depth limit of 10 and cycle detection; an unknown target
  resolves to an empty string, is listed in `Variable.Unresolved`, shown in the UI with a warning
  icon, and written as a warning line into the deploy log. It does not block a deploy.
- **Provided variables** (computed per replica by the orchestrator, passed to `ResolveVariables`):
  `PORT`, `FIFTY_PUBLIC_DOMAIN`, `FIFTY_PRIVATE_DOMAIN`, `FIFTY_PROJECT_ID`, `FIFTY_PROJECT_NAME`,
  `FIFTY_ENVIRONMENT_ID`, `FIFTY_ENVIRONMENT_NAME`, `FIFTY_SERVICE_ID`, `FIFTY_SERVICE_NAME`,
  `FIFTY_DEPLOYMENT_ID`, `FIFTY_REPLICA_ID`, `FIFTY_HOST`, `FIFTY_PROCESSES`,
  `FIFTY_GIT_COMMIT_SHA`, `FIFTY_GIT_BRANCH`, `FIFTY_GIT_REPO_OWNER`, `FIFTY_GIT_REPO_NAME`,
  `FIFTY_GIT_COMMIT_MESSAGE`, `FIFTY_GIT_AUTHOR`, `FIFTY_VOLUME_NAME`, `FIFTY_VOLUME_MOUNT_PATH`,
  `FIFTY_TCP_PROXY_DOMAIN`, `FIFTY_TCP_PROXY_PORT`, `FIFTY_TCP_APPLICATION_PORT`,
  `FIFTY_SNAPSHOT_ID` (the deployment id, used as a build snapshot id), `FIFTY_REPLICA_REGION`
  (the host name), and when `railway_aliases` is on (default) the same set as `RAILWAY_*`, plus
  the legacy `RAILWAY_ENVIRONMENT` (= environment name) that many apps still read. `PORT` is the
  target port, default 8080 when nothing is set or detected. User variables win over provided ones.
- **Not provided**: Railway's `OTEL_*` variables (Railway's tracing beta). Reference > Variables
  says so and shows how to set them to your own collector.
- **Config from variables** (Railway names and ours): `RAILWAY_DOCKERFILE_PATH`,
  `RAILWAY_RUN_UID`, `RAILWAY_SHM_SIZE_BYTES`, `RAILWAY_HEALTHCHECK_TIMEOUT_SEC`,
  `RAILWAY_DEPLOYMENT_OVERLAP_SECONDS`, `RAILWAY_DEPLOYMENT_DRAINING_SECONDS`,
  `RAILPACK_*` and `NIXPACKS_*` build hints (9.20.2), `NO_CACHE`, and the `FIFTY_` equivalents
  override the matching setting when the setting is empty.
- **Shared variables** live per environment. **Share** (from the shared variables page) adds
  `NAME=${{shared.NAME}}` to the chosen services as a staged change.
- **Sealed** variables can never be revealed again, are excluded from reveal, `5050 run`,
  environment duplication, service duplication, imports' exports and the Raw Editor; they still
  reach builds and deployments.
- **Suggested variables**: names found in `.env`, `.env.local`, `.env.production`, `.env.<suffix>`,
  `.env.example`, `.env.sample` and `.env.template` in the repo root or root directory (scan, 9.14).
  Only names are suggested, never values, from files that look like they hold real secrets
  (`.env`, `.env.local`, `.env.production`); example files also suggest their values.
- **Import variables from Heroku** (palette and Variables menu): paste the output of
  `heroku config --json` or `heroku config -s`; it lands as staged variables.
- **Canvas edges** come from the references (only names, never values).

### 9.4 Logs and the filter syntax (WP3 implements `pkg/logfilter`, WP2a fans out)

- Three types: **Build** (control plane build log files), **Deploy** (runtime stdout/stderr from
  agents) and **HTTP** (Caddy access logs, shown under the **Network** tab's **HTTP** sub-view, as
  on Railway). The Log Explorer and the CLI can mix services of one environment.
- Queries fan out to the hosts that ran the services in the window, merge by timestamp, and cap at
  `limit` (default 500, max 5000). Live tails subscribe on each host and merge.
- **Offline hosts**: a query that needs an offline host returns what the others have, plus the
  error and deploy lines that host shipped in its last 15 minutes (`o_host_logs`), and a marker
  "Host X is offline. Showing its last error and deploy lines; full logs return when it is back."
  The UI never shows an empty result without saying why.
- **Filter grammar** (`pkg/logfilter`, case-insensitive, Railway compatible):

```
expr    := term { [AND|OR] term }          (adjacent terms mean AND)
term    := [-] ( word | "phrase" | @attr:value | @attr:op number | @attr:a..b | replica:<id> | ( expr ) )
op      := > | >= | < | <=
attrs   := level, service, replica, deployment, stream, and any parsed JSON field (dotted paths, [i] for arrays);
           HTTP logs add method, path, host, httpStatus, responseTime (alias durationMs), srcIp,
           requestId, userAgent, bytes
```

  Example: `@level:error -healthcheck`, `@httpStatus:>=500 @path:/api`, `"connection refused" OR timeout`.
- SSE stream endpoint sends `: ping` every 15 s, sets `X-Accel-Buffering: no`, and uses a bounded
  queue that drops lines (with a "N lines dropped" marker) rather than blocking.

### 9.5 Staged changes, rename, duplicate and sync (WP1)

- Every edit of service config, variables, shared variables, volumes, service creation (without
  Deploy Now), service rename and service deletion is a row in `changes` for the environment.
  Edits to the same service and kind merge into one change. `GET /changes` returns them with a
  diff of committed vs staged values (secret values shown as `•••`).
- Applied immediately, not staged (same as Railway): domain create/delete, TCP proxies, canvas
  positions and groups, database and template creation, and "Deploy Now" creation.
- **Validation on commit** (all or nothing, errors name the field): names unique and at most 32
  characters (Railway's limit), cron valid, replicas >= 1 and == 1 when the service has a volume,
  port 1..65535, mount path absolute and not `/`, memory >= 64 MB, health path starts with `/`,
  references syntactically valid.
- **Rename** (inline in the panel header, Update Info, or the palette) is staged. On commit it
  rewrites every variable in the environment that references the old name (`${{api.X}}` becomes
  `${{backend.X}}`) in the same commit, and the Details diff lists those rewrites under the rename.
  The private DNS name changes with it on the next deploy of the service; the old name stays an
  alias for 24 hours (`name_aliases`). Shared variables and the service's own variables are
  rewritten too.
- **Commit** (`POST /changes/commit {message, deploy}`) applies all changes in one transaction,
  bumps `rev` per changed service, writes `config_revisions` and a `commits` row, records activity
  with the message, builds the `CommitPlan` and calls the orchestrator. With `deploy: false`
  (Alt+Deploy) config is committed and only deletes and scaling are applied.
- Discard one (`DELETE /changes/{id}`) or all (`DELETE /changes`).
- **Duplicate environment** stages a create for every service of the source with its config and
  non-sealed variables, plus shared variables; nothing deploys until the user presses Deploy.
- **Duplicate service** (right-click) stages a copy named `<name>-copy` with config and non-sealed
  variables, no domains, no volume data (a new empty volume when the source has one).
- **Environment Sync** (`POST /environments/{eid}/sync {from}`): compares the source environment
  with this one and stages the difference: services only in the source (tag **New**), services
  whose config or variable names or non-secret values differ (**Edited**), services only here
  (**Removed**, staged as deletes only when the user ticks them; unticked by default). Sealed and
  secret values are never copied; a variable that exists in the source but not here is staged with
  an empty value and flagged "Set a value". The banner and the Details modal show the tags; Deploy
  commits as usual.

### 9.6 Deployment actions and the registry (WP2a)

| Action | Effect | Allowed when |
|---|---|---|
| Redeploy | New deployment from the same image, config snapshot and variable snapshot as the chosen deployment | any deployment with a retained image |
| Rollback | Same as redeploy of an older deployment; trigger `rollback`, `rollbackOf` set; the service's committed config and variables are not changed | image retained (`canRollback`) |
| Deploy latest commit | Build the head of the branch with the current committed config | GitHub sources |
| Restart | Restart the active deployment's containers one replica at a time (hard restart) | active or crashed |
| Abort | Cancel a queued or building deployment; stop new replicas of a deploying one; old keeps serving | QUEUED, BUILDING, DEPLOYING |
| Remove | Stop the active deployment; service shows "No deployment" | SUCCESS, CRASHED |

- **Retention**: the registry keeps the newest 10 images per service (setting) and, for each
  service, only the build cache ref of its newest build (`<CacheRef>` is overwritten per build and
  older cache manifests are deleted). Rollback to a deployment whose image is gone is not offered;
  Redeploy from its commit is.
- **Garbage collection** (weekly, 04:00 Sunday host time, and when the registry passes its cap,
  6.6): the orchestrator pauses the build queue and waits for running pushes to end, switches the
  registry to read-only (`REGISTRY_STORAGE_MAINTENANCE_READONLY`), runs `registry garbage-collect
  --delete-untagged`, then switches it back and resumes the queue. Pulls keep working throughout.
  E35 runs GC while a build is queued.
- **Bucket storage** (Settings > Backups, "Store images in the bucket"): the registry uses its S3
  storage driver against the configured bucket, so images and rollback history survive the loss of
  the control host. Off by default (it adds bucket traffic per deploy).
- **Credentials**: app hosts get a pull-only credential (`HostConfig.RegistryPull`); build hosts
  also get the push credential (`RegistryPush`).

### 9.7 Replicas and "use all cores" (WP2a decides, WP3 executes)

- `Replicas` (default 1) spread across hosts (9.2). Public traffic picks a random healthy replica.
- `Processes` (setting label **Processes**, toggle **Use all cores** sets it to `auto`):
  - `auto` resolves to `floor(cpuLimit)` or the host's CPU count when unlimited, at least 1, at
    most 32. Default is `1`. Workers and crons default to 1 even with auto unless set explicitly.
  - **Node mode**: when the build reported `IsNode` and the effective start command is
    `node [flags] <file>` (also detected through `npm start` when `package.json` `scripts.start`
    is exactly `node [flags] <file>`), the agent wraps it with cluster.cjs. The UI shows
    "Runs N Node processes sharing port P (cluster mode)".
  - **Other runtimes**: the agent sets `WEB_CONCURRENCY=N` and `FIFTY_PROCESSES=N`, which
    gunicorn, uvicorn workers, puma and many frameworks read. The UI shows "Sets WEB_CONCURRENCY=N;
    your server must read it" with a link to the guide listing common servers.
  - When neither applies, the UI suggests replicas instead ("Replicas on one host also use all
    cores").
  - The UI warns that memory use grows with processes and that work that must run once should
    check `CLUSTER_WORKER` (unset or `1`).
- **Singleton** (7.1) and **Max surge** (7.3) sit next to Replicas under Settings > Scale.

### 9.8 Restart policy (WP3)

`on_failure` (default, max 10) and `always` map to Docker restart policies `on-failure:N` and
`unless-stopped`, so containers restart even while the agent is down. `never` maps to `no`. The
agent counts restarts from Docker events; when `on_failure` is exhausted the replica reports
`crashed` and the deployment becomes `CRASHED`, with webhooks `deployment.crashed`. OOM kills report
`oomKilled` and the diagnosis "Out of memory at X MB (limit)".

### 9.9 Cron (WP2b schedules, WP3 runs)

- `cronSchedule` uses standard 5-field syntax plus `@hourly`, `@daily`, `@weekly`, `@monthly`
  (`github.com/robfig/cron/v3` parser), evaluated in `cronTimezone` (default UTC). The UI presets
  are labelled exactly **Every hour**, **Every day**, **Every week**, **Every month**.
- A cron service's deployment builds or pulls the image and becomes `SUCCESS` ("Scheduled"). Each
  run is a `job` on an eligible host using the active deployment's image, config and variable
  snapshot, recorded in `o_cron_runs` with status, exit code and logs.
- **Exactly once per slot**: the run row is inserted with UNIQUE(service, environment,
  scheduled_at) before the job is dispatched, so a control plane restart at a boundary cannot run
  one slot twice.
- Overlap `skip` (default) skips a run while the previous is running; `queue` runs it after.
  `cronTimeoutSec` kills a run. **Run now** starts a run immediately.
- **Control plane outages**: cron is scheduled by the control plane, so runs stop while `5050d` is
  down (Honest limits say so). On start the scheduler inserts a `missed` row for every slot it
  missed in the last 24 hours ("Missed while the control plane was down") and does not replay them.

### 9.10 Metrics (WP2a stores, WP3 samples, WP5 HTTP)

- Agents send `metrics` every 30 s. The orchestrator writes raw points per replica and the
  service sum, plus host points, into `metrics.db` (6.3), and turns network counters into rates at
  read time.
- `GET .../metrics?range=` accepts `15m`, `1h`, `6h`, `24h`, `7d`, `14d`, `30d` or `from`/`until`
  (Unix ms, any span within 30 days) and returns about 240 points; series `cpu` (vCPU), `mem`
  (bytes), `rx`/`tx` (bytes per second), `disk` (volume bytes), HTTP `req_2xx`..`req_5xx` per
  second, `error_rate` (5xx / all), and `p50`, `p90`, `p95`, `p99` in ms, plus deployment markers
  carrying the commit (sha, message, author). During a deploy, old and new replicas are summed.
- **Postgres metrics** (database panel Metrics tab): connections by state, cache hit ratio, and
  the top 10 queries by total time from `pg_stat_statements` (query text normalized, as
  Postgres stores it), read through the agent probe every 60 s.

### 9.11 Domains, TLS and TCP proxies (WP1 config, WP2a routes, WP5 Caddy)

- **Generated domain**: `<service slug>-<4 random [a-z0-9]>.<base domain>`, for non-production
  environments `<service slug>-<env slug>-<4 random>.<base domain>`. The base domain defaults to
  `<public ip with dashes>.sslip.io`; with `--domain example.com` and a wildcard A record it is
  `example.com`. Port: the service's target port, or the only detected listening port, or a
  prompt when there are several.
- **Custom domain**: the UI shows an `A` record to every ingress host's public IP (recommended,
  works at the apex) or a `CNAME` to the service's generated domain, each with a copy button. The
  control plane checks DNS through `1.1.1.1` and `8.8.8.8` every 30 s for 10 minutes after
  creation, then every 10 minutes, and shows "Waiting for DNS", "Issuing certificate", "Active" or
  the error. The route is pushed at once; the primary ingress issues the certificate when DNS
  points at it. **Retry certificate** forces a new attempt after a failure. An active domain shows
  "Certificate valid until <date>". A Cloudflare-proxied record is detected (Cloudflare IP ranges)
  and shown with "Cloudflare proxy detected: set SSL/TLS mode to Full (strict)".
- **Wildcard custom domains** (`*.example.com`) need DNS-01 and a DNS provider integration; they
  are v0.2. The custom domain form refuses `*` with that explanation.
- **Outbound IP**: Settings > Networking shows "Outbound traffic leaves from <public IPs of the
  hosts this service runs on>" with a copy button, so you can allowlist it with partners (a paid
  add-on on Railway).
- **IPv6**: v0.1 is IPv4. AAAA records are not needed; containers have no IPv6.
- **Private domain**: `<name>.internal` (Settings > Networking, editable). Editing it is a staged
  change applied on the next deploy; the old name stays an alias for 24 hours.
- **Console domain**: changeable in Settings > General; the old domain keeps working for 24 hours.
- **Trusted proxies**: rate limits, audit IPs and login lockouts use the client IP from
  `X-Forwarded-For` only when the request came from Caddy on loopback, and Caddy itself trusts
  forwarded headers only from the ranges in Settings > General > Trusted proxies (empty by
  default; a "Cloudflare" preset fills Cloudflare's published ranges).
- **TCP proxy**: picks a free host port in 20000-29999 on every ingress host, shown as
  `<base domain or IP>:<port>`. Database templates do not create one by default; **Public access**
  in their Settings > Networking does.

### 9.12 Volumes (WP1 config, WP2b placement and data, WP3 Docker volumes)

- One volume per service, created from the New menu (pick a service and a mount path), the
  right-click menu **Attach Volume** (mount path), or Settings. It is a Docker named volume
  `5050-<vol id>` under the Docker data root (the data disk when there is one). Mounted at
  container start, root owned; **Run as user** `0` (or `RAILWAY_RUN_UID=0`) fixes permission
  problems, as on Railway.
- **Volume panel** (click the volume tab on a card): name (editable), mount path (staged edit),
  host, usage with the host's free space, "Uses host disk: no fixed size", **Backups** (9.13),
  **Wipe Volume** (type the volume name; stops the service, empties the volume, starts it again;
  audited; immediate) and **Delete volume** (staged, type the name).
- **Size**: Docker named volumes on ext4 cannot be capped, so v0.1 has no size limit and no Live
  Resize (nothing to resize). The volume grows into the host disk. `alert_at_pct` (default 80 of
  the host filesystem, or an absolute GB threshold) fires `volume.high` to webhooks, email and the
  bell. Real caps with XFS project quotas are v0.2.
- Deleting a volume keeps the data for 48 hours (the agent renames it to
  `5050-trash-<id>-<ts>` and a janitor removes it later).

### 9.13 Databases and backups (WP2b, templates in `templates/`)

- **Templates** (section 10) create a service with the official image, a volume, generated
  credentials and Railway-named variables. Memory settings are rendered from the placed host's RAM
  and the service's memory limit.
- **Bucket settings** (Settings > Backups): endpoint, bucket, region, prefix, a **write key**
  (put and get only; it goes into data containers and backup jobs) and an optional **retention
  key** (delete rights; used only by retention jobs run from the control host), path style,
  with **Test connection** (put, get, delete a small object with the right key for each). Until a
  bucket is configured every database shows a "Backups off" badge. The Backups guide explains
  bucket versioning and object lock, so a compromised database or a member with `5050 ssh` cannot
  destroy history.
- **Encryption**: WAL-G uses `WALG_LIBSODIUM_KEY` derived from the recovery key; restic uses the
  per-install restic password in the recovery kit. Backups in the bucket are useless without the
  recovery key.
- **Postgres**: `archive_mode=on`, `archive_timeout=60`, `archive_command='/opt/5050/bin/archive-wal
  %p'`. The script exits 0 without uploading only while backups are not configured; once configured
  it calls `wal-g wal-push` and exits non-zero on failure so Postgres keeps the segment and
  retries. Base backups (`wal-g backup-push`) run on the schedule (default daily 03:00 UTC);
  retention (`wal-g delete retain FULL <n> --confirm`, n from `retainDays`, default 7) runs as a
  control host job with the retention key. Saving bucket settings redeploys data services so
  archiving picks up credentials, then takes a base backup.
- **WAL growth guard**: the agent probe reports `pg_wal` size and the age of the oldest
  unarchived segment. Above 1 GB or 10 minutes of failed archiving, `backup.failed` fires and the
  database card shows "WAL archiving failing for 12 min, pg_wal is 1.4 GB. The volume fills if
  this continues." The Backups guide documents recovery (fix the bucket, archiving catches up; as
  a last resort turn archiving off for that database, which stops point-in-time restore until the
  next base backup). E36 proves recovery with no gap.
- **MySQL**: backups are not in v0.1. The MySQL template shows "Backups not supported yet" and
  the docs show a `mysqldump` cron service recipe.
- **MongoDB**: on schedule, a job runs `mongodump --archive --gzip` piped to `restic backup
  --stdin --stdin-filename dump.archive.gz`, then `restic forget --keep-daily <n>` with the
  retention key; restore uses `mongorestore --archive --gzip` into a copy.
- **Redis**: `redis-cli BGSAVE`, wait until `LASTSAVE` changes and `rdb_bgsave_in_progress` is 0,
  then a job backs up only the completed `dump.rdb` (copied to a temp file first, never the
  live AOF directory) with restic. Restore writes the RDB into a new volume with `appendonly`
  rebuilt on first start.
- **Any other volume**: a job with the volume mounted read-only runs `restic backup /data` (crash
  consistent; the docs say so) and `forget`.
- restic repositories are `s3:<endpoint>/<bucket>/<prefix>/<service id>`.
- **Restore to a copy**: always creates a new service `<name>-restored-YYYYMMDD-HHMM` (in the same
  or another environment, on a host you pick) with a new volume, copies the source's variables,
  runs the template's restore recipe against the new volume (Postgres: `wal-g backup-fetch` plus
  `recovery.signal`, `restore_command`, optional `recovery_target_time`,
  `recovery_target_action=promote`, archiving off in the copy until promoted), then starts it. The
  source is never touched. The UI shows the time it recovered to.
- **Promote**: the restored service takes the original's name (so `${{Postgres.DATABASE_URL}}`
  references resolve to it), the original becomes `<name>-old-<stamp>` and stops, dependents
  redeploy, archiving turns on in the promoted copy and a base backup runs.
- **Major version upgrade**: "Upgrade to a new major version" in the database panel runs a
  dump of the source and a restore into a new copy on the chosen major (Postgres `pg_dumpall` |
  `psql`; MongoDB `mongodump` | `mongorestore`), then offers Promote. Changing the image's major
  version on the existing volume is refused (7.3).
- **Security patches**: the database panel checks weekly for a newer patch of the same major in
  `images.json` releases and shows "Postgres 17.6 is available" with **Update**, a normal
  redeploy (a short gap, 7.3).
- **Backup health**: last good base backup older than 26 hours, archiving failing (above), or a
  failed job sets `stale` and fires `backup.failed` webhooks, email, the bell and activity.
- **Monthly restore test**: on the first Sunday of each month a job restores the newest backup of
  each database into a throwaway container on the database's host, runs the template's check
  (`SELECT count(*) FROM pg_class`, `PING`, `db.stats()`), records `verified_at` and deletes the
  container. A failure fires `backup.failed` ("The monthly restore test failed").
- **Backups tab**: list (time, size, trigger, status, verified), **Back up now**, **Lock**,
  **Delete**, **Edit schedule**, **Restore** (pick a backup or, for Postgres, a point in time).
- **Connect** (button in the database panel header): a modal with the private URL, the public URL
  (when Public access is on), masked until revealed, copy buttons, and the `5050 connect <name>`
  command.
- **Regenerate credentials** (database panel menu, Admin, confirm): sets a new password in the
  database (`ALTER ROLE`, `CONFIG SET requirepass` plus ACL, `db.changeUserPassword`), updates the
  service's variables, and redeploys every service that references them, in one commit.
- **Extensions** (Postgres): the Data tab lists available and installed extensions
  (`pg_available_extensions`) with **Enable** (`CREATE EXTENSION`, audited). `pgvector` and
  `postgis` are available only in their templates' images.
- **Data tab**: tables with row estimates and sizes (Postgres, MySQL), collections with counts
  (MongoDB), connection count, and for Redis a read-only key browser (`SCAN` with a pattern,
  type, TTL, size, and the first 1 KB of a value, revealed per key and audited). There is no free
  query box and no row editing in v0.1 (Coming from Railway says so and points to `5050 connect`).
  Identifiers come from the stored config, never from the request.

### 9.14 GitHub (WP1)

- **Public repos, no app**: a source repo may be `https://github.com/owner/name`. The control plane
  resolves the branch and commit with `git ls-remote`, reads commit details from the public GitHub
  API when reachable, and scans files through the public contents API. When the API answers 403 or
  429 (rate limited), the scan falls back to a shallow `git clone --depth 1 --filter=blob:limit=1m`
  into a temp dir. `autoDeploy` for public repos polls `git ls-remote` every 2 minutes.
- **GitHub App** (private repos and push webhooks): Settings > GitHub > **Create GitHub App** uses
  the GitHub App manifest flow (no copying of keys): name `5050cloud-<install name>`, webhook URL
  `<console>/api/v1/github/webhook`, permissions `contents: read`, `metadata: read`,
  `pull_requests: read`, `checks: read`, events `push`. The callback stores the app id, private key,
  webhook secret and client credentials encrypted. Then **Install** opens GitHub's install page.
  Repo pickers list repos of all installations; branches via the API.
- **Inline connect**: when no app exists, every GitHub repo picker (New project, + Create, Empty
  service Source) shows **Connect GitHub**, which runs the manifest flow and the install in a popup
  and returns to the same picker with its state kept. Pickers also have **Refresh** and a
  **Configure GitHub App** link (opens the installation's settings on GitHub to add repos).
- **Push webhook**: verify `X-Hub-Signature-256`; for each service instance with that repo and
  branch and `autoDeploy` on, apply watch paths (gitignore-style, `**`, `*`, `?`, `!` negation, the
  first push or a force push deploys everything; watch paths from a config file win, 9.20) against
  the changed files (or the compare API when the payload is truncated) and call `Deploy` or record
  a `SKIPPED` deployment.
- **Scan** (`POST /github/scan`, one dialog for every entry point): Dockerfile presence, config
  files (`railway.json`, `railway.toml`, `5050.yaml`, `5050.json`, `nixpacks.toml`) with what they
  set, Railpack provider guess from manifest files, `package.json` start script and engines, JS
  monorepo packages (pnpm/npm/yarn/bun workspaces, skipping lint/config/test packages) to tick,
  variable names from the `.env*` files in 9.3, suggested port from `EXPOSE` or common defaults.
- Login with GitHub is not in v0.1 (section 23).

### 9.15 Auth, roles, tokens, email, notifications, audit, webhooks (WP1)

- **First run**: `/setup?token=` creates the owner. The setup token is single use and expires in
  24 hours; `5050d setup-token` on the host prints a new one.
- **Passwords**: argon2id (m=64 MiB, t=3, p=2), minimum 10 characters. Login is rate limited to
  10 attempts per minute per IP and per email.
- **Email (SMTP)**: Settings > General > Email: host, port, TLS mode (`starttls`, `tls`, `none`),
  user, password, from address, **Send test email**. Without SMTP, email features are hidden and
  invites show a link to copy.
- **Forgot password**: `/forgot` sends a reset link (1 hour, single use, `password_resets`). The
  response never says whether the email exists. Without SMTP the page says "Ask an admin, or run
  `sudo 5050d reset-password --email you@example.com` on the control host", which prints a
  one-time reset link. Resetting revokes the user's sessions.
- **Email verification**: with SMTP set, new users get a verification link; unverified users can
  log in, but email notifications go only to verified addresses.
- **2FA**: TOTP (`pquerna/otp`), set up in the avatar menu > Security with a QR code, confirmed by
  a code, with 10 single-use recovery codes. Login asks for the code after the password. Admins
  can require 2FA for everyone (Settings > Members). `5050d reset-2fa --email` on the host removes
  it.
- **Sessions**: cookie `5050_session` (HttpOnly, Secure, SameSite=Lax, 30 day sliding expiry); the
  server stores only a hash. Every state-changing request authenticated by cookie must send
  `X-5050-CSRF: 1` (the server sends no CORS headers, so other sites cannot add it).
- **API tokens**: `Authorization: Bearer 5050_<40 chars>`, stored as SHA-256. Scope `account`
  acts as the user; scope `environment` can deploy, read logs and set variables in one environment.
- **Roles**: every user has an install role. A project may also grant a user `editor`, `viewer`
  or `none` (Project Settings > Members); the project grant replaces the install role for that
  project, except that Owner and Admin always keep full access. Members default to editor on every
  project unless a grant says otherwise.

| Action | Owner | Admin | Member / project Editor | Viewer / project Viewer |
|---|:-:|:-:|:-:|:-:|
| View projects, canvas, deployments, logs, metrics | yes | yes | yes | yes |
| Reveal variables, `5050 run`, shell, connect | yes | yes | yes | no |
| Create and change services, variables, domains, volumes; deploy, roll back | yes | yes | yes | no |
| Delete services, environments, projects; restore and promote databases; wipe volumes | yes | yes | no | no |
| Hosts, GitHub App, bucket and SMTP settings, members, upgrades, audit log | yes | yes | no | no |
| Transfer ownership, reveal recovery key | yes | no | no | no |

- **Invites**: by email (with SMTP) or as a link with a chosen role, an optional project, a use
  limit and an expiry (default 7 days). **Transfer Ownership** (Owner only, password and 2FA
  re-check) makes another Admin the owner and the old owner an Admin.
- **Public read-only projects** (Project Settings > General > Visibility): anyone with the link
  sees the canvas, statuses and deployment list, never variables, logs or settings.
- **Notifications**: per user, per event (`deployment.failed`, `deployment.crashed`,
  `backup.failed`, `disk.high`, `volume.high`, `monitor.triggered`, `host.offline`), by email
  and in the bell (`notifications`). Defaults: all in-app; email on for Admins for every event,
  for Members on failed and crashed deploys of projects they deployed to.
- **Install notifications** (Settings > General > Alerts): emails and one webhook for platform
  events: `host.offline`, `disk.high`, `disk.critical`, `backup.stale`, `upgrade.rolled_back`,
  `certificate.failing` (an ACME failure for 24 hours), `agent.version_skew`, `host.duplicate`,
  `controlplane.readonly`.
- **Audit**: middleware writes an `audit_log` row for every non-GET API request, every reveal,
  login, failed login, shell or tunnel session, 2FA change and agent join, with the actor, action
  (`<resource>.<verb>` from the route table), target and IP, never values. A test walks the route
  table and fails if a mutating route has no audit action name.
- **Log masking**: values of variables whose name matches
  `(?i)(SECRET|TOKEN|PASSWORD|PASSWD|PASS|KEY|PRIVATE|CREDENTIAL|DSN|AUTH)` or that are sealed, and
  the password part of any URL-valued variable, are replaced by `•••` in build and deploy logs when
  they are at least 6 characters long. `Config.SecretValues` supplies the list.
- **Webhooks**: per project; events listed in `api.Webhook`, plus the preset **Critical events**
  (`deployment.crashed`, `replica.oom_killed`, `volume.high`, `disk.high`, `backup.failed`,
  `monitor.triggered`). Body `{type, timestamp, project, environment, service, deployment,
  details}`; header `X-5050-Signature: sha256=<hex HMAC of the body with the webhook secret>` and
  `X-5050-Event`; 10 s timeout; 3 retries with backoff; **Test** sends a sample. Slack and Discord
  formatting is v0.2 (signed JSON only).
- **SSRF guard**: webhook deliveries, the backup endpoint test and GitHub public API fetches use a
  dialer that resolves the name and checks the address at connect time (so DNS rebinding cannot
  slip through), refusing loopback, link-local, metadata, private and pod/mesh ranges unless the
  owner allowed private destinations in settings.

### 9.16 Upgrades and maintenance (WP6 `internal/upgrade`, triggered by WP2a)

- `5050 upgrade` (CLI), the **Upgrade** button (Settings > Upgrades), or `sudo 5050d upgrade
  [--version vX.Y.Z]` on the host.
- Steps: resolve the release and verify `checksums.txt.minisig` with the embedded minisign public
  key(s) and the tarball SHA-256; take a state snapshot (9.17); unpack to
  `/opt/5050/releases/<new>`; launch a detached helper (`systemd-run --unit 5050-upgrade
  /opt/5050/releases/<new>/5050d upgrade-helper ...`) that swaps `/opt/5050/current`, restarts
  `5050d`, and polls `http://127.0.0.1:5050/readyz` for up to 90 s. Ready means: the version equals
  the target, migrations applied, the agent hub accepting, and at least as many agents connected as
  before (within the 90 s).
- On failure the helper restores the symlink, restores the state snapshot if the schema version
  changed (in adopt mode, 9.17.1, so nothing deployed in between is removed), restarts the old
  version and writes `rolled_back` with the reason to `upgrade.json`; `upgrade.rolled_back` fires.
- **Agents** are upgraded after the control plane, one host at a time, with `proto.Upgrade`. The
  agent stages the binary, writes a pending marker and restarts through systemd; the new agent
  reverts to the old binary and restarts if it cannot connect within 90 s, and reports the failed
  version. E29 proves this with a build tagged `e2e_broken_agent`.
- **Version skew**: agents one minor version behind the control plane keep working. An agent newer
  than the control plane (after a manual downgrade) speaks the protocol version the control plane
  advertises in `hello_ack` and ignores fields it does not get; more than one minor apart in either
  direction raises `agent.version_skew` and the Hosts page offers "Upgrade agent".
- **Infrastructure images** (Caddy, registry, BuildKit) are pinned per release. An upgrade that
  changes a pin replaces the container: BuildKit between builds, the registry with the build queue
  paused, Caddy one ingress host at a time (stop, start with `--resume`; about 2 s without 80/443
  on that host, so a single-ingress install has a 2 s gap; the release notes say when a release
  changes the Caddy pin).
- **Host maintenance** (Hosts > host menu > **Maintenance**, or `5050 host maintenance HOST`):
  reschedules volume-less replicas elsewhere, then stops the host's remaining services in reverse
  dependency order (apps before the databases they reference) with their stop grace, and marks the
  host `maintenance` so you can upgrade Docker or the kernel and reboot. **End maintenance** starts
  them in dependency order. The Upgrading guide shows rolling OS reboots across hosts.
- **Release signing keys**: `install.sh` and `internal/upgrade` embed the current minisign key and
  accept a next key once announced. Rotation: a release signed by the current key ships the next
  key; the release after is signed by the next key. On key compromise: a security advisory, a new
  installer with only the new key, and `5050 upgrade --pubkey <new>` documented in SECURITY.md.
- The console shows "Update available: vX.Y.Z" when the latest release is newer (checked daily;
  `--no-update-check` disables it).

### 9.17 Control-plane state backup, replication and restore (WP1)

- **Continuous**: when a bucket is configured, `5050d` runs Litestream (pinned) as a child process
  that streams `state.db` to `s3://<bucket>/<prefix>/_state/litestream/` with the write key. RPO is
  about 1 second while the control host is up. The Backups settings page shows the last replicated
  position and lag.
- **Hourly**: `VACUUM INTO` a temp file, encrypt with `filippo.io/age` using the recovery key as an
  scrypt passphrase, write `/var/lib/5050/backups/state-<ts>.db.age`, keep 24 hourly and 7 daily,
  and upload to `<bucket>/<prefix>/_state/` when a bucket is configured (kept 30 days). This is a
  second layer; without a bucket it is the only one, so the docs state an RPO of 1 hour on a single
  disk without a bucket.
- The **recovery key** is `5050R-` plus the base32 master key in groups. It decrypts state backups
  and, being the master key, every secret in them. The installer prints it only on a TTY and always
  writes it to the recovery key file (4.1).
- **Rotate the master key**: `sudo 5050d rotate-master-key` generates a new key, re-encrypts every
  `_enc` column and the encrypted pushes in one transaction, writes the new recovery key to a 0600
  file and prints its path, and takes a fresh snapshot. Older snapshots stay readable with the old
  key (the docs say to keep it until they expire).
- `sudo 5050d restore-state --from <file | s3://bucket/key | litestream> --recovery-key <key>
  [--s3-endpoint --s3-region --s3-access-key --s3-secret-key]` restores `state.db` (the S3 flags
  or `FIFTY_S3_*` variables are needed because the bucket credentials are inside the encrypted
  state), rewrites `master.key`, sets a new cluster epoch and starts in adopt mode.
- `5050d backup-state --out FILE` makes an on-demand snapshot.

#### 9.17.1 Adopt mode after a restore

Agents keep running whatever they ran when the control plane lost its memory, and a snapshot can
be up to an hour old (or seconds with Litestream). After `restore-state` (and after an upgrade
rollback that restored a snapshot) the control plane:

1. Sets a new epoch; every `desired_state` it sends carries `Epoch` and `Adopt: true`, so agents
   accept a lower generation but remove nothing.
2. On `hello`, sets `generation = max(stored, Hello.Applied) + 1` and imports every replica the
   agent reports. Replicas it knows are matched; replicas it does not know (deployed after the
   snapshot) go to `o_unknown_replicas` and keep running.
3. Hosts that joined after the snapshot have tokens the restored state does not know. Their
   connect is answered with `unknown_host`, and the Hosts page lists them under "Hosts not in the
   restored state" with **Re-adopt** (an Admin confirms; the host gets a new token over the open
   socket) or **Remove**.
4. The canvas shows a banner "Restored from a backup taken at <time>. N containers are running that
   this backup does not know about." with **Review**: per container **Adopt** (creates the service
   and deployment records from its labels and config snapshot when the service exists, or keeps it
   as an unmanaged replica) or **Remove**. Adopt mode ends when the list is empty or an Admin
   presses **Finish restore**; only then are removals allowed.

#### 9.17.2 Rebuild a dead control host on a new VM

1. On a new VM, run the installer with `--restore s3://bucket/prefix --recovery-key <key>` plus the
   S3 flags (or install normally, then `restore-state`). The installer skips creating a new host
   row, master key and setup token, restores the state (Litestream first, else the newest hourly
   snapshot), and the new local agent **re-adopts host index 1** (`5050-agent join --replace
   hst_...` with a one-time token printed by `restore-state`), taking over its mesh address
   `100.96.0.1` and registry address. The old host record's machine-id and WireGuard key are
   replaced.
2. Other agents keep their tokens. They reconnect through the console domain (point its DNS at the
   new IP, or use a domain with a short TTL; the docs explain both) and get the new WireGuard peer
   and registry endpoint in their next desired state.
3. Services whose volumes were on the dead host switch to **Host lost**: "This volume was on a host
   that is gone. Restore from backup." with **Restore to a copy** onto a host you pick (9.13), then
   Promote.
4. The registry is re-seeded: from the bucket when image storage is in the bucket (9.6), else
   services show "Image missing: Redeploy from commit" and app hosts keep running the images they
   already have locally.
5. E28 runs this whole procedure.

### 9.18 Failure diagnosis catalog (WP2a assembles, WP3 and WP4 detect)

Exactly one sentence, shown above the logs and in the CLI. Each row has a unit test with a
captured fixture.

| Situation | Sentence |
|---|---|
| Container listens on other ports than the target | "Your app listens on 3000 but the target port is 8080. Set PORT=8080 in your app or change the target port to 3000." |
| Listens on 127.0.0.1 only | "Your app listens on 127.0.0.1:3000, which is unreachable from outside the container. Listen on 0.0.0.0." |
| Health check status | "Health check /health returned 404. Check the path in Settings > Deploy." |
| Health check timeout | "Nothing answered on port 8080 within 300 s. Make sure the app starts and listens on PORT." |
| OOM | "Out of memory at 512 MB (limit). Raise the memory limit or lower Processes." |
| Exits right after start | "The app exited with code 1 right after starting: <last stderr line>." |
| No start command | "No start command was found. Set one in Settings > Deploy or add a start script." |
| Image pull: not found / unauthorized | "Image nginx:1.99 was not found." / "The registry refused access to ghcr.io/acme/app. Add registry credentials in Settings > Source." |
| Railpack: nothing detected | "Railpack could not detect a language in /. Set the root directory or add a Dockerfile." |
| Node engine mismatch | "The build needs Node >= 20 (package.json engines). Set NODE_VERSION or update engines." |
| Missing lockfile for `npm ci` | "npm ci needs a package-lock.json. Commit one or set a custom install command." |
| Dockerfile missing | "No Dockerfile at Dockerfile. Check the Dockerfile path in Settings > Build." |
| Config file invalid | "railway.json line 7: unknown field buildComand. Did you mean buildCommand?" |
| Disk full | "The build host ran out of disk. Free space or add a data disk." |
| Build timeout | "The build took longer than 30 minutes and was stopped. Raise the build timeout in Settings > Build." |
| Pre-deploy failed / timed out | "Pre-deploy command exited with code 1: <line>." / "Pre-deploy command timed out after 300 seconds." |
| Volume plus replicas | "A service with a volume can only run 1 replica." (validation, before deploy) |
| No eligible host | "No online host has the roles app. Add a host or change placement." |
| No build host | "No online host has the role build. Bring one online or give a host the build role." |
| No capacity | "No host has 512 MB free for this replica. Free memory, lower the reservation or add a host." |
| No network slot | "No network slot left on host X: it already runs 254 environments." |
| Volume host lost | "This volume was on a host that is gone. Restore it from a backup." |
| Major version change | "Major version changes need a dump and restore. Use Upgrade to a new major version in the database panel." |

### 9.19 Disk, log and image retention (WP2a, WP2b, WP3)

Host image retention 3 per service; registry retention 10 images per service plus one cache ref;
registry size cap (6.6); BuildKit cache GC at 20 GB; prune before builds above 80% disk; disk
alerts at 75% and 85%, ballast at 95% (6.6); Docker json logs capped at 20 MB x 3 per container;
agent log store 7 days **and** at most 1 GB per service per host and 10% of the filesystem per host
(oldest hourly files deleted first, and `disk.high` names the service using most); `o_host_logs`
5 MB per host; uploads deleted 1 hour after their build.

### 9.20 Config as code (WP1 merges, WP4 reads, `pkg/configfile`)

#### 9.20.1 Files and priority

At build time WP4 looks in the service's root directory (or **Config file path**, Settings >
Source) for, in this order, the first that exists: `5050.yaml`, `5050.json`, `railway.json`,
`railway.toml`. `pkg/configfile` parses it into `manifest.RailwayConfig` (Railway's schema:
`build.builder`, `build.buildCommand`, `build.dockerfilePath`, `build.watchPatterns`,
`build.railpackVersion`, `deploy.startCommand`, `deploy.preDeployCommand`,
`deploy.healthcheckPath`, `deploy.healthcheckTimeout`, `deploy.restartPolicyType`,
`deploy.restartPolicyMaxRetries`, `deploy.cronSchedule`, `deploy.numReplicas`,
`deploy.overlapSeconds`, `deploy.drainingSeconds`, `deploy.sleepApplication` (ignored with a
notice), `deploy.multiRegionConfig` (replicas only), and `environments.<name>` blocks with the same
fields). Our own files use the same schema plus our fields (`deploy.processes`,
`deploy.placement`, `deploy.singleton`).

Effective config, highest priority first (Railway's order): the `environments.<current env>`
block of the file, then the file's base fields, then the service settings in the UI. The merge
(`Config.MergeConfigFile`) happens per deployment; the snapshot records the merged result and the
file's commit and path. Watch patterns from the file apply to the next push.

**In the UI**: every field set by the file shows the file icon next to it, disabled for editing,
with a hover "Set by railway.json line 12: "startCommand": "node server.js"" and a link to the
file on GitHub. The Details tab of a deployment lists which fields came from the file. Unknown
fields fail the build (9.18); fields we know but do not support produce one notice line each in
the build log, for example "railway.json: sleepApplication is not supported in 5050cloud v0.1 and
was ignored."

#### 9.20.2 Nixpacks and Railpack hints

- `builder: NIXPACKS` builds with Railpack (8.2 step 3) and logs the notice.
- `nixpacks.toml`: `[start] cmd` becomes the start command and `[phases.build] cmds` the build
  command when those are not set elsewhere; `[variables]` become build-time variables;
  `[phases.setup] nixPkgs`, `aptPkgs` map to `RAILPACK_PACKAGES` and `RAILPACK_DEPLOY_APT_PACKAGES`.
  Everything else (custom phases, `providers`, `nixLibs`, `[staticAssets]`) is ignored with one
  log line naming each ignored key.
- `NIXPACKS_BUILD_CMD`, `NIXPACKS_START_CMD`, `NIXPACKS_INSTALL_CMD` map to the Railpack
  equivalents; `NIXPACKS_NODE_VERSION`, `NIXPACKS_PYTHON_VERSION` map to `RAILPACK_*` version
  variables. Other `NIXPACKS_*` variables are listed as ignored.
- `railpack.json` in the repo is passed to Railpack unchanged.

### 9.21 Monitors (WP2a evaluates, WP1 delivers)

- Per service (Metrics tab > **Monitors**, or Settings > Monitors): metric `cpu` (% of limit or
  of host), `memory` (% of limit), `disk` (volume % of host filesystem, or absolute GB), or
  `http_5xx_rate` (% of requests), a threshold, a duration N minutes (1 to 60), and channels
  (email to chosen members, any project webhook). The orchestrator evaluates every minute from the
  30 s points; a monitor fires once on entering the alarm state and once on recovery
  (`monitor.triggered`, `monitor.resolved`), and shows a red dot on the card while firing.
- New Postgres, MySQL, Redis and MongoDB services get a default disk monitor at 80%.

### 9.22 Imports (WP1)

- **Import from Railway** (`5050 import railway --token <project token>` or **New > Import from
  Railway**, which asks for the token): reads the project through Railway's public GraphQL API
  (`https://backboard.railway.com/graphql/v2`) with a project token: environments, services,
  sources (repo, branch, root directory, image), service settings (builder, build and start
  commands, health check, restart policy, replicas, cron, watch paths, pre-deploy), variables
  (non-sealed values; sealed ones arrive with empty values flagged "Set a value"), shared
  variables, custom and generated domains (generated ones become new generated domains here),
  volumes (mount paths, not data), TCP proxies and databases (recognised by their template image
  and recreated from our templates, keeping their variable names). Everything lands as staged
  changes in a new project, with an import report (`imports.report`): what landed, what was
  skipped and why. Nothing deploys until you press Deploy. The token is used once and never
  stored.
- **Database data**: the "Move a database from Railway" guide pipes `pg_dump` from Railway's
  public URL into `5050 connect Postgres` (`pg_dump "$RAILWAY_URL" | 5050 connect Postgres --
  psql`), with the same for MySQL, Redis (`redis-cli --rdb`) and MongoDB.
- **Docker Compose**: drag a `docker-compose.yml` onto the canvas (or **New > Docker Compose**,
  or `5050 import compose FILE`). `pkg/compose` maps `services.<name>.image` or `build` (context
  and dockerfile, needs a repo), `command`, `environment`/`env_file` contents pasted in,
  `ports` (first one becomes the target port; published ports become a generated domain or a TCP
  proxy suggestion), `volumes` (named volumes become volumes; bind mounts are skipped with a
  reason), `depends_on` (deploy order), `deploy.replicas`, `healthcheck` (HTTP path guessed when
  it curls one) and `restart`. Well-known database images become our templates. Unsupported keys
  are listed in the report. Everything is staged.
- **Heroku variables**: 9.3.

### 9.23 Usage (WP2a computes, WP1 serves)

Project Settings > **Usage**: per service and in total for the chosen period (this month, last
month, last 30 days, custom), vCPU hours, memory GB hours, egress GB (from network tx of
containers to non-pod addresses), volume GB months, and backup bucket bytes. Below the table:
"The same usage on Railway at list prices of <date> would cost about $X", computed from the
prices in `site/data/railway-prices.json` (dated, sourced, also used by the landing page), with
a link to the source. Install-wide usage per host is under Settings > Hosts.

### 9.24 Activity, notifications and presence (WP1)

- **Activity panel** (top bar, and **Activity** inside a service panel, scoped to that service):
  every commit (message, author, services, time) and deployment (status badge, trigger), plus
  domain, backup, host, alert and member events, filterable by service and kind, infinite scroll.
  Clicking a commit opens its full diff (as the Details modal showed it, secrets masked) and
  message.
- **Bell**: the user's `notifications` (crashes, failures, backup, disk and monitor alerts,
  host offline), unread count, mark read, links to the place.
- **Presence**: every console client on an environment sends a heartbeat every 15 s
  (`POST /environments/{eid}/presence`); the events stream carries `presence` events, and the
  canvas shows avatars of the other viewers. Card moves are saved and broadcast as `positions`
  events, so everyone's canvas updates live.

---

## 10. Templates (WP2b, `templates/`)

Each template is `templates/<id>/template.yaml` (`manifest.Template`) plus its files and icon.
`templates/tools.json` pins helper binaries `[{name, version, urls: {amd64, arm64}, sha256:
{amd64, arm64}, path}]` for `wal-g`, `restic` and `litestream`. Images are official upstream
images pinned to an exact tag, with digests in `templates/images.json`. All YAML goes through
`sigs.k8s.io/yaml` (camelCase field names).

### 10.1 Databases (the **Database** menu)

| Template | Image | Volume | Variables (Railway names) | Backups |
|---|---|---|---|---|
| `postgres` (**Postgres**) | `postgres:17.x-bookworm` (18 selectable) | `/var/lib/postgresql/data` | `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `DATABASE_URL`, `DATABASE_PUBLIC_URL` (when public access is on) | WAL-G continuous + base, point-in-time restore to a copy |
| `mysql` (**MySQL**) | `mysql:8.4.x` | `/var/lib/mysql` | `MYSQLHOST`, `MYSQLPORT`, `MYSQLUSER`, `MYSQLPASSWORD`, `MYSQLDATABASE`, `MYSQL_ROOT_PASSWORD`, `MYSQL_URL`, `MYSQL_PUBLIC_URL` | not in v0.1 (badge) |
| `redis` (**Redis**) | `redis:8.x` with `--appendonly yes --requirepass` and `maxmemory-policy noeviction` | `/data` | `REDISHOST`, `REDISPORT`, `REDISUSER`, `REDISPASSWORD`, `REDIS_URL`, `REDIS_PUBLIC_URL` | restic of a completed RDB |
| `mongodb` (**MongoDB**) | `mongo:8.0.x` | `/data/db` | `MONGOHOST`, `MONGOPORT`, `MONGOUSER`, `MONGOPASSWORD`, `MONGO_INITDB_ROOT_USERNAME`, `MONGO_INITDB_ROOT_PASSWORD`, `MONGO_URL`, `MONGO_PUBLIC_URL` | `mongodump` archive with restic |

Rules:

- Passwords use `${{secret(32)}}`, evaluated once at creation.
- Postgres memory: `shared_buffers` = 25% of the memory limit (or of host RAM / 4 when unlimited),
  `effective_cache_size` = 75% of the same, `work_mem` 4 MB to 64 MB scaled, `max_connections`
  100; also `pg_stat_statements`, `log_min_duration_statement=1000`, `shm_size` 256 MB, stop grace
  120 s, health check `pg_isready` (TCP plus a `pg_isready` exec probe), and a metrics probe
  script for 9.10.
- MySQL, Redis and MongoDB get stop grace 60 s. MongoDB gets `--wiredTigerCacheSizeGB` from the
  limit.
- Each template has `data:` set so the Data tab and `5050 connect` know the client (`psql`,
  `mysql`, `redis-cli`, `mongosh`).
- Template creation is immediate (not staged) and deploys at once, as on Railway.
- Templates must be re-runnable: provisioning scripts create roles only if missing.
- `MajorVersions` lists selectable majors; changing major on an existing volume is refused (7.3).

### 10.2 App templates (the **Template** menu)

Twelve templates ship in v0.1, each with a manifest, an icon, a one-line tagline, a docs link and
pinned images. Multi-service templates set `DependsOn`, so they deploy in order (9.1).

| Id | Services | Required inputs (generated ones prefilled and editable) |
|---|---|---|
| `n8n` | n8n + Postgres | `N8N_ENCRYPTION_KEY` (`secret(32)`) |
| `umami` | Umami + Postgres | `APP_SECRET` (`secret(32)`) |
| `plausible` | Plausible CE + Postgres + ClickHouse | `SECRET_KEY_BASE` (`secret(64)`), `TOTP_VAULT_KEY` (`secret(32)` base64) |
| `ghost` | Ghost + MySQL | `url` (prefilled with the generated domain) |
| `uptime-kuma` | Uptime Kuma (volume) | none |
| `metabase` | Metabase + Postgres | `MB_ENCRYPTION_SECRET_KEY` (`secret(32)`) |
| `minio` | MinIO (volume, console domain) | `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD` (`secret(32)`) |
| `pgvector` | Postgres with `pgvector` (the `postgres` rules, `pgvector/pgvector` image) | none |
| `postgis` | Postgres with PostGIS (`postgis/postgis` image) | none |
| `gitea` | Gitea (volume) + Postgres | `GITEA__security__SECRET_KEY` (`secret(32)`) |
| `meilisearch` | Meilisearch (volume) | `MEILI_MASTER_KEY` (`secret(32)`) |
| `directus` | Directus + Postgres + Redis | `SECRET` (`secret(32)`), admin email, admin password (`secret(16)`) |

- **Template detail** (`/new/template/<id>`, also opened from the Template menu): name, tagline,
  icon, docs link, the services it creates (with images, volumes and domains), the required
  variables form (generated values prefilled, editable, with a regenerate button; required fields
  block **Deploy** until filled), the environment to deploy into, and **Deploy**. Deploy creates
  the services and deploys them in dependency order, then opens the canvas.
- **Deep link and button**: `https://<console>/new/template/<id>?KEY=value` opens the detail with
  inputs prefilled. The docs give a **Deploy on 5050cloud** README button
  (`https://5050cloud.com/deploy?template=<id>` asks for your console URL once and remembers it in
  the browser, then redirects).
- Every template is covered by a golden render test (1 GB, 4 GB, 32 GB hosts) and by E-suite
  smoke runs of `n8n` and `uptime-kuma`; U19 deploys `umami` through the UI.

---

## 11. REST API (WP1 serves; WP2a and WP2b behind it; types in `pkg/api`)

Base path `/api/v1`. JSON in and out. Auth by session cookie (plus `X-5050-CSRF: 1` on mutating
requests) or `Authorization: Bearer`. Role column: minimum role (`-` public, `V`iewer, `M`ember or
project editor, `A`dmin, `O`wner). `{eid}` = environment id, `{sid}` = service id. Lists are
newest first.

### 11.1 System, auth, users

| Method | Path | Body -> Response | Role |
|---|---|---|---|
| GET | `/healthz` (no prefix) | 200 "ok" when the process runs | - |
| GET | `/readyz` (no prefix) | `{version, ready, readOnly, checks{db,migrations,hub,agents}}` | - (localhost gets details) |
| GET | `/version` | `{version, commit, date}` | - |
| GET | `/setup` | `SetupStatus` | - |
| POST | `/setup` | `SetupRequest` -> `Me` (sets cookie) | - (token) |
| POST | `/auth/login` | `LoginRequest` -> `Me` or `{needTotp: true, ticket}` | - |
| POST | `/auth/login/totp` | `{ticket, code}` -> `Me` | - |
| POST | `/auth/logout` | -> 204 | V |
| POST | `/auth/forgot` | `{email}` -> 204 (always) | - |
| POST | `/auth/reset` | `{token, password}` -> `Me` | - |
| POST | `/auth/verify-email` | `{token}` -> 204 | - |
| GET | `/me` | `Me` | V |
| PATCH | `/me` | `{name?, password?, currentPassword?}` -> `User` | V |
| POST | `/me/2fa/setup`, `/me/2fa/enable`, `/me/2fa/disable` | -> `{secret, otpauthUrl}` / `{code}` -> `{recoveryCodes}` / `{password, code}` -> 204 | V |
| GET, PUT | `/me/notification-prefs` | `[]NotificationPref` | V |
| GET | `/me/notifications?before=` | `Page[Notification]` | V |
| POST | `/me/notifications/read` | `{ids[] or all}` -> 204 | V |
| POST | `/cli/auth/start` | -> `CLIAuthStart` | - |
| POST | `/cli/auth/approve` | `{code}` -> 204 | V |
| POST | `/cli/auth/poll` | `CLIAuthPoll` -> `CLIAuthPollResponse` | - |
| GET, POST | `/tokens` | `CreateTokenRequest` -> `CreateTokenResponse` | V (own) |
| DELETE | `/tokens/{id}` | -> 204 | V (own) |
| GET | `/members` | `[]User` | V |
| GET, POST | `/invites` | `{role, email?, projectId?, maxUses?, expiresInHours?}` -> `Invite` (with link) | A |
| DELETE | `/invites/{id}` | -> 204 | A |
| GET | `/invites/{token}` | `InvitePreview` | - |
| POST | `/invites/accept` | `{token, email, name, password}` -> `Me` | - |
| PATCH, DELETE | `/members/{id}` | `{role}` -> `User` | A (owner changes: O) |
| POST | `/members/{id}/transfer-ownership` | `{password, code?}` -> 204 | O |
| GET | `/audit?before=&limit=` | `Page[AuditEntry]` | A |

### 11.2 Install, settings, hosts, upgrades

| Method | Path | Body -> Response | Role |
|---|---|---|---|
| GET | `/install` | `Install` | V |
| PATCH | `/install` | `UpdateInstallRequest` -> `Install` (name, base domain, console domain, ACME email, trusted proxies, registry cap, build timeout default) | A |
| GET, PUT | `/settings/backups` | `BackupSettings` (secrets write-only) | A |
| POST | `/settings/backups/test` | -> `{ok, error?}` | A |
| GET, PUT | `/settings/smtp` | `SMTPSettings` (password write-only) | A |
| POST | `/settings/smtp/test` | `{to}` -> `{ok, error?}` | A |
| GET, PUT | `/settings/alerts` | `InstallNotifications` | A |
| POST | `/settings/recovery-key` | `{password}` -> `{recoveryKey}` (audited) | O |
| GET | `/hosts` | `[]Host` (with conflicts, reachability, usage) | V |
| POST | `/hosts/join-tokens` | `CreateJoinTokenRequest` -> `JoinToken` | A |
| PATCH | `/hosts/{id}` | `UpdateHostRequest` -> `Host` (name, roles, drain, reserve) | A |
| POST | `/hosts/{id}/maintenance` | `{on}` -> `Host` | A |
| POST | `/hosts/{id}/readopt`; DELETE `/hosts/{id}/conflicts` | -> `Host` (9.17.1, 7.1) | A |
| DELETE | `/hosts/{id}` | -> 204 (refused while it runs replicas, unless `?force=1`) | A |
| GET | `/hosts/{id}/metrics?range=` | `MetricsResponse` | V |
| POST | `/upgrade` | `UpgradeRequest` -> `UpgradeStatus` | A |
| GET | `/upgrade` | `UpgradeStatus` | V |
| GET, POST | `/registry-credentials` | `{name, server, username, password}` -> `RegistryCredential` | M |
| DELETE | `/registry-credentials/{id}` | -> 204 (refused while a service uses it) | M |
| GET | `/restore/unknown-replicas` | `[]UnknownReplica` | A |
| POST | `/restore/unknown-replicas/{id}/adopt`, `/remove`; POST `/restore/finish` | -> 204 | A |

### 11.3 Projects, environments, canvas, changes

| Method | Path | Body -> Response | Role |
|---|---|---|---|
| GET | `/projects?sort=activity\|created\|name` | `[]Project` with `summary` and mini canvas | V |
| POST | `/projects` | `CreateProjectRequest` (name optional: a two-word name is generated) -> `Project` (with `production`) | M |
| GET, PATCH | `/projects/{pid}` | `UpdateProjectRequest` (name, description, publicRead) -> `Project` | V / M (publicRead: A) |
| DELETE | `/projects/{pid}` | `{confirm: "<name>"}` -> 204 (stops everything, deletes volumes after 48 h) | A |
| GET, PUT | `/projects/{pid}/members` | `[]ProjectGrant` | V / A |
| GET | `/projects/{pid}/usage?from=&until=` | `UsageResponse` | V |
| POST | `/projects/{pid}/environments` | `CreateEnvironmentRequest` -> `Environment` | M |
| PATCH | `/environments/{eid}` | `UpdateEnvironmentRequest` (name, egress) -> `Environment` | M (egress: A) |
| DELETE | `/environments/{eid}` | `{confirm}` -> 204 (not the last one) | A |
| POST | `/environments/{eid}/sync` | `{from: eid}` -> `ChangesResponse` (staged, with tags) | M |
| GET | `/environments/{eid}/canvas` | `Canvas` | V |
| PUT | `/environments/{eid}/positions` | `SavePositionsRequest` -> 204 (broadcast) | M |
| PUT | `/environments/{eid}/groups` | `[]Group` -> `[]Group` | M |
| POST | `/environments/{eid}/presence` | `{cursor?}` -> 204 | V |
| GET | `/environments/{eid}/changes` | `ChangesResponse` | V |
| DELETE | `/environments/{eid}/changes` and `/changes/{cid}` | -> 204 | M |
| POST | `/environments/{eid}/changes/commit` | `CommitRequest` -> `CommitResponse` | M (deletes in the set: A) |
| GET | `/environments/{eid}/activity?service=&kind=&before=` | `Page[Activity]` | V |
| GET | `/commits/{id}` | `CommitDetail` (message, author, full diff, secrets masked) | V |
| GET | `/events?environment={eid}` | SSE stream of `Event` (status, steps, positions, presence, changes) | V |
| GET, PUT | `/environments/{eid}/shared-variables` | `VariablesResponse` / `SetVariablesRequest` (staged) | V / M |
| POST | `/environments/{eid}/shared-variables/reveal` | `RevealRequest` -> `RevealResponse` | M |
| POST | `/environments/{eid}/shared-variables/share` | `{name, services[]}` -> 204 (staged) | M |
| GET | `/environments/{eid}/export` | `5050.yaml` text (`manifest.ProjectFile`) | V |
| POST | `/imports/railway` | `{token, projectName?}` -> `Import` (creates a project with staged changes) | M |
| POST | `/environments/{eid}/imports/compose` | YAML body -> `Import` (staged) | M |
| POST | `/environments/{eid}/services/{sid}/imports/heroku-env` | text body -> `Import` (staged) | M |
| GET | `/imports/{id}` | `Import` with report | M |
| GET | `/search?q=` | `SearchResult` (projects, services, environments, settings pages, deployments by commit message) for the palette | V |

### 11.4 Services

| Method | Path | Body -> Response | Role |
|---|---|---|---|
| POST | `/environments/{eid}/services` | `CreateServiceRequest` -> `CreateServiceResponse` | M |
| GET | `/environments/{eid}/services/{sid}` | `Service` (config with `source` map for file-set fields) | V |
| PATCH | `/environments/{eid}/services/{sid}/config` | `UpdateServiceConfigRequest` -> `Service` (staged) | M |
| PATCH | `/environments/{eid}/services/{sid}/info` | `{name?, icon?}` -> `Service` (rename staged, icon immediate) | M |
| POST | `/environments/{eid}/services/{sid}/duplicate` | -> `Service` (staged) | M |
| DELETE | `/environments/{eid}/services/{sid}` | `{confirm}` -> 204 (staged) | A |
| GET | `.../services/{sid}/variables` | `VariablesResponse` | V |
| PUT | `.../services/{sid}/variables` | `SetVariablesRequest` -> `VariablesResponse` (staged) | M |
| POST | `.../services/{sid}/variables/reveal` | `RevealRequest` -> `RevealResponse` (audited) | M |
| GET | `.../services/{sid}/deployments?limit=&q=` | `[]Deployment` | V |
| POST | `.../services/{sid}/deploy` | `DeployRequest` -> `Deployment` ("Deploy latest commit") | M |
| POST | `.../services/{sid}/uploads` | gzip tar body (max 500 MB), `?message=` -> `UploadResponse` | M |
| GET | `/deployments/{did}` | `DeploymentDetail` | V |
| POST | `/deployments/{did}/redeploy`, `/rollback`, `/restart`, `/abort`, `/remove` | -> `Deployment` | M |
| POST | `/replicas/{rid}/move` | `{hostId?}` -> 204 (singleton move after confirm, 7.1) | A |
| GET | `/deployments/{did}/build-logs?follow=1` | `LogsResponse` or SSE | V |
| GET | `/logs?environment=&services=&deployment=&type=deploy,build,http&q=&since=&until=&limit=&around=` | `LogsResponse` (`around` = line id for View in Context) | V |
| GET | `/logs/stream?...same` | SSE of `[]LogLine` | V |
| GET | `.../services/{sid}/metrics?range=&from=&until=&view=sum\|replica` | `MetricsResponse` | V |
| GET | `.../services/{sid}/db-metrics?range=` | `DBMetricsResponse` (Postgres) | V |
| GET, POST | `.../services/{sid}/monitors`; PATCH, DELETE `/monitors/{id}` | `Monitor` | V / M |
| GET, POST | `.../services/{sid}/domains` | `CreateDomainRequest` -> `Domain` (immediate) | M |
| PATCH, DELETE | `/domains/{id}` | `{port}` -> `Domain` | M |
| POST | `/domains/{id}/check`, `/domains/{id}/retry-certificate` | -> `Domain` | M |
| POST, DELETE | `.../services/{sid}/tcp-proxies[/{id}]` | `{targetPort}` -> `TCPProxy` | M |
| POST | `.../services/{sid}/volume` | `CreateVolumeRequest` -> `Volume` (staged) | M |
| GET, PATCH, DELETE | `.../services/{sid}/volume` | `VolumeDetail` / `{name?, mountPath?, alertAtPct?}` / `{confirm}` (staged) | V / M / A |
| POST | `.../services/{sid}/volume/wipe` | `{confirm}` -> 204 (immediate, audited) | A |
| GET | `.../services/{sid}/backups` | `BackupsResponse` | V |
| POST | `.../services/{sid}/backups` | -> `Backup` (now) | M |
| PUT | `.../services/{sid}/backups/schedule` | `BackupSchedule` | M |
| POST | `/backups/{id}/lock`, DELETE `/backups/{id}` | `{locked}` | M / A |
| POST | `.../services/{sid}/restore` | `RestoreRequest` (backup or time, target environment and host) -> `{serviceId, deployment}` | A |
| POST | `.../services/{sid}/promote` | `PromoteRequest` -> 204 | A |
| POST | `.../services/{sid}/major-upgrade` | `{version}` -> `{serviceId, deployment}` | A |
| GET | `.../services/{sid}/connect-info` | `ConnectInfo` (masked) | M |
| POST | `.../services/{sid}/regenerate-credentials` | -> `CommitResponse` | A |
| GET | `.../services/{sid}/data` | `DataResponse` (tables, collections, sizes) | M |
| GET, POST | `.../services/{sid}/extensions` | `[]Extension` / `{name}` -> `Extension` (audited) | M / A |
| GET | `.../services/{sid}/redis/keys?match=&cursor=` | `RedisKeysPage` | M |
| POST | `.../services/{sid}/redis/keys/reveal` | `{key}` -> `{value}` (first 1 KB, audited) | M |
| GET | `.../services/{sid}/cron-runs`; POST `.../cron-runs` | `[]CronRun` / run now | V / M |
| GET (WebSocket) | `.../services/{sid}/shell?replica=&cols=&rows=&session=` | binary terminal stream; text frames `{"resize":[c,r]}` and `{"exit":code}`; `session` reattaches a tmux-style session kept for 10 min | M |
| GET (WebSocket) | `.../services/{sid}/tunnel?port=` | binary raw TCP stream | M |

### 11.5 Templates, GitHub, webhooks

| Method | Path | Body -> Response | Role |
|---|---|---|---|
| GET | `/templates?q=` | `[]TemplateSummary` | V |
| GET | `/templates/{id}` | `TemplateDetail` (services, inputs with generated defaults) | V |
| POST | `/environments/{eid}/templates/{id}/deploy` | `{inputs}` -> `TemplateDeployResponse` | M |
| GET | `/icons/{name}` | SVG/PNG icons for templates, sources and the devicon picker | - |
| GET | `/github` | `GitHubStatus` | V |
| POST | `/github/app` | `{owner?: "org", returnTo?}` -> `{formUrl, manifest}` | A |
| GET | `/github/app/callback?code=` | redirect to `returnTo` or Settings > GitHub | A |
| GET | `/github/repos?q=&refresh=1` | `[]GitHubRepo` | M |
| GET | `/github/branches?repo=` | `[]string` (works for public URLs too) | M |
| POST | `/github/scan` | `ScanRequest` -> `ScanResult` (builder, start command, config files, packages, variable names, port) | M |
| POST | `/github/webhook` | GitHub delivery (signature checked) | - |
| GET, POST | `/projects/{pid}/webhooks` | `Webhook` | A |
| PATCH, DELETE | `/webhooks/{id}` | `Webhook` | A |
| POST | `/webhooks/{id}/test` | -> `{status, responseCode}` | A |

### 11.6 Agent endpoints (WP2a, mounted at `/api/agent/v1/`)

| Method | Path | Auth |
|---|---|---|
| POST | `/join` | join token in body (rate limited 10 per minute per IP) |
| GET (WebSocket) | `/connect` | host token bound to machine-id and WireGuard key (7.1) |
| GET | `/uploads/{id}` | host token; serves a `5050 up` tarball to the build host |

The agent endpoints are also served on `agents.<console domain>` so operators can firewall them
separately; the console domain keeps serving them for compatibility.

### 11.7 Conventions

- Errors: `api.Error`, codes `unauthorized`, `forbidden`, `not_found`, `conflict`, `validation`,
  `rate_limited`, `unavailable` (also read-only mode), `internal`.
- Request bodies are capped at 1 MiB except uploads. Unknown JSON fields are rejected.
- Mutations return within 100 ms of the write for the optimistic UI (16.1); long work continues
  through events.
- The SPA is served for every non-API path; hashed assets get `Cache-Control: immutable`,
  `index.html` gets `no-cache`, with CSP `default-src 'self'; img-src 'self' data:
  https://avatars.githubusercontent.com; style-src 'self' 'unsafe-inline'; connect-src 'self'`.

---

## 12. CLI `5050` (WP7, MIT)

Install: `curl -fsSL https://5050cloud.com/install.sh | sh -s -- --cli` (CLI only, to
`~/.local/bin` or `/usr/local/bin`, plus the `railway` alias unless `--no-alias` or a real
`railway` binary is already on `PATH`), or the release archives for macOS, Linux and Windows
(Windows builds are labelled experimental).

Config: `~/.config/5050/config.json` (0600) holds instances (`url -> token`) and links
(`absolute dir -> {instance, project, environment, service}`). `FIFTY_URL` and `FIFTY_TOKEN`
override for CI; `RAILWAY_TOKEN` and `RAILWAY_API_TOKEN` are read too when the binary runs as
`railway`. Every command accepts `-p/--project`, `-e/--environment`, `-s/--service`, `--json`,
`-y/--yes`. Exit code non-zero on failure. Output is plain text without colour when not a TTY.
Interactive pickers are numbered prompts.

**`railway` alias**: the same binary installed as a `railway` symlink. When `argv[0]` is
`railway` it accepts Railway's verbs and flags as documented in `railway-docs.md` section 19
(including `railway up --service`, `railway variables --set`, `railway add -d postgres`), prints
one line the first time ("Using 5050cloud at <url>; run `5050 alias remove` to undo"), and
answers verbs we do not have with "railway <verb> is not available in 5050cloud. See
5050cloud.com/docs/coming-from-railway". `5050 alias install|remove` manages the symlink.

| Command | What it does | Key flags |
|---|---|---|
| `login [URL]` | Browser login (device code), stores a token; `--token` for headless | `--token`, `--browserless` |
| `logout`, `whoami` | | |
| `init` | Create a project and link this directory | `-n/--name` |
| `link` / `unlink` | Link this directory to project, environment, service | `-p -e -s` |
| `status` | Linked project, environment, service, its status and URL | |
| `list` | Projects | |
| `open` | Open the canvas (or `--service` panel) in the browser | |
| `up [PATH]` | Pack the directory (respects `.gitignore` and `.5050ignore`), upload, deploy into the linked service (creates one with `--new`), stream build and deploy logs | `-d/--detach`, `-c/--ci`, `-m/--message`, `--new`, `--name`, `--no-gitignore`, `--path-as-root` |
| `redeploy`, `restart`, `down` | Redeploy the active deployment, restart it, remove it | `-y` |
| `deploy --template ID` | Add a template | `-v KEY=VALUE` |
| `templates` | `list`, `search QUERY` | |
| `add` | Add a service: `-d/--database postgres\|mysql\|redis\|mongo`, `--repo owner/name`, `--image nginx:1.27`, `--empty NAME` | `-v KEY=VALUE` |
| `import` | `railway --token T` (staged project), `compose FILE`, `heroku-env FILE` | |
| `logs [DEPLOYMENT]` | Deploy logs (default), build logs, or HTTP logs; follow by default on a TTY | `-b/--build`, `--http`, `--method`, `--status`, `--path`, `--request-id`, `--latest`, `-n/--lines`, `-f/--filter`, `-S/--since`, `-U/--until`, `--no-follow` |
| `variables` (alias `variable`) | `list` (names, `--kv` reveals), `set K=V [K=V...]` (staged and committed unless `--stage`), `delete K` | `--kv`, `--stdin`, `--stage`, `--skip-deploys` |
| `run -- CMD` | Run a local command with the service's resolved variables | |
| `shell` | Local subshell with the variables loaded | `--silent` |
| `ssh [-- CMD]` | Shell inside a running replica (WebSocket exec) | `--replica`, `--session NAME` (reattach) |
| `connect [SERVICE]` | Open `psql`, `mysql`, `redis-cli` or `mongosh` through a local tunnel; `--tunnel-only` prints the local URL; `-- ARGS` passes arguments and stdin through (for `pg_dump | 5050 connect`) | `-P/--port`, `--tunnel-only` |
| `domain [DOMAIN]` | No argument: generate a domain; with one: add a custom domain and print the DNS records; `list`, `delete`, `status DOMAIN`, `update DOMAIN --port`, `certificate retry DOMAIN` | `--port` |
| `tcp-proxy` | `list`, `add --port`, `delete` | |
| `environment` | Switch; `new NAME [--duplicate ENV]`, `delete`, `rename`, `sync --from ENV`, `config` (print the environment's effective config as JSON), `edit --service-config SVC DOT.PATH VALUE [--stage]` | |
| `service` | Switch the linked service; `list`, `status [--all]`, `scale N`, `source connect --repo\|--image`, `source disconnect`, `rename NAME` | |
| `scale N` | Set replicas (commits) | |
| `volume` | `list`, `add --mount PATH`, `delete`, `wipe` | |
| `cron` | `list` runs, `run` now | |
| `metrics` | Print CPU, memory, network and HTTP summaries for a range | `--range` |
| `usage` | Usage for the period and the Railway comparison | `--from`, `--until` |
| `config pull` | Write `5050.yaml` for the environment (no lock-in export) | `--file` |
| `host` | `add [--roles app]` prints the join command; `list`; `drain HOST`; `maintenance HOST on\|off` | admin |
| `upgrade` | Upgrade the install (control plane then agents), wait and report | `--version` |
| `alias install\|remove` | Manage the `railway` symlink | |
| `completion bash\|zsh\|fish`, `version` | | |

`5050 up` packs with `archive/tar` + gzip, skips `.git` and ignored files (unless
`--no-gitignore`), refuses more than 500 MB, prints `Uploaded 3.2 MB`, then the build log, then the
first deploy log lines, then `Deployed: https://...` and exits 0, or prints the diagnosis and exits
1. `--path-as-root` uploads PATH as the archive root instead of the linked root directory.

---

## 13. Web console (WP8a, WP8b, WP8c, `web/`)

Start from `reference/console-web` (copied into `web/` by WP8a), keep its stack and kit, and fix
every item in `docs/research/reuse.md` section 2.2 (private strings, `depot` names, copy that names
our vendors, comments that say values were copied from another product's stylesheet). Package name
`@5050cloud/web`. Our own theme: shift neutrals and the accent so it is clearly ours, add a light
theme (system default, toggle in the avatar menu). Types come from `make gen-ts` (tygo over
`pkg/api` into `web/src/api/types.gen.ts`, committed; CI fails if stale). The reference console is
about 2,000 lines of mostly read-only views; most of this section is new work.

Every mutation is optimistic: the UI applies it at once (staged colour, banner count, status), sends
the request, and rolls back with a toast on error.

### 13.1 Screens

| Route | Screen | Owner |
|---|---|---|
| `/setup`, `/login`, `/forgot`, `/reset/:token`, `/verify/:token`, `/invite/:token`, `/cli-auth` | Owner creation, login (with the TOTP step), forgot and reset password, email verification, invite accept, CLI approval ("Approve login for code WXYZ-1234?") | WP8a |
| `/` | Dashboard: project cards with a mini canvas and online summary ("3/4 online"), grid/list, search, stars, sort **Recent Activity**, **Creation Date**, **A-Z**, **New** (`Cmd+/`), Hosts card with disk bars and **Add host**, update banner, restore banner (9.17.1) | WP8a |
| `/new` | Searchable list **GitHub Repository**, **Database**, **Template**, **Docker Image**, **Empty Project**, **Import from Railway**, **Docker Compose**. GitHub flow: repo search with **Refresh**, **Configure GitHub App**, inline **Connect GitHub**, or "Paste a public repo URL"; branch; the shared scan dialog; **Deploy Now** / **Add Variables**. Docker image input with live name validation and a registry credential picker with inline **Add credential** | WP8a |
| `/new/template/:id` | Template detail (10.2) | WP8a |
| `/new/import/railway` | Token input, progress, import report, then the canvas with staged changes | WP8a |
| `/project/:pid` | Canvas (13.2), left rail Architecture, Observability, Logs, Settings, environment switcher, top bar Activity and bell | WP8a |
| `/project/:pid/service/:sid/:tab?` | Service panel (13.3) over the canvas | WP8b |
| `/project/:pid/volume/:vid` | Volume panel (9.12) | WP8b |
| `/project/:pid/service/:sid/deployment/:did/:tab?` | Deployment view (13.5): **Details**, **Build Logs**, **Deploy Logs**, **Network** (sub-view **HTTP**) | WP8b |
| `/project/:pid/logs` | Log Explorer (environment-wide, service and type filters, histogram, live) | WP8b |
| `/project/:pid/observability` | Per-service CPU and memory sparklines, HTTP error rate, hosts this environment uses | WP8b |
| `/project/:pid/settings/:section` | **General** (name, description, visibility, project id), **Usage** (9.23), **Environments** (create, rename, duplicate, delete, egress), **Shared Variables**, **Webhooks** (events, Critical events preset, **Test**, deliveries), **Members** (project grants), **Tokens** (environment tokens), **Danger** (delete individual services, delete project) | WP8c |
| `/settings/:section` | Install settings: **General** (name, base domain, console domain, ACME email, trusted proxies, Email/SMTP with **Send test email**, Alerts), **Hosts** (list, Add host, roles, maintenance, conflicts, reachability, usage), **GitHub**, **Registry credentials** (list, add, delete, where used), **Backups** (bucket, write and retention keys, test, image storage in bucket, state backups and Litestream lag, recovery key reveal), **Members** (invite by email or link, roles, require 2FA, transfer ownership), **API tokens**, **Upgrades**, **Audit log** | WP8c |
| `/account/:section` | Profile, Security (password, 2FA, recovery codes, sessions), Notifications (per event: email, in app) | WP8c |
| Activity panel and bell | 9.24, slide-over from the top bar | WP8a |

### 13.2 Canvas (WP8a)

- **+ Create** (top right), right-click on empty canvas, and `Cmd+K` all open the same New menu:
  GitHub Repo, Database (Postgres, MySQL, Redis, MongoDB), Template, Docker Image, Empty Service,
  Volume (asks for the service and mount path). **GitHub Repo** opens the same scan dialog as
  `/new` (detected builder, start command, config file, monorepo packages to tick).
- Drag a `docker-compose.yml` onto the canvas to import it (9.22).
- Right-click a card: **Open**, **Duplicate** (sealed values are not copied), **Attach Volume**,
  **Update Info** (rename and a searchable devicon icon picker), **Add to Group**, **Delete**
  (staged). Right-click empty space: New menu, **New Group**, **Hide Connections** / **Show
  Connections**, **Tidy**.
- Groups: create from the right-click menu, rename, icon, drag cards in and out, collapse to one
  card that summarises statuses ("4 services: 3 Active, 1 Crashed").
- `Cmd+Z` / `Cmd+Shift+Z` undo and redo card moves and group changes (local history of 50).
- Cards: icon, name, domain, status glyph and line from 9.1.1 ("Deployed 3m ago via GitHub",
  "Building", "Build failed", "Crashed" with an inline **Restart** button, "All replicas failing
  health checks; serving anyway", "Pending: no capacity", "No deployment", cron "Next run in 4m,
  last run completed"), volume tab with usage, replica count badge, monitor red dot, "Generate
  Domain" nudge when a web service listens without a domain.
- Staged: added and edited cards and fields drawn in the staged colour (purple family), deleted
  cards dimmed with a strike, sync tags **New** / **Edited** / **Removed**, and one floating banner
  "N changes" with **Details**, **Discard**, **Deploy** (`Shift+Enter`; Alt-click or
  `Alt+Shift+Enter` commits without deploying). Details shows each change with old and new values
  (renames list their reference rewrites), an x per change, a commit message box, and
  `Cmd+Enter` to commit.
- Presence avatars of other viewers top right; positions from other viewers animate in live.
- Empty environment: centred call to action "Add a service" opening the New menu.
- Reference edges, hover highlight, drag with snap, zoom and fit, positions saved.
- Live: subscribe to `/api/v1/events`; no polling faster than 30 s as a fallback.

### 13.3 Service panel tabs (WP8b)

The header shows the icon and the name (click to rename, staged), the public URL, the private
name, the host(s) and replica count, and for databases a **Connect** button (9.13). Tabs:

- **Deployments**: active card, in-progress card with the expandable step list, diagnosis line in
  red, history with **Show Skipped**, three-dot menu by state (View logs, Restart, Redeploy,
  Rollback, Remove, Abort); rollback confirm says "Restores the image and the variables this
  deployment ran with". Empty state offers the first deploy for every source type (for an empty
  service: "Connect a repo or image" plus the `5050 up` command). A Crashed card has an inline
  **Restart**.
- **Variables**: "N Service Variables", **Shared Variable**, **Raw Editor** (paste `.env` or JSON;
  shows the parsed result before applying), **New Variable** with `${{` autocomplete in name and
  value (services, then their variables, including provided ones, and the template functions),
  multi-line with `Cmd+Enter`, row menu (Copy, Edit, Seal, Remove), eye to reveal, reference rows
  show the raw `${{...}}` with the resolved target and a warning for unresolved targets,
  suggested variables banner, **Import from Heroku**, "N provided variables" collapsible, "Add a
  reference" buttons for database services. Changes go to the canvas banner.
- **Metrics**: CPU (vCPU with the limit), Memory (with the limit), Disk (volume), Network in/out,
  HTTP requests by status class, **Error Rate**, **Response Time** p50/p90/p95/p99; hover cursors
  are synced across charts; deployment markers show the commit on hover; `Shift`+drag zooms every
  chart and pushes a history entry so browser Back restores the range; ranges 15m, 1h, 6h, 24h,
  7d, 14d, 30d, a custom range ("2 hours ago" to "now", or calendar); Sum/Replica toggle;
  **Monitors** list and **New monitor**. Postgres services add Connections, Cache hit ratio and Top
  queries.
- **Settings** (long page, right-hand section index, `/` focuses the filter; each field edits the
  staged config and shows the staged colour; fields set by a config file show the file icon and
  are read-only with the source line on hover, 9.20). Railway's placement is kept:
  - **Source**: source repo (installed-app repo picker with inline **Connect GitHub**, or a pasted
    public repo URL) and branch, or an image (with live name validation and a registry credential
    picker with inline **Add credential**); switching between repo and image; connect,
    disconnect; root directory; **Config file path**; auto deploy toggle; "Check for a newer
    image"; disabled rows **Wait for CI** and **Auto Updates** with "Not in v0.1".
  - **Networking**: public domains (Generate Domain with port, Custom Domain with DNS records,
    live status, certificate expiry and **Retry certificate**, edit port, delete), TCP proxy,
    private domain (editable), outbound IP, egress shown from the environment, **Allow metadata
    and host access** (Admin).
  - **Build**: builder (Auto, Railpack, Dockerfile; Nixpacks shown as "Nixpacks (built with
    Railpack)"), Railpack version, Dockerfile path, custom build command, watch paths (one per
    line), no-cache toggle, build timeout.
  - **Deploy**: start command, pre-deploy command (timeout appears once a command is typed), cron
    schedule with presets (**Every hour**, **Every day**, **Every week**, **Every month**), live
    validation, next three run times, timezone, overlap and timeout, health check path and
    timeout, **Resource Limits** (vCPU and memory per replica, memory reservation), **Restart
    Policy** and max retries, **Teardown** (overlap and draining seconds), run as user, shared
    memory size, disabled row **Serverless** with "Not in v0.1".
  - **Scale**: **Replicas**, **Max surge**, **Singleton**, **Processes** with the **Use all cores**
    toggle and the mode explanation (9.7), **Placement** (roles or pinned hosts) with where
    replicas run now.
  - **Danger**: delete service (type the name; staged).
- **Backups** (databases and services with a volume), **Data** (databases), **Cron Runs** (crons,
  with **Run now** and missed rows), **Activity** (this service), **Shell** (menu item "Open shell"
  with an xterm.js terminal over the shell WebSocket, lazy-loaded).

### 13.4 Command palette and keyboard (WP8a)

- `Cmd+K` opens the palette. It searches commands and `/search` results (projects, services,
  environments, settings pages, deployments by commit message). Items with children open a
  sub-menu with `Arrow Right` or `Enter`; `Backspace` on an empty query goes back.
- Commands: navigation (any project, service, environment, settings page), the New menu items,
  **New Environment**, **Deploy latest commit**, **Redeploy** with an adaptive label ("Redeploy
  service image" for image services, "Redeploy latest deployment" when there is no source, hidden
  when there is no deployment), **Restart**, **Rollback**, **Open shell**, **Update service info**
  (rename, icon), **Hide connections**, **Import variables from Heroku**, **Toggle theme**,
  **Copy private URL**. `Shift+Enter` on "No results" opens "Report a missing command" (a GitHub
  issue link with the query).
- Shortcuts: `Cmd+/` new project, `G D`/`G V`/`G M`/`G S` tabs, `Escape` back or close, `/`
  filters, `Shift+Enter` deploy staged changes, `Alt+Shift+Enter` commit without deploying,
  `Cmd+Enter` in Details commits, `Cmd+Z`/`Cmd+Shift+Z` on the canvas.
- Polish: relative times with absolute tooltips, copy buttons on every id, URL, DNS record and
  connection string, skeleton loaders, type-the-name confirms only for destructive actions, one
  `StatusGlyph` everywhere.
- The palette has vitest unit tests (ranking, sub-menus, adaptive labels) and Playwright U26.

### 13.5 Deployment view and logs (WP8b)

- Opens **Build Logs** automatically for a failed build and **Deploy Logs** for a crash or failed
  health check.
- Header: service / short id, status badge, commit, trigger, and an actions menu (Redeploy,
  Rollback, Restart, Remove, Abort, Copy id).
- Log views: live tail that pauses when you scroll up and shows a **Resume** pill; column toggles
  (time, replica, level, service); click a line to open a side panel with its flattened JSON
  attributes, where clicking a value adds it as a filter; right-click a line for **View in
  Context** (the surrounding lines, `around=`) and **Copy**; the "N log lines dropped: more than
  500 lines per second" line rendered as a warning; offline host marker (9.4).
- **Network** tab with the **HTTP** sub-view: method, path, status, response time, request id,
  client IP, with the same filters.
- **Details**: config snapshot (fields from a config file marked), image, digest, commit, variable
  names, notices.

### 13.6 Railway features not in v0.1

Where Railway shows a control we do not have, we show it disabled with a tooltip "Not in v0.1:
<what to do instead>" and a link to the Coming from Railway row: PR environments (Environments
page), Wait for CI and image Auto Updates (Settings > Source), Serverless (Settings > Deploy),
Buckets and Functions (New menu), the observability widget board (Observability), AI **Diagnose**
fix PR (next to the diagnosis sentence), Live Resize (volume panel), wildcard domains (Custom
Domain form), the SQL query box (Data tab).

### 13.7 Mobile

Down to 375 px wide: no horizontal page scroll, the panel becomes full screen, the canvas is a
list of cards with status glyphs, and the useful actions on a phone work: see status, open logs,
**Restart**, **Redeploy**, **Rollback**. Editing settings on a phone is possible but not
optimised.

### 13.8 UI end-to-end suite (`web/e2e/`, Playwright)

Runs against a real install (`E2E_CONSOLE_URL`, `E2E_SETUP_TOKEN`, plus `E2E_SMTP_URL` for the
Mailpit container the harness runs, and `E2E_GITHUB_*` for the GitHub App tests) and is part of the
release gate. Every test drives only the UI; API calls are used only to read state for assertions.
Each WP8x sub-package owns the specs for its screens (`web/e2e/specs/<feature>/`); WP8a owns the
harness. The suite runs on its own fresh install (19.3), so U1 is the only consumer of the setup
token.

| ID | Flow |
|---|---|
| U1 | Setup owner from the installer link, log in, log out, enable 2FA, log in with a TOTP code |
| U2 | New project > GitHub Repository > paste public repo URL > Deploy Now > Active; Generate Domain > URL serves 200 |
| U3 | Canvas + Create > Docker Image `traefik/whoami:<pinned>` > staged card > Deploy > Active |
| U4 | + Create > Database > Postgres > online, Variables tab lists `DATABASE_URL`; Connect modal shows both URLs and the CLI command |
| U5 | New Variable `DATABASE_URL` with autocomplete `${{Postgres.DATABASE_URL}}` > banner "1 change" > Details diff > Deploy > edge appears |
| U6 | Settings: change start command and replicas to 2, staged colour, Deploy, two replicas listed |
| U7 | Rollback from the deployments menu restores the old variable value (checked through the app's response) |
| U8 | Failed deploy (wrong target port) opens Deploy Logs with the diagnosis sentence; the previous deployment stays Active; a failed build opens Build Logs |
| U9 | Logs: build logs stream; deploy logs show the app's line; Log Explorer `@level:error`; side panel value click adds a filter; View in Context; scroll up pauses and Resume continues; Network > HTTP lists a request |
| U10 | Metrics: CPU, memory, Error Rate and p90 series; marker hover shows the commit; synced cursor; Shift+drag zoom then Back restores; custom range |
| U11 | Environments: + New Environment > Duplicate > staged services > Deploy; rename the environment |
| U12 | Settings > Hosts > Add host shows the join command with roles; Backups settings save and test |
| U13 | Delete a service from Project Settings > Danger and from the canvas (type name, staged, Deploy) and delete a project |
| U14 | 375 px: dashboard, canvas list and panel usable, no horizontal scroll, Restart works; light and dark themes render |
| U15 | Custom domain: add, DNS records shown with copy buttons, live status moves to Active (or to "Waiting for DNS" without `E2E_CUSTOM_DOMAIN`), Retry certificate |
| U16 | TCP Proxy on Postgres (Public access): address shown; `psql` through it from the runner works |
| U17 | Right-click > Attach Volume (mount path) on a service; New > Volume with the service picker; volume panel shows usage; Wipe Volume after typing the name |
| U18 | Create a cron service from an image, preset **Every hour**, next runs shown, Deploy, **Run now**, Cron Runs lists a Completed run |
| U19 | `/new/template/umami`: services listed, generated secret prefilled and edited, Deploy; Postgres goes Active before Umami starts; the app serves 200 |
| U20 | Empty Service > Settings > Source: paste a public repo URL, deploy; then switch to an image and deploy |
| U21 | Private image: add registry credentials inline from the Source picker; deploy succeeds; Settings > Registry credentials lists and deletes it |
| U22 | Abort a building deployment; Remove the active one ("No deployment"); Restart from a Crashed card |
| U23 | Shared Variable created, Share to two services, both staged and deployed |
| U24 | Seal a variable: reveal is gone, Raw Editor hides it, the app still receives it |
| U25 | Raw Editor: paste `.env` text, then JSON; parsed result shown; staged; deployed |
| U26 | Cmd+K: sub-menu with Arrow Right and Backspace; jump to a settings page; search a deployment by commit message; adaptive Redeploy label; Hide connections; Update service info |
| U27 | Keyboard: G D, G V, G M, G S; Shift+Enter deploys; Alt+Shift+Enter commits without deploying; Cmd+Enter in Details |
| U28 | Environment Sync from staging into production: New/Edited tags, Details review, Deploy |
| U29 | Backups: Back up now; Restore to a copy at a point in time; the copy has the row; Promote; the app sees the copy |
| U30 | Invite link with role Viewer; the viewer sees read-only controls (no Deploy, no reveal, no Settings edits); a project grant makes them Editor on one project only |
| U31 | Create GitHub App from the /new inline Connect GitHub, return to the picker, pick a private repo, Deploy; a push auto-deploys; a push outside the watch paths shows Skipped |
| U32 | Webhook with the Critical events preset; Test shows the response code; a crash delivers `deployment.crashed`; a CPU monitor fires to the same webhook |
| U33 | Change the console domain; the new one serves with a valid certificate; the old one works for the grace period |
| U34 | Canvas + Create > GitHub Repo on a monorepo: the scan dialog lists packages; ticking two creates two staged services with root directories |
| U35 | New > Import from Railway against the recorded fixture server (live project when `E2E_RAILWAY_TOKEN` is set): staged services, variables, domains, volumes and a Postgres; report lists skipped items; Deploy |
| U36 | Drag a `docker-compose.yml` (web + Postgres + Redis) onto the canvas: three staged services, a volume, deploy order respected |
| U37 | Rename `api` to `backend`: Details lists the reference rewrites; after Deploy the dependent service resolves `${{backend.X}}`; `api.internal` still answers for 24 h |
| U38 | Duplicate a service (sealed value absent); Update Info icon picker; Cmd+Z and Cmd+Shift+Z on a move; Hide Connections; New Group, drag in and out, collapse shows the status summary |
| U39 | Two browsers on one environment: each sees the other's avatar; a card moved in one moves in the other within 1 s |
| U40 | Forgot password: email arrives in Mailpit, reset works, old session ends; notification prefs: a failed deploy sends an email and lights the bell; Activity panel opens a commit diff and filters by service |
| U41 | Project Settings > Usage shows per-service numbers and the Railway price line; General: rename the project, change the description, set public read-only and open it logged out |
| U42 | Database panel: Regenerate credentials redeploys dependents and the app still connects; Postgres Extensions > Enable `pg_trgm`; Redis key browser lists and reveals a key; MongoDB Data tab lists a collection |

Screenshots of U2 to U10, U19 and U28 are saved for the landing page and docs
(`web/e2e/screens/`). `web/e2e/labels.json` (generated from section 2) is checked against the
rendered strings by `labels.spec.ts`.

---

## 14. Docs site and landing page (WP9, `site/`)

Astro with Starlight, pinned exactly. `site/` builds to static files (`npm run build`), deployed
by `.github/workflows/site.yml` to GitHub Pages (custom domain `5050cloud.com` when its DNS is
set; otherwise the Pages URL). `install/install.sh` is copied to `site/public/install.sh` at build
time so `https://5050cloud.com/install.sh` serves it. Every page is also published as Markdown
(`/<path>.md`), plus `/llms.txt` (index) and `/llms-full.txt` (all pages), as Railway does. The
site also serves `/deploy` (the Deploy on 5050cloud button target, 10.2).

### 14.1 Landing page (`site/src/pages/index.astro`)

Sections, in order:

1. **Hero**: "Railway's developer experience on servers you own." Sub line: "One command turns any
   Linux VM into a canvas of services with git deploys, databases, logs and rollbacks. Open source.
   No per-seat, per-minute or per-server fees." The install command with a copy button. Buttons:
   **Get started** (Quick start), **GitHub**.
2. **Screens**: real screenshots from the UI suite (canvas, deploy logs, variables with references,
   metrics), light and dark.
3. **Why it costs less**: the side-by-side table from `docs/research/competitors.md` section 4.4
   (Hetzner, DigitalOcean, AWS reserved and on demand, Azure, two Hetzner hosts, Render, Fly.io,
   Heroku vs Railway steady load), with the workload stated, sources linked, the date, the
   exchange rate assumption, and the honest notes: low average use favours Railway; egress
   included on Hetzner EU and DigitalOcean; growth is flat on a VM; you own patching (we automate
   it). Plus "See your own number: the Usage page shows what your use would cost on Railway."
4. **Use every core you pay for**: the E20 result (requests per second, 1 process vs auto, on the
   VM size named in `report.json`, for example "Azure D2as v5, 2 vCPU") with the script link; how
   Node cluster mode and `WEB_CONCURRENCY` work.
5. **Add servers with one command**: the join command, "the agent dials out: no inbound management
   ports, works behind NAT", replicas across hosts with built-in load balancing, private networking
   over WireGuard, a second public entry point with `--roles ingress`.
6. **Reliable by default**, each worded to its tested condition: database backups to any
   S3-compatible bucket with point-in-time restore to a copy (E15); zero-downtime deploys for
   services without a volume, for public and private traffic (E7, E33); the old version keeps
   serving if a deploy fails (E9); rollbacks that restore variables and do not expire after a few
   days (E8); `5050 upgrade` reverts itself if the new version is unhealthy (E17, E29); apps keep
   serving while the `5050d` process is stopped or restarting, and across a worker reboot (E22,
   E22b); the control plane's state streams to your bucket and rebuilds on a new VM (E28).
7. **Move from Railway in minutes**: `5050 import railway`, `railway.json` honoured, the `railway`
   CLI alias, the same variable names and reference syntax (E30, E31, E13).
8. **No lock-in**: Railpack builds (Railway's own builder), standard Docker images,
   `5050 config pull` exports everything to a file, AGPL-3.0 server and MIT CLI.
9. **Compared with**: table vs Railway, Coolify, Dokploy, Kamal, Dokku using only facts from
   `competitors.md` section 2 (canvas, references, multi-host, replicas on one host, zero downtime,
   backups with restore, inbound ports needed, license).
10. **Honest limits** (14.1.1).
11. **FAQ** and footer (GitHub, docs, license, security policy).

Claims rule: every claim on the page maps to a passing e2e test id or a cited source in
`competitors.md`. `site/claims.json` lists `{claim, evidence, condition}` and the docs verifier
checks it against `report.json`. A KEEP feature (section 23) without a passing test comes off the
page. Prices carry the research date and "check current prices"; no "cheaper for everyone", no
star counts, no idle RAM comparisons (competitors.md 5, "Claims to avoid").

#### 14.1.1 Honest limits (landing page, and a docs page with details)

- One control host. With one host, a host failure stops everything until it is rebuilt (9.17.2).
  With more hosts, apps on other hosts keep serving (with a second ingress host, 9.2.1), but
  deploys, cron and the console stop until the control host is back or rebuilt.
- Cron runs pause while the control plane is down; missed runs are listed, not replayed.
- Databases run on one host each. No high availability, no automatic failover.
- Services with a volume have a short gap on every deploy.
- Without a second ingress host, all public traffic enters through one machine. No global edge,
  CDN or DDoS protection.
- No autoscaling. Volumes have no size cap; they share the host disk.
- With a bucket, control plane state is replicated within seconds; without one, the RPO is the
  last hourly snapshot on the same disk.
- Metrics are kept 30 days. Logs are kept 7 days on each host.
- IPv4 only. Ubuntu 24.04 is the tested OS.
- You own the hardware and the OS (security updates are automated, reboots are yours).

### 14.2 Docs structure (Railway's information architecture, our names)

- **Get started**: Quick start (install, open the link, deploy a repo, add Postgres), The basics
  (projects, environments, services, deployments, variables, hosts), Coming from Railway (14.3),
  Production checklist.
- **Guides**: Deploy from GitHub, Deploy a Docker image, Deploy with the CLI (`5050 up`), Config
  as code (`railway.json`, `5050.yaml`), Import from Railway, Move a database from Railway, Import
  Docker Compose, Templates and the Deploy on 5050cloud button, Add Postgres/MySQL/Redis/MongoDB,
  Variables and references, Staged changes and environment sync, Custom domains and HTTPS, Use all
  cores, Add a host (and the two-ingress setup), Replicas and zero-downtime deploys (including
  keep-alive timeouts for internal callers), Health checks, Rollbacks, Cron jobs, Volumes, Backups
  and restore to a copy, Database major upgrades, Monorepos and watch paths, Pre-deploy
  migrations, Private networking, Quarantined environments, Monitors and alerts, Email and
  notifications, Members, roles and 2FA, Upgrading and host maintenance, Backing up the control
  plane, Rebuild the control host, Rotate the master key, Behind Cloudflare, Troubleshooting failed
  deploys (one section per diagnosis in 9.18).
- **Reference**: Install options (4.1), Service settings (every field in `api.ServiceConfig` with
  default and Railway equivalent), Config file schema, Variables reference (provided variables and
  aliases, reference syntax, template functions), Deployment statuses (9.1.1), Log filter syntax,
  Templates, API (every endpoint in section 11), Agent protocol, Architecture, Security model,
  Ports and firewall, Limits (8.4, section 16).
- **CLI**: overview, global options, the `railway` alias, one page per command in section 12.
- **Compare**: Railway, Coolify, Dokploy, Kamal, Dokku, a plain VPS.
- **Contribute**: building from source, the Makefile, writing a template.

Page types and writing conventions follow `railway-docs.md` section 23: one-sentence opening (also
the meta description), numbered steps that start with the place, exact UI names in bold, callouts,
"Next steps" at the end.

### 14.3 Coming from Railway (one row per difference)

| Railway | In 5050cloud v0.1 | What to do instead |
|---|---|---|
| PR environments (incl. Focused and Bot PR environments) | Not yet (v0.2) | Duplicate an environment and point a service at the branch |
| Wait for CI | Not yet | Deploy from CI with `5050 up -c` or a GitHub Action calling `5050 redeploy` |
| Deployment approvals | Not yet | Turn off auto deploy and deploy by hand |
| Image Auto Updates with maintenance windows | Not yet | "Check for a newer image" in Settings > Source, or a cron that calls `5050 redeploy` |
| Serverless (app sleeping) | Not available | Scale to 0 is v0.2; idle apps cost only RAM on your own VM |
| Buckets | Not yet | The MinIO template |
| Functions | Not available | A small service or a cron service |
| Observability widget board | Not yet | The Observability page and Metrics tabs |
| AI Diagnose and fix PR | Rule-based sentence | The diagnosis sentence and the Troubleshooting guide |
| Data tab SQL editor and row editing | Read-only tables and Redis keys | `5050 connect` |
| Metrics for 30 days | Same, 30 days | |
| Wildcard custom domains | Not yet | One custom domain per name, or `--domain` with a wildcard A record for generated names |
| Static egress IPs (paid) | Free: your hosts' IPs | Settings > Networking shows them |
| Volume Live Resize | Not needed: volumes use the host disk | Add a data disk |
| Regions | Your hosts and roles | Add a host in another location |
| `railway` CLI | `5050`, plus the `railway` alias | Same commands |
| Login with GitHub, magic link email login | Email and password with 2FA | |
| Slack/Discord formatted webhooks | Signed JSON | A small relay service, or v0.2 |
| `config plan/apply` style config sync | `5050 config pull` and `railway.json` in the repo | v0.2 Git mode |

---

## 15. Security model

| Area | Rule |
|---|---|
| Exposure | 5050d listens on 127.0.0.1 only; the world reaches it through Caddy on 443. Agents expose nothing. Registry binds to loopback, the mesh address and the build network gateway, with htpasswd auth (push and pull-only users). Caddy's admin API is a root-only unix socket. |
| Agent identity | Join tokens are single use, expire in 1 hour, stored hashed, rate limited. Host tokens are 32 random bytes, stored hashed, bound to the host's machine-id and WireGuard public key, rotated every 30 days over the open socket; a cloned VM is refused (7.1). Removing a host revokes it. The channel is TLS to the console certificate. Agent mTLS is v0.2. |
| Secrets at rest | Variables, registry, S3 and SMTP credentials, GitHub App key, webhook secrets, TOTP secrets: XChaCha20-Poly1305 under `/etc/5050/master.key` (0600) with row-bound additional data (`internal/cp/secretbox`). The agent's desired-state cache is encrypted with the host key. `5050d rotate-master-key` rotates. |
| Secrets in transit | Never sent to the browser except through audited reveal. Agents receive resolved env only for replicas they run. Clone tokens are short-lived installation tokens, never in URLs or logs. Build variables never appear on a command line (8.2). |
| Backups | Encrypted (WAL-G libsodium, restic, age). Data containers hold a write-only bucket key; deletes use a separate retention key on the control host. |
| Logs | Secret values masked (9.15). Build args never carry secrets; Railpack secrets use BuildKit secret mounts. |
| Containers | No `--privileged`, no Docker socket mounts, default seccomp and AppArmor, `no-new-privileges`, default capabilities. Per-environment networks with isolation rules; no access to host addresses, the mesh or the metadata endpoint by default (8.4). |
| Builds | BuildKit in a container on its own bridge, RUN steps on a bridge network, no host network entitlement, no access to host addresses or metadata (8.2). |
| Shell strings | None. Every Docker and host command is an argv array; a start command with shell operators becomes `["sh","-c",cmd]` inside the container only. |
| Web | Cookie flags, CSRF header, CSP (11.7), no CORS, rate-limited login keyed on the real client IP (trusted proxies, 9.11), argon2id, TOTP 2FA, constant-time comparisons for tokens. |
| SQL | Parameters only. Data tab identifiers come from stored config; there is no free query box. |
| SSRF | Webhooks, the backup endpoint test and GitHub fetches check the resolved address at connect time (9.15). |
| Supply chain | All versions pinned; tool downloads verified by SHA-256; releases signed with minisign (key rotation in 9.16); installer verifies the signature for downloads and archives; `govulncheck` and `npm audit --omit=dev` in CI; gitleaks in CI. |
| Audit | Every mutation, reveal, login, 2FA change, shell, tunnel and agent join (9.15). |
| Disclosure | `SECURITY.md` with a private reporting address and a 90-day policy. |

---

## 16. Scale and performance targets

| Target | Number | Checked by |
|---|---|---|
| Hosts per install | 50 (mesh index allows 127) | S1 |
| Services per install | 1,000 service instances, 2,000 replicas, 300 environments | S1 |
| Desired-state fan-out after a commit touching one service | p95 < 2 s to `state_applied` on all affected online hosts | S1 |
| API latency | canvas with 50 services p95 < 150 ms; list endpoints p95 < 100 ms; mutations p95 < 100 ms | S1 |
| 5050d memory | idle < 120 MB RSS, at S1 load < 400 MB | S1, S3 |
| Agent memory | idle < 60 MB RSS with 20 replicas | S3 |
| Control plane idle CPU | < 3% of one core on a 2 vCPU VM | S3 |
| Install time | < 180 s to the printed link on a 2 vCPU VM | E1 |
| First deploy | < 5 min from the curl to a 200 from the app | E1 + E3 |
| Zero downtime, public | 0 failed requests during a redeploy at 50 rps (services without a volume) | E7 |
| Zero downtime, internal | 0 failed requests from an internal caller at 50 rps with keep-alive under 30 s | E33 |
| Host failover | routes withdrawn at 60 s; traffic served from other hosts within 150 s of a host dying (Caddy passive health checks and `lb_try_duration` cover the first 60 s); lost replicas rescheduled within 200 s | E19 |
| Throughput with Processes auto | >= 1.6x a single process on a 2 vCPU non-burstable VM | E20 |

### 16.1 Perceived latency budgets (the console must feel fast)

Measured with Playwright traces on the UI suite's install (`web/e2e/perf.spec.ts`), Lighthouse for
cold loads, and the E harness for deploy timings. Network: the runner to the VM, no throttling;
cold load uses Lighthouse's default mobile throttling for P1 and P2 as well, with the budget
doubled.

| ID | Budget | Check |
|---|---|---|
| P1 | Dashboard interactive under 1.5 s on a cold load | Lighthouse TTI and a Playwright trace |
| P2 | Canvas with 20 services interactive under 1.5 s on a cold load | Playwright trace (first card clickable) |
| P3 | Service panel opens under 150 ms after the click | Playwright trace |
| P4 | An edit shows the staged colour and the banner count under 100 ms (optimistic) | Playwright trace |
| P5 | A deployment status change reaches the canvas within 1 s of the event | event timestamp vs DOM mutation |
| P6 | A log line reaches the live tail within 2 s | app writes a marked line; time to DOM |
| P7 | Redeploying an existing image to Active within 20 s (1 replica, no pre-deploy, health check passing at once) | E harness |
| P8 | Image-only service create to Active within 30 s (image already cached on the host) | E harness |
| P9 | A rebuild that reuses cache is at least 40% faster end to end (push to Active) than the first build | E harness on `examples/node-cpu` |
| P10 | `web/` initial JavaScript at most 300 KB gzipped; xterm, charts and the template icon set are lazy-loaded | `make web` size check in CI |

---

## 17. Build, Makefile, CI and release (WP6; M0 lands the first working versions)

### 17.1 Makefile targets

| Target | Does |
|---|---|
| `make deps` | `go mod download`, `npm ci` in `web/` and `site/` |
| `make web` | Build the console into `web/dist` and check the P10 size budget |
| `make dist-placeholder` | Create `web/dist/index.html` if missing so Go builds without Node |
| `make build` | `bin/5050d`, `bin/5050-agent`, `bin/5050` for the host OS/arch with ldflags from `pkg/version` |
| `make cross` | linux amd64/arm64 for all three; darwin amd64/arm64 and windows amd64 (experimental) for the CLI |
| `make test` | `go test -race -count=1` over the module's packages (`./...` without stray Go packages under `web/node_modules`) plus `npm test` (Vitest) in `web/` |
| `make test-linux` | The `integration` tag inside a privileged `golang:1.27.1-bookworm` container on the local Docker (works on Docker Desktop for macOS), with the Docker socket mounted; `TEST_LINUX_PKGS` defaults to `./internal/agent/...` |
| `make cover` | Coverage report; fails under the thresholds in 18.1 |
| `make lint` | `gofmt -l`, `go vet`, `golangci-lint run` (v2.14.0, whose standard set includes staticcheck: the standalone staticcheck releases at least two weeks old cannot read Go 1.27 export data), `tsc --noEmit` and `eslint` in `web/`, `shellcheck install/*.sh e2e/*.sh scripts/*.sh` (local binary, else the pinned `koalaman/shellcheck:v0.11.0` image) |
| `make gen-ts` | Regenerate `web/src/api/types.gen.ts` with tygo v0.2.21 and the MSW mocks with `pkg/api/internal/mockgen` (every JSON fixture is checked by `tsc` with `satisfies` against the generated types); `make check-gen` fails when they are stale |
| `make tools`, `make vulncheck` | Install the pinned Go tools into `bin/tools`; run govulncheck v1.8.0 |
| `make labels`, `make contracts` | Planner only: regenerate `web/e2e/labels.json` and `e2e/v1-checklist.json` from this file; rewrite `docs/contracts.sha256` |
| `make check-public` | gitleaks, the deny list (`DENYLIST=reference/denylist.txt` when present), private IP regex (RFC 1918 outside our documented 10.128.0.0/9 examples and documentation ranges), em dash and banned-word check (`e2e/banned-words.txt`, itself excluded) on docs, UI copy and comments, SPDX headers, CLI license boundary, no `CONTRACT-STUB` (reported while stubs are expected, a failure with `STRICT_STUBS=1`, which the release and V6 set), contract hashes. It checks the files git would publish (tracked plus untracked, not ignored), so `reference/` and `node_modules/` are never scanned |
| `make dev` | Run 5050d (`--dev`, data in `./.dev`) and an agent against local Docker (Linux), plus Vite on 5173 with a proxy to 5050d; on macOS, 5050d with fakeorch and Vite only |
| `make site` | Build the docs site |
| `make release-snapshot` | goreleaser snapshot into `dist/`, signed with the test key in `e2e/keys/` |
| `make e2e HOST=user@ip [HOST2=user@ip] SUITE=...` | Cross-build linux/amd64, snapshot, serve it over HTTP from the runner, run `e2e/run.sh` with `--release-base` and the test public key |
| `make e2e-ui` | Playwright suite against `E2E_CONSOLE_URL` |
| `make loadtest` | S1 with fake agents against a local 5050d |
| `make clean` | Remove `bin/`, `dist/`, build outputs |

### 17.2 CI (`.github/workflows/`)

- `ci.yml` on every push and PR: lint, test with race, cover, build, `check-public`, `govulncheck`,
  web build with the size budget, site build, `go mod tidy` diff check, `make gen-ts` diff check,
  `make test-linux` on the Linux runner.
- `release.yml` on tag `v*`: goreleaser builds `5050_<ver>_linux_<arch>.tar.gz` (5050d,
  5050-agent, 5050, systemd units, LICENSE files), `5050-cli_<ver>_<os>_<arch>.tar.gz`,
  `checksums.txt`, signs it with minisign (secret `MINISIGN_KEY`) into `checksums.txt.minisig`,
  publishes a GitHub Release with notes, and updates `latest.txt`.
- `site.yml` on pushes to `main` touching `site/` or `install/`: build and deploy to Pages.
- The e2e suites need VMs and run outside GitHub Actions (by the release verifiers and a nightly
  run against `main` once M1 passes) before a tag.

### 17.3 Release artefacts and versions

v0.1.0 is the first tag. The minisign public key is embedded in `install/install.sh` and
`internal/upgrade`. Release notes list what the e2e run covered, with the VM SKU and OS, and say
when infrastructure image pins changed (9.16).

### 17.4 E2E harness rules (WP6)

- **Release path**: every builder e2e run installs through `--release-base` from a local HTTP
  server serving a snapshot signed with the test key, so the download, minisign and SHA-256 path
  in `install.sh` and `internal/upgrade` is exercised before the tag. `--archive` verifies the
  same files next to the tarball (4.1).
- **Let's Encrypt**: builder runs use `--acme-ca staging`; verifier runs use production. The random
  console label (4.1) avoids the duplicate certificate limit.
- **Image pulls**: the harness runs a pull-through cache (`registry:2` in proxy mode, with an
  authenticated Docker Hub account when `E2E_DOCKERHUB_*` is set) on the runner and passes it to the
  install as `HostConfig.Images` mirrors. Preflight prints the remaining Docker Hub quota.
- **Pinned fixtures**: the GitHub flow uses `examples/node-hello` (our own MIT sample, mirrored to a
  public test repository at a fixed commit) instead of a third-party repo at HEAD.
- **Mail**: a Mailpit container on the runner is the SMTP server for U40 and E40.
- **VM lock**: `tools/test-vm.sh` takes a `flock` per VM; only one agent drives a VM at a time.

---

## 18. Test strategy

### 18.1 Unit and integration (each package, run by `make test`)

| Package | Must cover | Coverage floor |
|---|---|---|
| `internal/cp/core/vars` | reference parsing, recursion, cycles, unresolved, provided aliases, template functions, masking list, rename rewrites | 85% |
| `internal/cp/core/changes` | merge of edits, diff output with masked secrets, validation errors (32-char names), commit plan (rebuild vs redeploy vs scale), duplicate environment and service, environment sync tags | 80% |
| `internal/cp/core/api` | every route: auth required, role checks incl. project grants, CSRF, audit entry written, error shapes, read-only mode (table test over the route list) | 75% |
| `internal/cp/core/auth` | argon2id, sessions, reset tokens, TOTP and recovery codes, invite links, rate limits with trusted proxies | 85% |
| `internal/cp/core/github` | webhook signature, watch path matcher (incl. negation and first push), scan of fixture repos, shallow clone fallback | 80% |
| `internal/cp/core/imports` | Railway GraphQL fixtures to staged changes, Heroku env parsing | 80% |
| `pkg/configfile` | railway.json, railway.toml, 5050.yaml, nixpacks.toml parsing, priority merge, line map, unknown field suggestions (fuzz) | 90% |
| `pkg/compose` | compose fixtures to staged services, skipped keys report | 85% |
| `pkg/manifest` | golden round trip through `sigs.k8s.io/yaml` (camelCase) | 100% of types |
| `internal/cp/orch/engine` | state machine with a fake hub: success, failed health, abort, supersede, crash, restart mid-deploy (QUEUED and DEPLOYING), ordering by references, rollback uses snapshot, volume rollout restart on failure, max surge waves, queue timeout | 80% |
| `internal/cp/orch/sched` | placement scoring, reservations and Pending, volume pinning, offline withdrawal at 60 s and rescheduling, singleton, drain, maintenance order, IPAM allocation and release after confirm, slot exhaustion | 85% |
| `internal/cp/orch/hub` | join (single use, expiry, rate limit), connect auth bound to machine-id and key, duplicate host refusal, token rotation, generation = max+1, epoch and adopt, coalescing, reconnect | 75% |
| `internal/cp/orch/data` | backup and restore recipes per template, RDB wait, WAL guard thresholds, cron exactly-once and missed rows, registry GC sequencing, restore test scheduling | 75% |
| `internal/agent/core` | reconcile against a fake Docker API (create, remove stale, ignore unlabelled, no removal before fresh state, generation and epoch guard, adopt), cache encrypt and boot apply, health checker (HTTP codes, TCP, deadline, grace, panic), log level detection, masking, byte budgets, cluster wrap argv | 75% |
| `pkg/logfilter` | grammar, numeric ops, ranges, negation, phrases, attributes, fuzz test | 90% |
| `internal/agent/build` | builder choice incl. NIXPACKS mapping and railpackVersion, ARG extraction, argv construction with no values on argv, diagnosis fixtures, tarball path safety (fuzz) | 75% |
| `internal/agent/netw` | address plan, iptables rule rendering incl. HOSTIN and metadata (golden files), Caddy JSON rendering incl. unix admin, dynamic upstreams, stream_close_delay, ACME proxy on non-primary (golden files), DNS answers incl. draining and aliases, MTU and MSS, TCP proxy | 75% |
| `internal/upgrade` | signature verification incl. next key, symlink swap and rollback in a temp dir, agent pending marker and revert | 80% |
| `internal/cli` | command parsing, `railway` alias argv mapping, config file, `up` packing with ignores and `--path-as-root`, connect URL rewriting and stdin pass-through | 70% |
| `web` | `vitest` for variable autocomplete, staged diff rendering (renames, sync tags), settings form patches and file-icon fields, palette ranking and sub-menus, status labels, Raw Editor parsing | n/a |

Integration (`make test-linux`, and the Linux CI runner): `go test -tags integration
./internal/agent/...` runs the agent against the real Docker Engine: create a network, run a
replica, health check, swap, logs, metrics, exec, job, cluster wrap with a Node image, cache boot
apply, firewall restore before Docker, build sandbox escape attempts.

### 18.2 VM end-to-end (`e2e/run.sh`, WP6)

`e2e/run.sh --host ubuntu@IP [--host2 ubuntu@IP2] [--release-base URL --release-pubkey KEY |
--archive FILE] [--suite single|multi|upgrade|dr|ui|all]` runs from a laptop or runner over SSH
against fresh VMs (for our team, `tools/test-vm.sh reset t1|t2` gives fresh Ubuntu 24.04 VMs on a
non-burstable 2 vCPU size; that tool is private and not in the repo). It uses the built CLI and
`curl`, writes `e2e/out/<ts>/report.json` (test id, pass/fail, duration, evidence, VM SKU, OS) and
exits non-zero on any failure. Fixtures: `examples/pg-check` (Go app: `/health` runs `SELECT 1` on
`DATABASE_URL`, `/env/<NAME>` returns a variable, `/crash` exits 1, `/write` and `/read` use a
volume, `/ws` holds a websocket, `/call?url=` calls another service with keep-alive),
`examples/node-cpu` (single threaded Node HTTP server with a CPU-bound endpoint),
`examples/node-hello` (pinned GitHub fixture), `examples/railway-json-app` (a repo whose only
config is a `railway.json` with builder `NIXPACKS`, `startCommand`, `preDeployCommand`,
`healthcheckPath`, `restartPolicyType` and an `environments.production` override), `examples/worker`
(a singleton queue consumer that records its host in Postgres), MinIO (pinned) as the S3 endpoint,
image `traefik/whoami` (pinned).

| ID | Test | Pass condition |
|---|---|---|
| E1 | Fresh install | exit 0 in < 180 s; prints console URL and setup token; recovery key only in the 0600 file when not a TTY; console has a valid certificate (no `-k`) |
| E2 | Setup and CLI login | owner created; API token works with the CLI |
| E3 | Public GitHub repo, Deploy Now, generated domain | `examples/node-hello` at its pinned commit: `SUCCESS`; HTTPS 200 with a valid certificate; total time since E1 start < 300 s |
| E4 | Docker image service | `traefik/whoami` on port 80 serves 200 over HTTPS; P8 timing recorded |
| E5 | Postgres + reference | template online; `examples/pg-check` deployed with `5050 up` and `DATABASE_URL=${{Postgres.DATABASE_URL}}`; `/health` says db ok; canvas has the edge |
| E6 | Staged changes | config patch and variable set give 2 changes with correct diffs; commit deploys; discard works; rename rewrites a reference |
| E7 | Zero downtime, public | 2 replicas, 50 rps from host 2 (or the runner) for the whole redeploy: 0 non-2xx and 0 connection errors; P7 timing recorded |
| E8 | Rollback restores variables | FOO=1 deploy, FOO=2 deploy, rollback to the first: `/env/FOO` is 1; committed variable is still 2 |
| E9 | Failure diagnosis | target port 8080 for an app on 80: `FAILED`, diagnosis names both ports, previous deployment still serves |
| E10 | Crash restart | `/crash`: replica restarts and serves again within 30 s; restart count shown |
| E11 | Logs | build log stored; deploy log has the app's line; HTTP log has the request with status; `@level:error` filter; `5050 logs --http --status 500` works |
| E12 | Metrics | after 90 s CPU and memory series have points; HTTP requests and error rate counted |
| E13 | CLI and alias | `run -- env` has resolved `DATABASE_URL`; `ssh -- echo hi`; `connect Postgres -- -c 'select 1'`; `variables set/list`; `domain` generates one; `logs`; the same through `railway` alias commands copied from Railway's docs |
| E14 | Volume | written file survives a redeploy; usage shown; a failed redeploy of a volume service restarts the old container |
| E15 | Backups | MinIO bucket set and tested; base backup ok; insert row R at T; restore to a copy at T: copy has R; promote: app sees the copy; Redis and MongoDB backups restore; MySQL shows the badge; monthly restore test triggered by hand passes; the data container's key cannot delete objects |
| E16 | State backup and restore with adopt | `5050d backup-state`; then deploy service Z; wipe `state.db`; `restore-state`: projects, variables and running apps intact, Z still running and listed under unknown containers, nothing removed until Finish restore |
| E17 | Upgrade with rollback | upgrade to a snapshot built as v0.1.0-e2e.2 through `--release-base`: version reported, apps up; upgrade to a build tagged `e2e_broken_readyz`: automatic rollback within 120 s, state intact |
| E18 | Second host | join command online in < 120 s; replicas 2 spread one per host; whoami responses from both hosts; pg-check on host 2 reaches `Postgres.internal` on host 1 |
| E19 | Host failure | power off host 2 (or drop all its traffic with iptables): routes withdrawn by 60 s, all traffic served from host 1 within 150 s, lost replicas rescheduled within 200 s, host shown offline |
| E20 | Use all cores | `examples/node-cpu` at processes 1 vs auto on a 2 vCPU non-burstable VM; load from host 2 with `oha` (pinned), 60 s warm-up, 3 runs of 60 s, median: ratio >= 1.6; SKU in report.json |
| E21 | Idempotent installer | re-run install: exit 0; no container restarted (start times unchanged) |
| E22 | Control plane process down | stop 5050d for 60 s: every app URL keeps returning 200 |
| E22b | Worker reboot with the control plane down | stop 5050d, reboot host 2: after boot, apps on host 2 serve, private DNS answers, quarantine rules are in place before any container starts |
| E22c | Agent killed | `kill -9` the agent on host 2 with 5050d stopped: DNS keeps answering from the cache after restart, apps serve, nothing is removed |
| E23 | Isolation and quarantine | another environment cannot reach `Postgres.internal`'s IP; with egress blocked, `curl https://example.com` fails while the private network works; containers cannot reach `169.254.169.254`, the host's other gateways or the mesh |
| E24 | Uninstall | `--uninstall` removes units and binaries; `--purge` leaves no 5050 containers |
| E25 | Custom domain | when `E2E_CUSTOM_DOMAIN` is set: domain goes Active with a valid certificate; expiry date shown |
| E26 | Disk full | fill the control host disk to 100%: ballast released, console shows the read-only banner, apps keep serving, writes resume after freeing space; same on a data host: database alerts, no corruption |
| E27 | Control host power off | with host 2 as a second ingress: power off host 1; apps on host 2 keep serving through host 2's ingress; the console says the control plane is not responding |
| E28 | Rebuild the control host on a new VM | after E27, reset host 1, install with `--restore` from the Litestream replica in MinIO: state back within seconds of the failure, host 2 reconnects, the Postgres that lived on host 1 shows Host lost, Restore to a copy onto host 2 and Promote: the app works |
| E29 | Agent upgrade rollback | upgrade agents to a build tagged `e2e_broken_agent`: the agent reverts within 120 s and reports the failure; apps never stopped |
| E30 | Import from Railway | against the recorded GraphQL fixture server (and a live test project when `E2E_RAILWAY_TOKEN` is set): staged project with services, variables, domains, volume and Postgres; deploy succeeds |
| E31 | Config as code | `examples/railway-json-app` with no UI settings: log says "Nixpacks config detected, building with Railpack."; start, pre-deploy and health check path come from the file; production override applies; Service API `source` map points at the right lines |
| E32 | Websockets survive | an open websocket to service A survives a redeploy of service B and a new domain on service C for 5 minutes |
| E33 | Internal zero downtime | an internal caller at 50 rps with keep-alive to `api.internal` during a redeploy of `api`: 0 errors |
| E34 | Partition | drop host 2's traffic to host 1 for 5 minutes: the singleton worker is not started elsewhere (recorded hosts stay one), web replicas move, after healing exactly one worker runs and IPs are not reused |
| E35 | Registry GC during a build | start GC while a build is queued: the build waits, then pushes; no missing blobs; pulls work during GC |
| E36 | WAL archiving outage | stop MinIO for 10 minutes: alert fires, pg_wal grows and is shown; start MinIO: archiving catches up; restore to a time inside the outage has every row |
| E37 | Cloned VM | a second agent using host 2's token with a different machine-id is refused with "Duplicate host detected"; host 2 stays connected |
| E38 | Build sandbox | a Dockerfile whose RUN steps try `127.0.0.1:2019`, the Docker gateway, the mesh registry and `169.254.169.254` fails all four; `ps auxww` during a Railpack build shows no variable value |
| E39 | Database major upgrade | changing Postgres 17 to 18 on the volume is refused with the sentence; Upgrade to a new major version creates a copy on 18 with the data |
| E40 | Monitors and email | a CPU monitor at 50% for 1 minute fires to a webhook and to Mailpit; recovery is sent too |

### 18.3 Scale and soak (`e2e/fakeagent`, `e2e/loadtest`, WP6)

- **S1**: `make loadtest` starts a real 5050d and 50 fake agents (`e2e/fakeagent`: speaks
  `pkg/proto`, reports facts, accepts desired state, reports replicas healthy after a random 1 to
  5 s, sends metrics and log lines). Creates 100 projects x 10 services and 2,000 replicas, plus 300
  environments on 3 of the hosts to hit the slot limit and its message, then measures section 16
  numbers. Also kills and reconnects 10 agents and checks convergence, and checks that the `-wal`
  file stays under 64 MB.
- **S2**: 24 hour soak on 2 VMs, required for v0.1: continuous redeploys every 10 minutes, a 10 rps
  probe, a chatty service writing 500 lines per second, hourly backups: no failed probes outside
  planned host failures, no memory growth > 20% in 5050d or agents, disk usage stable (log byte
  budgets, registry retention and WAL archiving work). It starts as soon as M3 is green and runs
  beside the other verification work.
- **S3**: idle resource measurement on a fresh 2 vCPU VM after 30 minutes with 5 services.

---

## 19. Work packages

Thirteen packages after M0. Each owns disjoint directories. Interfaces are the frozen files in
5.3. Every package ships with unit tests (18.1), a short `README.md` in its root directory
explaining its design, and passes `make lint` for its files. "Depends on" means it needs the other
package for integration testing, not to start.

### 19.0 M0: planner fixes before fan-out (planner)

1. Land every contract change in 5.4, plus `networking.allowMetadata`, `build.timeoutMin` and
   `build.configPath` in `ServiceConfig`, and the new `pkg/api` types for every route in section
   11; fix the `cpapi.go` wiring comment; regenerate `docs/contracts.sha256`.
2. Commit `pkg/manifest/golden_test.go` and switch every YAML read to `sigs.k8s.io/yaml`.
3. Commit `go.mod`/`go.sum` with every module in 3.2 and 5.4 pinned; a working `Makefile` with
   `lint`, `test`, `test-linux`, `gen-ts`, `check-public`, `dev`; `.golangci.yml`; generated
   `types.gen.ts`; MSW mocks; `internal/cp/orch/README.md` with the `data.Service` interface;
   stubs for `internal/cp/orch/data`.
4. Check facts and record them in this file: whether `sslip.io` is on the Public Suffix List
   (4.2); how the pinned Railpack reads variable values without argv (8.2); that the pinned
   BuildKit supports `--oci-worker-net=bridge` inside the docker-container driver; that Caddy's
   `dynamic a` upstream accepts a custom resolver address; Railway's public GraphQL schema for
   the import (record fixtures from a test project); and every [O] label in section 2 and 9.1.1
   against the live Railway dashboard (through the Railway MCP or a browser session), then freeze
   `web/e2e/labels.json`.
5. Copy `reference/console-web` into `web/` (WP8a then owns it).

**M0 results (2026-10-22).**

| Check | Result |
|---|---|
| sslip.io on the Public Suffix List | No. The 50 per week Let's Encrypt limit is shared with every sslip.io user (4.2) |
| Railpack reads values without argv | Yes, v0.40.1: a bare `--env NAME` inherits the value from the process environment; no env file (8.2) |
| BuildKit `--oci-worker-net=bridge` in the docker-container driver | Yes, v0.33.1: `auto, bridge, cni, host` are accepted and the image ships the CNI bridge plugins (8.2) |
| Caddy `dynamic a` with a custom resolver | Yes, v2.11.7: `resolver.addresses` takes `udp/127.0.0.1:5354` (8.4) |
| Railway GraphQL schema | Introspection works without a token. The slice the importer reads (query fields `project`, `environment(s)`, `service`, `serviceInstance`, `variables`, `domains`, `tcpProxies`, `deployments` and types `Project`, `Environment`, `Service`, `ServiceInstance`, `ServiceSource`, `ServiceDomain`, `CustomDomain`, `Volume`, `VolumeInstance`, `TCPProxy`, `Deployment`, `DeploymentTrigger`, enums `Builder`, `RestartPolicyType`, `DeploymentStatus`) is in `e2e/fixtures/railway/schema.json`. Response fixtures from a test project need a Railway project token; M0 did not create a project on anyone's account. `e2e/requests/M0-railway-import-fixtures.md` asks for them; WP1 starts from the schema |
| Railway labels (section 2 and 9.1.1) | Not checked against the live dashboard: M0 had no signed-in Railway browser session, and the Railway MCP manages infrastructure, not dashboard strings. Instead every label was checked against Railway's docs of 2026-10-22 (`docs.railway.com/llms-full.txt`): 132 of 149 console labels appear verbatim. The 16 that do not are flagged `needsLiveCheck` in `web/e2e/labels.json`: `Recent Activity`, `Creation Date`, `Configure GitHub App`, `Hide Connections`, `New Group` and `Add to Group` (the docs write "New group" and "Add to group", for workspace groups), `N changes`, `G S`, `Healthcheck Timeout` (the docs write "Healthcheck timeout"), `Every hour`, `Every week`, `Every month`, `Error Rate`, `Response Time`, and the filter examples `"phrase"` and `-word`. `labels.json` is frozen with these strings; V1 compares them with the live dashboard and a mismatch becomes a planner defect that changes section 2 and the file together |
| Console copy | `reference/console-web` copied into `web/` without `node_modules`; every item of `reuse.md` 2.2 removed (workspace name and vendor label now from the API, no private repo links, `5050.*` storage keys, no private DNS suffix or IP, no vendor names, no comments about copying another product's stylesheet, GitHub avatars only for GitHub actors and switchable off); two icons of our own fleet's apps removed; package renamed `@5050cloud/web`; MSW, Vitest and ESLint added at pinned versions; npm dependencies resolved with `--before=2026-10-08` |

### 19.1 Order: a walking skeleton, then widen

- **M1 (thin vertical slice on t1)**: minimal `install.sh` (single host), `5050d` setup and login,
  the dev server with fakeorch (WP1 milestone 1), an image service with Deploy Now, hub and engine
  with 1 replica, agent reconcile, Caddy ingress with sslip.io, a Railpack build from the pinned
  GitHub fixture, and the WP8a shell, canvas and staged banner. It must pass E1 to E4 and U1 to U3
  before anyone starts backups, mesh, upgrades, cron, the Data tab or multi-host. Packages work on
  their unit-tested parts in parallel, but integration starts here.
- **M2 (single host complete)**: variables and references, staged changes, rollouts, logs,
  metrics, templates, Postgres backups, config files, imports, the full service panel. E1 to E16,
  E21 to E26, E30, E31, E38, E39 and U1 to U30.
- **M3 (multi-host and reliability)**: mesh, second host, second ingress, failover, partition,
  upgrade and agent rollback, DR rebuild, monitors. E17 to E20, E27 to E29, E32 to E37, E40,
  U31 to U42, S1, S3; S2 starts.
- **M4 (release)**: docs numbers and screenshots final, verifiers run, fix loop until all pass,
  publish (section 21).
- From M1 on, a nightly e2e on t1 runs against `main` and posts `report.json`.

### 19.2 Integrator and the defect loop

- The **integrator** (WP6 lead, or the lead planner) merges branches in this order: WP1, WP2a,
  WP2b, WP3, WP4, WP5, WP6, WP7, WP8a, WP8b, WP8c, WP9. The integrator may make small cross-dir
  fixes (under about 30 lines, no contract change) to unblock integration, with the owning WP
  reviewing the diff before the next merge.
- **Defect file**: `e2e/defects/<id>.md` with test id (E/U/P/S/V), owning WP, evidence (log lines,
  report.json entry, screenshot path), expected vs actual. A verifier failure becomes a defect
  assigned to the owning WP with the failing id; the WP fixes it in its own dirs, and the
  verifier re-runs that id. A defect that needs a contract change goes to the planner.

### 19.3 Suite run order on the VMs

1. `single`: reset t1, install, E1 to E16, E21 to E26, E30, E31, E38, E39 (the E suite creates the
   owner through the API with the setup token).
2. `ui`: reset t1, install, Playwright U1 (consumes the setup token through the UI) then U2 to
   U42, with t2 joined for U12 and U31 to U42 where needed.
3. `multi`: reset t1 and t2, install, join, E17 to E20, E22b, E22c, E27 to E29, E32 to E37, E40.
4. `upgrade` and `dr` are part of `multi` above (E17, E28, E29). The run order never shares a
   setup token between suites.

### WP1 cp-core: control plane API and domain model
- **Dirs**: `cmd/5050d/`, `internal/cp/core/` (subpackages `config`, `store`, `auth`, `api`,
  `vars`, `changes`, `github`, `audit`, `events`, `webhooks`, `mail`, `notify`, `imports`,
  `statebackup`, `templates` (loader and creation from `templates/`), `fakeorch`),
  `pkg/configfile/`, `pkg/compose/`.
- **Scope**: sections 6.1, 6.5, 6.6 (read-only mode), 8.3, 9.3, 9.5, 9.11 (config side), 9.14,
  9.15, 9.17 (incl. Litestream, adopt mode API, rotate key, rebuild), 9.20, 9.22, 9.23 (serving),
  9.24, 11 (all routes, delegating orchestrator routes), `5050d` subcommands `init`, `serve`,
  `setup-token`, `reset-password`, `reset-2fa`, `backup-state`, `restore-state`,
  `rotate-master-key`, `upgrade`, and the `e2e_broken_readyz` build tag; serving `web.Dist`.
- **Acceptance**: 18.1 rows for core, configfile and compose pass; milestone 1 is `5050d serve
  --dev` answering every section-11 route with typed fixtures; a scripted HTTP test goes setup ->
  2FA -> login -> project -> service (image) -> variables with references -> rename -> staged
  changes -> commit (fakeorch receives the right plan) -> reveal (audited) -> tokens -> CLI auth ->
  invite link -> forgot password with a fake mailer; every route in section 11 exists with the right
  role and audit action.

### WP2a cp-orchestrator: hosts, hub, engine, scheduling, routes, telemetry
- **Dirs**: `internal/cp/orch/` except `internal/cp/orch/data/` (subpackages `hub`, `engine`,
  `sched`, `ipam`, `routes`, `logs`, `metrics`, `monitors`, `usage`, `fakeconfig`).
- **Scope**: sections 6.2, 6.3, 7, 9.1, 9.2, 9.4 (fan-out), 9.6 (actions and registry
  credentials), 9.7 (decision), 9.10, 9.11 (routes, DNS, certificates), 9.16 (trigger, agent
  rollout, maintenance), 9.17.1 (adopt), 9.18 (assembly), 9.21, 9.23 (compute); implements
  `cpapi.Orchestrator`, delegating data routes to `data.Service`, and the agent endpoints 11.6.
- **Acceptance**: 18.1 rows for engine, sched and hub pass; an in-process test runs the real
  orchestrator with `fakeconfig` and two in-memory fake agents (`pkg/proto` over a real WebSocket)
  through join, deploy with surge waves, failed health keeps the old one, rollback with snapshot,
  scale, offline withdrawal and rescheduling, singleton, partition, adopt after restore, duplicate
  host, log query fan-out with an offline host, metrics storage and a firing monitor.
- **Depends on**: WP1 for integration, WP3 for real agents, WP2b for data routes.

### WP2b cp-data: cron, backups, restore, promote, templates
- **Dirs**: `internal/cp/orch/data/`, `templates/`.
- **Scope**: sections 9.6 (retention and GC), 9.9, 9.12 (placement and wipe), 9.13, 9.19 (registry
  side), 10; implements `data.Service`.
- **Acceptance**: 18.1 row for data passes; every template renders valid configs for 1 GB, 4 GB and
  32 GB hosts (golden files) and passes a `make test-linux` smoke start; backup and restore recipes
  run against MinIO in `make test-linux` for Postgres (with point in time), Redis and MongoDB; cron
  exactly-once under a simulated restart; GC sequencing test with a fake registry.
- **Depends on**: WP2a and WP3 for integration.

### WP3 agent-runtime
- **Dirs**: `cmd/5050-agent/`, `internal/agent/core/`, `internal/agent/assets/`, `pkg/logfilter/`.
- **Scope**: sections 7.2, 7.4, 7.5, 8.1, 9.8, cron and job execution, registry container, tool
  downloads, the desired-state cache, log tail shipping, maintenance drain, `5050-agent
  join|run|rejoin|version`, self-upgrade and revert wiring to `internal/upgrade`, the
  `e2e_broken_agent` build tag.
- **Acceptance**: 18.1 rows pass; integration tests pass in `make test-linux`; `cluster.cjs` test
  with a Node image shows N workers sharing a port and singleton `CLUSTER_WORKER=1`; agent survives
  a control plane restart without touching containers; boot from cache restores routes and DNS
  with no control plane; a reconnect re-sends hello with all replicas.
- **Depends on**: WP4 and WP5 for full function (uses their stubs until then).

### WP4 agent-build
- **Dirs**: `internal/agent/build/`.
- **Scope**: section 8.2 and the build rows of 9.18.
- **Acceptance**: 18.1 rows pass; integration test builds (a) a Node repo with Railpack, (b) a
  Dockerfile repo with an `ARG`, (c) an image import, (d) the railway.json fixture with builder
  NIXPACKS, each pushed to a local `registry:2`; the second build of (a) reuses cache (faster by at
  least 40%); secrets never appear in `docker history`, logs or `ps`; the sandbox test (E38's
  Dockerfile) fails all four probes; abort stops the build within 5 s.

### WP5 agent-net: networks, mesh, DNS, firewall, ingress
- **Dirs**: `internal/agent/netw/`.
- **Scope**: section 8.4, the HTTP access log parsing and `HTTPSample` aggregation, TCP proxy,
  Caddy lifecycle, `5050-net-restore`.
- **Acceptance**: 18.1 rows pass; `make test-linux` test with two network namespaces as two
  "hosts" joined by WireGuard: containers in one environment reach each other by `<name>.internal`,
  other environments, host addresses and the metadata address are blocked, egress block works with
  an allowlist, Caddy serves a route over the unix admin socket and swaps upstreams through the
  resolver without a reload and without dropping requests (load during swap: 0 errors), an open
  websocket survives a route set reload, unchanged config does not reload Caddy, MSS clamping is in
  place.

### WP6 install, upgrade, release and e2e
- **Dirs**: `install/`, `internal/upgrade/`, `e2e/`, `examples/`, `.github/`, `Makefile`,
  `.goreleaser.yaml`, `.golangci.yml`, `LICENSE`, `LICENSING.md`, per-directory MIT `LICENSE`
  files, `.gitignore`, `go.mod`/`go.sum` (after M0: final tidy only).
- **Scope**: sections 4, 9.16 (mechanics, key rotation), 17, 18.2, 18.3, the integrator role (19.2).
- **Also**: `e2e/banned-words.txt`, the contract hash check, the Railway GraphQL fixture server, the
  pull-through cache and Mailpit in the harness.
- **Acceptance**: `shellcheck` clean; installer passes E1, E21, E24 on a fresh Ubuntu 24.04 VM
  through `--release-base`; `internal/upgrade` tests pass; `make release-snapshot` produces signed
  artefacts (test key); `e2e/run.sh` runs every E test and produces `report.json`; `make
  loadtest` runs S1; `make check-public` passes on the repo.
- **Depends on**: all packages for the full e2e run.

### WP7 cli
- **Dirs**: `cmd/5050/`, `internal/cli/`.
- **Scope**: section 12 including the `railway` alias, using only `pkg/` (MIT boundary).
- **Acceptance**: 18.1 row passes; `go list -deps ./cmd/5050` has no AGPL packages; against
  `5050d --dev` with fakeorch every command runs end to end where the fake allows; `up` packs
  correctly (golden tar listing with ignores); `connect` rewrites URLs and passes stdin through;
  `--json` output for list commands; a table of Railway CLI invocations from `railway-docs.md`
  section 19 maps to our calls (golden test); help text matches section 12.
- **Depends on**: WP1 for integration.

### WP8a web shell, canvas and creation flows
- **Dirs**: `web/src/app/`, `web/src/api/` (client; `types.gen.ts` and mocks are generated),
  `web/src/kit/`, `web/src/features/{auth,dashboard,new,templates,import,canvas,palette,activity}/`,
  `web/e2e/` harness (`web/e2e/specs/{auth,dashboard,new,canvas,palette,activity}/`), `web/*`
  config files, `web/embed.go`.
- **Scope**: 13 intro, 13.1 rows owned by WP8a, 13.2, 13.4, 13.6 (shared disabled-control
  component), 13.7 (shell), the router, the API client with optimistic updates and the events
  stream, `StatusGlyph`, light theme, P1 to P5, P10.
- **Acceptance**: `tsc`, `eslint`, `vitest`, `vite build` pass; works against MSW mocks then
  `5050d --dev`; U1 to U3, U5, U11, U19, U26, U27, U28, U34 to U39 pass on a real install; labels
  spec passes; Lighthouse accessibility >= 90 on dashboard and canvas.
- **Depends on**: M0, WP1 dev server. Lands first among WP8 packages.

### WP8b web service panel, deployments, logs, metrics
- **Dirs**: `web/src/features/{service,variables,service-settings,deployment,logs,metrics,shell,data,volume,backups-tab,cron}/`,
  `web/e2e/specs/{service,deployment,logs,metrics,data}/`.
- **Scope**: 13.1 rows owned by WP8b, 13.3, 13.5, P6.
- **Acceptance**: vitest for the panel pieces; U4, U6 to U10, U15 to U18, U20 to U25, U29, U42
  pass on a real install.
- **Depends on**: WP8a (router, client, kit).

### WP8c web settings, hosts, members, usage
- **Dirs**: `web/src/features/{project-settings,settings,hosts,members,account,usage,backups-settings}/`,
  `web/e2e/specs/{settings,hosts,members,usage}/`.
- **Scope**: 13.1 rows owned by WP8c.
- **Acceptance**: vitest for forms; U12, U13, U14 (settings pages), U30 to U33, U40, U41 pass on a
  real install.
- **Depends on**: WP8a.

### WP9 docs site, landing page and project docs
- **Dirs**: `site/`, `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`.
- **Scope**: section 14, the README (what it is, install command, screenshot, feature list, link to
  docs, license), `site/data/railway-prices.json`, the `/deploy` button page.
- **Acceptance**: `npm run build` passes; every page in 14.2 exists with real content (no lorem,
  no "coming soon"); every CLI command in 12 and every endpoint in 11 is documented; every 14.3 row
  exists; `claims.json` maps every landing claim to evidence and its tested condition; link check
  clean; Lighthouse performance and accessibility >= 90 on mobile; no horizontal scroll at 375 px;
  `/llms.txt` and `.md` pages generated; no em dashes or banned words; screenshots from WP8's UI
  suite.
- **Depends on**: WP8 for screenshots, WP6 for E20 numbers.

---

## 20. Verifiers

Each verifier reports pass or fail with evidence (test ids, files, screenshots, measurements). A
fail lists concrete defects in the defect format (19.2). The release ships when all six pass on the
same commit.

| Verifier | Checks |
|---|---|
| **V1 DX vs Railway** | Works from a checklist generated from section 2 plus every DX item in section 23 (`e2e/v1-checklist.json`). Each row gets matched, better or missing, a screenshot, and the U or E id that proves it. Labels are compared mechanically (`labels.spec.ts` against `railway-ui.md` and the M0 live checks). U1 to U42 pass; P1 to P10 meet 16.1; no task in the v0.1 scope needs a file edit or SSH; a person who knows Railway can deploy a repo, add Postgres with a reference, import a Railway project and roll back without docs; failed deploys explain themselves; every 13.6 disabled control has its tooltip and Coming from Railway row |
| **V2 Install on a fresh VM** | E1 to E5, E21, E24 on a freshly reset Ubuntu 24.04 VM, timed against R1 to R3; the printed output matches 4.2; the quick start in the docs works word for word; the release path through `--release-base` |
| **V3 Scale and reliability** | E7, E10, E14 to E20, E22 to E22c, E23, E26 to E29, E32 to E37 on two fresh VMs; S1 and S3 numbers meet section 16; S2 24 hours green; upgrade and agent rollback really happen; backups and the control host rebuild really restore |
| **V4 Security and secrets** | Section 15 item by item; try: reading values without reveal, viewer escalation and project grants, CSRF without the header, cross-environment and host network access, metadata access, the build sandbox (E38), SSRF webhooks including DNS rebinding, path traversal in uploads, replaying a join token, a cloned host token, secrets in logs, image history and `ps`, Caddy admin access as a non-root user, a data container deleting backups, 2FA bypass, password reset token reuse; `make check-public` and gitleaks; deny list clean across git history |
| **V5 Docs and landing** | Section 14 checklist; every claim backed with its condition (`claims.json` against `report.json`); prices match `competitors.md` and `railway-prices.json` with dates; Honest limits complete (14.1.1); quick start reproduced on a fresh VM; CLI and API reference complete; Coming from Railway complete; mobile layout; dark and light; tone and writing rules |
| **V6 Code quality** | `make lint test cover` green with floors; no `CONTRACT-STUB`; no TODO without an issue link; contracts unchanged or changed through this spec; license boundary; SPDX headers; binary sizes (5050d < 60 MB with the UI, agent < 40 MB, CLI < 20 MB); P10 bundle budget; package READMEs exist; error messages are sentences; no dead code from the reference console |

---

## 21. Publish checklist

1. Move `docs/research/` into `reference/` (gitignored). Confirm `tools/test-vm.sh` and
   `reference/` are not tracked.
2. `make check-public DENYLIST=reference/denylist.txt` and gitleaks over the full history pass.
3. Fresh history: the public repo starts from a single squashed commit of the verified tree.
4. Create the public GitHub repository (`5050cloud/5050cloud` if the organization name is
   available, otherwise under the owner's account; update the module path and URLs in one change
   before tagging if it differs), push, add topics and description, enable Discussions and private
   vulnerability reporting.
5. Add the `MINISIGN_KEY` secret, tag `v0.1.0`, let `release.yml` publish the release.
6. Deploy the site with `site.yml`; point `5050cloud.com` at GitHub Pages when the DNS is ready.
7. Run V2's install test once more against the published release URL (no `--release-base`, no
   `--archive`). This is a re-check: the same code path already ran in every e2e run with the test
   key (17.4).

---

## 22. Deferred to v0.2

PR environments (focused and bot PR environments), Git mode (staged changes as pull requests) and
`5050 config plan|apply`, a highly available control plane, agent mTLS, Login with GitHub and
SSO/OIDC, a SQL query box and row editing in the Data tab, MySQL backups, Slack and Discord webhook
formatting, wildcard custom domains (DNS-01 with provider plugins), stable per-service virtual IPs
for internal traffic, WireGuard relay through the control host for NAT-to-NAT pairs, volume size
caps (XFS project quotas) and volume moves between hosts, Postgres high availability, a PgBouncer
toggle, Wait for CI, deployment approvals, buckets (MinIO or Garage as a first-class feature), a
community template index, the observability widget board, network flow and DNS log views, image
auto updates with maintenance windows, multi-arch services, scale to zero, IPv6, Ubuntu 22.04 and
Debian 12 as tested platforms, a Homebrew tap, an MCP server and agent skill, Cloudflare Tunnel and
DNS providers, external secret stores.

---

## 23. Decisions (revision 2)

What each critique item became. "Accepted" items are specified in the section named. Rejections
and changes say why.

### 23.1 DX critique

| Item | Decision |
|---|---|
| P0 config as code | Accepted: 9.20, D17, E31, file icon in 13.3 |
| P0 Nixpacks | Accepted as "map to Railpack with a log line" plus hints (9.20.2) and `railpackVersion` (8.2). Shipping Nixpacks itself was rejected: it is in maintenance and would double the builder surface |
| P0 Import from Railway | Accepted: 9.22, R15, E30, U35, CLI `import railway`, database move guide |
| P0 templates | Accepted: 12 app templates (10.2), detail screen, inputs form, deep link, Deploy button, U19 |
| P0 MongoDB | Accepted into v0.1 (10.1) with restic dump backups |
| P0 password recovery and email | Accepted: 9.15 (SMTP, reset, `reset-password`, verification, notification preferences) |
| P0 deploy-from-UI holes (a) to (f) | Accepted: registry credentials page and inline add (13.1, U21), Source accepts URL and switches repo/image (U20), shared scan dialog (13.2, U34), inline Connect GitHub (9.14, U31), volume service picker (U17), cron create and Run now (U18) |
| P0 UI suite does not prove R4 | Accepted: U15 to U42 |
| P0 speed budgets | Accepted: 16.1 P1 to P10 (P10 is the bundle ceiling) |
| P1 activity and bell | Accepted: 9.24 |
| P1 canvas parity | Accepted: 13.2, U38, U39 |
| P1 environment sync | Accepted: 9.5, U28 |
| P1 rename semantics | Accepted: 9.5 (rewrites in the same commit, 24 h alias, 32 characters), U37 |
| P1 provided variables and functions | Accepted: 9.3. OTEL variables are documented as absent rather than faked |
| P1 status vocabulary | Accepted: 9.1.1; aborted and superseded show `Removed` with the reason, to be confirmed live in M0 |
| P1 deployment view micro-interactions | Accepted: 13.5, Network > HTTP naming, CLI flags |
| P1 metrics gaps | Accepted, including 30-day retention (6.3) and Postgres metrics (9.10) |
| P1 monitors | Accepted in minimal form: 9.21, Critical events preset (9.15) |
| P1 volumes | Accepted the panel and Wipe Volume. Size limits: changed to "uses host disk" with alerts (9.12), because Docker named volumes on ext4 cannot be capped; XFS quotas are v0.2 |
| P1 database panel parity | Accepted: Connect, Regenerate credentials, major upgrade by restore to a copy, extensions, Redis key browser. "Allow writes" query box rejected: the build critique's cut wins (smaller V4 attack surface); `5050 connect` covers writes and 14.3 says so |
| P1 settings placement | Accepted: Railway placement kept (13.3), Processes and Placement under Scale, Railway-only fields as disabled rows |
| P1 members model | Accepted: per-project grants on top of install roles, invite links, Transfer Ownership, TOTP 2FA (9.15) |
| P1 palette | Accepted: 13.4, U26 |
| P1 CLI verb parity | Accepted except `config plan --detailed-exit-code` and the etag check: `config plan/apply` is cut for v0.1 (see build); the `railway` alias moves into v0.1 |
| P1 usage page | Accepted: 9.23, U41 |
| P1 project General and environment rename | Accepted: 13.1, U11, U41 |
| P1 domains and networking | Accepted: Retry certificate, expiry, outbound IP, IPv6 note, private domain rename semantics. Wildcard custom domains deferred to v0.2 (needs DNS-01 and provider plugins; on-demand TLS for wildcards is an abuse risk) |
| P2 Compose import | Accepted into v0.1: 9.22, U36 |
| P2 Coming from Railway rows and disabled controls | Accepted: 14.3, 13.6 |
| P2 polish items | Accepted: 13.1 (mini canvas, sort), 8.2 (image name validation), 9.9 (preset labels), 13.2 (`Cmd+Enter`), 9.1.1 (one glyph), 13.7 (mobile) |
| Process: V1 checklist and label column | Accepted: section 2 Railway label column, `labels.json`, `v1-checklist.json`, M0 live check of [O] labels |

### 23.2 Scale critique

| Item | Decision |
|---|---|
| P0 control host single point of failure | Accepted: said plainly (3.1, 14.1.1), `--roles ingress` and the two-ingress setup (9.2.1), data role default and warning (9.2), E27 |
| P0 DR on a new VM | Accepted: 9.17.2, E28 |
| P0 split brain on restore | Accepted: epoch, adopt mode, generation guard (7.2, 9.17.1), E16 deploys after the backup |
| P0 control plane RPO | Accepted: Litestream to the bucket plus hourly snapshots (9.17); RPO stated |
| P0 build isolation | Accepted: no host network, bridge workers, firewall for the build network (8.2), E38 |
| P0 Caddy admin | Accepted: unix socket 0600 (3.1, 8.4) |
| P0 secrets in argv | Accepted: values via the child environment (8.2), `ps` check in V4 and E38 |
| P0 websockets dropped on deploy | Accepted with a different mechanism: replica changes go through Caddy's dynamic upstreams and the agent's resolver, so deploys never reload Caddy; route set changes still use `/load` with `stream_close_delay 5m`. PATCHing routes by `@id` was rejected because any admin API change re-provisions the whole config in Caddy anyway. E32 |
| P1 fail open | Accepted: panic threshold (7.4) |
| P1 cutover blocks on offline hosts | Accepted: online hosts only, 15 s timeout (7.3) |
| P1 internal zero downtime | Accepted in part: draining IPs stay routable until drain ends and the docs set keep-alive limits (7.3), E33. Stable virtual IPs are v0.2 |
| P1 failover timing | Accepted: routes withdrawn at 60 s, numbers made consistent (7.1, 16), E19 uses power off or iptables |
| P1 partition duplicates | Accepted: Singleton setting, IPs held until confirmed removal (7.1, 9.2), E34 |
| P1 volume services on failure | Accepted: A restarted on failure, major version guard (7.3, 9.13), E14, E39 |
| P1 capacity and surge | Accepted: reservations, host reserve, Pending, maxSurge (9.2, 7.3) |
| P1 noisy neighbours | Accepted: app slice, BuildKit caps, OOM score protection, swap equal to memory (3.1, 8.1) |
| P1 agent restart and reboot without the control plane | Accepted: encrypted cache, net-restore before Docker, DNS from cache (7.2, 8.4), E22b, E22c |
| P1 registry GC, cache refs, credentials, S3 | Accepted: 9.6 |
| P1 container access to host and metadata | Accepted: `5050-HOSTIN`, metadata and mesh blocked by default with an Admin opt-in (8.4), E23 |
| P1 agent channel | Changed: token rotation, binding to machine-id and WireGuard key, rate limits, a separate agent hostname (7.1, 11.6). mTLS by default was cut for v0.1 by the build critique (scope); the binding and rotation close the stolen-file case except for a full host compromise, where mTLS would not help either |
| P1 cloned VMs | Accepted: duplicate host detection (7.1), E37 |
| P1 multi-ingress TLS | Accepted: one ACME primary, challenge proxy, certificate push (8.4); sslip.io PSL check in M0; staging CA and the random console label (4.1) |
| P1 WireGuard MTU and NAT | Accepted the MTU and MSS fix and NAT detection (8.4). Relay through the control host deferred to v0.2 |
| P1 disk full on the control host | Accepted: ballast, registry cap, read-only mode (6.6), E26 |
| P1 log store unbounded | Accepted: byte budgets (9.19) |
| P1 Postgres WAL growth | Accepted: WAL guard and recovery docs (9.13), E36; size_limit labelled as not enforced (9.12) |
| P1 backup integrity | Accepted: encryption, write-only key in containers, retention key on the control host, object lock docs, monthly restore test (9.13) |
| P1 Redis consistency | Accepted: completed RDB only (9.13) |
| P1 key management | Accepted: `rotate-master-key`, key file when not a TTY, S3 flags for restore (4.1, 9.17) |
| P1 cron during outages | Accepted: Honest limits, missed rows, unique slot key (9.9) |
| P1 logs of a dead host | Accepted: error and deploy line shipping, offline marker (8.1, 9.4) |
| P1 upgrades and maintenance (a) to (f) | Accepted: maintenance mode, infra image upgrades, E29, version skew, key rotation, database patch path (9.16, 9.13) |
| P2 SQLite rules | Accepted: 6.5 |
| P2 scale limits | Accepted: limits and message (8.4), S1 with 300 environments |
| P2 sysctls and preflight | Accepted: 4.2 |
| P2 build queue edge cases | Accepted: queue timeout, build timeout setting, mirrored builder images (9.1, 8.2) |
| P2 webhook SSRF | Accepted: connect-time check (9.15) |
| P2 proxies | Accepted: trusted proxies (9.11) |
| P2 operator alerts | Accepted: install notifications (9.15) |
| P2 test gaps | Accepted: E22b, E22c, E26 to E29, E32 to E38; S2 is a required 24 hour soak (the build critique's 2 hour cut is rejected for S2, because WAL, retention and memory growth do not show in 2 hours; it runs in parallel with other verification so it does not lengthen the schedule much) |
| P2 claims accuracy | Accepted: claims reworded to tested conditions (14.1), Honest limits list (14.1.1) |

### 23.3 Build critique

| Item | Decision |
|---|---|
| YAML contract bug | Accepted: `sigs.k8s.io/yaml`, golden test (3.2, 5.4) |
| Agent cache contract gap | Accepted: 7.2, E22b |
| Generation after restore | Accepted: 7.1 step 3, 9.17.1, E16 |
| cpapi wiring comment | Accepted: 5.4 |
| Template ownership | Accepted: 8.3 |
| Shared-file contention | Accepted: M0 commits go.mod, Makefile, lint config, types and mocks; worktrees; merge order (0, 19.0, 19.2) |
| Split WP2 | Accepted: WP2a and WP2b with `data.Service` pinned in the orch README |
| Split WP8 | Accepted: WP8a, WP8b, WP8c with their own feature dirs |
| Frontend has nothing to run against | Accepted: MSW mocks in M0, dev server as WP1 milestone 1 (8.3) |
| Walking skeleton | Accepted: 19.1 M1 to M4 and a nightly e2e |
| Cuts | Accepted: agent mTLS, Login with GitHub, Data tab query box, MySQL backups, `config plan/apply`, Slack/Discord formatting, E26 other-OS claim (R1 now claims only Ubuntu 24.04), Windows CLI experimental. Rejected: cutting S2 to 2 hours (see 23.2) |
| Keep list | Accepted: each KEEP feature maps to an E test (E15, E8, E20, E18, E17, E23) and claims.json enforces it |
| Let's Encrypt limits | Accepted: random console label, staging CA flag, PSL check (4.1, 4.2, 17.4) |
| Docker Hub limits | Accepted: pull-through cache and mirrors, quota preflight (4.2, 17.4) |
| Untested release path | Accepted: `--release-base` with a test key in every e2e run; `--archive` verification defined (4.1, 17.4) |
| E20 measurement | Accepted: load from host 2 with pinned `oha`, warm-up, 3 runs, median, non-burstable SKU in the report and the claim (E20, 14.1) |
| External drift | Accepted: pinned fixture repo, shallow clone fallback (9.14, 17.4) |
| E19 timers | Accepted: 7.1 step 6, 16, E19 assertions |
| Missing hooks and token contention | Accepted: `e2e_broken_readyz` to WP1, `e2e_broken_agent` to WP3, suite run order (19.3) |
| Builder infra | Accepted: `make test-linux`, VM `flock`, linux/amd64 cross-build in `make e2e` (0, 17.1) |
| Integrator and defect loop | Accepted: 19.2 |
