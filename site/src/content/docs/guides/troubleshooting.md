---
title: Troubleshooting failed deploys
description: Every failed or crashed deployment shows one sentence that names the cause; this page explains each one and what to do.
---

Every failed or crashed deployment shows one sentence that names the cause; this page explains each one and what to do.

The sentence appears above the logs in the deployment view and in the CLI. Open **Build Logs** or **Deploy Logs** for the detail.

## Your app listens on another port

"Your app listens on 3000 but the target port is 8080. Set PORT=8080 in your app or change the target port to 3000."

Read `PORT` in your app, or set the target port in **Settings > Networking**.

## Your app listens on 127.0.0.1

"Your app listens on 127.0.0.1:3000, which is unreachable from outside the container. Listen on 0.0.0.0."

Bind to `0.0.0.0` (for example `app.listen(port, '0.0.0.0')`, `--host 0.0.0.0`).

## Health check returned an error

"Health check /health returned 404. Check the path in Settings > Deploy."

Fix the path, or make the endpoint return 2xx. See [Health checks](/docs/guides/health-checks).

## Nothing answered in time

"Nothing answered on port 8080 within 300 s. Make sure the app starts and listens on PORT."

Check the deploy log for a startup error. Slow starts can raise **Healthcheck timeout**.

## Out of memory

"Out of memory at 512 MB (limit). Raise the memory limit or lower Processes."

## The app exited right after starting

"The app exited with code 1 right after starting: <last stderr line>."

The last line usually names the problem: a missing variable, a missing file, a failed connection.

## No start command

"No start command was found. Set one in Settings > Deploy or add a start script."

Add a `start` script to `package.json`, a `Procfile`, or a start command.

## Image not found or access refused

"Image nginx:1.99 was not found." or "The registry refused access to ghcr.io/acme/app. Add registry credentials in Settings > Source."

## Railpack found nothing to build

"Railpack could not detect a language in /. Set the root directory or add a Dockerfile."

## Node version

"The build needs Node >= 20 (package.json engines). Set NODE_VERSION or update engines."

## Missing lockfile

"npm ci needs a package-lock.json. Commit one or set a custom install command."

## Dockerfile missing

"No Dockerfile at Dockerfile. Check the Dockerfile path in Settings > Build."

## Config file invalid

"railway.json line 7: unknown field buildComand. Did you mean buildCommand?"

## Build host out of disk

"The build host ran out of disk. Free space or add a data disk."

Old images and build cache are pruned automatically above 80% disk use; a full disk usually means a volume is filling it.

## Build timeout

"The build took longer than 30 minutes and was stopped. Raise the build timeout in Settings > Build."

## Pre-deploy failed

"Pre-deploy command exited with code 1: <line>." or "Pre-deploy command timed out after 300 seconds."

The old version keeps serving. Fix the migration and deploy again.

## Volume plus replicas

"A service with a volume can only run 1 replica."

## No eligible host

"No online host has the roles app. Add a host or change placement."

## No build host

"No online host has the role build. Bring one online or give a host the build role."

## No capacity

"No host has 512 MB free for this replica. Free memory, lower the reservation or add a host."

The deployment waits as **Pending: no capacity** for up to 10 minutes first.

## No network slot

"No network slot left on host X: it already runs 254 environments."

## Volume host lost

"This volume was on a host that is gone. Restore it from a backup."

See [Rebuild the control host](/docs/guides/rebuild-control-host) and [Backups and restore](/docs/guides/backups).

## Major version change

"Major version changes need a dump and restore. Use Upgrade to a new major version in the database panel."

See [Database major upgrades](/docs/guides/database-major-upgrades).
