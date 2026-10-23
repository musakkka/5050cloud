---
title: Cloudflare in front
nav: Cloudflare in front
description: Proxy your domains through Cloudflare while 5050cloud keeps issuing certificates and logs your visitors' real IP addresses.
---

Proxy your domains through Cloudflare while 5050cloud keeps issuing certificates and logs your visitors' real IP addresses.

## Requirements

- Your own domain on Cloudflare. `sslip.io` names cannot be proxied.
- The DNS records from [Your domains](/docs/guides/console-domain) and [Custom domains and HTTPS](/docs/guides/custom-domains).

## DNS in Cloudflare

1. In Cloudflare's dashboard, open your domain, then **DNS**, **Records**.
2. Add the records 5050cloud shows for each name: an A record to your ingress host's IP, or a CNAME to the service's generated name.
3. Set **Proxy status** to **Proxied** for the names you want behind Cloudflare.

5050cloud notices a proxied record and shows, next to the domain: "This record points at Cloudflare's proxy. Set SSL mode to Full (strict)."

## SSL/TLS mode

In Cloudflare, go to **SSL/TLS**, **Overview** and choose **Full (strict)**. 5050cloud serves a valid Let's Encrypt certificate, so strict checking passes.

Do not use **Flexible**. Cloudflare would then talk plain HTTP to your host, 5050cloud would redirect it to HTTPS, and the browser would loop.

## The first certificate

5050cloud gets certificates with Let's Encrypt's HTTP challenge on port 80. Through Cloudflare's proxy this works, with one trap: if **Always Use HTTPS** is on in Cloudflare (**SSL/TLS**, **Edge Certificates**), Cloudflare redirects the challenge to HTTPS and it fails, because your host has no certificate yet.

Pick one way through:

1. Leave the record **DNS only** until 5050cloud shows **Active** for the domain, then switch it to **Proxied**.
2. Or turn **Always Use HTTPS** off until the domain is **Active**, then turn it back on.

If a certificate failed, fix the setting, then select **Retry certificate** in the service's **Settings**, **Networking**, or run `5050 domain certificate retry shop.example.com`. Renewals use the same challenge, so keep a way through, for example a Cloudflare rule that skips the HTTPS redirect for `/.well-known/acme-challenge/*`.

> **Not yet:** the console's Cloudflare note does not mention **Always Use HTTPS**, and a failed certificate stays on **Issuing certificate** instead of showing Cloudflare's reason. [What is not in this release](/docs/reference/honest-limits#not-yet).

## Real client IPs

Behind the proxy, every request comes from a Cloudflare address. Tell 5050cloud which addresses to trust, so it takes the visitor's IP from the `X-Forwarded-For` header for those sources only:

1. Copy Cloudflare's current IPv4 ranges from their published list at `https://www.cloudflare.com/ips-v4`.
2. In the console, go to **Settings**, **General**.
3. Paste the ranges into **Trusted proxies**, one per line, for example `173.245.48.0/20`.
4. Select **Save**.

Compare the list with Cloudflare's page now and then, and update it when theirs changes.

> **Not yet:** there is no button that fills **Trusted proxies** with Cloudflare's ranges, so paste them by hand. [What is not in this release](/docs/reference/honest-limits#not-yet).

## The console behind Cloudflare

The console works behind the proxy the same way. Joined hosts keep a long-lived WebSocket to the console; Cloudflare supports WebSockets but may close an idle one, and the agent reconnects on its own. If you prefer to keep agent traffic off Cloudflare, leave the console's record **DNS only**.

## Lock the origin

Optional: in your cloud provider's firewall, allow TCP 80 and 443 only from Cloudflare's ranges. Visitors then cannot skip Cloudflare by calling your IP. Generated `sslip.io` names stop working from outside, since they point straight at your IP.

## What success looks like

- The domain shows **Active** in the service's **Settings**, **Networking**, with the Cloudflare note.
- HTTP logs show your visitors' IPs, not Cloudflare's.
- Pages load with no redirect loop.

## Next steps

- [Custom domains and HTTPS](/docs/guides/custom-domains)
- [Firewall](/docs/guides/firewall)
