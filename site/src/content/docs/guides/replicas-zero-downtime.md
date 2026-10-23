---
title: Replicas and zero-downtime deploys
description: Run several replicas of a service across your hosts and deploy new versions without dropping requests.
---

Run several replicas of a service across your hosts and deploy new versions without dropping requests.

> **Beta:** Spreading replicas across hosts works in 0.1.0 but is not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

## Set replicas

In **Settings > Scale**, set **Replicas**, or run `5050 scale 2`. Replicas spread across eligible hosts. Public traffic and private DNS pick a random healthy replica.

## How a deploy rolls out

1. New replicas start in waves of **Max surge** (default 1, or a percentage such as `25%`).
2. Each new replica must pass its [health check](/docs/guides/health-checks). If one fails, the new deployment is removed and the old one keeps serving untouched.
3. Routes and private DNS switch to the new replicas on every host.
4. Old replicas leave public routes at once, stay reachable for in-flight connections for **Draining seconds** (default 30), then get SIGTERM and, after the stop grace, SIGKILL.

**Overlap seconds** (default 0) keeps the old version running after the switch.

## Internal callers and keep-alive

Services that call each other over the private network with pooled keep-alive connections keep working during the drain window. Set idle keep-alive timeouts under 30 seconds in your HTTP clients so they never reuse a connection to a stopped replica. Go and Node clients retry idempotent requests when a reused connection closes.

## Services with a volume

A volume can be mounted by only one container, so a service with a volume runs 1 replica and its deploys stop the old container before starting the new one: a short gap on every deploy. If the new one fails its health check, the old container is started again.

## Singleton

**Singleton** (on by default for workers) keeps a service from being started on another host while its host is offline, so work never runs twice during a network split. The panel offers **Move now** when you know the host is gone.

## Next steps

- [Use all cores](/docs/guides/use-all-cores)
- [Rollbacks](/docs/guides/rollbacks)
