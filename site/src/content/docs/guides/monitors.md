---
title: Monitors and alerts
description: Get an email or webhook when a service's CPU, memory, disk or error rate stays above a threshold.
---

Get an email or webhook when a service's CPU, memory, disk or error rate stays above a threshold.

## Add a monitor

1. Open the service's **Metrics** tab and select **Monitors**, or go to **Settings > Monitors**.
2. Pick a metric: `cpu` (% of the limit or of the host), `memory` (% of the limit), `disk` (volume % of the host filesystem, or GB), or `http_5xx_rate` (% of requests).
3. Set the threshold and how many minutes it must hold (1 to 60).
4. Pick channels: email to chosen members and any project webhook.

Monitors are checked every minute. A monitor fires once when it enters the alarm state (`monitor.triggered`) and once on recovery (`monitor.resolved`). The service card shows a red dot while it fires.

New Postgres, MySQL, Redis and MongoDB services get a disk monitor at 80%.

## Next steps

- [Email and notifications](/docs/guides/email-notifications)
