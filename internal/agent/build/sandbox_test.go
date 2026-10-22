// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func isCreate(c cmdSpec) bool {
	return len(c.Args) >= 2 && c.Args[0] == "buildx" && c.Args[1] == "create"
}

func TestEnsureRunningCreatesSandboxedBuilder(t *testing.T) {
	r := &fakeRunner{}
	e := newFakeEngine()
	b := testBuilder(t, r, e, nil)
	if err := b.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	creates := r.find(isCreate)
	if len(creates) != 1 {
		t.Fatalf("create calls = %d", len(creates))
	}
	args := strings.Join(creates[0].Args, " ")
	for _, want := range []string{
		"--name fifty5050 --driver docker-container",
		"--driver-opt image=" + BuildKitImage,
		"--driver-opt network=5050-build",
		"--driver-opt memory=4294967296 --driver-opt memory-swap=4294967296",
		"--driver-opt cpu-period=100000 --driver-opt cpu-quota=300000",
		"--buildkitd-flags=--oci-worker-net=bridge",
		"--buildkitd-config " + filepath.Join(b.opts.DataDir, "build", "buildkitd.toml"),
		"--bootstrap",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("create argv lacks %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "network.host") || strings.Contains(args, "network=host") {
		t.Errorf("the builder must not get host networking: %s", args)
	}
	cfg, err := os.ReadFile(filepath.Join(b.opts.DataDir, "build", "buildkitd.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"insecure-entitlements = []", `maxUsedSpace = "20GB"`, `[registry."172.30.0.1:5000"]`, "http = true"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("buildkitd.toml lacks %q:\n%s", want, cfg)
		}
	}
	if b.endpoint != "172.30.0.1:5000" {
		t.Errorf("endpoint = %q", b.endpoint)
	}

	// Same image and settings, container running: nothing is recreated.
	e.found, e.running, e.image = true, true, BuildKitImage
	if err := b.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if n := len(r.find(isCreate)); n != 1 {
		t.Errorf("an unchanged builder was recreated (%d creates)", n)
	}
	// Stopped: started, not recreated.
	e.running = false
	if err := b.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(r.find(func(c cmdSpec) bool { return strings.Join(c.Args, " ") == "buildx inspect --bootstrap fifty5050" })) != 1 {
		t.Errorf("a stopped builder must be started")
	}
	// A new image replaces the builder and keeps its cache.
	e.running = true
	if err := b.EnsureRunning(context.Background(), "registry.local/5050/buildkit@sha256:1111"); err != nil {
		t.Fatal(err)
	}
	if n := len(r.find(isCreate)); n != 2 {
		t.Errorf("a new image must recreate the builder (%d creates)", n)
	}
	if len(r.find(func(c cmdSpec) bool { return strings.Join(c.Args, " ") == "buildx rm --keep-state fifty5050" })) < 2 {
		t.Errorf("replacing must keep the BuildKit state")
	}
	// Build without an image keeps the last one asked for.
	e.image = "registry.local/5050/buildkit@sha256:1111"
	if err := b.ensure(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if n := len(r.find(isCreate)); n != 2 {
		t.Errorf("Build must not switch the builder back to the pin (%d creates)", n)
	}
}

func TestEnsureRunningErrors(t *testing.T) {
	e := newFakeEngine()
	e.noNet = true
	b := testBuilder(t, &fakeRunner{}, e, nil)
	err := b.EnsureRunning(context.Background(), "")
	want := "The build network 5050-build does not exist. The agent creates it on hosts with the build role; check the agent log for networking errors."
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}

	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, _ func(string)) (bool, error) {
		if len(c.Args) >= 2 && c.Args[1] == "version" {
			return true, &exitError{Cmd: "docker", Code: 1, Last: "docker: 'buildx' is not a docker command."}
		}
		return false, nil
	}}
	b = testBuilder(t, r, nil, nil)
	err = b.EnsureRunning(context.Background(), "")
	if err == nil || err.Error() != "Docker Buildx is not installed on this host. Install the docker-buildx-plugin package and restart the agent." {
		t.Errorf("err = %v", err)
	}

	e = newFakeEngine()
	e.gateway = ""
	b = testBuilder(t, &fakeRunner{}, e, nil)
	if err := b.EnsureRunning(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "no IPv4 gateway") {
		t.Errorf("err = %v", err)
	}
}

type fakeNetwork struct {
	agentapi.Network
	name string
	err  error
}

func (f fakeNetwork) BuildNetwork(context.Context) (string, error) { return f.name, f.err }

func TestEnsureRunningUsesNetwork(t *testing.T) {
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, func(o *Options) {
		o.Network = fakeNetwork{name: "5050-build-x"}
		o.Images = func() map[string]string { return map[string]string{"dockerhub-mirror": "http://10.0.0.5:5001/"} }
		o.BuildRegistry = "10.9.9.9:5000"
	})
	if err := b.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.find(isCreate)[0].Args, " "), "network=5050-build-x") {
		t.Errorf("the network name from netw must be used")
	}
	cfg, _ := os.ReadFile(filepath.Join(b.opts.DataDir, "build", "buildkitd.toml"))
	for _, want := range []string{`[registry."10.9.9.9:5000"]`, `mirrors = ["10.0.0.5:5001"]`, `[registry."10.0.0.5:5001"]`} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("buildkitd.toml lacks %q:\n%s", want, cfg)
		}
	}
	b = testBuilder(t, &fakeRunner{}, nil, func(o *Options) { o.Network = fakeNetwork{err: errors.New("iptables failed")} })
	if err := b.EnsureRunning(context.Background(), ""); err == nil || err.Error() != "The build network could not be set up: iptables failed." {
		t.Errorf("err = %v", err)
	}
}

func TestOldBuildxUsesConfigFlag(t *testing.T) {
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, line func(string)) (bool, error) {
		if len(c.Args) >= 2 && c.Args[1] == "version" {
			line("github.com/docker/buildx v0.11.2 9872040")
			return true, nil
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, nil)
	if err := b.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(r.find(isCreate)[0].Args, " ")
	if !strings.Contains(args, " --config ") || strings.Contains(args, "--buildkitd-config") {
		t.Errorf("old buildx needs --config: %s", args)
	}
}

func TestPrune(t *testing.T) {
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, nil)
	if err := b.Prune(context.Background(), 1<<30); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.calls[0].Args, " "); got != "buildx prune --builder fifty5050 --force --max-used-space 1073741824" {
		t.Errorf("argv = %s", got)
	}
	r = &fakeRunner{hook: func(_ context.Context, c cmdSpec, line func(string)) (bool, error) {
		if argAfter(c.Args, "--force") == "--max-used-space" {
			line("unknown flag: --max-used-space")
			return true, &exitError{Cmd: "docker", Code: 125}
		}
		return false, nil
	}}
	b = testBuilder(t, r, nil, nil)
	if err := b.Prune(context.Background(), -5); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.calls[1].Args, " "); got != "buildx prune --builder fifty5050 --force --keep-storage 0" {
		t.Errorf("fallback argv = %s", got)
	}
	r = &fakeRunner{hook: func(context.Context, cmdSpec, func(string)) (bool, error) {
		return true, &exitError{Cmd: "docker", Code: 1, Last: "no builder"}
	}}
	b = testBuilder(t, r, nil, nil)
	if err := b.Prune(context.Background(), 1); err == nil || !strings.HasPrefix(err.Error(), "The build cache could not be pruned") {
		t.Errorf("err = %v", err)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := map[string]bool{"v0.13.0": true, "v0.12.9": false, "v1.0.0": true, "0.25.0-desktop.1": true, "": true}
	for v, want := range cases {
		if got := versionAtLeast(v, 0, 13); got != want {
			t.Errorf("versionAtLeast(%q) = %v", v, got)
		}
	}
}

func TestNewDefaults(t *testing.T) {
	b := New(Options{}).(*builder)
	if b.opts.DataDir != "/var/lib/5050" || b.opts.BuildNetworkName != "5050-build" || cap(b.slots) < 1 {
		t.Errorf("defaults = %+v", b.opts)
	}
	var _ agentapi.Builder = b
	_ = proto.BuildJob{}
}
