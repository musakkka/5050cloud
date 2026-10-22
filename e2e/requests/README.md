# e2e/requests

Requests from builders to the planner or the integrator, one file each,
named `<work package id>-<topic>.md` (for example `WP3-proto-replica-labels.md`).
Use one when you need a change to a frozen contract (docs/SPEC.md 5.3), to
`go.mod`/`go.sum`, or to a shared file you do not own. Keep working with a
local adapter in your own directories until the request is answered.

```markdown
# <work package>: <one-line request>

- From: WP3
- To: planner | integrator
- Files: pkg/proto/proto.go
- Blocking: yes | no (what you do meanwhile)

## Why
The behaviour in docs/SPEC.md that needs it.

## Proposed change
The exact field, method or module and version.
```
