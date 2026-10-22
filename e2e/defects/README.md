# e2e/defects

One file per defect, named `<id>.md`, where `<id>` is the failing test or
check id plus a short slug (for example `E9-diagnosis-port.md`). Verifiers
write them; the owning work package fixes the defect in its own directories
and the verifier re-runs that id (docs/SPEC.md 19.2).

```markdown
# <test id>: <one-line title>

- Test: E9 (or U8, P3, S1, V4, ...)
- Owner: WP2a
- Found by: V3 on <commit>
- Status: open | fixed in <commit> | verified

## Expected
What section 1, 2, 16 or 18 of docs/SPEC.md says should happen.

## Actual
What happened, in full sentences.

## Evidence
Log lines, the report.json entry, screenshot paths under e2e/out/.
```

A defect that needs a contract change is assigned to the planner instead.
