---
title: 5050cloud and Dokploy
description: Dokploy runs on Docker Swarm; 5050cloud runs its own agent on plain Docker with a WireGuard mesh and needs no external registry.
---

Dokploy runs on Docker Swarm; 5050cloud runs its own agent on plain Docker with a WireGuard mesh and needs no external registry.

Facts as of 2026-10-22, from Dokploy's docs.

| | 5050cloud | Dokploy |
|---|---|---|
| License | AGPL-3.0 server, MIT CLI | Apache-2.0 plus a proprietary directory |
| Runs on | Docker with its own agent | Docker Swarm |
| Multi-server | Agent join, built-in registry | Swarm cluster; needs a registry |
| Inbound ports | 80, 443 and WireGuard between hosts | Swarm ports between nodes |
| Variable references | Yes, Railway syntax | No |
| Zero-downtime deploys | Yes for services without a volume | Swarm rolling updates with health checks |
| Backups | Point-in-time Postgres, restore to a copy | Database and volume backups to S3 |
| Builds | Railpack or Dockerfile | Nixpacks, Railpack, buildpacks |
