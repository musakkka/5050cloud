---
title: Firewall
description: Open only the ports 5050cloud needs, with your cloud provider's firewall, the installer's --firewall rules, or ufw.
---

Open only the ports 5050cloud needs, with your cloud provider's firewall, the installer's `--firewall` rules, or ufw.

## The ports

| Port | On which hosts | From | For |
|---|---|---|---|
| TCP 80 and 443 | Ingress hosts | Anywhere | HTTPS. Port 80 redirects to 443 and answers Let's Encrypt's HTTP challenge |
| UDP 51820 | Every host, once you have two or more | Your other hosts | The WireGuard mesh between hosts |
| TCP 20000 to 29999 | Ingress hosts | Anywhere | Only if you use TCP proxies; each proxy gets one port in this range |
| TCP 22 | Every host | You | SSH, for you. 5050cloud does not use it |

There is no inbound management port. Joined hosts dial out to the console on 443 and keep one WebSocket open.

## Option A: your cloud provider's firewall

This is the simplest choice and works on any provider that has security groups or a cloud firewall.

1. In your provider's console, create a firewall (security group) for your 5050cloud hosts.
2. Add inbound rules: TCP 22 from your own IP, TCP 80 and 443 from anywhere (`0.0.0.0/0`).
3. With two or more hosts, add UDP 51820 from the other hosts' public IPs, or from the group itself where the provider allows a rule that names the group.
4. If you use TCP proxies, add TCP 20000 to 29999 from anywhere on ingress hosts, or only the ports your proxies use. **Settings**, **Networking** on a service shows the port its proxy got.
5. Leave outbound open, or allow the destinations under [Outbound](#outbound).
6. Attach the firewall to every host.

## Option B: the installer's `--firewall`

`--firewall` writes nftables rules to `/etc/nftables-5050.conf` and a unit, `5050-firewall.service`, that loads them at boot. The rules drop all inbound traffic except:

- TCP 22, 80 and 443 from anywhere;
- the whole TCP proxy range, 20000 to 29999, from anywhere;
- UDP 51820 from anywhere (WireGuard ignores packets that do not carry a known key, so an open port is safe);
- the loopback, the mesh interface `wg5050` and the Docker bridges.

Install with it:

```sh
curl -fsSL {{GET}} | sudo sh -s -- --firewall
```

Add it to a host that is already installed by running the installer again with `--firewall`. The other steps see that they are done and change nothing.

`--firewall` refuses to run next to an active ufw or firewalld, so two firewalls never fight over the same traffic. Turn one of them off first.

## Option C: ufw

When ufw is active, the installer leaves it alone and prints the ports to allow. Allow them yourself:

```sh
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 51820/udp
sudo ufw allow 20000:29999/tcp
```

Skip the last line if you do not use TCP proxies. ufw's rules sit next to the agent's own chains, whose names start with `5050-`; neither changes the other.

## Outbound

Hosts need HTTPS out to GitHub (releases and repositories), Docker registries, Let's Encrypt and your backups bucket. Joined hosts need HTTPS to the console domain. Hosts with replicas of one environment need UDP 51820 to each other.

## Option D: no open ports at all

[Private mode](/docs/guides/private-mode) goes further than any of these: nothing is open, not even SSH or the web ports, and public traffic comes through a Cloudflare tunnel. It uses the same nftables table as `--firewall`.

## Check from outside

From your laptop, for a host at `203.0.113.10`:

```sh
nc -zv 203.0.113.10 443
nc -zv 203.0.113.10 80
```

From another host, for the mesh:

```sh
nc -zvu 203.0.113.11 51820
```

UDP checks with `nc` cannot always tell open from filtered. **Settings**, **Hosts** in the console is the real test: it says when two hosts cannot reach each other over the mesh.

## What success looks like

- The console loads over HTTPS from your laptop.
- A second host joins, and **Settings**, **Hosts** shows no warning that two hosts cannot reach each other.
- With `--firewall`, `sudo nft list table inet fifty_filter` prints the rules above.

## Next steps

- [Ports and firewall reference](/docs/reference/ports-firewall)
- [Add a host](/docs/guides/add-a-host)
- [Cloudflare in front](/docs/guides/behind-cloudflare)
- [Private mode](/docs/guides/private-mode)
