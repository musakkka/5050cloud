// SPDX-License-Identifier: AGPL-3.0-only

// Package config reads and writes /etc/5050/config.json, the control
// plane's install configuration (docs/SPEC.md 4.2 step 7 and 6.4), and
// creates the key files next to it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
)

// Default paths on a host.
const (
	DefaultPath    = "/etc/5050/config.json"
	DefaultDataDir = "/var/lib/5050"
	DefaultListen  = "127.0.0.1:5050"
)

// File is config.json. Settings people change later (name, domains,
// trusted proxies) are copied into state.db at first start and edited
// there; this file keeps what the process needs before the database opens.
type File struct {
	DataDir       string `json:"dataDir"`
	Listen        string `json:"listen"`
	InstallName   string `json:"installName,omitempty"`
	PublicIP      string `json:"publicIp"`
	BaseDomain    string `json:"baseDomain"`
	ConsoleDomain string `json:"consoleDomain"`
	// Private: no inbound ports, public traffic comes through a Cloudflare tunnel.
	Private   bool   `json:"private,omitempty"`
	ACMEEmail string `json:"acmeEmail,omitempty"`
	// ACMECA is "staging", "production" or a directory URL.
	ACMECA        string `json:"acmeCa,omitempty"`
	MasterKeyPath string `json:"masterKeyPath"`
	HostKeyPath   string `json:"hostKeyPath"`
	PodCIDR       string `json:"podCidr,omitempty"`
	MeshCIDR      string `json:"meshCidr,omitempty"`
	// AllowPrivateDestinations lets webhooks reach private addresses (9.15 SSRF guard).
	AllowPrivateDestinations bool `json:"allowPrivateDestinations,omitempty"`
	// Release and update check overrides (e2e harness).
	ReleaseBase   string `json:"releaseBase,omitempty"`
	LatestURL     string `json:"latestUrl,omitempty"`
	NoUpdateCheck bool   `json:"noUpdateCheck,omitempty"`
	// API overrides for the Railway importer's and GitHub's endpoints (fixture servers in e2e).
	RailwayAPI string `json:"railwayApi,omitempty"`
	GitHubAPI  string `json:"githubApi,omitempty"`
	// ConsoleURL overrides the derived https://<console domain> (development).
	ConsoleURL string `json:"consoleUrl,omitempty"`
	// Insecure serves cookies without the Secure flag (plain HTTP, development only).
	Insecure bool `json:"insecure,omitempty"`
}

// Defaults fills empty fields.
func (f *File) Defaults() {
	if f.DataDir == "" {
		f.DataDir = DefaultDataDir
	}
	if f.Listen == "" {
		f.Listen = DefaultListen
	}
	if f.InstallName == "" {
		f.InstallName = "5050cloud"
	}
	if f.BaseDomain == "" && f.PublicIP != "" {
		f.BaseDomain = SslipDomain(f.PublicIP)
	}
	if f.ConsoleDomain == "" && f.BaseDomain != "" {
		f.ConsoleDomain = "console-" + ids.Random(4, ids.LowerAlnum) + "." + f.BaseDomain
	}
	if f.MasterKeyPath == "" {
		f.MasterKeyPath = "/etc/5050/master.key"
	}
	if f.HostKeyPath == "" {
		f.HostKeyPath = "/etc/5050/host.key"
	}
	if f.PodCIDR == "" {
		f.PodCIDR = "10.128.0.0/9"
	}
	if f.MeshCIDR == "" {
		f.MeshCIDR = "100.96.0.0/16"
	}
	if f.ACMECA == "" {
		f.ACMECA = "production"
	}
}

// Validate reports the first problem with the file as a sentence.
func (f *File) Validate() error {
	if f.PublicIP != "" {
		ip := net.ParseIP(f.PublicIP)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("the public IP %q is not an IPv4 address", f.PublicIP)
		}
	}
	for _, d := range []struct{ name, v string }{{"base domain", f.BaseDomain}, {"console domain", f.ConsoleDomain}} {
		if d.v != "" && !ValidDomain(d.v) {
			return fmt.Errorf("the %s %q is not a valid domain name", d.name, d.v)
		}
	}
	if f.Private {
		for _, d := range []string{f.BaseDomain, f.ConsoleDomain} {
			if IPNameDomain(d) {
				return errors.New(PrivateNameMessage(d))
			}
		}
	}
	if _, _, err := net.SplitHostPort(f.Listen); err != nil {
		return fmt.Errorf("the listen address %q must be host:port", f.Listen)
	}
	for _, c := range []string{f.PodCIDR, f.MeshCIDR} {
		if _, _, err := net.ParseCIDR(c); err != nil {
			return fmt.Errorf("%q is not a CIDR range", c)
		}
	}
	return nil
}

// ConsoleBase returns the console URL without a trailing slash.
func (f *File) ConsoleBase() string {
	if f.ConsoleURL != "" {
		return strings.TrimRight(f.ConsoleURL, "/")
	}
	if f.ConsoleDomain != "" {
		return "https://" + f.ConsoleDomain
	}
	return "http://" + f.Listen
}

// StatePath is state.db.
func (f *File) StatePath() string { return filepath.Join(f.DataDir, "state.db") }

// SslipDomain returns "<dashed ip>.sslip.io".
func SslipDomain(ip string) string { return strings.ReplaceAll(ip, ".", "-") + ".sslip.io" }

// IPNameDomain reports whether d is a name that resolves to an IP address by
// its spelling (sslip.io, nip.io). Such a name points at the server itself, so
// it cannot go through a tunnel.
func IPNameDomain(d string) bool {
	d = strings.ToLower(strings.TrimSuffix(d, "."))
	for _, s := range []string{"sslip.io", "nip.io"} {
		if d == s || strings.HasSuffix(d, "."+s) {
			return true
		}
	}
	return false
}

// PrivateNameMessage is the refusal for an IP name on a private host.
func PrivateNameMessage(d string) string {
	return d + " cannot be used on a private host, because sslip.io and nip.io names point straight at the server and nothing listens there. Use a domain you control in Cloudflare."
}

// ValidDomain checks a host name.
func ValidDomain(d string) bool {
	if len(d) > 253 || d == "" {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(d, "."), ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}

// ErrNotFound is returned by Load when the file does not exist.
var ErrNotFound = errors.New("config file not found")

// Load reads path and fills defaults.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	f.Defaults()
	return &f, f.Validate()
}

// Save writes path atomically with mode 0600.
func Save(path string, f *File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return WriteSecret(path, append(b, '\n'))
}

// WriteSecret writes data to path atomically with mode 0600, creating the directory (0700).
func WriteSecret(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// EnsureKey creates a 32-byte random key file (0600) when missing and returns its box.
func EnsureKey(path string) (*secretbox.Box, bool, error) {
	if b, err := secretbox.LoadFile(path); err == nil {
		return b, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("cannot read the key file %s: %w", path, err)
	}
	k, err := secretbox.GenerateKey()
	if err != nil {
		return nil, false, err
	}
	if err := WriteSecret(path, k); err != nil {
		return nil, false, fmt.Errorf("cannot write the key file %s: %w", path, err)
	}
	b, err := secretbox.New(k)
	return b, true, err
}
