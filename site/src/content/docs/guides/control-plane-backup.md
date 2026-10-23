---
title: Control plane backup
description: The control plane snapshots its database every hour to the host's disk; copy those snapshots off the host so you can restore after a disk loss.
---

The control plane snapshots its database every hour to the host's disk; copy those snapshots off the host so you can restore after a disk loss.

## What is protected

`state.db` holds users, projects, settings, variables (encrypted) and deployment history. Your apps' data lives in their volumes and database backups, not here: see [Backups bucket](/docs/guides/backups-bucket).

## What ships in 0.1.0

- Every hour, `5050d` writes an encrypted snapshot to `/var/lib/5050/backups/state-<time>.db.age`.
- It keeps 24 hourly and 7 daily snapshots and deletes older ones.
- Snapshots are encrypted with your recovery key. Without the key they cannot be read.
- **Settings**, **Backups**, **Control plane state** shows **Last snapshot**.

There is no streaming and no automatic upload to the bucket in 0.1.0. The **Litestream** and **Position** fields on that page stay at "Not streaming" and "None", and the switch that mentions Litestream has no effect yet.

## Take a snapshot now

```sh
sudo 5050d backup-state --out /root/state.db.age
```

## Copy snapshots off the host

A snapshot on the same disk is lost with the disk. Copy the newest one to your bucket every hour with any S3 tool. For example, with the AWS CLI installed on the control host and its own credentials in `/root/.aws/credentials`:

```sh
sudo tee /etc/cron.hourly/5050-state-offsite >/dev/null <<'SCRIPT'
#!/bin/sh
set -eu
f=$(ls -1t /var/lib/5050/backups/state-*.db.age | head -n 1)
aws s3 cp "$f" "s3://my-5050-backups/5050/_state/" --endpoint-url https://s3.example.com
SCRIPT
sudo chmod 0755 /etc/cron.hourly/5050-state-offsite
```

Use a key for this that is separate from the backup write key, with put rights on `_state/` only. The files stay encrypted on the way and in the bucket.

## Recovery point

Up to one hour on the host, plus however often you copy snapshots off it. With the hourly copy above, a lost disk costs at most the last hour or two of console changes. Running apps and their data are not affected by it.

## What success looks like

- `sudo ls -lt /var/lib/5050/backups/` shows a new `state-*.db.age` file every hour.
- The copies appear under `_state/` in your bucket.

## Next steps

- [Restore](/docs/guides/restore)
- [Rebuild the control host](/docs/guides/rebuild-control-host)
