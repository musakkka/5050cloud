---
title: Upgrading and host maintenance
nav: Upgrades
description: Upgrade 5050cloud with one command that reverts itself if the new version is unhealthy, and take hosts out for OS work without downtime.
---

Upgrade 5050cloud with one command that reverts itself if the new version is unhealthy, and take hosts out for OS work without downtime.

> **Beta:** Upgrading with `5050 upgrade` works in 0.1.0 but is not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

## Before you upgrade

1. Read the release notes on the repository's Releases page. They say when a release changes the pinned Caddy image, which means about 2 seconds without ports 80 and 443 on each ingress host, one host at a time.
2. Take a state snapshot to keep next to the automatic one:

```sh
sudo 5050d backup-state --out /root/state-before-upgrade.db.age
```

## Upgrade

Pick one:

- In the console, **Settings**, **Upgrades**, select **Upgrade now**.
- From your laptop: `5050 upgrade`.
- On the control host: `sudo 5050d upgrade`.

Add `--version vX.Y.Z` to the commands to pick a release. Then:

1. The release signature and checksum are verified.
2. A state snapshot is taken.
3. The new control plane starts and must report ready within 90 seconds, with as many agents connected as before.
4. If it does not, the old version and its state are restored and the `upgrade.rolled_back` alert fires with the reason.
5. Agents are then upgraded one host at a time. An agent that cannot connect within 90 seconds goes back to its old binary and reports it.

Apps keep running throughout.

## Follow progress

**Settings**, **Upgrades** shows each step. On the control host:

```sh
journalctl -u 5050-upgrade -f
```

## When it rolls back

The console shows the old version, and the `upgrade.rolled_back` event carries the reason. Install alerts in **Settings**, **General**, **Alerts** can email it to you or send it to a webhook. Nothing needs undoing; fix the cause the reason names, or wait for the next release.

## Go back to an earlier release

To return to a known release on purpose, run the upgrade with that version:

```sh
sudo 5050d upgrade --version v0.1.0
```

If the newer release changed the database schema, the older binary may not read it. In that case restore the snapshot you took before upgrading, as described in [Restore](/docs/guides/restore#the-control-planes-state-on-the-same-host).

## Agents and version skew

Agents are upgraded after the control plane. **Settings**, **Hosts** shows each agent's version, and the `agent.version_skew` alert fires when a host runs a different version for long.

## Turn off the daily update check

The console checks once a day for a newer release and shows "Update available". To turn the check off, set `"noUpdateCheck": true` in `/etc/5050/config.json` on the control host, then restart:

```sh
sudo systemctl restart 5050d
```

There is no installer flag for it.

## OS updates

Security updates install automatically. Kernel updates that need a reboot show on **Settings**, **Hosts** with "A kernel update waits for a reboot. Start maintenance, then reboot the machine." Reboots are yours.

## Host maintenance

1. In **Settings**, **Hosts**, open the host's menu and select **Start maintenance**, or run `5050 host maintenance HOST on`.
2. Replicas without volumes move to other hosts; the rest stop in reverse dependency order (apps before their databases).
3. Upgrade Docker or the kernel and reboot.
4. Select **End maintenance**, or run `5050 host maintenance HOST off`. Services start again in dependency order.

For rolling reboots across hosts, do one host at a time and wait for **Online** before the next. Services with a volume on that host are down while it is in maintenance.

## What success looks like

- `5050d version` on the control host and **Settings**, **Upgrades** show the new version.
- Every host in **Settings**, **Hosts** shows the new agent version.
- Your apps answered throughout.

## Next steps

- [Control plane backup](/docs/guides/control-plane-backup)
- [Restore](/docs/guides/restore)
