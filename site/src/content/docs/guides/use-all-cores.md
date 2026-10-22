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

The end-to-end test E20 runs `examples/node-cpu` with 1 process and with `auto` on a 2 vCPU non-burstable VM, with load from a second host, and requires at least 1.6x the throughput.

## Next steps

- [Replicas and zero-downtime deploys](/docs/guides/replicas-zero-downtime)
