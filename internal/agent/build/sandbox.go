// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The BuildKit sandbox (docs/SPEC.md 8.2 step 8): buildx builder "5050",
// docker-container driver, pinned BuildKit image, on the 5050-build bridge,
// buildkitd --oci-worker-net=bridge so RUN steps never use the host
// network, no network.host entitlement, GC at 20 GB, capped at 50% of host
// memory and cpus-1 CPUs. The firewall (internal/agent/netw) drops traffic
// from the bridge to every host address, the mesh and 169.254.169.254,
// except the registry on the gateway port 5000.

const (
	gcMaxUsed     = "20GB"
	cpuPeriod     = 100000
	registryPort  = "5000"
	stateFileName = "builder.state"
)

// engine is the slice of the Docker Engine API the sandbox needs.
type engine interface {
	// NetworkGateway returns the IPv4 gateway of a Docker network, and
	// errNoNetwork when it does not exist.
	NetworkGateway(ctx context.Context, name string) (string, error)
	// HostResources returns the engine host's CPU count and memory.
	HostResources(ctx context.Context) (cpus int, memBytes int64, err error)
	// Container returns a container's image and whether it runs; found is
	// false when it does not exist.
	Container(ctx context.Context, name string) (image string, running, found bool, err error)
}

var errNoNetwork = errors.New("network not found")

// EnsureRunning starts or upgrades the BuildKit builder. It is idempotent:
// a builder with the same image and settings is left alone (and started if
// it stopped).
func (b *builder) EnsureRunning(ctx context.Context, image string) error {
	return b.ensureErr(b.ensure(ctx, image))
}

// ensureErr turns internal failures into plain errors for core.
func (b *builder) ensureErr(err error) error {
	var bf *buildFailure
	if errors.As(err, &bf) {
		return errors.New(bf.sentence)
	}
	return err
}

func (b *builder) ensure(ctx context.Context, image string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if image == "" {
		image = b.lastImage
	}
	if image == "" {
		image = b.images()["buildkit"]
	}
	if image == "" {
		image = BuildKitImage
	}
	if err := b.checkBuildx(ctx); err != nil {
		return err
	}

	netName := b.opts.BuildNetworkName
	if b.opts.Network != nil {
		n, err := b.opts.Network.BuildNetwork(ctx)
		if err != nil {
			return fail("The build network could not be set up: %s", strings.TrimSuffix(err.Error(), ".")+".")
		}
		if n != "" {
			netName = n
		}
	}
	gw, err := b.eng.NetworkGateway(ctx, netName)
	if errors.Is(err, errNoNetwork) {
		return fail("The build network %s does not exist. The agent creates it on hosts with the build role; check the agent log for networking errors.", netName)
	}
	if err != nil {
		return fail("The build network %s could not be inspected: %v.", netName, err)
	}
	endpoint := b.opts.BuildRegistry
	if endpoint == "" {
		if gw == "" {
			return fail("The build network %s has no IPv4 gateway, so builds cannot reach the registry.", netName)
		}
		endpoint = gw + ":" + registryPort
	}
	cpus, mem, err := b.eng.HostResources(ctx)
	if err != nil {
		return fail("The Docker Engine did not report host resources: %v.", err)
	}
	mirror := b.images()["dockerhub-mirror"]
	cfg := renderBuildkitdConfig(endpoint, mirror)
	dir := filepath.Join(b.opts.DataDir, "build")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail("The builder directory %s could not be created: %v.", dir, err)
	}
	cfgPath := filepath.Join(dir, "buildkitd.toml")
	if err := writeFileAtomic(cfgPath, []byte(cfg), 0o600); err != nil {
		return err
	}
	createArgs := b.createArgv(image, netName, cfgPath, cpus, mem)
	sum := sha256.Sum256([]byte(strings.Join(createArgs, "\x00") + "\x00" + cfg))
	want := hex.EncodeToString(sum[:])

	statePath := filepath.Join(dir, stateFileName)
	have, _ := os.ReadFile(statePath)
	cimg, running, found, err := b.eng.Container(ctx, builderContainer)
	if err != nil {
		return fail("The BuildKit container could not be inspected: %v.", err)
	}
	env := b.baseEnv()
	if err := os.MkdirAll(b.dockerConfigDir(), 0o700); err != nil {
		return fail("The builder configuration directory could not be created: %v.", err)
	}
	if found && strings.TrimSpace(string(have)) == want && cimg == image {
		if !running {
			if err := b.run.Run(ctx, cmdSpec{Path: b.dockerCLI, Args: []string{"buildx", "inspect", "--bootstrap", builderName}, Env: env}, nil); err != nil {
				return fail("The BuildKit builder could not be started: %v.", err)
			}
		}
	} else {
		// Replace the builder, keeping its cache volume.
		_ = b.run.Run(ctx, cmdSpec{Path: b.dockerCLI, Args: []string{"buildx", "rm", "--keep-state", builderName}, Env: env}, nil)
		var out []string
		if err := b.run.Run(ctx, cmdSpec{Path: b.dockerCLI, Args: createArgs, Env: env}, func(l string) { out = append(out, l) }); err != nil {
			return fail("The BuildKit builder could not be created: %v.", err)
		}
		if err := writeFileAtomic(statePath, []byte(want+"\n"), 0o600); err != nil {
			return err
		}
	}
	b.endpoint = endpoint
	b.lastImage = image
	return nil
}

// createArgv is `docker buildx create` for the sandboxed builder.
func (b *builder) createArgv(image, netName, cfgPath string, cpus int, mem int64) []string {
	args := []string{
		"buildx", "create",
		"--name", builderName,
		"--driver", "docker-container",
		"--driver-opt", "image=" + image,
		"--driver-opt", "network=" + netName,
		"--driver-opt", "restart-policy=unless-stopped",
	}
	if mem > 0 {
		half := strconv.FormatInt(mem/2, 10)
		args = append(args, "--driver-opt", "memory="+half, "--driver-opt", "memory-swap="+half)
	}
	if cpus > 0 {
		args = append(args,
			"--driver-opt", "cpu-period="+strconv.Itoa(cpuPeriod),
			"--driver-opt", "cpu-quota="+strconv.Itoa(max(1, cpus-1)*cpuPeriod),
		)
	}
	// Explicit flags replace buildx's defaults, which would allow the
	// network.host entitlement.
	args = append(args, "--buildkitd-flags=--oci-worker-net=bridge")
	if versionAtLeast(b.buildxVer, 0, 13) {
		args = append(args, "--buildkitd-config", cfgPath)
	} else {
		args = append(args, "--config", cfgPath)
	}
	return append(args, "--bootstrap")
}

// renderBuildkitdConfig is buildkitd.toml: no insecure entitlements, the
// OCI worker with GC, plain HTTP to the internal registry on the build
// network, and an optional Docker Hub mirror.
func renderBuildkitdConfig(endpoint, mirror string) string {
	var s strings.Builder
	s.WriteString("# Written by the 5050cloud agent. Changes are overwritten.\n")
	s.WriteString("insecure-entitlements = []\n\n")
	s.WriteString("[worker.oci]\n  enabled = true\n  gc = true\n")
	fmt.Fprintf(&s, "  maxUsedSpace = %q\n\n", gcMaxUsed)
	s.WriteString("[worker.containerd]\n  enabled = false\n\n")
	fmt.Fprintf(&s, "[registry.%q]\n  http = true\n", endpoint)
	mirror = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(mirror, "https://"), "http://"), "/")
	if mirror != "" && validHostPort(mirror) {
		fmt.Fprintf(&s, "\n[registry.\"docker.io\"]\n  mirrors = [%q]\n", mirror)
		fmt.Fprintf(&s, "\n[registry.%q]\n  http = true\n", mirror)
	}
	return s.String()
}

var hostPortRe = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

func validHostPort(s string) bool { return hostPortRe.MatchString(s) }

var buildxVersionRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// checkBuildx makes sure the docker CLI and its buildx plugin exist.
func (b *builder) checkBuildx(ctx context.Context) error {
	if b.buildxVer != "" {
		return nil
	}
	var out []string
	err := b.run.Run(ctx, cmdSpec{Path: b.dockerCLI, Args: []string{"buildx", "version"}, Env: b.baseEnv()}, func(l string) { out = append(out, l) })
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail("Docker Buildx is not installed on this host. Install the docker-buildx-plugin package and restart the agent.")
	}
	for _, l := range out {
		if m := buildxVersionRe.FindString(l); m != "" {
			b.buildxVer = m
			return nil
		}
	}
	b.buildxVer = "v0.0.0"
	return nil
}

func versionAtLeast(v string, major, minor int) bool {
	m := buildxVersionRe.FindStringSubmatch(v)
	if m == nil {
		return true // unknown versions are assumed current
	}
	ma, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	return ma > major || (ma == major && mi >= minor)
}

// Prune trims the BuildKit cache to keepBytes.
func (b *builder) Prune(ctx context.Context, keepBytes int64) error {
	if keepBytes < 0 {
		keepBytes = 0
	}
	n := strconv.FormatInt(keepBytes, 10)
	env := b.baseEnv()
	var out []string
	collect := func(l string) { out = append(out, l) }
	err := b.run.Run(ctx, cmdSpec{Path: b.dockerCLI, Args: []string{"buildx", "prune", "--builder", builderName, "--force", "--max-used-space", n}, Env: env}, collect)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if strings.Contains(strings.Join(out, "\n"), "unknown flag") {
		// Buildx before v0.17 only knows --keep-storage.
		err = b.run.Run(ctx, cmdSpec{Path: b.dockerCLI, Args: []string{"buildx", "prune", "--builder", builderName, "--force", "--keep-storage", n}, Env: env}, nil)
		if err == nil {
			return nil
		}
	}
	return fail("The build cache could not be pruned: %v.", err)
}
