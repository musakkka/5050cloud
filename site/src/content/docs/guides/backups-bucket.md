---
title: Backups bucket
description: Point 5050cloud at any S3-compatible bucket so every database and volume is backed up off the host and can be restored to a copy.
---

Point 5050cloud at any S3-compatible bucket so every database and volume is backed up off the host and can be restored to a copy.

> **Beta:** Backing up to a bucket works in 0.1.0 but is not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

## Create the bucket

1. At any S3-compatible provider, create a bucket, for example `my-5050-backups`. A MinIO server works too, as long as it runs on another machine: a backup on the same disk is lost with the disk.
2. Turn on versioning for the bucket. Turn on object lock as well if the provider has it. Then a stolen write key cannot destroy old backups.
3. Note the provider's endpoint (for example `https://s3.us-east-1.amazonaws.com`) and region. Some providers use the region `auto`.

## Create two keys

Use a prefix inside the bucket, for example `5050/`, and give each key access to that prefix only.

- **Write key.** Put, get and list. No delete. Data containers and backup jobs use it.
- **Retention key** (optional, recommended). Delete as well. Only retention jobs on the control host use it, so a leaked write key cannot erase backups.

On a provider with IAM-style policies, the write key's policy looks like this:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": ["s3:PutObject", "s3:GetObject"], "Resource": "arn:aws:s3:::my-5050-backups/5050/*" },
    { "Effect": "Allow", "Action": ["s3:ListBucket"], "Resource": "arn:aws:s3:::my-5050-backups", "Condition": { "StringLike": { "s3:prefix": ["5050/*"] } } }
  ]
}
```

The retention key's policy is the same with `"s3:DeleteObject"` added to the first statement's actions. Other providers express the same rights in their own key settings.

## Enter the settings

1. In the console, go to **Settings**, **Backups**. You need the **Admin** or **Owner** role.
2. Fill in **Endpoint**, **Bucket**, **Region** and **Prefix**.
3. Under **Write key**, fill in **Access key id** and **Secret**.
4. Under **Retention key (optional)**, fill in its **Access key id** and **Secret**.
5. Turn on **Path-style addressing** for MinIO and other providers that need it.
6. Select **Save**.

## Test it

Select **Test bucket**. It is available once a key is saved. A pass reads "The bucket accepted a write, a read and a list." A failure shows the provider's error, which usually names the problem: a wrong endpoint or region, a key without rights on the prefix, or path-style addressing turned off for a server that needs it.

## What happens on save

Data services redeploy so their backup jobs pick up the bucket, then each database takes its first base backup. Postgres starts continuous WAL archiving, so you can later restore to any point in time.

## Check every database

1. Open each database service on the canvas. The "No backup bucket is set" note is gone.
2. Open its **Backups** tab. After a few minutes it lists a first backup. **Back up now** takes one at once.

## Schedule and retention

Each database's **Backups** tab has a **Schedule** section: **Scheduled backups**, **Schedule (cron)**, **Timezone** and **Keep for (days)**, plus **Continuous WAL archiving (point-in-time restore)** for Postgres. The default is daily at 03:00 UTC, kept 7 days. Select **Save schedule** after a change.

> **Note:** The two switches under the form, **Back up the control plane state every hour and stream it with Litestream** and **Store built images in the bucket**, have no effect in 0.1.0. Control plane snapshots stay on the host's disk. [Control plane backup](/docs/guides/control-plane-backup) shows how to copy them to your bucket yourself.

## What success looks like

- **Test bucket** passes.
- Objects appear under your prefix in the bucket.
- Every database's **Backups** tab lists a recent backup.
- On the first Sunday of the month the newest backup of each database is restored into a throwaway container and checked; the tab then shows when the restore was last tested.

## Next steps

- [Backups and restore](/docs/guides/backups): what is backed up and how to restore to a copy.
- [Restore](/docs/guides/restore)
- [Control plane backup](/docs/guides/control-plane-backup)
