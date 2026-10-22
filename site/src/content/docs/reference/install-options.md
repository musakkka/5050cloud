---
title: Install options
description: Every flag of the installer, each also readable from a `FIFTY_<FLAG>` environment variable.
---

Every flag of the installer, each also readable from a `FIFTY_<FLAG>` environment variable.

## The command

```sh
curl -fsSL https://5050cloud.com/install.sh | sudo sh
curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --domain example.com --acme-email you@example.com
wget -qO- https://5050cloud.com/install.sh | sudo sh
```

The script is also at `install/install.sh` in the repo. It is POSIX `sh` and every step is safe to re-run: running it again does not restart healthy containers.

Piped into `sh`, it asks nothing. Run from a terminal, it offers a guided setup with the defaults shown. Every answer is also a flag or an environment variable, for example `FIFTY_DOMAIN=example.com`.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--version vX.Y.Z` | latest stable | Release to install |
| `--archive PATH` | none | Install from a local release tarball; `checksums.txt` and `checksums.txt.minisig` must sit next to it |
| `--release-base URL` | GitHub Releases | Download releases from another base URL; needs `--release-pubkey` |
| `--release-pubkey KEY` | the embedded key | minisign public key for `--release-base` |
| `--acme-ca staging\|production\|URL` | `production` | ACME directory |
| `--public-ip IP` | detected | Public IPv4 address used for `sslip.io` names and DNS instructions |
| `--domain example.com` | `<dashed-ip>.sslip.io` | Base domain for generated service domains (needs a wildcard A record) |
| `--console-domain NAME` | `console-<4 random>.<base domain>` | Where the console is served |
| `--acme-email EMAIL` | none | Email for Let's Encrypt notices |
| `--data-disk auto\|none\|/dev/X` | `auto` | Use a blank data disk for all state |
| `--no-swap` | swap when RAM < 8 GB | Skip the 2 GB swap file |
| `--no-auto-updates` | updates on | Skip unattended OS security updates |
| `--firewall` | off | Drop inbound traffic except SSH, 80, 443 and WireGuard from peers |
| `--join URL --token T` | none | Join an existing install as a worker host |
| `--roles app,ingress,data,build` | `app` | Roles of a joining host |
| `--ca-pin sha256:HEX` | none | Pin the console certificate when it uses an internal CA |
| `--recovery-key-file PATH` | `/etc/5050/recovery-key.txt` | Where the recovery key is written (0600) |
| `--restore s3://bucket/prefix --recovery-key K` | none | Rebuild a control host from a bucket, with `--s3-endpoint`, `--s3-region`, `--s3-access-key`, `--s3-secret-key` |
| `--cli` | | Install only the `5050` CLI |
| `--uninstall [--purge]` | | Remove units and binaries; `--purge` also deletes data, containers and volumes |
| `--force` | | Continue on an OS that is not supported |
| `--yes` | | Answer yes to every question |
| `--help` | | Show the flags |

## Supported systems

Ubuntu 24.04 (amd64, arm64) is tested. Ubuntu 22.04 and Debian 12 install with a "not tested in this release" warning.

## What it changes

- Installs Docker Engine from Docker's repository (unless Docker 24 or newer is there), plus `ca-certificates curl git iptables`.
- Formats and mounts a blank data disk at `/data` when `--data-disk auto` finds exactly one.
- Merges Docker settings into `/etc/docker/daemon.json` (`live-restore`, log rotation at 20 MB x 3).
- Writes kernel settings to `/etc/sysctl.d/90-5050.conf`.
- Verifies the release signature and checksum, unpacks it into `/opt/5050/releases/<version>/` and links `/usr/local/bin/5050d`, `5050-agent` and `5050`.
- Creates `/etc/5050/` (config, master key, host key, all 0600) and the systemd units.

## Uninstall

```sh
curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --uninstall
```

App containers and `/var/lib/5050` are kept unless you add `--purge`, which asks for confirmation unless `--yes` is also given.
