---
title: Custom domains and HTTPS
description: Every service can have a generated domain and your own domains, each with an automatic Let's Encrypt certificate.
---

Every service can have a generated domain and your own domains, each with an automatic Let's Encrypt certificate.

## Generated domains

In **Settings > Networking**, select **Generate Domain**, or run `5050 domain`. The name looks like `api-k3x9.203-0-113-10.sslip.io` (or `api-k3x9.example.com` when the install has a base domain with a wildcard record). It works at once with a valid certificate.

## Add a custom domain

1. In **Settings > Networking**, select **Custom Domain** and enter the name, or run `5050 domain shop.example.com`.
2. Create the DNS record shown: an `A` record to each ingress host's public IP (works at the apex), or a `CNAME` to the service's generated domain.
3. The status moves from **Waiting for DNS** to **Issuing certificate** to **Active**. DNS is checked every 30 seconds for 10 minutes, then every 10 minutes.

An active domain shows "Certificate valid until" a date. Certificates renew automatically. After a failure, select **Retry certificate** or run `5050 domain certificate retry DOMAIN`.

## Port

Each domain sends traffic to a target port: the service's port, the only port it listens on, or the one you choose (`--port`).

## Wildcards

Wildcard custom domains (`*.example.com`) are not in v0.1. For generated names under your own domain, install with `--domain example.com` and create a wildcard `A` record to the host.

## Cloudflare

A Cloudflare-proxied record is detected and shown with a hint to set SSL/TLS mode to **Full (strict)**. See [Behind Cloudflare](/docs/guides/behind-cloudflare).

## Next steps

- [Private networking](/docs/guides/private-networking)
