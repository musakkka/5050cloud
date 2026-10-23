---
title: Deploy with the CLI
description: `5050 up` uploads the current directory, builds it on your hosts and streams the logs until the deploy is live.
---

`5050 up` uploads the current directory, builds it on your hosts and streams the logs until the deploy is live.

## Steps

1. Install the CLI: `curl -fsSL {{GET}} | sh -s -- --cli`.
2. Log in: `5050 login https://console.example.com`.
3. In your app's directory, create or link a project: `5050 init -n my-app` or `5050 link`.
4. Deploy: `5050 up --new` the first time, `5050 up` afterwards.

```
$ 5050 up
Uploaded 3.2 MB
... build log ...
... first deploy log lines ...
Deployed: https://my-app-k3x9.203-0-113-10.sslip.io
```

## What is uploaded

The directory is packed as a gzipped tar, skipping `.git` and files matched by `.gitignore` and `.5050ignore`. Uploads over 500 MB are refused. `--no-gitignore` includes ignored files; `--path-as-root PATH` uploads only `PATH` as the root.

## From CI

```sh
export FIFTY_URL=https://console.example.com
export FIFTY_TOKEN=5050_xxxxxxxx
5050 up -c -s api -e production -m "$GIT_COMMIT_MESSAGE"
```

`-c` streams the build log and exits non-zero if the build or deploy fails. For CI, create an environment token in the project's **Project Settings**, **Tokens**. See [Set up the CLI](/docs/guides/cli-setup#tokens-for-servers-and-ci).

## Next steps

- [CLI reference: up](/docs/cli/up)
- [Config as code](/docs/guides/config-as-code)
