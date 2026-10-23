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
| TCP 20000 to 29999 | Ingress hosts | The internet | TCP proxies; each proxy gets one port in this range |
| TCP 22 | Every host | You | SSH (yours; 5050cloud does not use it) |

No management port is ever opened. Agents connect out to the console on 443.

## Outbound

Hosts need HTTPS to GitHub (releases and repos), Docker registries, Let's Encrypt and your bucket. Joined hosts need HTTPS to the console domain.

## `--firewall`

The installer's `--firewall` flag writes nftables rules to `/etc/nftables-5050.conf`, loaded at boot by `5050-firewall.service`. They drop inbound traffic except TCP 22, 80 and 443, the whole TCP proxy range 20000 to 29999 and UDP 51820, all from anywhere, plus the loopback, `wg5050` and the Docker bridges. WireGuard drops packets without a known key, so UDP 51820 open to everyone is safe. The flag refuses to run next to an active ufw or firewalld. Without it, use your cloud provider's firewall with the table above. The [Firewall guide](/docs/guides/firewall) walks through each option.

## Rules 5050cloud manages

The agent manages its own iptables chains (`5050-ISOLATE`, `5050-EGRESS`, `5050-NAT`, `5050-HOSTIN`) and changes nothing outside them. They keep environments apart, block containers from the host, the mesh and the metadata address, enforce quarantine, and keep container source addresses across hosts.
