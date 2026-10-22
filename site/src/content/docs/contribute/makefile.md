---
title: The Makefile
description: Every routine task in the repo is a `make` target; `make help` lists them.
---

Every routine task in the repo is a `make` target; `make help` lists them.

| Target | Does |
|---|---|
| `make deps` | Download Go modules and install npm packages for `web/` and `site/` |
| `make web` | Build the console into `web/dist` and check its size budget |
| `make dist-placeholder` | Create a placeholder console so Go builds without Node |
| `make build` | `bin/5050d`, `bin/5050-agent`, `bin/5050` for your OS and architecture |
| `make cross` | Linux amd64 and arm64 for all three; macOS and Windows (experimental) for the CLI |
| `make test` | Go tests with `-race`, plus `npm test` in `web/` |
| `make test-linux` | Integration tests inside a privileged Linux container with the Docker socket |
| `make cover` | Coverage report; fails under the floors |
| `make lint` | `gofmt`, `go vet` and golangci-lint |
| `make gen-ts` | Regenerate the console's TypeScript API types and mocks from `pkg/api` |
| `make tools`, `make vulncheck` | Install pinned Go tools; run govulncheck |
| `make check-public` | Secret scan, banned words and em dashes, private addresses, and licence headers |
| `make dev` | Run the control plane, an agent and the console dev server locally |
| `make site` | Build this site |
| `make release-snapshot` | A signed snapshot release in `dist/`, with the test key |
| `make e2e HOST=user@ip SUITE=...` | Build, serve a snapshot and run the VM end-to-end suite |
| `make e2e-ui` | The browser end-to-end suite against `E2E_CONSOLE_URL` |
| `make loadtest` | The scale test with fake agents against a local control plane |
| `make clean` | Remove build outputs |
