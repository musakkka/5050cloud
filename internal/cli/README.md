# internal/cli

The `5050` command line client (docs/SPEC.md section 12). MIT licensed, like
`cmd/5050`. It imports only `pkg/api`, `pkg/version` and permissive
third-party modules (cobra, coder/websocket, sigs.k8s.io/yaml), so
`go list -deps ./cmd/5050` never contains an AGPL package.

## Design

| File | What |
|---|---|
| `root.go` | `Main`, `App.Run` (argv[0] `railway` switches on the alias), the cobra tree and the global flags `-p -e -s --json -y` |
| `app.go` | Instance, token and link resolution, numbered pickers, confirmations, commit helper |
| `config.go` | `~/.config/5050/config.json` (0600): instances, default instance, directory links. `FIFTY_CONFIG` overrides the path |
| `client.go` | REST client for section 11: JSON calls, raw bodies, SSE streams and WebSockets, bearer token plus `X-5050-CSRF: 1` |
| `pack.go`, `ignore.go` | `5050 up` archive: tar + gzip, skips `.git`, nested `.gitignore` and `.5050ignore` (gitignore syntax with `!`, `/`, `**`), 500 MB cap |
| `railway.go` | Railway verb and flag translation, the "not available" answer, `alias install/remove` |
| `cmd_*.go` | One file per command group |

Resolution order for the instance: `FIFTY_URL`, the directory link, the
default instance. For the token: `FIFTY_TOKEN`, then `RAILWAY_TOKEN` or
`RAILWAY_API_TOKEN` when running as `railway`, then the stored token.
`-p/-e/-s` take a name or an id. Mutations that stage changes (`variables set`,
`scale`, `service source`, `volume add`, `environment edit`) commit right away
unless `--stage`; `--skip-deploys` commits without deploying.

`5050 up` archives the linked directory (or `PATH` with `--path-as-root`),
uploads it, follows the build log stream, polls the deployment until it ends,
prints the first deploy lines and `Deployed: https://...`, or the diagnosis
with exit code 1.

`5050 connect` reads `connect-info`, reveals the private URL, listens on
127.0.0.1 and forwards each local connection over the tunnel WebSocket. The
client gets the rewritten URL; passwords go through `PGPASSWORD`, `MYSQL_PWD`
and `REDISCLI_AUTH` where the client supports it. Arguments after `--` and
stdin are passed through.

## Tests

`go test ./internal/cli` runs against an in-process fake of the REST API:
login, link, status, `list --json`, variables set and commit (also through the
railway alias), `up` success and failure, the tunnel echo, URL rewriting, the
golden tar listing (`testdata/up.golden`), the golden Railway mapping
(`testdata/railway.golden`, every translation must parse) and the section 12
help check. Regenerate goldens with `go test ./internal/cli -update`.

## Later (not in v0.1.0 of the CLI)

- `ssh`: terminal resize after start (no SIGWINCH forwarding yet; the
  initial size is sent). Raw mode uses `stty`, so the interactive shell works
  on macOS and Linux; Windows gets a line-mode session.
- `ssh -- CMD` types the command into the remote shell followed by `exit $?`,
  because the shell route has no command parameter.
- End-to-end runs against `5050d --dev` with fakeorch were not done in this
  package; the fake server in `cli_test.go` covers the calls.
