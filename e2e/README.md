# e2e

End-to-end harness (docs/SPEC.md 17.4, 18.2, 18.3).

| Path | What |
|---|---|
| `run.sh` | VM suite over SSH; writes `e2e/out/<ts>/report.json` |
| `keys/` | minisign test key pair; snapshots are signed with it. Never used for a tagged release |
| `signtool/` | minisign keygen, sign and verify in Go (goreleaser signs with it) |
| `scripts/release-key.sh` | creates the real release key and embeds its public key |
| `scripts/check-release-keys.sh` | install.sh and internal/upgrade embed the same keys |
| `scripts/sign-checksums.sh` | goreleaser sign hook |
| `fixtures/`, `requests/`, `defects/` | fixtures, requests to other packages, defect loop |

Run it:

```
make release-snapshot
e2e/run.sh --host ubuntu@VM_IP --acme-ca staging
```

The VM must accept SSH as a user with passwordless sudo, reach the internet,
and have ports 80 and 443 open (the console certificate is checked).

Automated today: E1 (fresh install through `--release-base`), E21 (idempotent
re-run), E25 (custom domain on a nip.io name, `E2E_CUSTOM_DOMAIN` to use a real
one), E24 (uninstall and purge). The report lists every other E test as
`not_run`.

Later work, in order:
1. E2 to E20 and E25 to E40 against the CLI, with the fixtures of 18.2
   (`examples/pg-check`, `node-cpu`, `railway-json-app`, `worker`), MinIO and
   Mailpit on the runner.
2. The pull-through registry cache passed to the install as image mirrors.
3. The Railway GraphQL fixture server.
4. `e2e/fakeagent` and `e2e/loadtest` for S1 to S3 (`make loadtest`).
5. `--host2` multi-host suite and the upgrade suites E17 and E29 (need builds
   tagged `e2e_broken_readyz` and `e2e_broken_agent`).
