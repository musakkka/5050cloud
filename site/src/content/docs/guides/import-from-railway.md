---
title: Import from Railway
description: Bring a whole Railway project over as staged changes: services, sources, settings, variables, domains, volumes and databases.
---

Bring a whole Railway project over as staged changes: services, sources, settings, variables, domains, volumes and databases.

## Before you start

Create a **project token** in your Railway project settings. It is used once and never stored.

## Import

1. On the dashboard, select **New**, then **Import from Railway**, and paste the token. Or run `5050 import railway --token <token>`.
2. 5050cloud reads the project through Railway's public API and creates a new project with every environment.
3. Review the import report: what landed, what was skipped and why.
4. Set values for any variable flagged **Set a value** (sealed variables arrive empty).
5. Select **Deploy**.

## What comes over

| Item | How |
|---|---|
| Services and sources | Repo, branch, root directory or image |
| Settings | Builder, build and start commands, health check, restart policy, replicas, cron, watch paths, pre-deploy |
| Variables | Values of non-sealed variables, and shared variables |
| Domains | Custom domains as they are; generated domains become new generated domains |
| Volumes | Mount paths (not data) |
| TCP proxies | Recreated |
| Databases | Recognised by image and recreated from our templates, keeping their variable names |

Nothing deploys until you press **Deploy**.

## Data

Volume and database data do not move with the import. See [Move a database from Railway](/docs/guides/move-database-from-railway).

## Next steps

- [Coming from Railway](/docs/coming-from-railway)
- [Custom domains and HTTPS](/docs/guides/custom-domains)
