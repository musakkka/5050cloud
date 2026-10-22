// SPDX-License-Identifier: AGPL-3.0-only

package core

import "github.com/musakkka/5050cloud/pkg/proto"

// Pinned infrastructure images the runtime manages (docs/SPEC.md 8.1).
// HostConfig.Images may override any of them (mirrors). Caddy and BuildKit
// are pinned by their own packages (internal/agent/netw and
// internal/agent/build); the agent passes an override to them when the
// control plane sets one, and an empty string otherwise.
const (
	// RegistryImage is distribution's registry 3.0.0 (released 2025-04),
	// pinned by its multi-arch index digest (checked 2026-10-22 with
	// `docker buildx imagetools inspect registry:3.0.0`). The 2.8 line is
	// no longer maintained.
	RegistryImage = "registry:3.0.0@sha256:6c5666b861f3505b116bb9aa9b25175e71210414bd010d92035ff64018f9457e"
)

// Image keys in HostConfig.Images.
const (
	ImageKeyRegistry = "registry"
	ImageKeyCaddy    = "caddy"
	ImageKeyBuildKit = "buildkit"
)

// pinnedImage returns the override for key, or the pin.
func pinnedImage(h proto.HostConfig, key, pin string) string {
	if v := h.Images[key]; v != "" {
		return v
	}
	return pin
}
