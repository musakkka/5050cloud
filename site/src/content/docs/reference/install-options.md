---
title: Install options
description: Every flag of the installer, each also readable from a `FIFTY_<FLAG>` environment variable.
---

Every flag of the installer, each also readable from a `FIFTY_<FLAG>` environment variable.

## The command

```sh
{{INSTALL}}
{{INSTALL_SUDO}}
{{INSTALL_WGET}}
curl -fsSL {{GET}} | sudo sh -s -- --domain example.com --acme-email you@example.com
```

Run it as root, or with `sudo` in front of `sh`. The same script is `install/install.sh` in the repository. It is POSIX `sh` and every step is safe to re-run: running it again does not restart healthy containers, and an existing `/etc/5050/config.json` is kept.

Piped into `sh`, it asks nothing. Downloaded and run from a terminal (`{{INSTALL_GUIDED}}`), it asks for the base domain, the console address and the certificate email, with the defaults shown. `--yes` turns the questions off. Every answer is also a flag or an environment variable, for example `FIFTY_DOMAIN=example.com`. See [Install](/docs/guides/install).

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--version vX.Y.Z` | latest stable | Release to install |
| `--archive PATH` | none | Install from a local release tarball; `checksums.txt` and `checksums.txt.minisig` must sit next to it |
| `--release-base URL` | GitHub Releases | Download releases from another base URL; needs `--release-pubkey` |
| `--release-pubkey KEY` | the embedded key | minisign public key for `--release-base`, or for a release when the installer has no key built in |
| `--insecure-skip-verify` | off | With `--archive` only: skip the signature check, for test builds |
| `--acme-ca staging\|production\|URL` | `production` | ACME directory |
| `--public-ip IP` | detected | Public IPv4 address used for `sslip.io` names and DNS instructions |
| `--domain example.com` | `<dashed-ip>.sslip.io` | Base domain for generated service domains (needs a wildcard A record) |
| `--console-domain NAME` | `console-<4 random>.<base domain>` | Where the console is served |
| `--acme-email EMAIL` | none | Email for Let's Encrypt notices |
| `--data-disk auto\|none\|/dev/X` | `auto` | Use a blank data disk for all state |
| `--no-swap` | swap when RAM < 8 GB | Skip the 2 GB swap file |
| `--no-auto-updates` | updates on | Skip unattended OS security updates |
| `--firewall` | off | Drop inbound traffic except TCP 22, 80, 443, the TCP proxy range 20000 to 29999 and UDP 51820, all from anywhere. See [Firewall](/docs/guides/firewall) |
| `--private` | off | Open no inbound ports: public traffic comes through a Cloudflare tunnel and SSH is turned off. Needs `--domain` and a token. See [Private mode](/docs/guides/private-mode) |
| `--tunnel-token TOKEN` | none | Cloudflare tunnel token. Prefer `FIFTY_TUNNEL_TOKEN` or the guided setup, which keep it out of your shell history and the system's sudo log |
| `--keep-ssh-from CIDR` | SSH off | With `--private`: keep SSH, reachable only from this IPv4 range |
| `--no-ssh-hardening` | hardening on | Leave SSH settings alone. Otherwise passwords are turned off when an account has a key, and root login when another account with a key can use sudo |
| `--no-audit-log` | audit log on | Skip `auditd` and its rules |
| `--join URL --token T` | none | Join an existing install as a worker host |
| `--roles app,ingress,data,build` | `app` | Shown in the join command for reference; a joining host takes its roles from the join token |
| `--ca-pin sha256:HEX` | none | Pin the console certificate when it uses an internal CA. The internal CA fallback is later work, so this has no effect in 0.1.0 |
| `--recovery-key-file PATH` | `/etc/5050/recovery-key.txt` | Where the recovery key is written (0600) |
| `--restore SOURCE --recovery-key K` | none | Planned: rebuild a lost control host from its state. Not usable in 0.1.0: a bucket address is refused and a local file stops the installer after the restore. See [Rebuild the control host](/docs/guides/rebuild-control-host) |
| `--s3-endpoint URL`, `--s3-region R`, `--s3-access-key K`, `--s3-secret-key S` | none | Bucket settings for `--restore` |
| `--purge` | | With `--uninstall`: also delete data, containers and volumes |
| `--cli` | | Install only the `5050` CLI |
| `--uninstall [--purge]` | | Remove units and binaries; `--purge` also deletes data, containers and volumes |
| `--force` | | Continue on an OS that is not supported |
| `--yes` | | Answer yes to every question |
| `--help` | | Show the flags |

## Certificates at install time

The installer waits up to 120 seconds for the console's Let's Encrypt certificate. If it does not come, it warns and keeps retrying in the background. Switching the console to an internal certificate authority after that time is later work. The update check is not an installer flag: set `"noUpdateCheck": true` in `/etc/5050/config.json` to turn it off.

## Supported systems

Ubuntu 24.04 (amd64, arm64) is tested. Ubuntu 22.04 and Debian 12 install with a "not tested in this release" warning.

## What it changes

- Installs Docker Engine from Docker's repository (unless Docker 24 or newer is there), plus `ca-certificates curl git iptables`.
- Formats and mounts a blank data disk at `/data` when `--data-disk auto` finds exactly one.
- Merges Docker settings into `/etc/docker/daemon.json` (`live-restore`, log rotation at 20 MB x 3).
- Writes kernel settings to `/etc/sysctl.d/90-5050.conf`.
- Verifies the release signature and checksum, unpacks it into `/opt/5050/releases/<version>/` and links `/usr/local/bin/5050d`, `5050-agent` and `5050`.
- Writes `/etc/ssh/sshd_config.d/10-5050-hardening.conf`, when that cannot lock anyone out: see `--no-ssh-hardening`.
- Installs `auditd` with rules in `/etc/audit/rules.d/5050.rules`, which record changes to accounts, SSH, the firewall, Docker settings, systemd units and `/etc/5050`. Logs stay in `/var/log/audit`.
- Creates `/etc/5050/` (config, master key, host key, all 0600) and the systemd units.

## Uninstall

```sh
curl -fsSL {{GET}} | sudo sh -s -- --uninstall
```

App containers and `/var/lib/5050` are kept unless you add `--purge`, which asks for confirmation unless `--yes` is also given.
