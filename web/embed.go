// SPDX-License-Identifier: AGPL-3.0-only

// Package web embeds the built console (web/dist, produced by `make web`).
// Owned by WP8; see docs/SPEC.md 13.
package web

import "embed"

// Dist is the built single-page app. index.html is served for unknown paths.
//
//go:embed all:dist
var Dist embed.FS
