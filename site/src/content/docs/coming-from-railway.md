---
title: Coming from Railway
description: 5050cloud follows Railway names, variables and config files, so most projects move over unchanged; this page lists every difference in v0.1.
---

5050cloud follows Railway names, variables and config files, so most projects move over unchanged; this page lists every difference in v0.1.

## What stays the same

- The canvas, staged changes, environments and the service panel tabs.
- Variable names (`RAILWAY_PUBLIC_DOMAIN`, `RAILWAY_PRIVATE_DOMAIN` and the rest) are provided alongside `FIFTY_*` names, and the reference syntax `${{Service.VAR}}` and `${{shared.VAR}}` is the same.
- `railway.json` and `railway.toml` in your repo are read and applied. See [Config as code](/docs/guides/config-as-code).
- Builds use Railpack, Railway's builder. `builder: NIXPACKS` builds with Railpack and maps Nixpacks hints.
- Private names: `api.railway.internal` resolves as well as `api.internal`.
- The `railway` command works as an alias of the `5050` CLI. See [The railway alias](/docs/cli/railway-alias).
- Deployment statuses use Railway's labels.

## Differences

| Railway | In 5050cloud v0.1 | What to do instead |
|---|---|---|
| PR environments (incl. Focused and Bot PR environments) | Not yet (v0.2) | Duplicate an environment and point a service at the branch |
| Wait for CI | Not yet | Deploy from CI with `5050 up -c` or a GitHub Action calling `5050 redeploy` |
| Deployment approvals | Not yet | Turn off auto deploy and deploy by hand |
| Image Auto Updates with maintenance windows | Not yet | "Check for a newer image" in Settings > Source, or a cron that calls `5050 redeploy` |
| Serverless (app sleeping) | Not available | Scale to 0 is v0.2; idle apps cost only RAM on your own VM |
| Buckets | Not yet | The MinIO template |
| Functions | Not available | A small service or a cron service |
| Observability widget board | Not yet | The Observability page and Metrics tabs |
| AI Diagnose and fix PR | Rule-based sentence | The diagnosis sentence and the [Troubleshooting guide](/docs/guides/troubleshooting) |
| Data tab SQL editor and row editing | Read-only tables and Redis keys | `5050 connect` |
| Metrics for 30 days | Same, 30 days | |
| Wildcard custom domains | Not yet | One custom domain per name, or `--domain` with a wildcard A record for generated names |
| Static egress IPs (paid) | Free: your hosts' IPs | Settings > Networking shows them |
| Volume Live Resize | Not needed: volumes use the host disk | Add a data disk |
| Regions | Your hosts and roles | Add a host in another location |
| `railway` CLI | `5050`, plus the `railway` alias | Same commands |
| Login with GitHub, magic link email login | Email and password with 2FA | |
| Slack/Discord formatted webhooks | Signed JSON | A small relay service, or v0.2 |
| `config plan/apply` style config sync | `5050 config pull` and `railway.json` in the repo | v0.2 Git mode |

## Railway variables that are not provided

Railway's `OTEL_*` tracing variables are not set. Set them yourself to point at your own collector. See [Variables reference](/docs/reference/variables).

## Moving a project

1. [Import from Railway](/docs/guides/import-from-railway) brings services, settings, variables, domains and database services over as staged changes.
2. [Move a database from Railway](/docs/guides/move-database-from-railway) copies the data.
3. Point your DNS at your host and [add the custom domains](/docs/guides/custom-domains).

## Next steps

- [Compare: Railway](/docs/compare/railway)
- [Honest limits](/docs/reference/honest-limits)
