# internal/agent/build

The agent's builder. It implements `agentapi.Builder` (docs/SPEC.md 8.2): it
turns a `proto.BuildJob` into an image in the internal registry and returns a
`proto.BuildResult` with the digest, the detected provider and start command,
the parsed config-as-code file, notices, and on failure exactly one diagnosis
sentence from the catalog in 9.18.

```go
b := build.New(build.Options{
    DataDir: dataDir, DockerHost: dockerHost, Tools: tools, Network: nw,
    HostToken: live.Token, Images: live.Images,
})
res := b.Build(ctx, job, *host.RegistryPush, logf)
```

## How a build runs

| Step | File | What happens |
|---|---|---|
| Fetch | `source.go` | Git: `git init`, `git fetch --depth 1 origin <sha>` (falls back to the branch tip when a server refuses fetching by id), `git checkout`, submodules. The token travels as an `http.extraHeader` in `GIT_CONFIG_*` environment variables, never in the URL, argv or log. Uploads: downloaded with the host token, at most 500 MB, absolute paths, `..`, devices, links out of the tree and writes through links are refused. Work dir `<DataDir>/work/<build id>`, deleted afterwards. |
| Config file | `config.go` | `pkg/configfile.Load` over an `os.Root` of the checkout (a symlink in a repository can never make the agent read outside it). The overlay goes back in `BuildResult.ConfigFile`; the control plane merges it. Notices come from the same `configfile.Merge` the control plane runs. |
| Builder choice | `choose.go` | Priority per field: the file's `environments.<env>` block, the file, the service setting, then the variable (`RAILWAY_DOCKERFILE_PATH`, `FIFTY_DOCKERFILE_PATH`). `auto` uses a Dockerfile when one exists, else Railpack. `NIXPACKS` builds with Railpack and logs `Nixpacks config detected, building with Railpack.`; `nixpacks.toml` hints and `NIXPACKS_*` variables are mapped to their Railpack names. `railpackVersion` picks one of the pins. |
| Railpack | `railpack.go`, `railpackbin.go` | `railpack prepare` writes `plan.json` and `info.json` outside the build context; then `docker buildx build -f plan.json` with the pinned frontend as `BUILDKIT_SYNTAX` (the copy mirrored into the internal registry when present) and one `--secret` per variable the plan lists. The Railpack CLI is the verified tool from `agentapi.Tools`, or a pinned release downloaded and checked against its SHA-256. |
| Dockerfile | `dockerfile.go`, `buildx.go` | Only variables named by an `ARG` line become `--build-arg NAME`; `RUN --mount=type=secret,id=NAME` mounts become `--secret`. The log lists which names are visible at build time. |
| Image import | `image.go` | `go-containerregistry` copies the source image for the target platform by digest into the internal registry. Docker Hub short names are normalized; repository names must be lowercase. |
| Diagnosis | `diagnose.go` | Table-driven rules over the last 400 log lines, each with a captured fixture in `testdata/diagnose/`. |

Every command is an argv array run in its own process group with an explicit
environment (`exec.go`); nothing from the agent's own environment leaks into a
build. Abort (context cancel) sends SIGTERM to the group, which makes buildx
cancel the solve inside BuildKit, and SIGKILL after 2 seconds.

## Variable values never reach argv, layers or the log

- Railpack gets `--env NAME` on argv and the value in its environment
  (Railpack v0.40 reads a bare name from its environment).
- buildx gets `--build-arg NAME` and `--secret id=NAME,env=FIFTY_BUILD_SECRET_<n>`,
  with the values in its environment under alias names, so a variable called
  `PATH` cannot change how the CLI runs. Names the build tools use themselves
  (`PATH`, `HOME`, `DOCKER_*`, `BUILDKIT_*`, proxies, `LD_*`) are not passed.
- Railpack's `secrets-hash` build argument (it lands in a layer) is an HMAC
  under a per-host key, so it reveals nothing about the values.
- The registry push credential lives in the builder's own `DOCKER_CONFIG`
  (`<DataDir>/build/docker/config.json`, 0600).
- Every log line and the diagnosis go through a masker holding every variable
  value, the Git token and both registry passwords.

## The sandbox

`EnsureRunning` creates the buildx builder `fifty5050` (the spec calls it `5050`, but buildx names must start with a letter): driver `docker-container`,
the pinned BuildKit image, on the `5050-build` bridge (`Network.BuildNetwork`
creates it with its firewall rules), buildkitd `--oci-worker-net=bridge` so
`RUN` steps never share the host network, no `network.host` entitlement (a
`RUN --network=host` step fails), GC at 20 GB, memory capped at half the host
and CPU at `cpus-1`. It writes `buildkitd.toml` with plain HTTP to the
registry on the build network gateway and an optional Docker Hub mirror. It is
idempotent: a builder with the same image and settings is left running.

## Pins (`pins.go`)

Railpack 0.40.1 (current), 0.40.0 and 0.39.0, each with the SHA-256 of its
linux-musl release tarball per architecture and the digest of its frontend
image; BuildKit v0.33.1 by digest. The installer mirrors the frontend to
`<registry>/5050/railpack-frontend@<digest>` and BuildKit to `5050/buildkit`.

`HostConfig.Images` keys the builder reads: `buildkit` (BuildKit image),
`railpack-frontend` (frontend ref for the current Railpack pin) and
`dockerhub-mirror` (`host:port` of a Docker Hub pull-through cache).

## Tests

```sh
go test ./internal/agent/build/                     # unit tests, any OS
go test -run X -fuzz FuzzExtractArchive ./internal/agent/build/   # path safety fuzzing
make test-linux TEST_LINUX_PKGS=./internal/agent/build/          # real Docker
```

The unit tests use a fake runner and engine: builder choice, NIXPACKS mapping,
`railpackVersion`, ARG and secret extraction, the argv of every command
(asserting that no value is on it), diagnosis fixtures, tarball path safety
(with a fuzz test), abort and timeout.

The integration test (`integration_linux_test.go`, tag `integration`) runs
against the real Docker Engine with a `registry:2` behind htpasswd. It builds
a Railpack Node repo twice (the cached rebuild must take at most 60% of the
first), a Dockerfile with an `ARG`, an image import and the `railway.json`
fixture with builder `NIXPACKS`; it checks that no value appears in the image
config, history, layers, the log or any process's argv during the build;
runs E38's sandbox Dockerfile; checks that `RUN --network=host` is refused;
and aborts a build, which must stop within 5 seconds. The gateway, mesh and
metadata probes need the agent's firewall on a real host: run there as root
with `FIFTY_TEST_NETW=1` (the test then sets up the build network through
`internal/agent/netw`). When the machine has no docker CLI with buildx (the
golang image of `make test-linux`), the test copies both out of the pinned
`docker:28.5.1-cli` image.

## Status for v0.1.0

The unit tests pass on macOS and Linux. The integration test (real Docker,
cached rebuild timing, no-secret checks, sandbox probes, 5 second abort) has
not been run yet in this release cycle; run it on a Linux host as described
above before tagging. The config-file unknown-field sentence is adapted
locally until the request in `e2e/requests/WP4-configfile-unknown-field-sentence.md`
lands in `pkg/configfile`.
