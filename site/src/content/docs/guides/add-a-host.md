---
title: Add a host
description: Join another Linux VM with one command to run more replicas, databases or builds, or to add a second public entry point.
---

Join another Linux VM with one command to run more replicas, databases or builds, or to add a second public entry point.

## Join a host

1. In the console, go to **Hosts** and select **Add host**, or run `5050 host add`.
2. Tick the roles: `app` (default), `ingress`, `data`, `build`.
3. Copy the command and run it on the new VM:

```sh
curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --join https://console.example.com --token J-... --roles app
```

The token works once and expires after 1 hour. The host shows **Online** in under 2 minutes.

## No inbound management port

The agent dials out to the console over HTTPS and keeps one WebSocket open. Nothing listens for management traffic on the new host. Hosts behind NAT can join.

Hosts that run replicas of one environment talk over a WireGuard mesh on UDP 51820. At least one side of each pair must be reachable; the control host must be reachable on UDP 51820. If two hosts cannot reach each other, the **Hosts** page says so and placement keeps an environment's replicas off that pair.

## What moves where

- Replicas spread across eligible hosts; public traffic picks a random healthy replica on any host.
- Databases go to hosts with the `data` role. Once a second host exists, `data` defaults to a host other than the control host.
- A host with no connection for 60 seconds is offline: its replicas leave routes at once, and after another 2 minutes replicas without volumes are started elsewhere (singletons excepted).

## Two entry points

With one ingress host, all public traffic enters through the control host. Join a second host with `--roles app,ingress` and it serves ports 80 and 443 for every route, with certificates shared from the first. Then put both IPs behind one of:

- **DNS failover**: two `A` records for your domain (clients retry the other address), or a DNS provider health check that removes a dead address.
- **A load balancer**: a cloud load balancer or a small HAProxy VM with both hosts as backends and a TCP health check on 443.

`sslip.io` names point at one IP, so a two-ingress setup needs your own domain (`--domain`).

If the control host goes down, apps on other hosts keep serving through the second ingress host. Deploys, cron and the console wait until it is back or [rebuilt](/docs/guides/rebuild-control-host).

## Remove a host

**Hosts > host menu > Drain** moves replicas away; **Remove** revokes its token.

## Next steps

- [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime)
- [Ports and firewall](/docs/reference/ports-firewall)
