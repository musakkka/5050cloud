---
title: Behind Cloudflare
description: Proxy your domains through Cloudflare while 5050cloud keeps issuing certificates and sees real client IPs.
---

Proxy your domains through Cloudflare while 5050cloud keeps issuing certificates and sees real client IPs.

## Set up

1. Add the DNS records shown for your custom domain in Cloudflare, proxied (orange cloud).
2. In Cloudflare, set **SSL/TLS** mode to **Full (strict)**. Flexible mode causes redirect loops because 5050cloud redirects HTTP to HTTPS.
3. In 5050cloud, go to **Settings > General > Trusted proxies** and select the **Cloudflare** preset, so client IPs come from `X-Forwarded-For` only for Cloudflare's ranges.

5050cloud detects a proxied record and shows "Cloudflare proxy detected: set SSL/TLS mode to Full (strict)".

## Certificates

Certificate issuance uses the HTTP challenge, which works through the proxy. If issuance fails, pause the proxy for the domain, select **Retry certificate**, then turn the proxy back on.

## Generated domains

`sslip.io` names cannot be proxied. Use your own domain.
