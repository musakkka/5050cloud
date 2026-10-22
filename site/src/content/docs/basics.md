---
title: The basics
description: 5050cloud organises your apps into projects, environments, services and deployments, the same way Railway does, and runs them on hosts you own.
---

5050cloud organises your apps into projects, environments, services and deployments, the same way Railway does, and runs them on hosts you own.

## Projects

A project is a canvas of services that belong together, such as an API, a web app, a worker and their databases. Create one from the dashboard with **New project** or with `5050 init`.

## Environments

Every project starts with a `production` environment. Add more (for example `staging`) from the environment menu at the top of the canvas or with `5050 environment new staging --duplicate production`. Each environment has its own services, variables, domains and private network. Environments cannot reach each other's services.

## Services

A service runs one thing: code from a GitHub repo, a Docker image, a directory uploaded with `5050 up`, or a database template. A service has settings (source, build, deploy, networking), variables, an optional volume and domains. Services are **web** (they take traffic), **worker** (no port) or **cron** (run on a schedule).

## Deployments

Each deploy creates a deployment: a build (or image pull), then containers started from it. A deployment keeps a snapshot of the image, settings and variables it ran with, so a rollback restores exactly that. The statuses are listed in [Deployment statuses](/docs/reference/deployment-statuses).

## Variables

Variables are environment variables for a service. They can reference other services, as in `${{Postgres.DATABASE_URL}}`, or shared variables of the environment, as in `${{shared.API_KEY}}`. See [Variables and references](/docs/guides/variables).

## Staged changes

Edits to settings and variables are staged first. A banner shows how many changes are waiting; **Details** shows the diff and **Deploy** applies them all at once. See [Staged changes](/docs/guides/staged-changes).

## Hosts

A host is a Linux VM running the 5050cloud agent. The first host also runs the control plane and the console. Add more with one command; see [Add a host](/docs/guides/add-a-host). Hosts have roles:

| Role | What it does |
|---|---|
| `app` | Runs app replicas |
| `ingress` | Serves public traffic on ports 80 and 443 |
| `data` | Runs databases and other services with volumes |
| `build` | Builds images |

The first host has every role.

## Next steps

- [Quick start](/docs/quick-start)
- [Coming from Railway](/docs/coming-from-railway)
- [Architecture](/docs/reference/architecture)
