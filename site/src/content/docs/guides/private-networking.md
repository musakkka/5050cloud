---
title: Private networking
description: Services in one environment reach each other by name over a private network that spans every host.
---

Services in one environment reach each other by name over a private network that spans every host.

> **Beta:** Private networking between hosts works in 0.1.0 but is not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

## Names

Every service has a private name, `<name>.internal` (also `<name>.railway.internal`), set in **Settings > Networking**. Call it on the port your service listens on:

```
http://api.internal:8080
```

Or use references: `${{api.RAILWAY_PRIVATE_DOMAIN}}`.

DNS answers with the healthy replicas (TTL 5 seconds), on whichever hosts they run. Renaming keeps the old name working for 24 hours.

## Across hosts

With two or more hosts, traffic between them goes over WireGuard (UDP 51820). Source addresses are kept, so logs show the real caller.

## Isolation

- Environments and projects cannot reach each other.
- Containers cannot reach the host, the cloud metadata address `169.254.169.254`, Azure's WireServer `168.63.129.16` (except its DNS) or the mesh, unless an Admin turns on **Allow metadata** for that service.
- Private DNS keeps answering from the agent's cache while the control plane is down.

## Next steps

- [Quarantined environments](/docs/guides/quarantined-environments)
- [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime): keep-alive timeouts for internal callers.
