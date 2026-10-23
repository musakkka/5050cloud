---
title: CLI overview
description: The `5050` CLI deploys, inspects and manages your services from a terminal or CI, and also answers to `railway`.
---

The `5050` CLI deploys, inspects and manages your services from a terminal or CI, and also answers to `railway`.

## Install

The CLI installs on macOS, Linux and Windows (Windows builds are experimental). On macOS or Linux:

```sh
curl -fsSL {{GET}} | sh -s -- --cli
```

It goes to `/usr/local/bin` when that is writable (or you run it as root), else to `~/.local/bin`. Run `5050 alias install` to also answer to `railway`. Release archives for every platform are on the GitHub Releases page.

## Log in and deploy

```sh
5050 login https://console.example.com
cd my-app
5050 init -n my-app
5050 up --new
```

## Commands

| Command | What it does |
|---|---|
| [login](/docs/cli/login), [logout](/docs/cli/logout), [whoami](/docs/cli/whoami) | Manage the stored token |
| [init](/docs/cli/init), [link](/docs/cli/link), [unlink](/docs/cli/unlink) | Create and link projects |
| [status](/docs/cli/status), [list](/docs/cli/list), [open](/docs/cli/open) | See where you are |
| [up](/docs/cli/up) | Upload this directory and deploy it |
| [redeploy](/docs/cli/redeploy), [restart](/docs/cli/restart), [down](/docs/cli/down) | Act on the active deployment |
| [deploy](/docs/cli/deploy), [templates](/docs/cli/templates) | Deploy templates |
| [add](/docs/cli/add) | Add a database, repo, image or empty service |
| [import](/docs/cli/import) | Import from Railway, Docker Compose or Heroku |
| [logs](/docs/cli/logs) | Deploy, build and HTTP logs |
| [variables](/docs/cli/variables) | List, set and delete variables |
| [run](/docs/cli/run), [shell](/docs/cli/shell) | Run local commands with the service's variables |
| [ssh](/docs/cli/ssh) | Shell inside a running replica |
| [connect](/docs/cli/connect) | Database clients through a tunnel |
| [domain](/docs/cli/domain), [tcp-proxy](/docs/cli/tcp-proxy) | Public endpoints |
| [environment](/docs/cli/environment), [service](/docs/cli/service) | Switch and change environments and services |
| [scale](/docs/cli/scale), [volume](/docs/cli/volume), [cron](/docs/cli/cron) | Replicas, volumes and cron runs |
| [metrics](/docs/cli/metrics), [usage](/docs/cli/usage) | Resource use |
| [config](/docs/cli/config) | Export settings to a file |
| [host](/docs/cli/host), [upgrade](/docs/cli/upgrade) | Admin: hosts and upgrades |
| [alias](/docs/cli/alias), [completion](/docs/cli/completion), [version](/docs/cli/version) | The CLI itself |

## Next steps

- [Global options](/docs/cli/global-options)
- [The railway alias](/docs/cli/railway-alias)
