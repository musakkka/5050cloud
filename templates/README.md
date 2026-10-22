# templates

Built-in service templates (docs/SPEC.md 10), MIT licensed.

- `<id>/template.yaml`: the template manifest (services, variables, volume, files, tools, backup recipe, probes).
- `<id>/*`: files rendered into the service at deploy. Files ending in `.tmpl` are Go templates sized from the host (see `internal/cp/orch/data/render.go`).
- `_shared/`: scripts shared by several templates (restic volume backups). Not a template.
- `tools.json`: pinned helper binaries (wal-g, restic) with checksums per architecture.
- `images.json`: image digests, newest release per major version and helper images.

Databases: postgres, mysql, redis, mongodb. Apps: pgvector, postgis, n8n, umami, plausible, ghost, uptime-kuma, metabase, minio, gitea, meilisearch, directus.

`FS` in `embed.go` embeds all of it. A new template directory must be added to the `go:embed` lines.
