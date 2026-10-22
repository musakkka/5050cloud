---
title: Variables reference
description: The variables 5050cloud provides to every replica, their Railway aliases, the reference syntax and the variables that change settings.
---

The variables 5050cloud provides to every replica, their Railway aliases, the reference syntax and the variables that change settings.

## Provided variables

| Variable | Value |
|---|---|
| `PORT` | The target port (default 8080) |
| `FIFTY_PUBLIC_DOMAIN` | The service's first public domain |
| `FIFTY_PRIVATE_DOMAIN` | `<name>.internal` |
| `FIFTY_PROJECT_ID`, `FIFTY_PROJECT_NAME` | The project |
| `FIFTY_ENVIRONMENT_ID`, `FIFTY_ENVIRONMENT_NAME` | The environment |
| `FIFTY_SERVICE_ID`, `FIFTY_SERVICE_NAME` | The service |
| `FIFTY_DEPLOYMENT_ID` | The deployment |
| `FIFTY_SNAPSHOT_ID` | The deployment id, used as a build snapshot id |
| `FIFTY_REPLICA_ID` | This replica |
| `FIFTY_REPLICA_REGION`, `FIFTY_HOST` | The host this replica runs on |
| `FIFTY_PROCESSES` | Processes per replica |
| `FIFTY_GIT_COMMIT_SHA`, `FIFTY_GIT_BRANCH`, `FIFTY_GIT_REPO_OWNER`, `FIFTY_GIT_REPO_NAME`, `FIFTY_GIT_COMMIT_MESSAGE`, `FIFTY_GIT_AUTHOR` | The commit, for GitHub sources |
| `FIFTY_VOLUME_NAME`, `FIFTY_VOLUME_MOUNT_PATH` | The volume, when there is one |
| `FIFTY_TCP_PROXY_DOMAIN`, `FIFTY_TCP_PROXY_PORT`, `FIFTY_TCP_APPLICATION_PORT` | The TCP proxy, when there is one |

With **Railway aliases** on (the default), the same set is also provided as `RAILWAY_*` (for example `RAILWAY_PUBLIC_DOMAIN`), plus `RAILWAY_ENVIRONMENT` with the environment name. Your own variables win over provided ones.

## Not provided

Railway's `OTEL_*` tracing variables. To send traces to your own collector, set `OTEL_EXPORTER_OTLP_ENDPOINT` and the related variables yourself, for example as shared variables.

## Reference syntax

| Syntax | Meaning |
|---|---|
| `${{NAME}}` | Same service |
| `${{shared.NAME}}` | Shared variable of the environment |
| `${{Service Name.NAME}}` | Another service in the environment, by name (case-sensitive, spaces allowed) |

Resolution is recursive (up to 10 levels) with cycle detection. Unknown targets resolve to an empty string with a warning.

## Template functions

| Function | Result |
|---|---|
| `${{secret(len)}}` | Random string of `len` characters from `A-Za-z0-9` |
| `${{secret(len, "alphabet")}}` | Random string from the given characters |
| `${{randomInt(min, max)}}` | Random integer |

They are evaluated once, when the value is first saved.

## Variables that change settings

When the matching setting is empty, these override it: `RAILWAY_DOCKERFILE_PATH`, `RAILWAY_RUN_UID`, `RAILWAY_SHM_SIZE_BYTES`, `RAILWAY_HEALTHCHECK_TIMEOUT_SEC`, `RAILWAY_DEPLOYMENT_OVERLAP_SECONDS`, `RAILWAY_DEPLOYMENT_DRAINING_SECONDS`, `NO_CACHE`, `RAILPACK_*` and `NIXPACKS_*` build hints, and the `FIFTY_` equivalents.

## Masking

Values of variables whose names contain `SECRET`, `TOKEN`, `PASSWORD`, `PASSWD`, `PASS`, `KEY`, `PRIVATE`, `CREDENTIAL`, `DSN` or `AUTH`, sealed variables, and passwords inside URL values are replaced by dots in build and deploy logs when they are at least 6 characters long.
