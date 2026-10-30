---
title: Private mode
nav: Private mode
description: Run a 5050cloud host with no open inbound ports, with public traffic coming through a Cloudflare tunnel and SSH turned off.
---

Run a 5050cloud host with no open inbound ports. Public traffic comes through a Cloudflare tunnel that the host dials out to, and SSH is turned off.

## What this does and does not do

Private mode removes inbound attack surface: a scan of the host's public address finds nothing to talk to. It does not make the host unhackable. Nothing is. These can all still get in:

- a bug in an app you deploy, because visitors still reach your apps through the tunnel;
- a stolen account: Cloudflare, GitHub, your cloud provider, or the console itself;
- a compromised dependency, base image or update;
- anyone holding the tunnel token, who can run their own connector and take your traffic.

It also moves trust to Cloudflare, which terminates TLS and sees your traffic unencrypted.

## Requirements

- A fresh server. An install that already runs as a normal host cannot be converted yet.
- A domain on Cloudflare. `sslip.io` and `nip.io` names point straight at the server, so they cannot work and are refused.
- A way into the server that does not use SSH, which you have tried: your cloud provider's serial console or run-command.

## 1. Create the tunnel

1. In Cloudflare Zero Trust, open **Networks**, **Tunnels**, and create a tunnel of type **Cloudflared**.
2. Copy the token from the install command Cloudflare shows. It is the long string after `--token`. Do not run that command.
3. Add two **Public hostnames** to the tunnel, both with service `http://localhost:80`:
   - `console.example.com`
   - `*.example.com`

The wildcard hostname creates no DNS record by itself. In **DNS**, **Records**, add a proxied CNAME from `*` to `<tunnel id>.cfargotunnel.com`.

Cloudflare's free certificate covers one level of wildcard, so use `*.example.com` and not `*.apps.example.com` unless you have a certificate for that level.

## 2. Install

```sh
export FIFTY_TUNNEL_TOKEN='paste the token here'
curl -fsSL {{GET}} | sudo -E sh -s -- --private --domain example.com --console-domain console.example.com
```

The token is kept in `/etc/5050/tunnel-token`, readable by root only, and reaches `cloudflared` as a systemd credential, never on its command line. Run from a terminal, the guided setup asks for all of this and hides the token as you type.

`--tunnel-token TOKEN` also works, but puts the token in your shell history.

## What the installer checks before closing anything

The order is what keeps a bad token from locking you out:

1. It installs everything, with Caddy on loopback, and starts the tunnel. So far no port has been closed.
2. It waits for `cloudflared` to report a connection.
3. It fetches `https://<console domain>/readyz` with a random value, and checks that this host's Caddy logged that value. A stale record pointing at some other server cannot pass.
4. Only then does it load the firewall, and it checks the console again. If the console stopped answering, it removes the firewall again.
5. Last of all, it stops and disables SSH.

If any check fails, the install stops with an error, and SSH and every other port stay as they were. Fix the cause and run the installer again.

## What changes on the host

- **Firewall.** nftables drops all inbound and forwarded traffic except loopback, established connections, the Docker bridges and `wg5050`. Forwarded traffic matters because Docker publishes ports by forwarding, so a `docker run -p` you start by hand stays unreachable too.
- **Caddy** binds `127.0.0.1:80`, serves plain HTTP by host name and orders no certificates. Cloudflare holds the certificates.
- **WireGuard.** UDP 51820 is open only to the public addresses of joined hosts. The agent adds and removes them as hosts join and leave. On a single host it is closed to everyone.
- **SSH** is stopped and disabled.

## Keeping SSH for one address

```sh
curl -fsSL {{GET}} | sudo -E sh -s -- --private --domain example.com --keep-ssh-from 203.0.113.7/32
```

This is the only exception to the closed firewall. It takes one IPv4 range, of /8 or smaller. Check that the address is really yours and that it does not change.

## Adding hosts

Run the same flag on every host. Without `--private`, a joined host has its ports open.

```sh
curl -fsSL {{GET}} | sudo sh -s -- --join https://console.example.com --token JOIN_TOKEN --private
```

Joined hosts dial out to the console over 443, through the tunnel, as usual. Only a host with the `ingress` role also needs `--tunnel-token`; give it the same token to add a second connector.

Each host needs a public IPv4 address that other hosts can use. A host behind NAT cannot be told apart from a stranger, so its WireGuard traffic is dropped.

## What stops working

- **TCP proxies and Public Access on databases.** They need a public port. The console and the API refuse them. Use `5050 connect` from your machine instead.
- **`sslip.io` and `nip.io` names.** Refused in the installer, the API and the console.
- **Direct connections to your host's IP address.** Everything goes through your domain and Cloudflare.
- **Non-HTTP traffic from the internet**, which a tunnel with a public hostname does not carry here.
- **Custom domains without a step in Cloudflare.** A custom domain shows as active straight away, but also needs its own public hostname on the tunnel. 5050cloud does not touch your Cloudflare account, so it cannot add it or check it.
- **A Cloudflare outage or a suspended account** takes your apps and console offline, with nothing to fall back on.
- **`--restore`** cannot be combined with `--private` yet.

`cloudflared` does not update itself. It is pinned in the installer and its checksum is verified. Cloudflare stops supporting old versions after about a year, so run a newer installer on the host before then.

## How to get back in

The console shell and `5050 ssh` open a shell **inside a running service**. They do not reach the host. Use them for your apps.

For the host itself, use your cloud provider's serial console or run-command. On Azure:

```sh
az vm run-command invoke -g GROUP -n VM --command-id RunShellScript --scripts 'systemctl status 5050-tunnel'
```

To turn SSH back on from there, for your address only:

```sh
systemctl enable --now ssh
nft add rule inet fifty_filter input tcp dport 22 ip saddr 203.0.113.7/32 accept
```

That rule lasts until the next reboot. To keep it, run the installer again with `--keep-ssh-from`.

If you lose both the tunnel and your provider's console, you are locked out. Keep backups going to a bucket off the machine, so you can [rebuild](/docs/guides/rebuild-control-host).

To replace a token, run the installer again with the new one. It checks the new token, and goes back to the old one if the check fails.

## Protect the accounts that now matter

With no open ports, your accounts are the front door. Turn these on:

- **Cloudflare Access on the console hostname**, so only you get as far as the sign-in page. Exclude `/api/agent` and the other paths your joined hosts and CLI use, or use a service token for them, and test that a joined host still connects.
- **Two-factor sign-in on your Cloudflare, GitHub and cloud provider accounts.** Anyone in the Cloudflare account can read your traffic. Anyone in the cloud account can open the serial console.
- **[Two-factor sign-in in the console](/docs/guides/members-roles-2fa)** for every member. The console does not enforce this at login yet, so check it yourself.
- **Your cloud provider's activity log.** The host keeps its own audit log in `/var/log/audit`, but someone who reaches the host as root can erase it.

Leave your cloud firewall closed as well, as a second layer.

## What success looks like

- `nmap -p- YOUR_IP` from another network finds no open port.
- The console loads at `https://console.example.com`.
- A deployed service answers on its generated domain.
- `sudo nft list table inet fifty_filter` on the host shows the rules above.

## Next steps

- [Firewall](/docs/guides/firewall)
- [Honest limits](/docs/reference/honest-limits)
- [Security model](/docs/reference/security-model)
