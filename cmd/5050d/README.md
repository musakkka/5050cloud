# cmd/5050d

The control plane binary. `serve` opens `state.db`, builds `core.Core`, the
orchestrator (`internal/cp/orch`) and the HTTP server (`internal/cp/core/api`)
on `config.json`'s listen address (127.0.0.1:5050 behind Caddy).

| Command | What it does |
|---|---|
| `init` | Writes `config.json`, `master.key`, `host.key`, `state.db`, the 1 GB ballast file, a setup token (`--setup-url-file`), the recovery key (`--recovery-key-file`) and a join token for the local agent (`--join-token-file`). Every flag also reads `FIFTY_<FLAG>` |
| `serve [--dev]` | Runs the control plane. `--dev` uses a local data directory, the in-memory orchestrator and writes emails to the log |
| `setup-token`, `reset-password`, `reset-2fa` | Recovery helpers that work while 5050d runs |
| `backup-state --out FILE` | `VACUUM INTO` a copy and encrypt it with age, the recovery key as scrypt passphrase |
| `restore-state --from FILE --recovery-key K` | Restores `state.db` and `master.key`; the next `serve` starts in adopt mode |
| `upgrade`, `upgrade-helper` | Release switch through `internal/upgrade` |

`rotate-master-key` and restoring from a bucket are not in this build (see
`internal/cp/core/README.md`, Later work).
