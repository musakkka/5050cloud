---
title: Email and notifications
description: Send password resets, invites and alerts by email, choose which events reach you, and receive signed webhooks.
---

Send password resets, invites and alerts by email, choose which events reach you, and receive signed webhooks.

## SMTP

1. Go to **Settings > General > Email**.
2. Enter host, port, TLS mode (`starttls`, `tls` or `none`), user, password and from address.
3. Select **Send test email**.

Without SMTP, email features are hidden and invites give you a link to copy.

## Your notifications

In the avatar menu, open **Notifications** and pick, per event, email and in-app: `deployment.failed`, `deployment.crashed`, `backup.failed`, `disk.high`, `volume.high`, `monitor.triggered`, `host.offline`. The bell shows in-app notifications.

## Install alerts

**Settings > General > Alerts** sends platform events to chosen emails and one webhook: `host.offline`, `disk.high`, `disk.critical`, `backup.stale`, `upgrade.rolled_back`, `certificate.failing`, `agent.version_skew`, `host.duplicate`, `controlplane.readonly`.

## Webhooks

**Project Settings > Webhooks** sends events as JSON:

```json
{"type":"deployment.crashed","timestamp":"...","project":"...","environment":"...","service":"...","deployment":"...","details":{}}
```

Each request carries `X-5050-Event` and `X-5050-Signature: sha256=<hex HMAC-SHA256 of the body with the webhook secret>`. Verify the signature before trusting the body. Deliveries time out after 10 seconds and are retried 3 times. The **Critical events** preset picks crashes, OOM kills, disk, volume, backup and monitor events. **Test** sends a sample.

Slack and Discord formatting is not in v0.1; point the webhook at a small relay service.
