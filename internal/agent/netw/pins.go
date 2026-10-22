// SPDX-License-Identifier: AGPL-3.0-only

package netw

// CaddyImage is the pinned Caddy image (docs/SPEC.md 8.4): release v2.11.7
// of 2026-10-03, the multi-arch index digest as published on Docker Hub.
// HostConfig.Images["caddy"] may point it at a mirror; core passes that to
// Ingress.EnsureRunning. M0 checked this release's dynamic "a" upstreams
// with a custom resolver port.
const CaddyImage = "caddy:2.11.7@sha256:3095ad245c353e3bdb27a38f22a095c1cfa9e295ab21c0214c3e72b0e2960525"
