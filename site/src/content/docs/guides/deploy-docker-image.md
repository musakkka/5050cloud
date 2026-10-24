---
title: Deploy a Docker image
description: Run any public or private Docker image as a service, with the same variables, domains and scaling as a built service.
---

Run any public or private Docker image as a service, with the same variables, domains and scaling as a built service.

## Add the service

![The Add menu open on the canvas: GitHub Repo, Template, Database, Docker Image, Empty Service, Volume and Import Compose File](shot:palette "Add, then Docker Image.")

1. On the canvas, select **+ Add**, then **Docker Image**.
2. Enter the image, for example `nginx:1.27` or `ghcr.io/owner/app:1.4.2`.
3. Select **Deploy Now**.

From the CLI:

```sh
5050 add --image traefik/whoami -v PORT=80
```

## Private registries

Add the registry user and token in **Settings > Source > Registry credentials**. If the registry refuses access, the deployment fails with "The registry refused access to ... Add registry credentials in Settings > Source."

## Ports

Set the target port in **Settings > Networking** if the image does not listen on `PORT` (default 8080). The image's `EXPOSE` line is used as a hint.

## Newer images

Pin a tag. To pick up a new image, change the tag or select **Redeploy**; **Check for a newer image** in **Settings > Source** compares the tag's current digest with the running one.

## Next steps

- [Custom domains and HTTPS](/docs/guides/custom-domains)
- [Volumes](/docs/guides/volumes)
