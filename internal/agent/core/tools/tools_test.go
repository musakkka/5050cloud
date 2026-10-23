// SPDX-License-Identifier: AGPL-3.0-only

package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		typ := byte(tar.TypeReg)
		if strings.HasSuffix(name, "/") {
			typ = tar.TypeDir
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: typ}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestToolDownloads(t *testing.T) {
	plain := []byte("#!/bin/sh\necho railpack\n")
	archive := tarGz(t, map[string]string{"./restic_0.18.0/": "", "./restic_0.18.0/restic": "restic-binary"})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/railpack":
			_, _ = w.Write(plain)
		case "/restic.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	m := New(dir, srv.Client())
	m.Set([]proto.Tool{
		{Name: "railpack", Version: "0.40.1", URL: srv.URL + "/railpack", SHA256: sum(plain)},
		{Name: "restic", Version: "0.18.0", URL: srv.URL + "/restic.tar.gz", SHA256: "sha256:" + sum(archive), Path: "restic_0.18.0/restic"},
		{Name: "bad", Version: "1", URL: srv.URL + "/railpack", SHA256: strings.Repeat("0", 64)},
		{Name: "missing", Version: "1", URL: srv.URL + "/nope", SHA256: strings.Repeat("0", 64)},
		{Name: "nosum", Version: "1", URL: srv.URL + "/railpack"},
		{Name: "evil", Version: "../../x", URL: srv.URL + "/railpack", SHA256: sum(plain)},
		{Name: "wrongpath", Version: "1", URL: srv.URL + "/restic.tar.gz", SHA256: sum(archive), Path: "nothere"},
		{Name: "dirpath", Version: "1", URL: srv.URL + "/restic.tar.gz", SHA256: sum(archive), Path: "restic_0.18.0"},
	})
	ctx := context.Background()

	var wg sync.WaitGroup
	paths := make([]string, 5)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := m.Path(ctx, "railpack")
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()
	got, _ := os.ReadFile(paths[0])
	if !bytes.Equal(got, plain) {
		t.Fatal("railpack content")
	}
	st, _ := os.Stat(paths[0])
	if st.Mode().Perm() != 0o755 {
		t.Fatal(st.Mode())
	}
	before := hits.Load()
	if _, err := m.Path(ctx, "railpack"); err != nil || hits.Load() != before {
		t.Fatal("a verified tool must not be downloaded again")
	}
	p, err := m.Path(ctx, "restic")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "restic-binary" {
		t.Fatal(string(b))
	}
	for name, want := range map[string]string{
		"bad":       "checksum",
		"missing":   "404",
		"nosum":     "no valid SHA-256",
		"evil":      "not allowed",
		"wrongpath": "has no file",
		"dirpath":   "not a regular file",
		"unknown":   "not configured",
	} {
		if _, err := m.Path(ctx, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", name, err, want)
		}
	}
	if _, ok := m.Known("restic"); !ok {
		t.Fatal("Known")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}
