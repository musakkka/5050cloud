---
title: Backups and restore to a copy
description: Back up databases and volumes to any S3-compatible bucket, and restore to a new copy at a point in time without touching the original.
---

Back up databases and volumes to any S3-compatible bucket, and restore to a new copy at a point in time without touching the original.

> **Beta:** Database backups and restore work in 0.1.0 but are not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

## Set up a bucket

1. Create a bucket at any S3-compatible provider, or a MinIO server on another machine.
2. Create two keys: a **write key** (put, get and list) and a **retention key** (delete rights as well).
3. Go to **Settings**, **Backups**, enter the endpoint, bucket, region, prefix and both keys, select **Save**, then **Test bucket**.
4. Turn on bucket versioning or object lock. Then a compromised database or a member with shell access cannot destroy history: data containers only hold the write key, and only the control host uses the retention key.

> **One bucket, one trust level:** Postgres containers hold the write key and the WAL-G encryption key in their environment, because the database archives its own WAL. Anyone who can run code inside a database (for example anyone with the database password) can read that key and use it on the whole bucket prefix tree, including other projects' backups. Until backups move out of the data containers, turn backups on only for installs where every member is trusted with every project's data, or give each team its own install.

Until a bucket is set, each database's **Backups** tab says "No backup bucket is set." [Backups bucket](/docs/guides/backups-bucket) walks through every field, with a key policy example.

## What is backed up

| Service | Method | Default schedule |
|---|---|---|
| Postgres | WAL-G: continuous WAL archiving (every 60 seconds at most) plus base backups | Daily at 03:00 UTC, kept 7 days |
| Redis | A completed RDB snapshot, with restic | Daily |
| MongoDB | `mongodump` archive, with restic | Daily |
| Any volume | restic of the mounted volume (crash consistent) | Daily |
| MySQL | Not in v0.1; use a cron service running `mysqldump` | |

Backups are encrypted with keys derived from your recovery key. Without the recovery key they cannot be read.

## Restore to a copy

1. Open the database's **Backups** tab and select **Restore**.
2. Pick a backup or, for Postgres, a point in time.
3. Pick the environment and host.

5050cloud creates a new service `<name>-restored-YYYYMMDD-HHMM` with a new volume and the source's variables. The original is never touched. The panel shows the time the copy recovered to.

## Promote

Select **Promote** on the copy. It takes the original's name (so `${{Postgres.DATABASE_URL}}` now points at it), the original becomes `<name>-old-<stamp>` and stops, dependent services redeploy, and archiving starts on the promoted copy.

## Health

- A backup older than 26 hours, failing WAL archiving or a failed job raises a `backup.failed` alert.
- On the first Sunday of each month the newest backup of each database is restored into a throwaway container and checked.

## When WAL archiving fails

If the bucket is unreachable, Postgres keeps WAL segments on its volume and retries; the card says how long archiving has failed and how big `pg_wal` is. Fix the bucket and archiving catches up. As a last resort, turn archiving off for that database; point-in-time restore stops until the next base backup.

## MySQL recipe

Create a cron service from the `mysql:8.4` image with schedule `0 3 * * *`, `MYSQL_URL=${{MySQL.MYSQL_URL}}`, and a start command that runs `mysqldump` and uploads the file to your bucket with a tool of your choice.

## Next steps

- [Control plane backup](/docs/guides/control-plane-backup)
- [Restore](/docs/guides/restore)
