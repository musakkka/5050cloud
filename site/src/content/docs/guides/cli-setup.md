---
title: Set up the CLI
nav: The CLI
description: Install the 5050 CLI on your laptop or in CI, log in to your console, and optionally make it answer to railway.
---

Install the `5050` CLI on your laptop or in CI, log in to your console, and optionally make it answer to `railway`.

## Install

On macOS or Linux:

```sh
{{INSTALL_CLI}}
```

It installs only the `5050` command, no server parts, and needs no root. It goes to `/usr/local/bin` when you can write there (or run it as root), otherwise to `~/.local/bin`. If that folder is not on your `PATH`, the installer says so; add it in your shell's profile:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

With wget:

```sh
wget -qO- {{GET}} | sh -s -- --cli
```

Archives for every platform are on the repository's Releases page.

## Log in

```sh
5050 login https://console.example.com
```

The CLI prints a code and opens the console's **Approve CLI login** page in your browser. Check that the code matches and approve it. On a machine with no browser, add `--browserless` and open the printed link anywhere you are logged in.

Check it worked:

```sh
5050 whoami
```

The CLI keeps one token per console in `~/.config/5050/config.json` (mode 0600), so you can log in to several installs.

## Tokens for servers and CI

Create a token in the console. There are two kinds:

- **Settings**, **API tokens**: an account token. It acts as you, with your role.
- A project's **Project Settings**, **Tokens**, under **Environment tokens**: a token that can deploy, read logs and set variables in one environment. It suits CI best.

Copy the token at once; it is not shown again.

Then either log in with it:

```sh
5050 login https://console.example.com --token 5050_xxxxxxxx
```

or skip login and set two variables, which suits CI best:

```sh
export FIFTY_URL=https://console.example.com
export FIFTY_TOKEN=5050_xxxxxxxx
```

A GitHub Actions step that deploys the checked-out code to a linked service:

```yaml
- name: Deploy to 5050cloud
  env:
    FIFTY_URL: ${{ secrets.FIFTY_URL }}
    FIFTY_TOKEN: ${{ secrets.FIFTY_TOKEN }}
  run: |
    curl -fsSL {{GET}} | sh -s -- --cli
    export PATH="$HOME/.local/bin:$PATH"
    5050 up -c --service web
```

`-c` (`--ci`) streams the build logs and exits when the deployment finishes, with a non-zero code if it failed.

## Link a project

In your app's folder:

```sh
5050 link
5050 up
```

`5050 link` asks for the project, environment and service, and remembers them for this folder. `5050 up` uploads the folder, builds it and deploys it.

## The railway alias

```sh
5050 alias install
```

This adds a `railway` command next to `5050`, unless a real `railway` binary is already on your `PATH`. The first time you run it, it prints `Using 5050cloud at <url>; run 5050 alias remove to undo`. As `railway`, it accepts Railway's verbs and flags and reads `RAILWAY_TOKEN` and `RAILWAY_API_TOKEN`; put a 5050cloud token in them, not a Railway one. `5050 alias remove` takes it away. See [The railway alias](/docs/cli/railway-alias).

## Upgrade the CLI

Run the install command again. It replaces the binary in place.

## What success looks like

- `5050 whoami` prints your email and the console's address.
- `5050 status` (or `railway status` with the alias) prints the linked project, environment and service.

## Next steps

- [Deploy with the CLI](/docs/guides/deploy-with-cli)
- [CLI overview](/docs/cli/overview)
- [Global options](/docs/cli/global-options)
