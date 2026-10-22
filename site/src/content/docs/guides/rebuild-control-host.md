---
title: Rebuild the control host
description: If the control host is lost, install on a new VM with `--restore` and the cluster comes back from your bucket.
---

If the control host is lost, install on a new VM with `--restore` and the cluster comes back from your bucket.

## You need

- The bucket settings (endpoint, region, bucket and prefix, a key with read access).
- The recovery key (`5050R-...`).

## Steps

1. Create a new VM and run:

```sh
curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- \
  --restore s3://my-bucket/5050 --recovery-key 5050R-... \
  --s3-endpoint https://s3.example.com --s3-region auto \
  --s3-access-key AKIA... --s3-secret-key ...
```

2. The installer restores the state (from the continuous replica, or the newest hourly snapshot) and the new host takes over the old control host's identity and mesh address.
3. Point the console domain's DNS at the new IP (a short TTL helps). Other hosts reconnect through it and keep their tokens.
4. Services whose volumes were on the dead host show **Host lost**: select **Restore to a copy** onto a host you pick, then **Promote**. See [Backups and restore](/docs/guides/backups).
5. Images come back from the bucket when **Store images in the bucket** was on (**Settings > Backups**). Otherwise services show "Image missing: Redeploy from commit"; hosts keep running the images they already have.

## Practise

Run through this once on a spare VM, so you know your keys work.
