# M0: record Railway import response fixtures

- From: planner (M0)
- To: WP1 (imports) and WP6 (fixture server for E30 and U35)
- Files: e2e/fixtures/railway/
- Blocking: no

## Why
docs/SPEC.md 19.0 item 4 asks M0 to record fixtures of Railway's public
GraphQL API from a test project for the import (9.22, E30, U35). The schema
introspects without a token, so `e2e/fixtures/railway/schema.json` holds the
slice of types and query fields the importer reads (Project, Environment,
Service, ServiceInstance, domains, volumes, TCP proxies, deployment enums),
fetched on 2026-10-22. Response fixtures (a real project with services,
variables, domains, a volume and a Postgres) need a Railway project token for
a throwaway test project. The planner did not create one on anyone's account.

## Proposed change
Whoever holds a Railway test account creates a throwaway project with a
GitHub service, an image service, a Postgres, a volume, shared variables, a
custom domain and a TCP proxy, then records the importer's queries against it
into `e2e/fixtures/railway/project-*.json` with secrets replaced by
placeholders. Until then WP1 writes its parser against `schema.json` and
hand-made fixtures that follow it.
