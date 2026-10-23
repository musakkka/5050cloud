---
title: Rotate the master key
description: Rotating the key that encrypts every secret in the control plane is not available in 0.1.0; this page says how to protect the key until it is.
---

Rotating the key that encrypts every secret in the control plane is not available in 0.1.0; this page says how to protect the key until it is.

## In 0.1.0

The command exists and refuses:

```sh
sudo 5050d rotate-master-key
```

```text
rotate-master-key is not available in this release; keep the current recovery key safe
```

## Until it ships

- Keep the recovery key in a password manager, not on the host. It decrypts your backups and state snapshots.
- Keep `/etc/5050/` readable by root only. The installer writes the master key, the host key and `config.json` there with mode 0600.
- Limit who has root or `sudo` on the control host.
- If someone with host access leaves, rotate what they could have copied: API tokens in **Settings**, **API tokens**, the bucket keys in **Settings**, **Backups**, and SMTP and registry passwords. The master key protects data at rest, not credentials someone already read.

## What rotation will do

When it ships, the command will create a new key, re-encrypt every secret in one transaction, write the new recovery key to a 0600 file and take a fresh snapshot. Old snapshots stay encrypted with the old key, so keep the old recovery key until they expire.

## Next steps

- [Security model](/docs/reference/security-model)
- [Control plane backup](/docs/guides/control-plane-backup)
