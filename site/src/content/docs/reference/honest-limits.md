---
title: Honest limits
description: What 5050cloud v0.1 does not do, said plainly, with what to do about each one.
---

What 5050cloud v0.1 does not do, said plainly, with what to do about each one.

## Beta

These work in 0.1.0 but are not yet covered by the end-to-end tests that run on fresh VMs for every release. Use them, and keep backups and a way back.

- More than one host: [adding a host](/docs/guides/add-a-host), the [private WireGuard network](/docs/guides/private-networking) between hosts, and a second ingress host.
- [Replicas](/docs/guides/replicas-zero-downtime) spread across hosts, with load balancing between them. Replicas on one host are not beta.
- Database [backups](/docs/guides/backups) to an [S3-compatible bucket](/docs/guides/backups-bucket), and [restoring](/docs/guides/restore) them.
- [Upgrades](/docs/guides/upgrading) with `5050 upgrade`, and the automatic revert when the new version is unhealthy.

## One control host

With one host, a host failure stops everything until it is rebuilt. With more hosts, apps on other hosts keep serving (with a second ingress host), but deploys, cron and the console stop until the control host is back or rebuilt.

What to do: copy state snapshots off the host ([Control plane backup](/docs/guides/control-plane-backup)), keep database backups on, join a second host with the `ingress` role, and read [Rebuild the control host](/docs/guides/rebuild-control-host): a one-command rebuild on a new VM is not supported in 0.1.0.

## Cron pauses with the control plane

Cron runs are scheduled by the control plane. While it is down they do not run; missed runs are listed, not replayed.

## Databases run on one host each

No high availability and no automatic failover. If the database's host dies, restore a backup to a copy on another host and promote it. Keep backups on.

## Volumes mean a short gap on deploys

A volume can be mounted by one container, so the old container stops before the new one starts.

## One public entry point by default

Without a second ingress host, all public traffic enters through one machine. There is no global edge, CDN or DDoS protection; put Cloudflare or a similar service in front if you need them.

## No autoscaling, no volume caps

Replicas and processes are set by you. Volumes have no size cap and share the host disk; alerts fire at 80% of the filesystem.

## Recovery point for the control plane

The control plane snapshots its state every hour to the host's own disk, keeping 24 hourly and 7 daily snapshots. Streaming to the bucket is later work. Until then the recovery point is one hour, plus however often you copy the snapshots off the host.

## Retention

Metrics are kept 30 days. Logs are kept 7 days on each host.

## IPv4 and Ubuntu

IPv4 only; containers have no IPv6. Ubuntu 24.04 is the tested OS.

## You own the machines

You own the hardware and the OS. Security updates are automated; reboots are yours. See [Upgrading and host maintenance](/docs/guides/upgrading).

## Not yet

The one list of what this release does not do yet. Other pages link here instead of naming a release.

- The Railway project importer. Use the [manual move](/docs/guides/import-from-railway).
- Master key rotation. See [Rotate the master key](/docs/guides/rotate-master-key).
- Enforcing **Require two-factor authentication for everyone** at login. The setting is stored and shown.
- Console screens for adopt mode after a restore. See [Restore](/docs/guides/restore).
