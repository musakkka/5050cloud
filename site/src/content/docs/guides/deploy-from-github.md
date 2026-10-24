---
title: Deploy from GitHub
description: Connect a GitHub repo to a service and 5050cloud builds and deploys every push to its branch.
---

Connect a GitHub repo to a service and 5050cloud builds and deploys every push to its branch.

## Public repos

Public repos need no setup.

1. On the canvas, select **+ Add**, then **GitHub Repo**.
2. Paste the repo URL, for example `https://github.com/owner/name`.
3. Review the scan: Dockerfile or Railpack, config files found, suggested port and variable names from `.env.example` files.
4. Select **Deploy Now**, or **Add Variables** to stage the service and set variables first.

Automatic deploys for public repos poll the branch every 2 minutes.

## Private repos and push webhooks

Private repos need a GitHub App, which 5050cloud creates for you:

1. Go to **Settings**, **GitHub**. Optional: type an **Organization** to create the app there.
2. Select **Connect GitHub** and confirm on GitHub. No keys are copied by hand.
3. Install the app on the repos it may read.
4. Repo pickers now list those repos. Pushes reach 5050cloud through the app's webhook and deploy at once.

[GitHub App](/docs/guides/github-app) covers each step, organizations, and what to change after a console domain change.

Any repo picker also offers **Connect GitHub** when no app exists yet, and keeps what you typed while you connect.

The app asks for read access to contents, metadata, pull requests and checks, and the `push` event.

## Branch and root directory

In **Settings > Source**, set **Branch** (default: the repo's default branch) and **Root directory** for monorepos. See [Monorepos and watch paths](/docs/guides/monorepos).

## How the build is chosen

- A `Dockerfile` in the root directory is used when present.
- Otherwise [Railpack](https://railpack.com) detects the language and builds an image.
- A `railway.json`, `railway.toml`, `5050.yaml` or `5050.json` file overrides settings. See [Config as code](/docs/guides/config-as-code).

## Next steps

- [Variables and references](/docs/guides/variables)
- [Custom domains and HTTPS](/docs/guides/custom-domains)
- [Troubleshooting failed deploys](/docs/guides/troubleshooting)
