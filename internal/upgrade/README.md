# internal/upgrade

Release download, verification, switch and rollback (docs/SPEC.md 9.16).

Layout on a host:

| Path | What |
|---|---|
| `/opt/5050/releases/<tag>/` | an unpacked, SHA-256 checked release (`.sha256` holds the tarball digest) |
| `/opt/5050/current` | link used by `5050d.service` and `/usr/local/bin/5050d`, `5050` |
| `/opt/5050/agent` | link used by `5050-agent.service`; switched on its own so agents follow the control plane one host at a time and can revert alone |
| `/var/lib/5050/upgrade.json` | control plane upgrade status (`api.UpgradeStatus`) |
| `/var/lib/5050/backups/pre-upgrade-<tag>/` | copy of `state.db` taken with 5050d stopped, just before the switch |
| `/var/lib/5050/agent/upgrade-pending.json` | an agent upgrade waiting for its first connect |
| `/etc/5050/release.json` | release base and extra keys written by the installer for `--release-base` |

Control plane: `Resolve` checks `checksums.txt.minisig` against the embedded
keys (current and next, `release-keys.txt`) and finds the tarball digest;
`StartControlPlane` stages the release and starts `5050d upgrade-helper`
through `systemd-run`; `RunHelper` stops 5050d, copies state.db, swaps
`current`, starts 5050d and polls `/readyz` for the target version, applied
migrations, the hub and at least as many agents as before. On failure it
swaps back, restores state.db when the migration tables changed (and leaves
`adopt-after-restore` for 9.17.1), starts the old version and records
`rolled_back`.

Agents: `UpgradeAgent` stages the release, writes the pending marker, swaps
`agent` and restarts the unit. `CheckPendingAgentUpgrade` runs at agent start:
the new agent clears the marker once it connects, and reverts (swap back,
restart) when it has not connected within 90 s of its first start or crashed
on start more than 5 times.

Later work (not in v0.1.0):
- An agent that crashes before it reaches `CheckPendingAgentUpgrade` is only
  caught by systemd restarts; a systemd `OnFailure=` revert unit would cover it.
- Infrastructure image pin changes (Caddy, registry, BuildKit) are handled by
  the agent's reconcile, not here.
- Coverage is about 73%; the 80% floor needs tests for the exec runner and
  error paths of `Stage`.
