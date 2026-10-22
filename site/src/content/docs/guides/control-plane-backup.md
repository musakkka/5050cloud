---
title: Backing up the control plane
description: The control plane keeps all projects, settings and encrypted variables in one database that is snapshotted hourly and, with a bucket, streamed continuously.
---

The control plane keeps all projects, settings and encrypted variables in one database that is snapshotted hourly and, with a bucket, streamed continuously.

## What is protected

`state.db` holds users, projects, settings, variables (encrypted) and deployment history. Your apps' data is in their volumes and database backups, not here.

## Layers

| Layer | When | Where | Recovery point |
|---|---|---|---|
| Continuous replication (Litestream) | With a bucket set | `<bucket>/<prefix>/_state/litestream/` | About 1 second |
| Hourly snapshot | Always | `/var/lib/5050/backups/`, and the bucket when set | 1 hour |

Snapshots are encrypted with your recovery key. 24 hourly and 7 daily snapshots are kept on disk; the bucket keeps them 30 days. **Settings > Backups** shows the replication position and lag.

Without a bucket, snapshots sit on the same disk as the database. Set a bucket.

## On-demand snapshot

```sh
sudo 5050d backup-state --out /root/state.db.age
```

## Restore on the same host

```sh
sudo 5050d restore-state --from litestream --recovery-key 5050R-...
```

`--from` also takes a file or `s3://bucket/key`, with `--s3-endpoint`, `--s3-region`, `--s3-access-key` and `--s3-secret-key` (the bucket keys are inside the encrypted state, so you pass them again).

After a restore the control plane starts in adopt mode: it removes nothing. Containers deployed after the snapshot keep running and are listed for you to **Adopt** or **Remove**; hosts that joined after the snapshot are listed for **Re-adopt**. Select **Finish restore** when the list is clean.

## Next steps

- [Rebuild the control host](/docs/guides/rebuild-control-host)
