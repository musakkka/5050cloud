---
title: Limits
description: The numbers 5050cloud v0.1 is built and tested for.
---

The numbers 5050cloud v0.1 is built and tested for.

## Cluster

| Limit | Value |
|---|---|
| Hosts per install | 50 tested (the mesh allows 127) |
| Service instances per install | 1,000 tested, with 2,000 replicas and 300 environments |
| Environments with replicas on one host | 254 |
| Replicas per environment per host | 253 |
| Processes per replica | 1 to 32 |

## Services

| Limit | Value |
|---|---|
| Service name | 1 to 32 characters |
| Variable name | Up to 256 characters |
| Variable value | 64 KiB |
| Replicas with a volume | 1 |
| Volumes per service | 1 |
| Minimum memory limit | 64 MB |
| Upload size (`5050 up`) | 500 MB |
| Build timeout | 30 minutes by default |
| Pre-deploy timeout | 300 seconds by default, up to 3600 |
| Health check timeout | 300 seconds by default |
| Request body | 1 MiB, except uploads |

## Retention

| Data | Kept |
|---|---|
| Metrics | 30 days |
| Logs | 7 days per host, at most 1 GB per service per host |
| Images per service in the registry | 10 (a setting), plus one build cache |
| Images per service on each host | 3 |
| Database base backups | 7 days by default |
| Control plane snapshots | 24 hourly and 7 daily on disk, 30 days in the bucket |
| Deleted volumes | 48 hours |
| Uploads | 1 hour after their build |

## Performance targets

| Target | Number |
|---|---|
| Install time on a 2 vCPU VM | Under 180 seconds |
| First deploy from the install command | Under 5 minutes |
| Failed requests during a redeploy at 50 requests per second | 0 (services without a volume) |
| Host failover | Routes withdrawn at 60 seconds, traffic served from other hosts within 150 seconds |
| `5050d` memory | Under 120 MB idle |
| Agent memory | Under 60 MB with 20 replicas |
