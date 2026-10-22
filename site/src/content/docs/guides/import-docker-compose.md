---
title: Import Docker Compose
description: Turn a `docker-compose.yml` into services on the canvas, staged for you to review before anything deploys.
---

Turn a `docker-compose.yml` into services on the canvas, staged for you to review before anything deploys.

## Import

- Drag the file onto the canvas, or select **New > Docker Compose**, or run `5050 import compose docker-compose.yml`.

## What is mapped

| Compose | 5050cloud |
|---|---|
| `image` | Docker image service |
| `build` (context, dockerfile) | Built service (needs a repo) |
| `command` | Start command |
| `environment`, `env_file` | Variables (file contents pasted in) |
| `ports` | First port becomes the target port; published ports suggest a domain or TCP proxy |
| Named `volumes` | Volumes |
| Bind mounts | Skipped, with a reason |
| `depends_on` | Deploy order |
| `deploy.replicas` | Replicas |
| `healthcheck` | Health check path when it calls an HTTP path |
| `restart` | Restart policy |

Well-known database images (Postgres, MySQL, Redis, MongoDB) become our database templates, with backups and connect support. Unsupported keys are listed in the import report.

## After the import

1. Review the staged changes in **Details**.
2. Replace hard-coded hostnames with references, for example `${{Postgres.DATABASE_URL}}`, or use private names such as `postgres.internal`.
3. Select **Deploy**.

## Next steps

- [Private networking](/docs/guides/private-networking)
