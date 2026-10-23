---
title: Use all cores
description: Run one process per vCPU in each replica so single-threaded runtimes like Node use every core of the host.
---

Run one process per vCPU in each replica so single-threaded runtimes like Node use every core of the host.

## Turn it on

1. Open the service, go to **Settings > Scale**.
2. Turn on **Use all cores** (it sets **Processes** to `auto`), or set a number.
3. Select **Deploy**.

`auto` means one process per vCPU of the service's CPU limit, or of the host when there is no limit, between 1 and 32.

## Node

When the start command is `node [flags] <file>` (directly, or as the `start` script that `npm start` runs), 5050cloud runs your app in Node cluster mode: N processes sharing one port. The panel shows "Runs N Node processes sharing port P (cluster mode)". Your code needs no change.

Work that must run once (a scheduler, a migration) should check `CLUSTER_WORKER`: it is unset or `1` in exactly one process.

## Other runtimes

5050cloud sets `WEB_CONCURRENCY=N` and `FIFTY_PROCESSES=N`. Many servers read `WEB_CONCURRENCY`:

| Server | How it uses it |
|---|---|
| gunicorn | Reads `WEB_CONCURRENCY` as the default worker count |
| uvicorn | `--workers $WEB_CONCURRENCY` |
| puma | `workers ENV.fetch("WEB_CONCURRENCY")` in `config/puma.rb` (the Rails default) |
| PHP-FPM, Go, Rust, Java | Already use every core; leave Processes at 1 |

## Replicas instead

Replicas on one host also use all cores, and they restart independently. Use replicas when your runtime cannot fork workers.

## Memory

Memory use grows with each process. Raise the memory limit when you raise Processes.

## The measured result

Measured on 2026-10-22 on a pre-release build, on one 2 vCPU VM (Ubuntu 24.04, 8 GB), with `oha` on the same VM through the HTTPS proxy, 16 connections, median of 3 runs of 30 s:

| Work per request | One process | Use all cores | On / off |
|---|---|---|---|
| 60,000 primes (about 2 ms) | 516 req/s | 554 req/s | 1.07x |
| 300,000 primes (32 ms) | 58.9 req/s | 60.4 req/s | 1.03x |

With **Use all cores** on, the console says "Runs 2 Node processes sharing port 8080 (cluster mode)" and both workers run at 100% CPU. The gain is small because this VM's two vCPUs are two hardware threads of one physical core (`lscpu` shows `Thread(s) per core: 2`), and this work keeps that core busy. Check `lscpu` on your VM: the setting helps most where the vCPUs are full cores. That case is not measured yet.

![The Scale settings of the api service: Replicas set to 2 and Use all cores ticked, with the note that it runs 2 Node processes sharing port 8080](shot:scale "Settings, Scale: Replicas and Use all cores.")

## Next steps

- [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime)
