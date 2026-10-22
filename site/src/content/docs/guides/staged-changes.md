---
title: Staged changes and environment sync
description: Edits collect as staged changes that you review as a diff and apply together with one Deploy, and Sync copies changes between environments.
---

Edits collect as staged changes that you review as a diff and apply together with one Deploy, and Sync copies changes between environments.

## Staged changes

Settings, variables, shared variables, volumes, renames, deletions and services created without **Deploy Now** are staged. The banner at the top of the canvas shows the count.

1. Select **Details** to see the diff of every change (secret values shown as dots).
2. Discard one change or all of them, if needed.
3. Select **Deploy**, add a message if you like, and confirm. Everything is applied in one commit and the affected services redeploy.

Hold **Alt** while selecting **Deploy** to commit without redeploying; only deletions and scaling apply at once.

These apply immediately instead of being staged, as on Railway: domains, TCP proxies, canvas positions, database and template creation, and **Deploy Now**.

Commits are validated all or nothing: names unique and at most 32 characters, valid cron, one replica for a service with a volume, ports 1 to 65535, absolute mount paths, at least 64 MB of memory, and valid references.

## Duplicate an environment

`5050 environment new staging --duplicate production`, or **New Environment > Duplicate**, stages every service with its settings and non-sealed variables. Nothing deploys until you press **Deploy**.

## Sync

1. Open the target environment and select **Sync** from the environment menu, or run `5050 environment sync --from production`.
2. Services only in the source are tagged **New**, services that differ **Edited**, and services only in the target **Removed** (staged as deletes only if you tick them).
3. Secret and sealed values are never copied; missing variables arrive empty and flagged **Set a value**.
4. Select **Deploy**.

## Next steps

- [Rollbacks](/docs/guides/rollbacks)
