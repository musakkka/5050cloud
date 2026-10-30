// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultsAndRoundTrip(t *testing.T) {
	f := &File{PublicIP: "203.0.113.7"}
	f.Defaults()
	if f.BaseDomain != "203-0-113-7.sslip.io" || !regexp.MustCompile(`^console-[a-z0-9]{4}\.203-0-113-7\.sslip\.io$`).MatchString(f.ConsoleDomain) {
		t.Fatalf("domains = %q %q", f.BaseDomain, f.ConsoleDomain)
	}
	if f.ConsoleBase() != "https://"+f.ConsoleDomain || f.StatePath() != "/var/lib/5050/state.db" {
		t.Fatal("derived paths")
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "etc", "config.json")
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v %v", st.Mode(), err)
	}
	g, err := Load(path)
	if err != nil || g.ConsoleDomain != f.ConsoleDomain {
		t.Fatalf("load = %+v %v", g, err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing file")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte("{"), 0o600)
	if _, err := Load(bad); err == nil {
		t.Fatal("bad JSON accepted")
	}
	dev := &File{Listen: "127.0.0.1:5050", ConsoleURL: "http://localhost:5173/"}
	if dev.ConsoleBase() != "http://localhost:5173" {
		t.Fatal("ConsoleURL override")
	}
	none := &File{Listen: "127.0.0.1:5050"}
	if none.ConsoleBase() != "http://127.0.0.1:5050" {
		t.Fatal("fallback console base")
	}
}

func TestValidate(t *testing.T) {
	for _, f := range []File{
		{PublicIP: "::1"}, {PublicIP: "nope"}, {BaseDomain: "bad_domain.com"}, {ConsoleDomain: "-x.com"},
		{Listen: "nonsense"}, {PodCIDR: "10.0.0.0"},
	} {
		f.Defaults()
		if f.Listen == "" {
			f.Listen = DefaultListen
		}
		if err := f.Validate(); err == nil {
			t.Errorf("%+v accepted", f)
		}
	}
	for _, d := range []string{"example.com", "a-b.example.co.uk", "x.y."} {
		if !ValidDomain(d) {
			t.Errorf("%q rejected", d)
		}
	}
	for _, d := range []string{"", "localhost", "a..b", string(make([]byte, 300)), "a.-b.com", "ex ample.com"} {
		if ValidDomain(d) {
			t.Errorf("%q accepted", d)
		}
	}
}

func TestKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k", "master.key")
	b, created, err := EnsureKey(path)
	if err != nil || !created || b == nil {
		t.Fatal(err)
	}
	ct, _ := b.Seal([]byte("x"), "a")
	b2, created, err := EnsureKey(path)
	if err != nil || created {
		t.Fatal("second call must load the key")
	}
	if pt, err := b2.Open(ct, "a"); err != nil || string(pt) != "x" {
		t.Fatal("reloaded key differs")
	}
	short := filepath.Join(t.TempDir(), "short.key")
	_ = os.WriteFile(short, []byte("abc"), 0o600)
	if _, _, err := EnsureKey(short); err == nil {
		t.Fatal("a short key file must fail")
	}
}

func TestPrivateRefusesIPNames(t *testing.T) {
	f := &File{PublicIP: "203.0.113.7", Private: true}
	f.Defaults()
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "cannot be used on a private host") {
		t.Fatalf("the sslip.io default must be refused: %v", err)
	}
	f = &File{PublicIP: "203.0.113.7", Private: true, BaseDomain: "example.com"}
	f.Defaults()
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	for d, want := range map[string]bool{"a.sslip.io": true, "sslip.io": true, "a.NIP.io.": true, "notsslip.io": false, "sslip.io.example.com": false} {
		if IPNameDomain(d) != want {
			t.Errorf("IPNameDomain(%q) != %v", d, want)
		}
	}
}
