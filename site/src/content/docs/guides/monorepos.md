---
title: Monorepos and watch paths
description: Deploy several services from one repo, each from its own directory, and build only the ones whose files changed.
---

Deploy several services from one repo, each from its own directory, and build only the ones whose files changed.

## One service per package

1. Add a GitHub repo service. The scan lists JS workspace packages (pnpm, npm, yarn, bun), skipping lint, config and test packages. Tick the ones to deploy.
2. Each becomes a service with its **Root directory** set.

For other layouts, set **Root directory** in **Settings > Source** yourself, for example `/services/api`.

## Watch paths

In **Settings > Build**, set **Watch paths** (gitignore style, relative to the repo root):

```
/services/api/**
/packages/shared/**
!**/*.md
```

A push that changes no matching file records a **Skipped** deployment with "No changed files matched the watch paths". The first push and force pushes deploy everything. Watch paths from a config file (`build.watchPatterns`) win over the setting.

## Shared code

When a service imports code from outside its root directory, keep the root at the repo root and set the build and start commands instead, for example `pnpm --filter api build`.
