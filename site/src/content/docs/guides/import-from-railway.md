---
title: Move a project from Railway
description: Move a Railway project to 5050cloud by recreating its services, copying its variables and moving its database data; the one-step importer comes in a later release.
---

Move a Railway project to 5050cloud by recreating its services, copying its variables and moving its database data; the one-step importer comes in a later release.

> **Note:** `5050 import railway` and **Import from Railway** in the console answer "Import from Railway is not available in this build yet" in 0.1.0. The steps below do the same job by hand, and most of them are one command each.

## Take stock

With Railway's CLI, in a folder linked to the project:

```sh
railway status
railway variables --json --service web > web.json
```

Run the second line once per service, changing `--service` and the file name. Write down each service's repo, branch and root directory, its databases, and its custom domains.

## Create the project

1. Log the `5050` CLI in to your console: see [Set up the CLI](/docs/guides/cli-setup).
2. Create the project:

```sh
5050 init -n my-app
```

3. Add each database with the same name it had on Railway, so references like `${{Postgres.DATABASE_URL}}` keep working. On the canvas, select **+ Create**, **Database**, then the engine, and rename it if needed. Or:

```sh
5050 add -d postgres
```

## Recreate the services

For each service built from a repo:

1. On the canvas, select **+ Create**, then **GitHub Repo**, and paste the repo's address.
2. Set the same **Branch** and **Root directory** in **Settings**, **Source**.
3. Name the service as it was named on Railway.

A `railway.json` or `railway.toml` in the repo is read and applied: start command, pre-deploy command, health check path and environment overrides come from it. See [Config as code](/docs/guides/config-as-code).

For a service built from an image, select **+ Create**, **Docker Image**, or run `5050 add --image <image>`.

Have a Docker Compose file? `5050 import compose docker-compose.yml` stages every service in it at once. See [Import Docker Compose](/docs/guides/import-docker-compose).

## Copy the variables

Railway's JSON output is one object of names and values. This loop sets each one on the linked 5050cloud service, skipping Railway's own `RAILWAY_*` names, which 5050cloud provides itself:

```sh
5050 link -s web
jq -r 'to_entries[] | select(.key | startswith("RAILWAY_") | not) | "\(.key)=\(.value)"' web.json |
  while IFS= read -r kv; do 5050 variables set "$kv" --skip-deploys; done
```

It needs `jq`. Values that span several lines do not survive the loop; set those in the service's **Variables** tab. Sealed variables on Railway come out empty, so set them by hand too.

Reference variables such as `${{Postgres.DATABASE_URL}}` and `${{shared.API_KEY}}` use the same syntax and work unchanged, as long as the services have the same names.

## Move the data

Copy each database with one pipe through `5050 connect`. See [Move a database from Railway](/docs/guides/move-database-from-railway).

## Deploy

1. On the canvas, the banner lists the staged changes. Select **Details** to read the diff.
2. Select **Deploy**.
3. Wait until every service shows **Active**.

## Move the domains

1. At your DNS provider, lower the TTL of each record to 300 seconds a day ahead.
2. Add each custom domain to its service: see [Custom domains and HTTPS](/docs/guides/custom-domains).
3. Switch the DNS records to the values the console shows, last, once everything else works.

## Keep your CLI habits

```sh
5050 alias install
```

`railway up`, `railway logs` and the rest now talk to your console. See [The railway alias](/docs/cli/railway-alias).

## What success looks like

- Every service shows **Active**.
- Row counts match between the Railway database and the 5050cloud one.
- The custom domains show **Active** on 5050cloud.
- You can stop the services on Railway.

## Next steps

- [Coming from Railway](/docs/coming-from-railway)
- [Move a database from Railway](/docs/guides/move-database-from-railway)
