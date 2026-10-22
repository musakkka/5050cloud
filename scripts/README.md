# scripts

Helpers the Makefile calls. Each one explains itself in its header.

| Script | Target | What |
|---|---|---|
| `check-public.sh` | `make check-public` | Secrets (gitleaks), deny list, private IPs, em dashes, banned words, SPDX headers, CLI license boundary, contract stubs, contract hashes |
| `contracts.sh` | `make contracts` | Check or rewrite `docs/contracts.sha256` |
| `cover.sh` | `make cover` | Coverage floors of docs/SPEC.md 18.1 |
| `web-size.sh` | `make web` | The P10 bundle budget (300 KB gzipped initial JavaScript) |
| `gen-labels.go` | `make labels` | `web/e2e/labels.json` and `e2e/v1-checklist.json` from the spec (build tag `ignore`, run with `go run`) |
