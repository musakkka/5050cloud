// SPDX-License-Identifier: AGPL-3.0-only

package build

// Pins for everything the builder runs (docs/SPEC.md 8.2 step 11). Each
// version was at least two weeks old when it was pinned. Upgrading a pin is a
// 5050cloud release, never a silent change on a host.
//
// Railpack ships as a CLI (run on the host to write the build plan) and a
// BuildKit frontend image (run inside BuildKit to turn the plan into an
// image). The two must come from the same release, so each RailpackPin
// carries both.

// RailpackPin is one Railpack release the builder can use.
type RailpackPin struct {
	Version string
	// Archives maps a Go architecture ("amd64", "arm64") to the release
	// tarball and its SHA-256.
	Archives map[string]RailpackArchive
	// FrontendDigest is the manifest list digest of
	// ghcr.io/railwayapp/railpack-frontend:v<Version>.
	FrontendDigest string
}

// RailpackArchive is one per-architecture Railpack release tarball.
type RailpackArchive struct {
	URL    string
	SHA256 string
}

// railpackPins lists the current pin first, then the two releases before it
// (docs/SPEC.md 8.2 step 3: "the current pin and the two before it").
var railpackPins = []RailpackPin{
	{
		Version: "0.40.1",
		Archives: map[string]RailpackArchive{
			"amd64": {
				URL:    "https://github.com/railwayapp/railpack/releases/download/v0.40.1/railpack-v0.40.1-x86_64-unknown-linux-musl.tar.gz",
				SHA256: "2842de93e68713af9037e0bc0a398d7da78f3b96aa4804303a638db2bc69bd30",
			},
			"arm64": {
				URL:    "https://github.com/railwayapp/railpack/releases/download/v0.40.1/railpack-v0.40.1-arm64-unknown-linux-musl.tar.gz",
				SHA256: "c24a064b586b8f4f8c2fab44dd5ef19253e4c6cc4e1df793b3ae19cd87f7a5d4",
			},
		},
		FrontendDigest: "sha256:f1973377693af30c9b37a92c97c661c07b277ccdc6be909213c74c771f8d2d6d",
	},
	{
		Version: "0.40.0",
		Archives: map[string]RailpackArchive{
			"amd64": {
				URL:    "https://github.com/railwayapp/railpack/releases/download/v0.40.0/railpack-v0.40.0-x86_64-unknown-linux-musl.tar.gz",
				SHA256: "52e5558f830b7349386e9448e04351d0834df041d4c31c20b7ec4d539f0d66d7",
			},
			"arm64": {
				URL:    "https://github.com/railwayapp/railpack/releases/download/v0.40.0/railpack-v0.40.0-arm64-unknown-linux-musl.tar.gz",
				SHA256: "86c5a3db888c710b2014262de55fc87a56c85c4e96ba7cd10894e5df210a212b",
			},
		},
		FrontendDigest: "sha256:fc6d5fa434c9310500dc18bebb0a4eb4854fee8546a6d7a090e7a36e39d9d153",
	},
	{
		Version: "0.39.0",
		Archives: map[string]RailpackArchive{
			"amd64": {
				URL:    "https://github.com/railwayapp/railpack/releases/download/v0.39.0/railpack-v0.39.0-x86_64-unknown-linux-musl.tar.gz",
				SHA256: "728407f5cdb9e9bc1cdd07f568419344a20e71b0a5a9fd90a9cfbaca0a6c94f7",
			},
			"arm64": {
				URL:    "https://github.com/railwayapp/railpack/releases/download/v0.39.0/railpack-v0.39.0-arm64-unknown-linux-musl.tar.gz",
				SHA256: "42eb3fa68e38f44be3610a7d74f714ec0d808d70c105fbe35e053b6e6cfb20be",
			},
		},
		FrontendDigest: "sha256:db24dc37640b6887c3d455b40876ea30f75182964479670cba6e4cde7ffef103",
	},
}

const (
	// RailpackFrontendRepo is the upstream frontend repository. Builds use
	// the mirrored copy in the internal registry when it exists.
	RailpackFrontendRepo = "ghcr.io/railwayapp/railpack-frontend"
	// MirrorFrontendRepo is the repository path, inside the internal
	// registry, where the installer mirrors the frontend image by digest.
	MirrorFrontendRepo = "5050/railpack-frontend"

	// BuildKitImage is the pinned BuildKit daemon (v0.33.1, manifest list).
	BuildKitImage = "moby/buildkit:v0.33.1@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea"
	// MirrorBuildKitRepo is where the installer mirrors the BuildKit image.
	MirrorBuildKitRepo = "5050/buildkit"
)

// CurrentRailpack returns the default Railpack pin.
func CurrentRailpack() RailpackPin { return railpackPins[0] }

// RailpackVersions lists the selectable Railpack versions, newest first.
func RailpackVersions() []string {
	out := make([]string, len(railpackPins))
	for i, p := range railpackPins {
		out[i] = p.Version
	}
	return out
}

// railpackPin finds a pin by version. A leading "v" is accepted.
func railpackPin(version string) (RailpackPin, bool) {
	if version == "" {
		return railpackPins[0], true
	}
	if version[0] == 'v' || version[0] == 'V' {
		version = version[1:]
	}
	for _, p := range railpackPins {
		if p.Version == version {
			return p, true
		}
	}
	return RailpackPin{}, false
}
