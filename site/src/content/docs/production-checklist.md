---
title: Production checklist
description: Work through this list before you put real traffic on a 5050cloud install.
---

Work through this list before you put real traffic on a 5050cloud install.

## Host

1. Use a non-burstable VM size with enough RAM for every service plus about 1 GB for 5050cloud itself.
2. Attach a blank data disk before installing; the installer uses it for all state (`--data-disk auto`). See [Install options](/docs/reference/install-options).
3. Keep unattended security updates on (the default). Plan reboots: see [Upgrading and host maintenance](/docs/guides/upgrading).
4. Open only the ports you need, with your cloud firewall or `--firewall`. See [Firewall](/docs/guides/firewall). For no open ports at all, see [Private mode](/docs/guides/private-mode).

## Backups

1. Store the recovery key from `/etc/5050/recovery-key.txt` in a password manager. Without it, backups cannot be decrypted.
2. In **Settings**, **Backups**, set an S3-compatible bucket and select **Test bucket**. Use a separate retention key and turn on bucket versioning. See [Backups bucket](/docs/guides/backups-bucket).
3. Check that every database's **Backups** tab lists a recent backup.
4. Copy the control plane's hourly state snapshots off the host; 0.1.0 keeps them on the host's disk only. See [Control plane backup](/docs/guides/control-plane-backup).

## Domains and HTTPS

1. Use your own domain with a wildcard A record (`--domain`) so names do not depend on your IP. See [Your domains](/docs/guides/console-domain).
2. Add custom domains and wait for **Active**. See [Custom domains and HTTPS](/docs/guides/custom-domains).
3. Behind Cloudflare, set SSL/TLS mode to Full (strict) and paste Cloudflare's ranges into **Trusted proxies**. See [Cloudflare in front](/docs/guides/behind-cloudflare).

## Services

1. Set a health check path for every web service. See [Health checks](/docs/guides/health-checks).
2. Run 2 replicas for web services that must not drop requests during deploys. See [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime).
3. Set memory limits so one service cannot starve the rest.
4. Turn on **Use all cores** for single-process runtimes such as Node. See [Use all cores](/docs/guides/use-all-cores).
5. Put database migrations in a pre-deploy command. See [Pre-deploy migrations](/docs/guides/pre-deploy).

## People and alerts

1. Set up SMTP before you invite people, so invites, password resets and alerts reach them. See [Email (SMTP)](/docs/guides/smtp).
2. Ask everyone to turn on 2FA. **Require two-factor authentication for everyone** in **Settings**, **Members** is stored but not yet enforced at login in 0.1.0. See [Members, roles and 2FA](/docs/guides/members-roles-2fa).
3. Add monitors for CPU, memory, disk and 5xx rate. See [Monitors and alerts](/docs/guides/monitors).

## Resilience

1. Read the [Honest limits](/docs/reference/honest-limits).
2. With a second host, join it with `--roles app,ingress` and put both IPs behind DNS failover or a load balancer. See [Add a host](/docs/guides/add-a-host).
3. Practise a [restore](/docs/guides/restore) once on a spare VM, and read what [a lost control host](/docs/guides/rebuild-control-host) means in 0.1.0.
