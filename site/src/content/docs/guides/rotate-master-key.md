---
title: Rotate the master key
description: Replace the key that encrypts every secret in the control plane, for example after someone with host access leaves.
---

Replace the key that encrypts every secret in the control plane, for example after someone with host access leaves.

## Rotate

```sh
sudo 5050d rotate-master-key
```

The command generates a new key, re-encrypts every secret column in one transaction, writes the new recovery key to a 0600 file and prints its path, and takes a fresh snapshot.

## Afterwards

1. Store the new recovery key and delete the file from the host.
2. Keep the old recovery key until the old snapshots expire (30 days in the bucket): they are still encrypted with it.
3. Rotate API tokens and bucket keys too if you suspect a leak; the master key protects data at rest, not credentials someone already copied.
