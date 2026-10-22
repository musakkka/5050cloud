# internal/cp/cpapi

The in-process contract between the two halves of `5050d` (docs/SPEC.md 8.3),
frozen for v0.1.

- `Config` is implemented by core (WP1): committed config, variables, config
  files, settings, monitors' definitions, activity, audit, notices and events.
- `Orchestrator` is implemented by the orchestrator (WP2a, delegating data
  services to WP2b through `data.Service`, see `internal/cp/orch/README.md`):
  hosts, the agent hub, deployments, routes, logs, metrics, monitors' state,
  usage, adopt mode, backups and upgrades.

Each half tests against a fake of the other (`internal/cp/core/fakeorch`,
`internal/cp/orch/fakeconfig`). Events flow through `Config.Publish`; there is
no separate bus. The wiring in `cmd/5050d` is in the package comment.
