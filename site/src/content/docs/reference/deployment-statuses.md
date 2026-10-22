---
title: Deployment statuses
description: The labels a deployment shows on the card, in the panel, in the CLI and in webhooks, and what each one means.
---

The labels a deployment shows on the card, in the panel, in the CLI and in webhooks, and what each one means.

| Status | Label | Meaning |
|---|---|---|
| `QUEUED` | Initializing | Waiting for a build host or for capacity |
| `BUILDING` | Building | The image is being built (the sub-line shows the step) |
| `DEPLOYING` | Deploying | Replicas are starting and being health checked |
| `SUCCESS` (active) | Active | Serving now ("Deployed 3m ago via GitHub") |
| `SUCCESS` (older) | Success | Succeeded earlier and was replaced |
| `COMPLETED` | Completed | A one-off or cron run exited with code 0 |
| `CRASHED` | Crashed | Exhausted its restarts; **Restart** is offered |
| `FAILED` | Failed | Build, pre-deploy or health check failed; the [diagnosis sentence](/docs/guides/troubleshooting) says why |
| `REMOVING` | Removing | Being stopped |
| `REMOVED` | Removed | Stopped and replaced |
| `CANCELLED` | Removed | Aborted by a person, superseded by a newer deployment, or timed out in the queue; the sub-line says which |
| `SKIPPED` | Skipped | "No changed files matched the watch paths" |

The labels match Railway's. Railway has no visible cancelled state, so an aborted or superseded deployment shows **Removed** with the reason.

## Restarts

The restart policy decides when a crashed container restarts: `on_failure` (default, up to 10 times), `always` or `never`. Restarts work even while the agent is down, because Docker performs them. When `on_failure` runs out, the deployment becomes **Crashed**. Out-of-memory kills say "Out of memory at X MB (limit)".
