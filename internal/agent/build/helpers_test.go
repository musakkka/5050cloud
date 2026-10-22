// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeRunner records every command and answers like docker, railpack and
// git would, without running anything.
type fakeRunner struct {
	mu    sync.Mutex
	calls []cmdSpec
	// hook, when set, runs first; returning handled=true skips the default.
	hook func(ctx context.Context, c cmdSpec, line func(string)) (handled bool, err error)
}

func (f *fakeRunner) Run(ctx context.Context, c cmdSpec, line func(string)) error {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if line == nil {
		line = func(string) {}
	}
	if f.hook != nil {
		if handled, err := f.hook(ctx, c, line); handled {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	args := c.Args
	switch {
	case len(args) >= 2 && args[0] == "buildx" && args[1] == "version":
		line("github.com/docker/buildx v0.37.2 0123456789abcdef")
	case len(args) >= 2 && args[0] == "buildx" && args[1] == "build":
		line("#1 [internal] load build definition")
		line("#9 pushing layers done")
		meta := argAfter(args, "--metadata-file")
		data, _ := json.Marshal(map[string]string{"containerimage.digest": "sha256:" + strings.Repeat("ab", 32)})
		if err := os.WriteFile(meta, data, 0o600); err != nil {
			return err
		}
	case len(args) >= 1 && args[0] == "prepare":
		line("Railpack 0.40.1")
		line("Detected Node")
		plan := map[string]any{
			"secrets": envNames(c.Env),
			"deploy":  map[string]any{"startCommand": "npm run start"},
		}
		data, _ := json.Marshal(plan)
		if err := os.WriteFile(argAfter(args, "--plan-out"), data, 0o600); err != nil {
			return err
		}
		info, _ := json.Marshal(map[string]any{"success": true, "detectedProviders": []string{"node"}})
		if err := os.WriteFile(argAfter(args, "--info-out"), info, 0o600); err != nil {
			return err
		}
	case len(args) >= 1 && args[0] == "--version":
		line("railpack version 0.40.1")
	}
	return nil
}

func (f *fakeRunner) find(pred func(cmdSpec) bool) []cmdSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []cmdSpec
	for _, c := range f.calls {
		if pred(c) {
			out = append(out, c)
		}
	}
	return out
}

func isBuildxBuild(c cmdSpec) bool {
	return len(c.Args) >= 2 && c.Args[0] == "buildx" && c.Args[1] == "build"
}

func isPrepare(c cmdSpec) bool { return len(c.Args) >= 1 && c.Args[0] == "prepare" }

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// envNames returns the names of an environment list that are not ours.
func envNames(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "PATH", "HOME", "TMPDIR", "NO_COLOR":
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func envValue(env []string, name string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v, true
		}
	}
	return "", false
}

// fakeEngine is a Docker Engine with one build network.
type fakeEngine struct {
	mu       sync.Mutex
	gateway  string
	noNet    bool
	cpus     int
	mem      int64
	image    string
	running  bool
	found    bool
	inspects int
}

func (e *fakeEngine) NetworkGateway(context.Context, string) (string, error) {
	if e.noNet {
		return "", errNoNetwork
	}
	return e.gateway, nil
}

func (e *fakeEngine) HostResources(context.Context) (int, int64, error) {
	return e.cpus, e.mem, nil
}

func (e *fakeEngine) Container(context.Context, string) (string, bool, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inspects++
	return e.image, e.running, e.found, nil
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{gateway: "172.30.0.1", cpus: 4, mem: 8 << 30}
}

// testBuilder returns a builder wired to fakes in a temp data dir.
func testBuilder(t *testing.T, r *fakeRunner, e *fakeEngine, mod func(*Options)) *builder {
	t.Helper()
	if r == nil {
		r = &fakeRunner{}
	}
	if e == nil {
		e = newFakeEngine()
	}
	o := Options{DataDir: t.TempDir(), DockerCLI: "/usr/bin/docker", Git: "/usr/bin/git", MaxParallel: 2}
	if mod != nil {
		mod(&o)
	}
	b := newBuilder(o, r, e)
	return b
}

// makeTarGz packs files (name -> content) into a .tar.gz.
func makeTarGz(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		body := files[n]
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeTree writes files under dir.
func writeTree(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	for n, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// collect returns a logf and the lines it saw.
func collect() (func(string), func() []string) {
	var mu sync.Mutex
	var lines []string
	return func(l string) {
			mu.Lock()
			lines = append(lines, l)
			mu.Unlock()
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), lines...)
		}
}
