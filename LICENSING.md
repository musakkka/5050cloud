# Licensing

5050cloud is licensed under the GNU Affero General Public License v3.0 only
(`LICENSE`), except the directories below, which are MIT licensed so you can
use them in your own tools. Each of them carries its own `LICENSE` file, and
every source file starts with an `SPDX-License-Identifier` line that says
which license applies.

| Directory | License | Why |
|---|---|---|
| `cmd/5050`, `internal/cli` | MIT | The `5050` CLI, which you install on your laptop and in CI |
| `pkg/` | MIT | API, agent protocol and manifest types, for clients and integrations |
| `templates/` | MIT | Service templates you can copy into your own setups |
| `examples/` | MIT | Sample apps |
| `install/` | MIT | The installer script and systemd units |
| `site/` | MIT | The docs site and landing page |
| `internal/agent/assets/cluster.cjs` | MIT | The Node cluster wrapper |

Everything else (`cmd/5050d`, `cmd/5050-agent`, `internal/` other than
`internal/cli`, and `web/`) is AGPL-3.0-only.

The CLI never imports AGPL code: `make check-public` fails when
`go list -deps ./cmd/5050` reaches `internal/cp`, `internal/agent`,
`internal/upgrade` or `web`.
