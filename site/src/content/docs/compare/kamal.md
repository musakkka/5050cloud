---
title: 5050cloud and Kamal
description: Kamal deploys containers from your laptop or CI over SSH; 5050cloud is a running platform with a console, databases and backups.
---

Kamal deploys containers from your laptop or CI over SSH; 5050cloud is a running platform with a console, databases and backups.

Facts as of 2026-10-22, from Kamal's docs.

| | 5050cloud | Kamal |
|---|---|---|
| License | AGPL-3.0 server, MIT CLI | MIT |
| Web UI | Yes | No |
| How it reaches servers | Agent dials out | SSH from your laptop or CI |
| Builds | Railpack or Dockerfile, on your hosts | Dockerfile required, pushed to a registry you provide |
| Multi-server | Built-in load balancing | Needs an external load balancer |
| Replicas on one host | Yes | One container per role per host |
| Databases | One click, with backups | Accessories: plain containers, no upgrades, no zero downtime |
| Backups | Built in | None built in |
| Zero-downtime deploys | Yes for services without a volume | Yes, through kamal-proxy |

Kamal fits teams that want deploys as code from CI with nothing running between deploys.
