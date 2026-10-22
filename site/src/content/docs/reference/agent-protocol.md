---
title: Agent protocol
description: Agents connect out to the control plane over one WebSocket and reconcile their host to the desired state they receive.
---

Agents connect out to the control plane over one WebSocket and reconcile their host to the desired state they receive.

The message types are defined in `pkg/proto/proto.go` (MIT licensed). This page explains how they fit together.

## Join

1. `POST /api/agent/v1/join` with a single-use join token and the host's facts, `/etc/machine-id` and WireGuard public key.
2. The response carries the host id, a long-lived host token (stored in `/etc/5050/agent.json`, 0600; the server keeps only a SHA-256), the mesh address and the registry address.

## Connect

1. `GET /api/agent/v1/connect`, upgraded to a WebSocket, with `Authorization: Bearer <host token>`. The channel is TLS to the console certificate.
2. The agent sends `hello`: facts, epoch, last applied generation, whether it runs from its local cache, and every replica it runs.
3. The server answers `hello_ack`, then `desired_state` with a generation higher than anything the agent applied.
4. Both sides ping every 20 seconds; 60 seconds of silence closes the socket. The agent reconnects with backoff from 1 to 30 seconds with jitter.

A second machine using the same token with a different machine-id or WireGuard key is refused with `duplicate_host`; the first stays connected.

Host tokens rotate every 30 days over the open socket (`token_rotate`); the old token works for 10 more minutes.

## Desired state

`desired_state` lists everything the host should run: networks, firewall rules, mesh peers, DNS entries, volumes, files, replicas, ingress routes and certificates. The agent:

- applies it in order (networks and firewall, volumes, files, replicas, ingress, DNS) and reports `state_applied`;
- caches it encrypted on disk and re-applies networking from the cache at boot, so a host reboot during a control plane outage keeps private DNS, isolation and routes;
- never stops a container until it has received a fresh `desired_state` on the current connection;
- refuses a lower generation unless the epoch changed; with `Adopt: true` it adds and changes but removes nothing;
- never touches containers without its labels;
- reconciles again every 30 seconds and on every Docker event.

## Requests and streams

| Request | Answer |
|---|---|
| `build` | many `build_log`, then `build_result` (buffered up to 1 hour if the socket drops) |
| `job` (pre-deploy, cron, backup, restore) | `job_log`, then `job_result` |
| `logs_query` / `logs_subscribe` | `logs_result` / `log_lines` until `logs_unsubscribe` |
| `exec_open`, `tcp_open` | Binary frames `[kind][stream id][payload]`, up to 1 MiB unacknowledged per stream |
| `upgrade` | The agent upgrades itself and reconnects |
| `maintenance` | Drain order for host maintenance |

Agents also send `metrics` every 30 seconds and `host_logs_tail` (recent error and deploy lines kept for offline hosts). The primary ingress host sends `cert_issued`.

Messages are capped at 16 MiB and use per-message deflate.

## Offline

A host with no connection for 60 seconds is offline: its replicas leave routes and DNS. After 2 more minutes, replicas without volumes that are not singletons are started on other hosts.
