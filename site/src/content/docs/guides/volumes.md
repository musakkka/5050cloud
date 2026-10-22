---
title: Volumes
description: Give a service persistent storage that survives deploys and restarts.
---

Give a service persistent storage that survives deploys and restarts.

## Add a volume

Select **+ Create > Volume** and pick a service and mount path, right-click a service and select **Attach Volume**, or run `5050 volume add --mount /data`. One volume per service.

The volume is a Docker named volume on the service's host (on the data disk when there is one). It is owned by root; if your app runs as another user and cannot write, set **Run as user** to `0` in **Settings > Deploy** (or `RAILWAY_RUN_UID=0`).

## Size

Volumes have no fixed size: they grow into the host disk. The volume panel shows usage and the host's free space. An alert fires at 80% of the host filesystem by default (`volume.alertAtPct`). To grow, add a bigger data disk.

## Deploys

A service with a volume runs 1 replica and has a short gap on every deploy. See [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime).

## Wipe and delete

- **Wipe Volume** (type the volume name) stops the service, empties the volume and starts it again.
- **Delete volume** is a staged change. The data is kept for 48 hours before removal.

## Backups

Any volume can be backed up to your bucket with restic. See [Backups and restore](/docs/guides/backups).
