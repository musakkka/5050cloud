---
title: Rebuild the control host
description: What to do when the control host is lost in 0.1.0, what is kept safe, and what the planned one-command rebuild will do.
---

What to do when the control host is lost in 0.1.0, what is kept safe, and what the planned one-command rebuild will do.

## The state of things in 0.1.0

A one-command rebuild of a lost control host, with the other hosts reconnecting on their own, is not supported end to end in this release. The installer's `--restore` flag passes a bucket address to `5050d restore-state`, which refuses it ("restoring straight from a bucket is not available in this release"), and taking over the old host's identity is not wired up yet.

Apps on other hosts keep running and, with a second ingress host, keep serving. Deploys, cron and the console stop until a control host is back.

## Be ready

- Keep the recovery key outside the host, in a password manager.
- Copy state snapshots off the host every hour: see [Control plane backup](/docs/guides/control-plane-backup).
- Keep database and volume backups on: see [Backups bucket](/docs/guides/backups-bucket).
- Use your own domain for the console and keep its DNS TTL short, so you can point it at a new VM quickly.

## Recover by hand

[Restore](/docs/guides/restore#a-lost-control-host) lists the manual steps: a fresh install, the same bucket, databases restored from their backups, services recreated from their repos, and the other hosts joined again.

## What the rebuild will do

When it ships, installing on a new VM with `--restore`, your bucket and the recovery key will bring back the control plane's state, take over the old control host's identity and mesh address, and let the other hosts reconnect through the console's domain. Services whose volumes were on the dead host will show **Host lost**, ready to restore to a copy on another host.

## Next steps

- [Restore](/docs/guides/restore)
- [Honest limits](/docs/reference/honest-limits)
