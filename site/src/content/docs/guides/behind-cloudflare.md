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

5050cloud notices a proxied record and shows, next to the domain: "Cloudflare proxy detected: set SSL/TLS mode to Full (strict), and turn off Always Use HTTPS until this domain is Active."

## SSL/TLS mode

In Cloudflare, go to **SSL/TLS**, **Overview** and choose **Full (strict)**. 5050cloud serves a valid Let's Encrypt certificate, so strict checking passes.

Do not use **Flexible**. Cloudflare would then talk plain HTTP to your host, 5050cloud would redirect it to HTTPS, and the browser would loop.

## The first certificate

5050cloud gets certificates with Let's Encrypt's HTTP challenge on port 80. Through Cloudflare's proxy this works, with one trap: if **Always Use HTTPS** is on in Cloudflare (**SSL/TLS**, **Edge Certificates**), Cloudflare redirects the challenge to HTTPS and it fails, because your host has no certificate yet.

Pick one way through:

1. Leave the record **DNS only** until 5050cloud shows **Active** for the domain, then switch it to **Proxied**.
2. Or turn **Always Use HTTPS** off until the domain is **Active**, then turn it back on.

If a certificate failed, fix the setting, then select **Retry certificate** in the service's **Settings**, **Networking**, or run `5050 domain certificate retry shop.example.com`. Renewals use the same challenge, so keep a way through, for example a Cloudflare rule that skips the HTTPS redirect for `/.well-known/acme-challenge/*`.

## Real client IPs

Behind the proxy, every request comes from a Cloudflare address. Tell 5050cloud which addresses to trust, so it takes the visitor's IP from the `X-Forwarded-For` header for those sources only:

1. In the console, go to **Settings**, **General**.
2. Under **Trusted proxies**, select **Use Cloudflare ranges**. It adds Cloudflare's IPv4 and IPv6 ranges, one per line.
3. Select **Save**.

The list is the one 5050cloud ships. Compare it with Cloudflare's page at `https://www.cloudflare.com/ips` now and then, and add any range that is missing.

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
