---
title: Add Postgres, MySQL, Redis or MongoDB
description: Add a database in one click; it gets a volume, generated credentials and the same variable names Railway uses.
---

Add a database in one click; it gets a volume, generated credentials and the same variable names Railway uses.

## Add a database

1. On the canvas, select **+ Create**, then **Database**, then the engine.
2. The database deploys at once onto a host with the `data` role.
3. In your app's **Variables**, reference it, for example `DATABASE_URL=${{Postgres.DATABASE_URL}}`.

From the CLI: `5050 add -d postgres` (or `mysql`, `redis`, `mongo`).

## Engines and variables

| Engine | Image | Variables |
|---|---|---|
| Postgres | `postgres:17` (18 selectable) | `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`, `DATABASE_URL`, `DATABASE_PUBLIC_URL` |
| MySQL | `mysql:8.4` | `MYSQLHOST`, `MYSQLPORT`, `MYSQLUSER`, `MYSQLPASSWORD`, `MYSQLDATABASE`, `MYSQL_URL`, `MYSQL_PUBLIC_URL` |
| Redis | `redis:8` with append-only persistence | `REDISHOST`, `REDISPORT`, `REDISUSER`, `REDISPASSWORD`, `REDIS_URL`, `REDIS_PUBLIC_URL` |
| MongoDB | `mongo:8.0` | `MONGOHOST`, `MONGOPORT`, `MONGOUSER`, `MONGOPASSWORD`, `MONGO_URL`, `MONGO_PUBLIC_URL` |

Passwords are generated once (32 characters). Memory settings such as Postgres `shared_buffers` are sized from the memory limit or the host's RAM.

## Connect from your laptop

```sh
5050 connect Postgres
```

Or select **Connect** in the database panel for the URLs (masked until revealed). Turn on **Public access** in **Settings > Networking** for a TCP proxy reachable from outside.

## Data tab

The **Data** tab lists tables with sizes (Postgres, MySQL), collections (MongoDB), Postgres extensions with **Enable**, and a read-only key browser for Redis. There is no query editor; use `5050 connect`.

## Backups

Postgres, Redis and MongoDB back up to your bucket. MySQL backups are not in v0.1: use a cron service that runs `mysqldump`. See [Backups and restore](/docs/guides/backups).

## Next steps

- [Database major upgrades](/docs/guides/database-major-upgrades)
- [Volumes](/docs/guides/volumes)
