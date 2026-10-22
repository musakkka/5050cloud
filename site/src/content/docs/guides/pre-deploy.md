---
title: Pre-deploy migrations
description: Run a command such as a database migration before each new version starts serving.
---

Run a command such as a database migration before each new version starts serving.

## Set the command

In **Settings > Deploy**, set **Pre-deploy command**, for example `npm run migrate` or `python manage.py migrate`. Or `deploy.preDeployCommand` in `railway.json`.

## How it runs

- It runs once per deployment, after the build and before any new replica starts, in a container from the new image with the service's variables and private network.
- If it exits non-zero, the deployment fails with "Pre-deploy command exited with code 1: <line>" and the old version keeps serving.
- It is stopped after 300 seconds by default (`deploy.preDeployTimeoutSec`, up to 3600).

## Write safe migrations

The old version keeps serving while the migration runs and until the switch. Make migrations backwards compatible: add columns before using them, and drop them in a later deploy.
