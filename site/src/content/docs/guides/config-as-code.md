---
title: Config as code
description: Keep a service's build and deploy settings in your repo with `railway.json`, `railway.toml`, `5050.yaml` or `5050.json`.
---

Keep a service's build and deploy settings in your repo with `railway.json`, `railway.toml`, `5050.yaml` or `5050.json`.

## Which file is read

At build time 5050cloud looks in the service's root directory (or the **Config file path** in **Settings > Source**) for the first of `5050.yaml`, `5050.json`, `railway.json`, `railway.toml`.

## Example

```json
{
  "$schema": "https://railway.com/railway.schema.json",
  "build": { "builder": "NIXPACKS", "buildCommand": "npm run build" },
  "deploy": {
    "startCommand": "node server.js",
    "preDeployCommand": "npm run migrate",
    "healthcheckPath": "/health",
    "restartPolicyType": "ON_FAILURE"
  },
  "environments": {
    "production": { "deploy": { "numReplicas": 2 } }
  }
}
```

## Priority

Highest first, as on Railway:

1. The `environments.<current environment>` block of the file.
2. The file's top-level fields.
3. The settings in the console.

A field set by the file shows a file icon in the console and cannot be edited there. Hover it to see the line, for example `Set by railway.json line 12`. The deployment's **Details** tab lists which fields came from the file.

## Supported fields

`build.builder`, `build.buildCommand`, `build.dockerfilePath`, `build.watchPatterns`, `build.railpackVersion`, `deploy.startCommand`, `deploy.preDeployCommand`, `deploy.healthcheckPath`, `deploy.healthcheckTimeout`, `deploy.restartPolicyType`, `deploy.restartPolicyMaxRetries`, `deploy.cronSchedule`, `deploy.numReplicas`, `deploy.overlapSeconds`, `deploy.drainingSeconds`, `deploy.multiRegionConfig` (replica count only), and the same inside `environments.<name>`. `5050.yaml` and `5050.json` also accept `deploy.processes`, `deploy.placement` and `deploy.singleton`.

- An unknown field fails the build with the line and a suggestion, for example "railway.json line 7: unknown field buildComand. Did you mean buildCommand?"
- A known field 5050cloud does not support, such as `deploy.sleepApplication`, is ignored with one notice line in the build log.

## Nixpacks

`builder: NIXPACKS` builds with Railpack and logs "Nixpacks config detected, building with Railpack." From `nixpacks.toml`, `[start] cmd`, `[phases.build] cmds`, `[variables]` and `[phases.setup] nixPkgs` and `aptPkgs` are mapped; other keys are ignored with a log line each. `NIXPACKS_BUILD_CMD`, `NIXPACKS_START_CMD`, `NIXPACKS_INSTALL_CMD` and the version variables are mapped to Railpack. A `railpack.json` is passed to Railpack unchanged.

## Export your settings

`5050 config pull` writes an environment's services and settings to `5050.yaml`.

## Next steps

- [Config file schema](/docs/reference/config-file)
- [Service settings](/docs/reference/service-settings)
