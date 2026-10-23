---
title: Restore
description: Pick the restore that matches what you lost, a database, a volume, the control plane's state or the whole control host, and follow its steps.
---

Pick the restore that matches what you lost, a database, a volume, the control plane's state or the whole control host, and follow its steps.

> **Beta:** Restoring from backups works in 0.1.0 but is not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

| You lost | Works in 0.1.0 | Go to |
|---|---|---|
| Rows in a database, or a whole database | Yes | [A database or a volume](#a-database-or-a-volume) |
| Files in a volume | Yes | [A database or a volume](#a-database-or-a-volume) |
| Projects, settings or services in the console | Yes, on the same host | [The control plane's state](#the-control-planes-state-on-the-same-host) |
| The control host itself | Partly, by hand | [A lost control host](#a-lost-control-host) |

> **Note:** Test a restore once on a spare VM before you need one. It is the only way to know your backups, your recovery key and your notes all work.

## A database or a volume

This needs a [backups bucket](/docs/guides/backups-bucket). The original is never touched.

1. On the canvas, open the database or the service with the volume, then its **Backups** tab.
2. Select **Restore** next to a backup, or **Restore to a point in time** for Postgres.
3. In **Restore to a copy**, pick the backup or the time, then the environment and host for the copy.
4. Select **Restore**. A new service named `<name>-restored-YYYYMMDD-HHMM` starts with its own volume.
5. Select **Open the copy** and check the data, for example with `5050 connect Postgres-restored-20261022-1400`.
6. When it is right, select **Promote a copy**, pick the copy and select **Promote**.

The copy takes the original's name, so `${{Postgres.DATABASE_URL}}` now points at it. The original becomes `<name>-old-<stamp>` and stops, services that reference it redeploy, and backups start on the promoted copy. Details are in [Backups and restore](/docs/guides/backups).

## The control plane's state on the same host

The control plane keeps projects, services, settings and members in one database. It takes an encrypted snapshot every hour into `/var/lib/5050/backups/`. To roll the state back to one of them on the same control host:

1. List the snapshots and pick one:

```sh
sudo ls -lt /var/lib/5050/backups/
```

2. Stop the control plane, restore, and start it again. Use your recovery key:

```sh
sudo systemctl stop 5050d
sudo 5050d restore-state --from /var/lib/5050/backups/state-<time>.db.age --recovery-key 5050R-...
sudo systemctl start 5050d
```

3. The control plane starts in adopt mode and removes nothing. Containers that the snapshot does not know about are listed for you to keep or remove.

`restore-state` needs a file. A bucket address such as `s3://...` is refused in 0.1.0 with "restoring straight from a bucket is not available in this release"; download the snapshot from your bucket first and pass the file.

> **Not yet:** the console has no screens for adopt mode. Use the API as an Admin: `GET /api/v1/restore/unknown-replicas` lists the unknown containers, `POST /api/v1/restore/unknown-replicas/{id}/adopt` or `.../remove` decides each one, and `POST /api/v1/restore/finish` ends adopt mode.

Hosts that joined after the snapshot was taken are unknown to it. In **Settings**, **Hosts**, select **Adopt again** on each one.

## A lost control host

Rebuilding a dead control host on a new VM with all hosts reconnecting is not supported end to end in 0.1.0. Keep these ready, so a manual recovery is possible:

- the recovery key, outside the host;
- recent state snapshots copied off the host (see [Control plane backup](/docs/guides/control-plane-backup));
- database and volume backups in your bucket.

A manual recovery today looks like this:

1. Install 5050cloud on a fresh VM with the [one-line install](/docs/guides/install).
2. Point the console and base domain records at the new VM.
3. In **Settings**, **Backups**, enter the same bucket and keys.
4. Recreate each database service with the same name, then restore its newest backup into it from the bucket.
5. Recreate the app services from their repos, and copy variables back.
6. Join the other hosts again with new join commands.

See [Rebuild the control host](/docs/guides/rebuild-control-host) for what is planned.

## What success looks like

- A restored database copy shows the rows you expected and the time it recovered to.
- After a state restore, the console shows your projects and adopt mode is finished.

## Next steps

- [Backups bucket](/docs/guides/backups-bucket)
- [Control plane backup](/docs/guides/control-plane-backup)
- [Honest limits](/docs/reference/honest-limits)
