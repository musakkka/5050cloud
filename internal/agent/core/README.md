# internal/agent/core

The agent runtime (docs/SPEC.md 8.1): the control plane session, hello and
reconnect, reconcile of the desired state, health checks and restarts, jobs
and cron runs, logs with byte budgets, metrics sampling, the registry
container, tool downloads, the encrypted desired-state cache with boot apply,
disk ballast, maintenance drain and the agent side of self-upgrade.

`cmd/5050-agent` wires it up (`join`, `run`, `rejoin`). `pkg/logfilter` is
the log filter parser and matcher (9.4). `internal/agent/assets` holds
`cluster.cjs`, the Node wrapper for "use all cores" (9.7).

Unit tests run on any OS against the fake engine in `engine/fake.go`:

    go test ./internal/agent/core/... ./pkg/logfilter ./internal/agent/assets

## Later work

- Linux integration tests against real Docker (network, replica, health
  check, swap, logs, metrics, exec, job, cluster wrap with Node) for
  `make test-linux` are not written yet.
- Self-upgrade calls `upgrade.UpgradeAgent` and
  `upgrade.CheckPendingAgentUpgrade` from `internal/upgrade` (WP6); the
  agent builds once those functions exist there.
