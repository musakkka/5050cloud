# internal/cp/orch/engine

The orchestrator itself (WP2a). `orch.New` in `internal/cp/orch/stub.go`
builds it; `internal/cp/orch/README.md` is frozen and holds the shared rules.

## Status for v0.1.0

Built and covered by tests: hosts, join and connect over a real WebSocket,
desired state push, IPAM plan, placement, routes, monitors evaluation and
usage estimates. The engine compiles against `upgrade.StartControlPlane`
from WP6's `internal/upgrade`.

## Later work

- `orch.Options.AgentMTLS` is accepted but not enforced. Agents authenticate
  with their rotating host token bound to the machine id and WireGuard key.
- The full in-process acceptance run from SPEC section 19 (two fake agents
  through surge waves, failed health, rollback, scale, offline rescheduling,
  singleton, partition, adopt, duplicate host, log fan-out with an offline
  host, metrics storage and a firing monitor) is still to be written. Today
  `engine_test.go` covers join, hello, first desired state and host online.
- No unit tests yet for `hub`, `logs`, `metrics` and `fakeconfig` beyond what
  the engine test exercises.
