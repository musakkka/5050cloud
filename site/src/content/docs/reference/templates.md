---
title: Templates
description: The database and app templates that ship with 5050cloud v0.1.
---

The database and app templates that ship with 5050cloud v0.1.

## Databases

| Id | Image | Volume | Backups |
|---|---|---|---|
| `postgres` | `postgres:17` (18 selectable) | `/var/lib/postgresql/data` | Continuous WAL plus daily base backups |
| `mysql` | `mysql:8.4` | `/var/lib/mysql` | Not in v0.1 |
| `redis` | `redis:8` | `/data` | RDB snapshot with restic |
| `mongodb` | `mongo:8.0` | `/data/db` | `mongodump` with restic |

## Apps

| Id | Services | Required inputs |
|---|---|---|
| `n8n` | n8n + Postgres | `N8N_ENCRYPTION_KEY` (generated) |
| `umami` | Umami + Postgres | `APP_SECRET` (generated) |
| `plausible` | Plausible CE + Postgres + ClickHouse | `SECRET_KEY_BASE`, `TOTP_VAULT_KEY` (generated) |
| `ghost` | Ghost + MySQL | `url` (prefilled with the generated domain) |
| `uptime-kuma` | Uptime Kuma (volume) | none |
| `metabase` | Metabase + Postgres | `MB_ENCRYPTION_SECRET_KEY` (generated) |
| `minio` | MinIO (volume, console domain) | `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD` (generated) |
| `pgvector` | Postgres with pgvector | none |
| `postgis` | Postgres with PostGIS | none |
| `gitea` | Gitea (volume) + Postgres | `GITEA__security__SECRET_KEY` (generated) |
| `meilisearch` | Meilisearch (volume) | `MEILI_MASTER_KEY` (generated) |
| `directus` | Directus + Postgres + Redis | `SECRET`, admin email, admin password (generated) |

Images are pinned per release. Template manifests live in `templates/` in the repo (MIT licensed).

## Next steps

- [Templates and the Deploy on 5050cloud button](/docs/guides/templates)
- [Writing a template](/docs/contribute/writing-a-template)
