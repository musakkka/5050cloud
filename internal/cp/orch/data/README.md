# internal/cp/orch/data

Data services for the orchestrator (WP2b, docs/SPEC.md 9.6, 9.9, 9.12, 9.13, 9.19, 10).
`New` returns a `Service` (declared in `interface.go`, frozen in `internal/cp/orch/README.md`).

What is here:

- `cron.go`: cron scheduler with exactly-once runs across restarts, missed slots recorded (not replayed), overlap skip and queue, Run now.
- `backups.go`, `walguard.go`, `s3.go`, `bucket.go`: backup schedules, WAL-G continuous archiving for Postgres, Redis RDB and MongoDB dumps, restic for volumes, retention with the separate retention key, locked backups, the WAL guard and backup health.
- `restore.go`: restore to a copy (point-in-time for Postgres), promote, major upgrade.
- `render.go`, `catalog.go`: template rendering per deploy (files, mounts, tools, memory sizing from host facts).
- `registry.go`: registry retention and garbage collection (pause builds, read-only, collect, read-write, resume) and the monthly restore test.
- `creds.go`, `inspect.go`, `volume.go`: regenerate credentials, data summary, extensions, Redis keys, volume wipe.
- `fakerunner/`: a test `Runner`; never used by shipped code.

Tests: `go test ./internal/cp/orch/data/...`.

Later work (not in v0.1.0 package tests):

- Golden config files per template and host size (today the tests render every template at 1, 4 and 32 GB and check the output is complete, but do not compare against stored files).
- Backup and restore recipes run against a real MinIO and real database containers in `make test-linux`; the unit tests here check job shape and bookkeeping through the fake runner.
- Template smoke-starts in `make test-linux`.
