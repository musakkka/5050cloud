---
title: Ports and firewall
description: The ports 5050cloud needs open, and the firewall rules it manages on each host.
---

The ports 5050cloud needs open, and the firewall rules it manages on each host.

## Inbound

| Port | Hosts | From | For |
|---|---|---|---|
| TCP 80, 443 | Ingress hosts | The internet | HTTPS (80 redirects and serves ACME challenges) |
| UDP 51820 | Every host, with 2 or more hosts | Your other hosts | WireGuard mesh |
| TCP 20000 to 29999 | Ingress hosts | The internet | Only the TCP proxy ports you allocate |
| TCP 22 | Every host | You | SSH (yours; 5050cloud does not use it) |

No management port is ever opened. Agents connect out to the console on 443.

## Outbound

Hosts need HTTPS to GitHub (releases and repos), Docker registries, Let's Encrypt and your bucket. Joined hosts need HTTPS to the console domain.

## `--firewall`

The installer's `--firewall` flag adds nftables rules that drop inbound traffic except SSH, 80, 443 and WireGuard from peers. Allocated TCP proxy ports must be opened too if you use them. Without it, use your cloud provider's firewall with the table above.

## Rules 5050cloud manages

The agent manages its own iptables chains (`5050-ISOLATE`, `5050-EGRESS`, `5050-NAT`, `5050-HOSTIN`) and changes nothing outside them. They keep environments apart, block containers from the host, the mesh and the metadata address, enforce quarantine, and keep container source addresses across hosts.
