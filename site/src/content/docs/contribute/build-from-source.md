---
title: Building from source
description: Build the control plane, the agent, the CLI, the console and this site from a clone of the repo.
---

Build the control plane, the agent, the CLI, the console and this site from a clone of the repo.

## Requirements

- Go (the version in `go.mod`).
- Node.js 22 and npm, for the console in `web/` and this site in `site/`.
- Docker, for Linux integration tests and for running an agent locally.

## Build

```sh
git clone {{REPO}}
cd 5050cloud
make deps
make build      # bin/5050d, bin/5050-agent, bin/5050
make web        # the console, embedded into 5050d
```

## Run locally

```sh
make dev
```

On Linux this runs `5050d` in dev mode with data in `./.dev`, an agent against your local Docker, and the console dev server on port 5173. On macOS it runs `5050d` with a fake orchestrator and the console only.

## Test

```sh
make test         # Go tests with -race, plus the console's unit tests
make test-linux   # integration tests in a privileged Linux container
make lint
```

## The site

```sh
cd site
npm run build     # into site/dist
npm test
```

## Licensing

The server is AGPL-3.0-only; the CLI, `pkg/`, `templates/`, `examples/`, `install/` and `site/` are MIT. See `LICENSING.md`. Every source file starts with an SPDX line.
