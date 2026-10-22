---
title: Honest limits
description: What 5050cloud v0.1 does not do, said plainly, with what to do about each one.
---

What 5050cloud v0.1 does not do, said plainly, with what to do about each one.

## One control host

With one host, a host failure stops everything until it is rebuilt. With more hosts, apps on other hosts keep serving (with a second ingress host), but deploys, cron and the console stop until the control host is back or rebuilt.

What to do: set a bucket so state streams off the host, turn on **Store images in the bucket**, join a second host with `--roles app,ingress`, and practise [rebuilding the control host](/docs/guides/rebuild-control-host).

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

With a bucket, control plane state is replicated within seconds. Without one, the recovery point is the last hourly snapshot, on the same disk.

## Retention

Metrics are kept 30 days. Logs are kept 7 days on each host.

## IPv4 and Ubuntu

IPv4 only; containers have no IPv6. Ubuntu 24.04 is the tested OS.

## You own the machines

You own the hardware and the OS. Security updates are automated; reboots are yours. See [Upgrading and host maintenance](/docs/guides/upgrading).
