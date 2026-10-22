---
title: Writing a template
description: A template is a directory in `templates/` with a manifest that describes the services to create, their images, volumes, variables and inputs.
---

A template is a directory in `templates/` with a manifest that describes the services to create, their images, volumes, variables and inputs.

## Layout

```
templates/
  umami/
    template.yaml
    icon.svg
```

## Manifest

```yaml
id: umami
name: Umami
tagline: Simple, privacy-friendly web analytics
docs: https://umami.is/docs
inputs:
  - name: APP_SECRET
    description: Secret used to sign sessions
    required: true
    generate: secret(32)
services:
  - name: Postgres
    template: postgres
  - name: Umami
    image: ghcr.io/umami-software/umami:postgresql-v2.19.0
    dependsOn: [Postgres]
    variables:
      DATABASE_URL: ${{Postgres.DATABASE_URL}}
      APP_SECRET: ${{input.APP_SECRET}}
    networking:
      port: 3000
    domain: true
```

The schema is `Template` in `pkg/manifest/manifest.go`; field names are camelCase and the file is read with `sigs.k8s.io/yaml`. Check an existing template in `templates/` for the exact shape before you start.

## Rules

- Pin images to an exact version.
- Generate secrets with `secret(n)` or `randomInt(min, max)`; never put real secrets in the manifest.
- Use `dependsOn` so services deploy in order.
- Make provisioning re-runnable: create users and databases only if missing.
- Add a golden render test (the existing templates show how) and run `make test`.

## Submit

Open a pull request with the template, its icon and a line in the template list of the docs. See [CONTRIBUTING.md](https://github.com/musakkka/5050cloud/blob/main/CONTRIBUTING.md).
