---
title: Install
description: Install 5050cloud on a fresh Linux VM with one command, or walk through a guided setup that asks for your domain and email.
---

Install 5050cloud on a fresh Linux VM with one command, or walk through a guided setup that asks for your domain and email.

## Before you install

- A VM with a public IPv4 address and root access, running Ubuntu 24.04 on amd64 or arm64. Ubuntu 22.04 and Debian 12 install with a "not tested in this release" warning. Any other system stops unless you pass `--force`.
- At least 1 GB of RAM (2 GB or more is recommended) and 10 GB of free disk on `/`.
- Ports 80 and 443 free on the VM and open in your cloud provider's firewall. See [Firewall](/docs/guides/firewall).
- Optional: a blank second disk of 20 GB or more. The installer finds it (`--data-disk auto`) and keeps all state on it.
- Optional: a domain you control. Without one, the console and your services get `sslip.io` names with real certificates. See [Your domains](/docs/guides/console-domain).

The installer turns on time sync (`systemd-timesyncd`) if nothing keeps the clock in sync.

## One command

On the VM:

```sh
{{INSTALL}}
```

Not root? Put `sudo` in front of `sh`:

```sh
{{INSTALL_SUDO}}
```

With wget instead of curl:

```sh
{{INSTALL_WGET}}
```

Piped like this, the script asks nothing and uses safe defaults: the `sslip.io` name for your IP, a console at `console-<4 random characters>.<base domain>`, and Let's Encrypt production certificates.

To set your own domain and an email for certificate notices in the same line:

```sh
curl -fsSL {{GET}} | sudo sh -s -- --domain apps.example.com --acme-email you@example.com
```

> **Note:** The script is the file `install/install.sh` in the repository, so you can read it before you run it. To read it first here, download it with `curl -fsSLo install.sh {{GET}}`.

## Guided setup

Download the script first, then run it from a terminal. Piped into `sh`, the script's input is the script itself, so it cannot ask anything.

```sh
{{INSTALL_GUIDED}}
```

As a user other than root, run `sudo sh install.sh` for the second part. It asks three things, with the default in brackets. Press Enter to keep a default.

1. **Base domain for your apps.** The default is the free `sslip.io` name for your IP, for example `203-0-113-10.sslip.io`. A domain of your own needs a wildcard A record to the VM first.
2. **Console address.** The default is `console-<4 random characters>.<base domain>`.
3. **Email for certificate notices.** Optional.

Every answer is also a flag and an environment variable named `FIFTY_<FLAG>`, for example `--domain` and `FIFTY_DOMAIN`, for scripts and cloud-init. `--yes` turns the questions off in a terminal too. [Install options](/docs/reference/install-options) lists every flag.

## What the installer prints

At the end it prints the console's setup link and where the recovery key is:

```text
5050cloud v0.1.0 is ready.

  Open this link to create your account (valid 24 hours, works once):
  https://console-ab12.203-0-113-10.sslip.io/setup?token=...
```

In a terminal it also prints the recovery key itself. Under cloud-init, where the output goes to a log, it prints only the file path, `/etc/5050/recovery-key.txt`.

## Create the owner account

1. Open the setup link in your browser.
2. Enter your name, email and a password of at least 10 characters.
3. You land on an empty dashboard. You are the owner of this install.

The link works once and expires after 24 hours. Lost it? On the VM, run:

```sh
sudo 5050d setup-token
```

## Move the recovery key off the host

The recovery key decrypts your backups and state snapshots. Copy the contents of `/etc/5050/recovery-key.txt` into a password manager, then delete the file:

```sh
sudo cat /etc/5050/recovery-key.txt
sudo rm /etc/5050/recovery-key.txt
```

The owner can show it again later in **Settings**, **Backups**, with **Show recovery key** and their password.

## If the certificate does not come

The installer waits up to 120 seconds for the console's certificate. When it does not arrive in time it prints a warning, finishes, and keeps retrying in the background. You do not need to run it again.

1. From your laptop, check that port 80 reaches the VM: `curl -I http://<console domain>`.
2. With your own domain, check the record: `dig +short <console domain> @1.1.1.1` must print the VM's IP.
3. Check your cloud firewall allows TCP 80 and 443 from anywhere. Let's Encrypt connects from many addresses.

Running the installer again is safe. Every step checks before it changes anything, and an existing `/etc/5050/config.json` is kept as it is.

With `--domain`, the installer checks that the console name and a random name under the base domain resolve to the server's address before it asks for a certificate. If not, it prints the A records to add and stops. Pass `--skip-dns-check` to install anyway.

## Cloud-init

Paste this into your provider's user data field. It installs on first boot with no questions and writes the recovery key where you choose:

```yaml
#cloud-config
runcmd:
  - curl -fsSL {{GET}} | sh -s -- --domain apps.example.com --acme-email you@example.com --recovery-key-file /root/5050-recovery-key.txt
```

Read the setup link afterwards with `sudo cat /etc/5050/setup-url.txt`.

## What success looks like

- `systemctl status 5050d 5050-agent` shows both units as active.
- `curl -fsS https://<console domain>/readyz` from your laptop returns 200 with no certificate warning.
- In the console, **Settings**, **Hosts** lists one host with the roles control, build, ingress, app and data.

## Next steps

- [Your domains](/docs/guides/console-domain)
- [Firewall](/docs/guides/firewall)
- [Backups bucket](/docs/guides/backups-bucket)
- [Quick start](/docs/quick-start): deploy a repo and add Postgres.
