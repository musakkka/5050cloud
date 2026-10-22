# pkg/api

The REST contract between `5050d` and its clients (the console and the CLI),
frozen for v0.1 (docs/SPEC.md 5.3, section 11). MIT licensed.

| File | What |
|---|---|
| `types.go` | Every request and response body. JSON names are camelCase, timestamps are Unix milliseconds, IDs are prefixed strings. |
| `routes.go` | `Routes`: every endpoint with method, path, minimum role, body types, stream kind and audit action name. It is section 11 in Go. |
| `status.go` | `StatusLabel`, the one mapping from deployment status to the word every client shows (9.1.1). |
| `internal/mockgen` | Generates the console's MSW mocks from `Routes` (`make gen-ts`). |

Tests: `routes_test.go` registers every route on a `net/http.ServeMux` (so
patterns cannot conflict), requires an audit name on every mutating route,
and reads the tables of docs/SPEC.md section 11 to check that the table and
the spec name the same paths. `tags_test.go` requires a camelCase JSON tag on
every field.

Changing anything here is a contract change: edit docs/SPEC.md first, then
the types, then run `make gen-ts` and `make contracts` (planner only).
`web/src/api/types.gen.ts` is generated from this package with tygo.
