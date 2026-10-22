// SPDX-License-Identifier: AGPL-3.0-only

package statecache

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestRoundTripAndSecrecy(t *testing.T) {
	dir := t.TempDir()
	key, err := LoadOrCreateKey(filepath.Join(dir, "etc", "host.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, "etc", "host.key"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("host key mode %v, want 0600", st.Mode().Perm())
	}
	again, err := LoadOrCreateKey(filepath.Join(dir, "etc", "host.key"))
	if err != nil || !bytes.Equal(key, again) {
		t.Fatal("the host key must be stable once created")
	}
	c, err := New(Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Load(); !errors.Is(err, ErrNoCache) {
		t.Fatalf("want ErrNoCache, got %v", err)
	}
	ds := proto.DesiredState{Generation: 7, Epoch: "ep1", Host: proto.HostConfig{RegistryPull: proto.Registry{Password: "pull-secret-value"}},
		Replicas: []proto.ReplicaSpec{{ID: "rep_1", Env: map[string]string{"DATABASE_URL": "postgres://u:supersecret@db/x"}}}}
	if err := c.Save(Entry{State: ds, AppliedAt: 42}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(Path(dir))
	for _, s := range []string{"supersecret", "pull-secret-value", "rep_1", "DATABASE_URL"} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("cache file contains %q in plain text", s)
		}
	}
	st, _ = os.Stat(Path(dir))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode %v, want 0600", st.Mode().Perm())
	}
	e, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if e.State.Generation != 7 || e.State.Replicas[0].Env["DATABASE_URL"] != "postgres://u:supersecret@db/x" || e.AppliedAt != 42 {
		t.Fatalf("round trip lost data: %+v", e)
	}

	// Another key cannot open it.
	other, _ := New(Path(dir), []byte("0123456789abcdef0123456789abcdef"))
	if _, err := other.Load(); err == nil || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Fatalf("want a decrypt error, got %v", err)
	}
	// Tampering is detected.
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(Path(dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Load(); err == nil {
		t.Fatal("tampered cache must not load")
	}
	if err := os.WriteFile(Path(dir), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Load(); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("want damaged, got %v", err)
	}
	if err := c.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(); err != nil {
		t.Fatal("removing a missing cache is fine")
	}
}

func TestKeyFormats(t *testing.T) {
	if _, err := New("x", []byte("short")); err == nil {
		t.Fatal("short key must fail")
	}
	// Whitespace around the key does not change it.
	a, _ := New(filepath.Join(t.TempDir(), "c"), []byte("0123456789abcdef0123"))
	p := filepath.Join(t.TempDir(), "c")
	b, _ := New(p, []byte("  0123456789abcdef0123\n"))
	a.path = p
	if err := a.Save(Entry{AppliedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Load(); err != nil {
		t.Fatalf("trimmed key should open the cache: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "k"), []byte("tiny"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(filepath.Join(dir, "k")); err == nil {
		t.Fatal("a short existing key must be refused")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "f")
	if err := WriteFileAtomic(p, []byte("one"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("two"), 0o640); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two" {
		t.Fatal(string(b))
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Fatalf("temporary files left behind: %v", ents)
	}
	if err := WriteFileAtomic(filepath.Join(p, "under-a-file"), nil, 0o600); err == nil {
		t.Fatal("expected an error")
	}
}
