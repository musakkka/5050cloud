# WP6: 5050d subcommands and flags the installer and the upgrade helper call

- From: WP6
- To: WP1 (cmd/5050d), integrator
- Files: cmd/5050d
- Blocking: yes for a working install (the installer and upgrade code are done and call exactly this)

## Why
docs/SPEC.md 4.2 steps 7 to 9 and 9.16. `install/install.sh` and
`internal/upgrade` run `5050d`; the argv below is what they pass today.

## Proposed change
1. `5050d init --config FILE --data-dir DIR --public-ip IP --base-domain D
   --console-domain C --acme-ca URL [--acme-email E] [--release-base URL]
   --recovery-key-file PATH --setup-url-file PATH --join-token-file PATH`.
   Writes config.json, master.key, host.key, state.db, the ballast, then
   writes the recovery key (`5050R-...`, 0600), the full setup URL
   (`https://<console>/setup?token=...`, 0600) and a local agent join token
   (0600) to those files. The installer prints them; `init` itself should not
   print the recovery key. `--acme-ca` is always a full directory URL.
2. `5050d restore-state --from SRC --recovery-key K` accepts the same flags as
   `init` (the installer passes them on `--restore`) and reads `FIFTY_S3_*`.
3. `5050d serve --config /etc/5050/config.json` (the unit also sets
   `FIFTY_CONFIG`). `/healthz` answers 200 once listening on 127.0.0.1:5050.
4. `5050d upgrade-helper ARGS`: `os.Exit(upgrade.HelperMain(args, os.Stderr))`.
   `StartControlPlane` launches it through systemd-run.
5. After an upgrade rollback that restored state.db, the helper leaves
   `<data dir>/adopt-after-restore`. On start, 5050d should enter adopt mode
   (9.17.1) when that file exists and then delete it.
6. `5050-agent join` accepts `--roles app,ingress,data,build` (the installer
   passes `--roles` for joining hosts) and prints `registry-address: HOST:PORT`
   (it does today) so the installer can add it to Docker's insecure registries.
