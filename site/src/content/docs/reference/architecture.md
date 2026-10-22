---
title: Architecture
description: 5050cloud is a control plane, an agent on every host, and Caddy, a registry and BuildKit run as containers by the agent.
---

5050cloud is a control plane, an agent on every host, and Caddy, a registry and BuildKit run as containers by the agent.

## Processes

| Process | Where | Listens on |
|---|---|---|
| `5050d` (control plane and console) | Control host | `127.0.0.1:5050` only |
| `5050-agent` | Every host | Nothing public; private DNS on each environment's gateway; an upstream resolver on `127.0.0.1:5354` on ingress hosts |
| `5050-net-restore` | Every host | Nothing; restores firewall rules and WireGuard from the cache before Docker starts |
| Caddy | Ingress hosts | `:80`, `:443`; its admin API is a root-only unix socket |
| Registry | Control host | `127.0.0.1:5000` and the mesh address, with push and pull-only users |
| BuildKit | Build hosts | Nothing; builds run on their own bridge network |
| WireGuard `wg5050` | Every host, with 2 or more hosts | UDP 51820, peers only |
| TCP proxy (in the agent) | Ingress hosts | Allocated ports in 20000 to 29999 |
| Litestream (child of `5050d`) | Control host, with a bucket | Nothing; streams state to the bucket |
| Your apps | Per placement | Nothing on the host |

The control plane never connects to an agent. Agents dial out.

## Data

- `state.db` (SQLite) on the control host: users, projects, settings, encrypted variables, deployments. Snapshotted hourly and streamed to your bucket.
- `metrics.db`: 30 days of metrics.
- Logs stay on each host for 7 days and are fetched on demand.
- Images live in the registry on the control host, or in your bucket when **Store images in the bucket** is on.

## A deploy

1. A push, `5050 up` or **Deploy** creates a deployment.
2. The control plane sends a `build` to a build host; the image is pushed to the registry.
3. New replicas are placed on eligible hosts and listed in each host's desired state.
4. Agents pull, start and health check them.
5. Routes and private DNS switch to the new replicas; old ones drain and stop.

## Networking

Each environment gets one Docker bridge per host with a `/24` from `10.128.0.0/9`. Hosts link over WireGuard, and container addresses route across it. Firewall chains keep environments apart and block the host, the mesh and the metadata address. See [Private networking](/docs/guides/private-networking).

## What the control host carries

By default the control host runs the control plane, the registry, the console and every role. With one host it is a single point of failure; with several it is still where deploys, cron and the console live. A second ingress host, a bucket for state and images, and the [rebuild procedure](/docs/guides/rebuild-control-host) limit the damage. A highly available control plane is planned for v0.2.

## Next steps

- [Agent protocol](/docs/reference/agent-protocol)
- [Security model](/docs/reference/security-model)
