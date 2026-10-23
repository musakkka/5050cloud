---
title: Quick start
description: Install 5050cloud on a Linux VM, open the console, deploy a GitHub repo and add Postgres.
---

Install 5050cloud on a Linux VM, open the console, deploy a GitHub repo and add Postgres.

## What you need

- A VM with a public IPv4 address and root access. Ubuntu 24.04 is the tested OS (amd64 or arm64). 2 vCPU and 4 GB of RAM is a comfortable start.
- Ports 80 and 443 open to the internet in your cloud provider's firewall. See [Firewall](/docs/guides/firewall).
- No domain. Without one, the console and your services get `sslip.io` names with real certificates.

## Install

On the VM:

```sh
{{INSTALL}}
```

With wget:

```sh
{{INSTALL_WGET}}
```

Piped like this, the script asks no questions and uses safe defaults. For a guided setup that asks for your domain and email, download it first and run it in a terminal:

```sh
{{INSTALL_GUIDED}}
```

Every answer is also a flag or a `FIFTY_*` environment variable; see [Install](/docs/guides/install) and [Install options](/docs/reference/install-options).

The installer sets up Docker, the control plane (`5050d`), the agent, Caddy for HTTPS and a private registry. It finishes with a one-time setup link:

```text
5050cloud v0.1.0 is ready.

  Open this link to create your account (valid 24 hours, works once):
  https://console-ab12.203-0-113-10.sslip.io/setup?token=...
```

> **Note:** The recovery key is written to `/etc/5050/recovery-key.txt` (mode 0600). Copy it into a password manager and delete the file. It decrypts your backups and state snapshots.

## Create your account

1. Open the printed link in your browser.
2. Create the owner account: name, email and a password of at least 10 characters.
3. You land on an empty dashboard.

The setup link works once and expires after 24 hours. `sudo 5050d setup-token` on the host prints a new one.

## Deploy a repo

1. On the dashboard, select **New project**, then **GitHub Repository**.
2. Paste a public repo address, for example `{{REPO}}`, and set **Root directory** to `examples/node-hello`. Private repos need the [GitHub App](/docs/guides/github-app).
3. Select **Deploy Now**.
4. The service card shows **Building**, then **Deploying**, then **Active**.
5. Open the service, go to **Settings**, **Networking** and select **Generate Domain**. Open the address.

![The Networking settings of a web service: a public domain marked Active with its certificate expiry date, Generate Domain and Custom Domain buttons, a TCP proxy and the private domain web.internal](shot:networking "Settings, Networking, after Generate Domain: the name is Active with its certificate.")

## Add Postgres

1. On the canvas, select **+ Create**, then **Database**, then **Postgres**.
2. Open your app service, go to **Variables** and add `DATABASE_URL` with the value `${{Postgres.DATABASE_URL}}`.
3. Select **Deploy** in the banner at the top of the canvas to apply the staged change.

![The Variables tab of the api service: DATABASE_URL set to ${{Postgres.DATABASE_URL}} and REDIS_URL set to ${{Redis.REDIS_URL}}](shot:variables "A reference to Postgres in the Variables tab, written the way Railway writes it.")

The canvas now draws a line from your app to Postgres, the dashed lines below.

![The project canvas: web, api with 2 replicas, worker, Postgres and Redis, joined by dashed reference lines](shot:canvas "A larger project on the canvas. Each dashed line is a variable reference.")

## What success looks like

- The generated address opens your app over HTTPS with no certificate warning.
- The app's **Variables** tab shows `DATABASE_URL` resolved to the Postgres service.

## Next steps

- [Your domains](/docs/guides/console-domain) and [Custom domains and HTTPS](/docs/guides/custom-domains).
- [Backups bucket](/docs/guides/backups-bucket): turn on database backups.
- [Set up the CLI](/docs/guides/cli-setup): `5050 up` from your laptop.
- [Production checklist](/docs/production-checklist).
