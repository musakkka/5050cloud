---
title: Your domains
description: Serve the console and your services' generated names from your own domain instead of sslip.io, at install time or later.
---

Serve the console and your services' generated names from your own domain instead of sslip.io, at install time or later.

## Why use your own domain

- `sslip.io` names share one Let's Encrypt limit (50 certificates a week per registered domain) with everyone who uses `sslip.io`. Your own domain has its own limit.
- A second ingress host, DNS failover and Cloudflare all need names you control. An `sslip.io` name always points at one IP.
- Names like `api.apps.example.com` do not change when your VM's IP does.

Custom domains for a single service, such as `shop.example.com`, are a separate step: see [Custom domains and HTTPS](/docs/guides/custom-domains).

## DNS records

At your DNS provider, for a host with the public IP `203.0.113.10` and the base domain `apps.example.com`:

| Type | Name | Value | For |
|---|---|---|---|
| A | `*.apps` | `203.0.113.10` | Every generated service name, such as `api-k3x9.apps.example.com` |
| A | `console` | `203.0.113.10` | The console at `console.example.com` |

Most DNS panels want the name relative to your domain (`*.apps`, `console`); some want the full name. If the console sits under the base domain (the default, `console-ab12.apps.example.com`), the wildcard record covers it and one record is enough.

Check each record from your laptop before you install. It must print the host's IP. With `--domain` the installer runs the same check against 1.1.1.1 and, if a name does not point at the server, prints the records to add and stops. Pass `--skip-dns-check` to install anyway.

```sh
dig +short console.example.com @1.1.1.1
dig +short test.apps.example.com @1.1.1.1
```

## At install time

```sh
curl -fsSL {{GET}} | sudo sh -s -- --domain apps.example.com --console-domain console.example.com --acme-email you@example.com
```

`--domain` sets the base domain for generated names. `--console-domain` sets the console's address; without it the console is `console-<4 random characters>.<base domain>`. The [guided setup](/docs/guides/install#guided-setup) asks for both.

## Change the console domain later

1. Create the A record for the new name at your DNS provider, and wait until `dig +short <new name> @1.1.1.1` prints the host's IP.
2. In the console, go to **Settings**, **General** and edit **Console domain**.
3. Select **Save**. The console asks for a certificate for the new name.
4. Open the new address.
5. Run `5050 login https://<new console domain>` on each laptop, and update the console URL in CI secrets.

The old name keeps working for 24 hours, so you can sign in there while you move bookmarks and laptops. A name that a service already uses cannot become the console domain.

> **Warning:** If you created a GitHub App, its webhook still points at the old console name. On GitHub, edit the app's webhook URL, callback URL and setup URL to the new name, or pushes stop deploying. See [GitHub App](/docs/guides/github-app#after-a-console-domain-change).

> **Warning:** Hosts you joined connect to the console by the address in their join command. The control host's own agent talks to `5050d` locally and is not affected. With more than one host, plan to join the other hosts again after you change the console domain.

## Change the base domain later

1. Create the wildcard A record for the new base domain, for example `*.apps2.example.com`, to the host's IP.
2. In **Settings**, **General**, edit **Base domain** and select **Save**.

New generated domains use the new base domain. Services that already have a generated name keep it; to move one, delete its generated domain in the service's **Settings**, **Networking** and select **Generate Domain** again.

## Certificate notices

**ACME email** in **Settings**, **General** is where the certificate authority sends expiry notices. 5050cloud renews certificates on its own; a notice usually means renewal failed for a domain that no longer points at your host.

## What success looks like

- The browser shows a valid certificate on the new console name.
- **Settings**, **General** shows the new **Console domain** and **Base domain**.
- A new **Generate Domain** gives a name under your base domain, and it opens with HTTPS.
- A push to a connected repo still starts a deployment.

## Next steps

- [Custom domains and HTTPS](/docs/guides/custom-domains)
- [Cloudflare in front](/docs/guides/behind-cloudflare)
- [Add a host](/docs/guides/add-a-host)
