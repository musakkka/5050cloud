---
title: Global options
description: Every `5050` command accepts the same options for picking a project, environment and service, for JSON output and for skipping prompts.
---

Every `5050` command accepts the same options for picking a project, environment and service, for JSON output and for skipping prompts.

## Options

| Option | Meaning |
|---|---|
| `-p, --project` | Project name or id (overrides the link) |
| `-e, --environment` | Environment name or id |
| `-s, --service` | Service name or id |
| `--json` | Print JSON instead of text |
| `-y, --yes` | Answer yes to confirmations |

## Configuration

- `~/.config/5050/config.json` (mode 0600) holds your consoles (URL and token) and directory links (directory to project, environment and service).
- `FIFTY_URL` and `FIFTY_TOKEN` override the stored console and token, for CI.
- When the binary runs as `railway`, `RAILWAY_TOKEN` and `RAILWAY_API_TOKEN` are read too.

## Behaviour

- The exit code is non-zero on failure.
- Output is plain text without colour when standard output is not a terminal.
- Interactive pickers are numbered prompts. In CI pass the flags instead.

## Example: deploy from CI

```sh
export FIFTY_URL=https://console.example.com
export FIFTY_TOKEN=5050_xxxxxxxx   # an environment-scoped token
5050 up -c -p shop -e production -s api
```
