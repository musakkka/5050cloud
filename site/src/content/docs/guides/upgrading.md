---
title: Upgrading and host maintenance
description: Upgrade 5050cloud with one command that reverts itself if the new version is unhealthy, and take hosts out for OS work without downtime.
---

Upgrade 5050cloud with one command that reverts itself if the new version is unhealthy, and take hosts out for OS work without downtime.

## Upgrade

Run `5050 upgrade`, select **Upgrade** in **Settings > Upgrades**, or run `sudo 5050d upgrade` on the control host. Add `--version vX.Y.Z` to pick a release.

1. The release signature and checksum are verified.
2. A state snapshot is taken.
3. The new control plane starts and must report ready within 90 seconds, with as many agents connected as before.
4. If it does not, the old version and its state are restored and `upgrade.rolled_back` fires with the reason.
5. Agents are then upgraded one host at a time. An agent that cannot connect within 90 seconds reverts to its old binary and reports it.

Apps keep running throughout. A release that changes the pinned Caddy image restarts Caddy one ingress host at a time (about 2 seconds without ports 80 and 443 on that host); the release notes say when.

The console shows "Update available" when a newer release exists (checked daily).

## OS updates

Security updates install automatically. Kernel updates that need a reboot are shown on the **Hosts** page; reboots are yours.

## Host maintenance

1. **Hosts > host menu > Maintenance**, or `5050 host maintenance HOST on`.
2. Replicas without volumes move to other hosts; the rest stop in reverse dependency order (apps before their databases).
3. Upgrade Docker or the kernel and reboot.
4. **End maintenance** (`5050 host maintenance HOST off`) starts them in dependency order.

For rolling OS reboots across hosts, do one host at a time and wait for **Online** before the next. Services with a volume on that host are down while it is in maintenance.
