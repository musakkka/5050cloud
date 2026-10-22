// SPDX-License-Identifier: AGPL-3.0-only

// Package upgrade downloads, verifies and switches 5050cloud releases with
// automatic rollback (docs/SPEC.md 9.16). The control plane switches through
// a detached helper process; agents switch themselves and revert when the
// new binary cannot reach the control plane. README.md explains the layout.
package upgrade

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/upgrade/minisign"
	"github.com/musakkka/5050cloud/pkg/version"
)

// Default locations on a host (docs/SPEC.md 4.2 and 6.4).
const (
	DefaultReleaseBase = "https://github.com/musakkka/5050cloud/releases/download"
	DefaultLatestURL   = "https://github.com/musakkka/5050cloud/releases/latest/download/latest.txt"
	DefaultInstallDir  = "/opt/5050"
	DefaultDataDir     = "/var/lib/5050"
	DefaultControlURL  = "http://127.0.0.1:5050"
	// DefaultReleaseFile is written by install/install.sh when it ran with
	// --release-base or --release-pubkey, so later upgrades use the same
	// release source and keys.
	DefaultReleaseFile = "/etc/5050/release.json"
)

//go:embed release-keys.txt
var embeddedKeys string

// Options configures every function of this package. Zero fields take the
// defaults above; Defaults fills them.
type Options struct {
	// ReleaseBase is where releases are fetched from, default
	// "https://github.com/musakkka/5050cloud/releases/download". A release
	// lives at <ReleaseBase>/<tag>/<file>.
	ReleaseBase string
	// LatestURL returns the latest stable tag as plain text.
	LatestURL  string
	InstallDir string // /opt/5050
	DataDir    string // /var/lib/5050
	Arch       string // runtime.GOARCH

	// PublicKeys are the minisign keys (base64 lines) a release may be
	// signed with. Empty means the embedded current and next keys plus the
	// keys in ReleaseFile.
	PublicKeys []string
	// ReleaseFile is read by Defaults for ReleaseBase, LatestURL and extra
	// public keys; default /etc/5050/release.json. Set it to "-" to skip.
	ReleaseFile string
	// Version is the version of the running binary, default version.Version.
	Version string
	// ControlURL is the local control plane, default http://127.0.0.1:5050.
	ControlURL string
	// HTTP is the client for downloads and /readyz; default has a 10 minute
	// overall timeout for downloads.
	HTTP *http.Client
	// Run executes host commands (systemd-run, systemctl, 5050d); default
	// runs them with os/exec.
	Run Runner
	// ReadyTimeout bounds the wait for the new control plane (default 90 s);
	// AgentConnectTimeout the wait for a new agent's first hello (90 s).
	ReadyTimeout        time.Duration
	AgentConnectTimeout time.Duration
	// PollInterval is how often readiness and connection are checked (1 s).
	PollInterval time.Duration
	// Snapshot writes a state snapshot to path before a control plane
	// upgrade. Default: "<this executable> backup-state --out <path>".
	Snapshot func(path string) error
	// AgentsConnected returns the agents connected right now; the new control
	// plane must reach at least this many. Default: read local /readyz.
	AgentsConnected func() int
	// Now is the clock (tests).
	Now func() time.Time
}

// releaseFile is /etc/5050/release.json.
type releaseFile struct {
	ReleaseBase string   `json:"releaseBase,omitempty"`
	LatestURL   string   `json:"latestUrl,omitempty"`
	PublicKeys  []string `json:"publicKeys,omitempty"`
}

// Defaults returns o with every empty field filled.
func (o Options) Defaults() Options {
	if o.ReleaseFile == "" {
		o.ReleaseFile = DefaultReleaseFile
	}
	var rf releaseFile
	if o.ReleaseFile != "-" {
		if b, err := os.ReadFile(o.ReleaseFile); err == nil {
			_ = json.Unmarshal(b, &rf)
		}
	}
	if o.ReleaseBase == "" {
		o.ReleaseBase = rf.ReleaseBase
	}
	if o.ReleaseBase == "" {
		o.ReleaseBase = DefaultReleaseBase
	}
	o.ReleaseBase = strings.TrimRight(o.ReleaseBase, "/")
	if o.LatestURL == "" {
		o.LatestURL = rf.LatestURL
	}
	if o.LatestURL == "" {
		if o.ReleaseBase == DefaultReleaseBase {
			o.LatestURL = DefaultLatestURL
		} else {
			o.LatestURL = o.ReleaseBase + "/latest.txt"
		}
	}
	if len(o.PublicKeys) == 0 {
		o.PublicKeys = append(EmbeddedKeys(), rf.PublicKeys...)
	}
	if o.InstallDir == "" {
		o.InstallDir = DefaultInstallDir
	}
	if o.DataDir == "" {
		o.DataDir = DefaultDataDir
	}
	if o.Arch == "" {
		o.Arch = runtime.GOARCH
	}
	if o.Version == "" {
		o.Version = version.Version
	}
	if o.ControlURL == "" {
		o.ControlURL = DefaultControlURL
	}
	o.ControlURL = strings.TrimRight(o.ControlURL, "/")
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 10 * time.Minute}
	}
	if o.Run == nil {
		o.Run = ExecRunner{}
	}
	if o.ReadyTimeout == 0 {
		o.ReadyTimeout = 90 * time.Second
	}
	if o.AgentConnectTimeout == 0 {
		o.AgentConnectTimeout = 90 * time.Second
	}
	if o.PollInterval == 0 {
		o.PollInterval = time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// EmbeddedKeys returns the release keys built into this binary.
func EmbeddedKeys() []string {
	var out []string
	for _, l := range strings.Split(embeddedKeys, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// keys parses o.PublicKeys.
func (o Options) keys() ([]minisign.PublicKey, error) {
	var out []minisign.PublicKey
	for _, s := range o.PublicKeys {
		k, err := minisign.ParsePublicKey(s)
		if err != nil {
			return nil, fmt.Errorf("the release public key %q cannot be used: %w", s, err)
		}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, errors.New("this build has no release signing key, so it cannot verify a release. Pass the key with --pubkey (or --release-pubkey to the installer)")
	}
	return out, nil
}

// Paths on the host derived from Options.
func (o Options) releasesDir() string        { return filepath.Join(o.InstallDir, "releases") }
func (o Options) releaseDir(v string) string { return filepath.Join(o.releasesDir(), v) }

// CurrentLink is the control plane and CLI link, /opt/5050/current.
func (o Options) CurrentLink() string { return filepath.Join(o.InstallDir, "current") }

// AgentLink is the agent's own link, /opt/5050/agent, so agents can switch
// after the control plane and revert on their own (docs/SPEC.md 9.16).
func (o Options) AgentLink() string     { return filepath.Join(o.InstallDir, "agent") }
func (o Options) statusPath() string    { return filepath.Join(o.DataDir, "upgrade.json") }
func (o Options) agentStateDir() string { return filepath.Join(o.DataDir, "agent") }
