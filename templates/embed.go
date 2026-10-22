// SPDX-License-Identifier: MIT

// Package templates embeds the built-in service templates
// (templates/<id>/template.yaml plus their files and icon), tools.json (the
// pinned helper binaries such as wal-g and restic) and images.json (image
// digests, the newest release per major version and helper images). See
// docs/SPEC.md 10 and README.md in this directory.
//
// Core (WP1) reads a template's manifest and variables at creation; the
// orchestrator's data services (internal/cp/orch/data, WP2b) render its files,
// mounts, tools and backup recipes at each deploy.
package templates

import "embed"

// FS holds every template directory, tools.json and images.json. Paths are
// relative to this directory ("postgres/template.yaml"). Directories whose
// name starts with "_" hold files shared by several templates and are not
// templates themselves.
//
//go:embed tools.json images.json _shared
//go:embed postgres mysql redis mongodb pgvector postgis
//go:embed n8n umami plausible ghost uptime-kuma metabase minio gitea meilisearch directus
var FS embed.FS
