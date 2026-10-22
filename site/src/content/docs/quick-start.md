---
title: Quick start
description: Install 5050cloud on a Linux VM, open the console, deploy a GitHub repo and add Postgres in about five minutes.
---

Install 5050cloud on a Linux VM, open the console, deploy a GitHub repo and add Postgres in about five minutes.

## What you need

- A VM with a public IPv4 address and root access. Ubuntu 24.04 is the tested OS (amd64 or arm64). 2 vCPU and 4 GB of RAM is a comfortable start.
- Ports 80 and 443 open to the internet.
- No domain is needed. Without one, services get `sslip.io` names with real certificates.

## 1. Install

On the VM, run:

```sh
curl -fsSL https://5050cloud.com/install.sh | sudo sh
```

Or with wget:

```sh
wget -qO- https://5050cloud.com/install.sh | sudo sh
```

When the script is piped it asks no questions and uses safe defaults. In a terminal (`sudo sh install.sh` after downloading it) it offers a guided setup. Every answer is also a flag or a `FIFTY_*` environment variable; see [Install options](/docs/reference/install-options).

The installer sets up Docker, the control plane (`5050d`), the agent, Caddy for HTTPS and a private registry. It finishes with the console URL and a one-time setup link:

```
5050cloud v0.1.0 is ready.

  Open this link to create your account (valid 24 hours, works once):
  https://console-ab12.203-0-113-10.sslip.io/setup?token=...
```

> **Note:** The recovery key is written to `/etc/5050/recovery-key.txt` (mode 0600). Copy it somewhere safe. It decrypts your state backups and is the only way to rebuild the control host.

## 2. Open the link

1. Open the printed link in your browser.
2. Create the owner account: name, email and a password of at least 10 characters.
3. You land on an empty dashboard.

The setup link works once and expires after 24 hours. `sudo 5050d setup-token` on the host prints a new one.

## 3. Deploy a repo

1. On the dashboard, select **New project**, then **GitHub repo**.
2. Paste a public repo URL, for example `https://github.com/musakkka/5050cloud`, and set **Root directory** to `examples/node-hello`. Private repos need the GitHub App; see [Deploy from GitHub](/docs/guides/deploy-from-github).
3. Select **Deploy Now**.
4. The service card shows **Building**, then **Deploying**, then **Active**.
5. Open the service, go to **Settings > Networking** and select **Generate Domain**. Open the URL.

## 4. Add Postgres

1. On the canvas, select **+ Create**, then **Database**, then **Postgres**.
2. Open your app service, go to **Variables** and add `DATABASE_URL` with the value `${{Postgres.DATABASE_URL}}`.
3. Select **Deploy** in the banner at the top of the canvas to apply the staged change.

The canvas now draws an edge from your app to Postgres.

## Next steps

- [The basics](/docs/basics): projects, environments, services and deployments.
- [Deploy with the CLI](/docs/guides/deploy-with-cli): `5050 up` from your laptop.
- [Backups and restore](/docs/guides/backups): turn on database backups to a bucket.
- [Production checklist](/docs/production-checklist).
