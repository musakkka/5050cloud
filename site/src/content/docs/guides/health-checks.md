---
title: Health checks
description: A health check tells 5050cloud when a new deployment is ready for traffic and when a running replica should be taken out of routes.
---

A health check tells 5050cloud when a new deployment is ready for traffic and when a running replica should be taken out of routes.

## Set a path

In **Settings > Deploy**, set **Healthcheck path**, for example `/health`. Or set `deploy.healthcheckPath` in `railway.json`.

- With a path, the agent sends `GET` to each replica every 3 seconds, with `Host` set to the service's first domain, `X-Forwarded-Proto: https` and `User-Agent: 5050cloud-healthcheck`. Any 2xx or 3xx passes; redirects are not followed.
- Without a path, the check is a TCP connect to the target port.
- Workers without a port are healthy once they have run for 10 seconds.

After the first pass the agent waits 5 seconds and checks once more. A replica that does not pass within **Healthcheck timeout** (default 300 seconds) fails the deployment with a diagnosis, for example "Health check /health returned 404. Check the path in Settings > Deploy."

## While running

After a deployment is active the agent keeps checking every 30 seconds. Three failures in a row take that replica out of routes until it passes again.

If every replica fails at once (for example a shared database is slow), the route keeps all of them and the card shows "All replicas failing health checks; serving anyway". A degradation does not turn into an outage.

## Tips

- Make the path cheap. Check the process, not every dependency.
- Listen on `0.0.0.0` and on `PORT`.

## Next steps

- [Troubleshooting failed deploys](/docs/guides/troubleshooting)
