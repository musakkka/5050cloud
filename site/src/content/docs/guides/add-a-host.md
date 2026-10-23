---
title: Add a host
description: Join another Linux VM with one command to run more replicas, databases or builds, or to add a second public entry point.
---

Join another Linux VM with one command to run more replicas, databases or builds, or to add a second public entry point.

> **Beta:** Running more than one host works in 0.1.0 but is not yet covered by the end-to-end tests. See [Honest limits](/docs/reference/honest-limits#beta).

## Before you start

- The new VM meets the same needs as the first: see [Install](/docs/guides/install#before-you-install).
- The new VM can reach the console's domain over HTTPS (443). It needs no inbound port for this.
- Hosts that will run replicas of one environment need UDP 51820 open between them. See [Firewall](/docs/guides/firewall).

## Create the join command

1. In the console, go to **Settings**, **Hosts** and select **Add host**. Or run `5050 host add` on your laptop.
2. Optional: type a **Name**. When it is empty, the machine's host name is used.
3. Tick the **Roles** the host should have: `app` (the default), `ingress`, `data`, `build`.
4. Select **Create join command** and copy it.

The roles are stored in the token, so the roles you tick here are the ones the host gets. The `--roles` part of the printed command is there for you to read. The token works once and expires after 1 hour.

## Run it on the new VM

On the new VM, paste the command exactly as the console printed it. It looks like this:

```sh
curl -fsSL {{GET}} | sudo sh -s -- --join https://console.example.com --token J-... --roles app
```

The installer prepares the VM the same way as the first one, then joins. When it finishes it prints "This host joined the install." The host shows **Online** in **Settings**, **Hosts** once its agent connects.

## No inbound management port

The agent dials out to the console over HTTPS and keeps one WebSocket open. Nothing listens for management traffic on the new host, so hosts behind NAT can join.

Hosts that run replicas of one environment talk over a WireGuard mesh on UDP 51820. At least one side of each pair must be reachable, and the control host must be reachable on UDP 51820. If two hosts cannot reach each other, **Settings**, **Hosts** says so and placement keeps an environment's replicas off that pair.

## What moves where

- Replicas spread across eligible hosts, and public traffic picks a random healthy replica on any host.
- Databases go to hosts with the `data` role. Once a second host exists, new databases default to a host other than the control host.
- A host with no connection for 60 seconds is offline: its replicas leave the routes at once, and after another 2 minutes replicas without volumes start elsewhere.

## Two entry points

With one ingress host, all public traffic enters through the control host. Join a second host with the roles `app` and `ingress` and it serves ports 80 and 443 for every route, with certificates shared from the first. Then put both IPs in front of your users with one of:

- **DNS with two addresses.** Two A records for each name, one per ingress host. Browsers try the other address when one fails. A DNS provider with health checks can also remove a dead address for you.
- **A load balancer.** A cloud load balancer or a small HAProxy VM with both hosts as backends and a TCP health check on 443.

For `shop.example.com` with ingress hosts at `203.0.113.10` and `203.0.113.11`:

| Type | Name | Value |
|---|---|---|
| A | `shop` | `203.0.113.10` |
| A | `shop` | `203.0.113.11` |

`sslip.io` names point at one IP, so two entry points need your own domain. See [Your domains](/docs/guides/console-domain).

If the control host goes down, apps on other hosts keep serving through the second ingress host. Deploys, cron and the console wait until the control host is back.

## Host menu

Each host in **Settings**, **Hosts** has a menu:

- **Drain** moves its replicas to other hosts and places nothing new on it. **Stop draining** undoes it.
- **Start maintenance** stops or moves its workloads for OS work; **End maintenance** starts them again. See [Upgrades](/docs/guides/upgrading#host-maintenance).
- **Edit name and roles** changes the name, the roles and **Memory kept free for the system (MB)**.
- **Remove host** revokes its credentials. Drain it first.

From the CLI: `5050 host list`, `5050 host drain HOST` and `5050 host maintenance HOST on`.

## What success looks like

- The new VM printed "This host joined the install."
- **Settings**, **Hosts** shows it **Online** with the roles you ticked.
- `5050 host list` lists it.
- With 2 replicas of a service, responses come from both hosts.

## Next steps

- [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime)
- [Firewall](/docs/guides/firewall)
- [Cloudflare in front](/docs/guides/behind-cloudflare)
