---
title: The railway alias
description: The `5050` binary also answers to `railway`, so scripts and habits built on Railway's CLI keep working.
---

The `5050` binary also answers to `railway`, so scripts and habits built on Railway's CLI keep working.

## How it works

`5050 alias install` adds a `railway` symlink next to the `5050` binary. It leaves a real `railway` binary alone if one is already on your `PATH`. When the binary runs as `railway`, it accepts Railway's verbs and flags, for example:

```sh
railway login
railway link
railway up --service api
railway variables --set "FOO=bar"
railway add -d postgres
railway logs
railway run -- npm run migrate
```

The first time, it prints one line: `Using 5050cloud at <url>; run 5050 alias remove to undo`.

A verb 5050cloud does not have answers with `railway <verb> is not available in 5050cloud. See 5050cloud.com/docs/coming-from-railway` and exits non-zero.

## Manage the alias

```sh
5050 alias install
5050 alias remove
```

## Tokens

As `railway`, the CLI also reads `RAILWAY_TOKEN` and `RAILWAY_API_TOKEN`. Use a 5050cloud API token as the value, not a Railway token.

## Next steps

- [Coming from Railway](/docs/coming-from-railway)
- [CLI overview](/docs/cli/overview)
