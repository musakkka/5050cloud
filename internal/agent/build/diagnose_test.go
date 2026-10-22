// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each build row of docs/SPEC.md 9.18 against a captured log. The npm, yarn
// and buildx fixtures were captured from real runs; the Railpack ones from
// Railpack 0.40.1.
func TestDiagnoseFixtures(t *testing.T) {
	cases := []struct {
		file string
		ctx  diagContext
		want string
	}{
		{"railpack_nothing_detected.log", diagContext{}, "Railpack could not detect a language in /. Set the root directory or add a Dockerfile."},
		{"railpack_nothing_detected.log", diagContext{RootDir: "apps/web/"}, "Railpack could not detect a language in /apps/web. Set the root directory or add a Dockerfile."},
		{"npm_ci_lockfile.log", diagContext{}, "npm ci needs a package-lock.json. Commit one or set a custom install command."},
		{"node_engine_yarn.log", diagContext{}, "The build needs Node >= 30 (package.json engines). Set NODE_VERSION or update engines."},
		{"node_engine_npm.log", diagContext{}, "The build needs Node >= 30 (package.json engines). Set NODE_VERSION or update engines."},
		{"dockerfile_missing.log", diagContext{DockerfilePath: "Nope.Dockerfile"}, "No Dockerfile at Nope.Dockerfile. Check the Dockerfile path in Settings > Build."},
		{"dockerfile_missing.log", diagContext{}, "No Dockerfile at Dockerfile. Check the Dockerfile path in Settings > Build."},
		{"base_image_not_found.log", diagContext{}, "Image node:99.99.99-nonexistent was not found."},
		{"base_image_unauthorized.log", diagContext{}, "The registry refused access to ghcr.io/5050cloud-private-nonexistent/app. Add registry credentials in Settings > Source."},
		{"disk_full.log", diagContext{}, "The build host ran out of disk. Free space or add a data disk."},
		{"no_start_command.log", diagContext{}, "No start command was found. Set one in Settings > Deploy or add a start script."},
		{"generic.log", diagContext{}, SentenceGeneric},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "diagnose", c.file))
			if err != nil {
				t.Fatal(err)
			}
			// The runner drops empty lines; do the same.
			var lines []string
			for _, l := range strings.Split(string(data), "\n") {
				if strings.TrimSpace(l) != "" {
					lines = append(lines, l)
				}
			}
			if got := diagnose(lines, c.ctx); got != c.want {
				t.Errorf("diagnose(%s)\n got: %s\nwant: %s", c.file, got, c.want)
			}
		})
	}
}

func TestDiagnoseOrderDiskFirst(t *testing.T) {
	lines := []string{
		"failed to resolve source metadata for docker.io/library/node:22: write /var/lib/buildkit: no space left on device",
	}
	if got := diagnose(lines, diagContext{}); got != SentenceDiskFull {
		t.Errorf("got %q, want the disk full sentence", got)
	}
}

func TestNodeEngineForms(t *testing.T) {
	cases := map[string]string{
		`npm error notsup Required: {"node":">=20"}`:                                                                    "Node >= 20",
		`error a@1.0.0: The engine "node" is incompatible with this module. Expected version "^18.0.0". Got "22.1.0"`:   "Node ^18.0.0",
		`ERR_PNPM_UNSUPPORTED_ENGINE  Unsupported environment (bad pnpm and/or Node.js version) Expected version: >=20`: "Node >= 20",
	}
	for line, want := range cases {
		got := diagnose([]string{line}, diagContext{})
		if !strings.Contains(got, want) {
			t.Errorf("diagnose(%q) = %q, want it to mention %q", line, got, want)
		}
	}
}

func TestTimeoutSentence(t *testing.T) {
	cases := map[int]string{
		1800: "The build took longer than 30 minutes and was stopped. Raise the build timeout in Settings > Build.",
		60:   "The build took longer than 1 minute and was stopped. Raise the build timeout in Settings > Build.",
		90:   "The build took longer than 90 seconds and was stopped. Raise the build timeout in Settings > Build.",
		0:    "The build took longer than 30 minutes and was stopped. Raise the build timeout in Settings > Build.",
	}
	for sec, want := range cases {
		if got := timeoutSentence(sec); got != want {
			t.Errorf("timeoutSentence(%d) = %q, want %q", sec, got, want)
		}
	}
}

func TestImageNameHelpers(t *testing.T) {
	cases := map[string][2]string{
		"docker.io/library/node:22":         {"node:22", "node"},
		"index.docker.io/acme/app:1":        {"acme/app:1", "acme/app"},
		"ghcr.io/acme/app@sha256:abc":       {"ghcr.io/acme/app@sha256:abc", "ghcr.io/acme/app"},
		"localhost:5000/svc:tag":            {"localhost:5000/svc:tag", "localhost:5000/svc"},
		"registry.example.com:443/team/app": {"registry.example.com:443/team/app", "registry.example.com:443/team/app"},
	}
	for in, want := range cases {
		short := shortImageName(in)
		if short != want[0] {
			t.Errorf("shortImageName(%q) = %q, want %q", in, short, want[0])
		}
		if repo := repositoryOf(short); repo != want[1] {
			t.Errorf("repositoryOf(%q) = %q, want %q", short, repo, want[1])
		}
	}
}
