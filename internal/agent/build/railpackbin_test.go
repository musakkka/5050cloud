// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRailpackBinaryDownloadAndVerify(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"LICENSE": "x", "railpack": "#!/bin/sh\necho railpack\n"})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	sum := sha256.Sum256(archive)
	pin := RailpackPin{Version: "9.9.9", Archives: map[string]RailpackArchive{
		runtime.GOARCH: {URL: srv.URL + "/railpack.tar.gz", SHA256: hex.EncodeToString(sum[:])},
	}}
	b := testBuilder(t, &fakeRunner{}, nil, nil)
	log, lines := collect()
	p, err := b.railpackBinary(context.Background(), pin, log)
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(b.opts.DataDir, "tools", "railpack", "9.9.9", "railpack") {
		t.Errorf("path = %s", p)
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary must be executable: %v %v", st, err)
	}
	if !strings.Contains(strings.Join(lines(), "\n"), "Downloading Railpack 9.9.9") {
		t.Errorf("log = %v", lines())
	}
	// Cached: no second download.
	if _, err := b.railpackBinary(context.Background(), pin, log); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Errorf("downloads = %d, want 1", hits.Load())
	}
	// A wrong checksum is refused.
	bad := pin
	bad.Version = "9.9.8"
	bad.Archives = map[string]RailpackArchive{runtime.GOARCH: {URL: srv.URL, SHA256: strings.Repeat("0", 64)}}
	if _, err := b.railpackBinary(context.Background(), bad, log); err == nil || !strings.Contains(err.Error(), "did not match its pinned checksum") {
		t.Errorf("err = %v", err)
	}
	// No archive for this architecture.
	none := RailpackPin{Version: "9.9.7", Archives: map[string]RailpackArchive{}}
	if _, err := b.railpackBinary(context.Background(), none, log); err == nil || !strings.Contains(err.Error(), "has no release for") {
		t.Errorf("err = %v", err)
	}
}

func TestRailpackBinaryFromTools(t *testing.T) {
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, func(o *Options) { o.Tools = fakeTools{path: "/opt/tools/railpack"} })
	p, err := b.railpackBinary(context.Background(), CurrentRailpack(), func(string) {})
	if err != nil || p != "/opt/tools/railpack" {
		t.Errorf("p = %s err = %v", p, err)
	}
}

func TestRailpackBinaryToolsWrongVersion(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"railpack": "bin"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) }))
	defer srv.Close()
	sum := sha256.Sum256(archive)
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, line func(string)) (bool, error) {
		if len(c.Args) == 1 && c.Args[0] == "--version" {
			line("railpack version 0.30.0")
			return true, nil
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, func(o *Options) { o.Tools = fakeTools{path: "/opt/tools/railpack"} })
	pin := CurrentRailpack()
	pin.Archives = map[string]RailpackArchive{runtime.GOARCH: {URL: srv.URL, SHA256: hex.EncodeToString(sum[:])}}
	p, err := b.railpackBinary(context.Background(), pin, func(string) {})
	if err != nil || p == "/opt/tools/railpack" {
		t.Errorf("a Tools binary of another version must not be used: %s %v", p, err)
	}
}

func TestExtractRailpackErrors(t *testing.T) {
	if _, err := extractRailpack([]byte("nope")); err == nil {
		t.Errorf("not gzip")
	}
	if _, err := extractRailpack(makeTarGz(t, map[string]string{"README.md": "x"})); err == nil {
		t.Errorf("no binary")
	}
}

func TestPinsAreComplete(t *testing.T) {
	for _, p := range railpackPins {
		for _, arch := range []string{"amd64", "arm64"} {
			a, ok := p.Archives[arch]
			if !ok || len(a.SHA256) != 64 || !strings.Contains(a.URL, "/v"+p.Version+"/") {
				t.Errorf("pin %s %s: %+v", p.Version, arch, a)
			}
		}
		if !strings.HasPrefix(p.FrontendDigest, "sha256:") || len(p.FrontendDigest) != 71 {
			t.Errorf("pin %s frontend digest %q", p.Version, p.FrontendDigest)
		}
	}
	if !strings.Contains(BuildKitImage, "@sha256:") || strings.Contains(BuildKitImage, "latest") {
		t.Errorf("BuildKit must be pinned by digest: %s", BuildKitImage)
	}
	if _, ok := railpackPin("v0.40.1"); !ok {
		t.Errorf("a leading v is accepted")
	}
}
