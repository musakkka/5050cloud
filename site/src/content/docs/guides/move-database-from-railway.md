---
title: Move a database from Railway
description: Copy a database from Railway into a 5050cloud database service with one pipe through `5050 connect`.
---

Copy a database from Railway into a 5050cloud database service with one pipe through `5050 connect`.

## Before you start

1. In Railway, turn on the database's public network access and copy its public URL (`DATABASE_PUBLIC_URL` and the like).
2. In 5050cloud, create the same database type (or import the project) and link your directory: `5050 link -s Postgres`.
3. Install the client tools locally (`pg_dump` and `psql`, `mysqldump` and `mysql`, `redis-cli`, `mongodump` and `mongorestore`).
4. Stop writes to the Railway database, or plan a short final sync.

## Postgres

```sh
pg_dump "$RAILWAY_URL" --no-owner --no-acl | 5050 connect Postgres -- psql
```

## MySQL

```sh
mysqldump --single-transaction --routines "$RAILWAY_DB" -h "$HOST" -P "$PORT" -u "$USER" -p"$PASSWORD" | 5050 connect MySQL -- mysql
```

## Redis

```sh
redis-cli -u "$RAILWAY_REDIS_URL" --rdb dump.rdb
```

Then load `dump.rdb` into the new service's volume: `5050 connect Redis --tunnel-only` gives you a local URL for tools that replay keys, or copy the file in with `5050 ssh` and restart.

## MongoDB

```sh
mongodump --uri "$RAILWAY_MONGO_URL" --archive --gzip | 5050 connect MongoDB -- mongorestore --archive --gzip
```

## Check and switch

1. Compare row or key counts.
2. Point services at the new database with `${{Postgres.DATABASE_URL}}` (imports already do).
3. Deploy, then turn off public access on the Railway database.

## Next steps

- [Backups and restore](/docs/guides/backups)
