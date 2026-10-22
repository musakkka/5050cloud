---
title: Cron jobs
description: Run a service on a schedule instead of keeping it running.
---

Run a service on a schedule instead of keeping it running.

## Set a schedule

1. In **Settings > Deploy**, set **Cron schedule**. Presets: **Every hour**, **Every day**, **Every week**, **Every month**, or any 5-field crontab such as `*/15 * * * *`.
2. Optionally set the time zone (default UTC), the overlap rule and a timeout.
3. Select **Deploy**.

The deployment builds or pulls the image and shows **Active** with "Scheduled". Each run starts a container from it, records status, exit code and logs, and exits.

## Behaviour

- **Exactly once per slot**: a control plane restart at a boundary does not run a slot twice.
- **Overlap**: `skip` (default) skips a run while the previous one is running; `queue` runs it afterwards.
- **Timeout**: a run that takes longer is stopped.
- **Run now** in the Cron tab (or `5050 cron run`) starts a run immediately.

## When the control plane is down

Cron is scheduled by the control plane, so runs stop while it is down. When it starts again it lists every slot it missed in the last 24 hours as "Missed while the control plane was down" and does not replay them.

## Next steps

- [CLI: cron](/docs/cli/cron)
