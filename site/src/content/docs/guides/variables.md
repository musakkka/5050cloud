---
title: Variables and references
description: Set environment variables per service, share them across an environment, and reference other services with `${{Service.VAR}}`.
---

Set environment variables per service, share them across an environment, and reference other services with `${{Service.VAR}}`.

## Set variables

1. Open the service and go to **Variables**.
2. Select **New Variable**, or **Raw Editor** to paste `.env` content.
3. Select **Deploy** in the banner to apply.

From the CLI: `5050 variables set KEY=value`.

Names match `[A-Za-z_][A-Za-z0-9_]*` (up to 256 characters). Values are UTF-8, up to 64 KiB, multi-line allowed. Values are stored encrypted and shown only when you reveal them; reveals are audited.

## References

![The Variables tab of the api service: DATABASE_URL set to ${{Postgres.DATABASE_URL}} and REDIS_URL set to ${{Redis.REDIS_URL}}](shot:variables "References show in colour; select a value to see what it resolves to.")

| Syntax | Resolves to |
|---|---|
| `${{NAME}}` | Another variable of the same service |
| `${{shared.NAME}}` | A shared variable of the environment |
| `${{Postgres.DATABASE_URL}}` | A variable of another service, by service name (case-sensitive) |
| `${{api.RAILWAY_PUBLIC_DOMAIN}}` | A provided variable of another service |

Text and references can mix: `https://${{api.RAILWAY_PUBLIC_DOMAIN}}/v1`. An unknown reference resolves to an empty string, shows a warning icon, and writes a warning line in the deploy log; it does not block the deploy. Renaming a service rewrites references to it.

## Generated values

`${{secret(32)}}`, `${{secret(16, "abcdef0123456789")}}` and `${{randomInt(1000, 9999)}}` are evaluated once when you save, and the result is stored.

## Shared variables

Open **Project Settings > Shared Variables** for the environment. **Share** adds `NAME=${{shared.NAME}}` to the services you pick.

## Sealed variables

A sealed variable can never be revealed again. It still reaches builds and deployments, but not `5050 run`, duplication or exports.

## Suggested variables

When a repo has `.env.example`, `.env.sample` or `.env.template` files, their names (and example values) are suggested. Real `.env` files only suggest names.

## Heroku

Paste the output of `heroku config --json` or `heroku config -s` into **Import variables from Heroku** in the Variables menu, or run `5050 import heroku-env FILE`.

## Next steps

- [Variables reference](/docs/reference/variables): provided variables and aliases.
- [Staged changes](/docs/guides/staged-changes)
