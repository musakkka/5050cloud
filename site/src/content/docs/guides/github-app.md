---
title: GitHub App
description: Create a GitHub App for your install so 5050cloud can read private repos and deploy on every push, without copying keys by hand.
---

Create a GitHub App for your install so 5050cloud can read private repos and deploy on every push, without copying keys by hand.

## Do you need it

- **Public repos** work without it. 5050cloud checks the branch for new commits every 2 minutes and deploys them.
- **Private repos** need it, and so do deploys that start the moment you push.

## Before you start

The app's webhook and callback point at your console's address, so set that first. If you plan to move the console to your own domain, do it now: see [Your domains](/docs/guides/console-domain). The console must be reachable from the internet, because GitHub sends every push to it.

You need the **Admin** or **Owner** role in 5050cloud. To create the app under a GitHub organization you must be an owner of that organization.

## Create the app

1. In the console, go to **Settings**, **GitHub**.
2. Optional: type the organization's name in **Organization**. Leave it empty to create the app on your own GitHub account.
3. Select **Connect GitHub**. GitHub opens and shows the app it is about to create, named `5050cloud-<your install's name>`.
4. On GitHub, confirm the name and select **Create GitHub App**.
5. GitHub sends you back to the console, which stores the app's keys. No key passes through your clipboard.

Finish within an hour of selecting **Connect GitHub**, and do not restart `5050d` meanwhile. The console keeps the half-finished flow in memory, so a restart or a long wait ends in "This GitHub App link expired". Start again from step 1 if you see it.

## Install it on your repos

1. After creation, GitHub shows the install page. Choose **All repositories** or **Only select repositories**, then select **Install**.
2. Back in the console, **Settings**, **GitHub** says the app is connected and lists the accounts it is installed on.

Later:

- **Configure GitHub App** opens the app's install settings on GitHub, to add or remove repos.
- **Install on another account** installs the same app on another user or organization.

## What the app can do

The app asks for read access to repository contents, metadata, pull requests and checks, and it receives the `push` event. It cannot write to your repos.

## Deploy a private repo

On the canvas, select **+ Create**, then **GitHub Repo**. The repo picker now lists every repo the app can read. See [Deploy from GitHub](/docs/guides/deploy-from-github) for the rest.

## After a console domain change

The app's addresses are set when it is created. After you [change the console domain](/docs/guides/console-domain#change-the-console-domain-later), update them on GitHub, or pushes stop deploying:

1. On GitHub, open **Settings**, **Developer settings**, **GitHub Apps**, then your app (for an organization app, the organization's **Settings**, **Developer settings**, **GitHub Apps**).
2. In **General**, set **Webhook URL** to `https://<new console domain>/api/v1/github/webhook`.
3. Set **Callback URL** to `https://<new console domain>/api/v1/github/app/callback` and **Setup URL** to `https://<new console domain>/settings/github?installed=1`.
4. Select **Save changes**.

## Check the webhook

On GitHub, open the app's **Advanced** tab. **Recent Deliveries** lists every push sent to your console with its response code. A 200 means the console received it. A failed delivery shows the error, and **Redeliver** sends it again.

## What success looks like

- The repo picker lists your private repos.
- A push to a connected branch starts a deployment within seconds.
- **Recent Deliveries** on GitHub shows 200 responses.

## Next steps

- [Deploy from GitHub](/docs/guides/deploy-from-github)
- [Monorepos and watch paths](/docs/guides/monorepos)
