// SPDX-License-Identifier: AGPL-3.0-only

// Package orch implements cpapi.Orchestrator (WP2, docs/SPEC.md 9). The work
// lives in the engine subpackage; this file is the constructor 5050d calls.
package orch

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/engine"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/templates"
)

type Options struct {
	DB      *sql.DB
	Config  cpapi.Config
	DataDir string // /var/lib/5050
	// PublicURL is the console URL agents and build jobs use, e.g. https://console.203-0-113-7.sslip.io
	PublicURL string
	// LocalURL is how the agent on the control host reaches 5050d, e.g. http://127.0.0.1:5050
	LocalURL string
	// PodCIDR and MeshCIDR default to 10.128.0.0/9 and 100.96.0.0/16.
	PodCIDR  string
	MeshCIDR string
	// AgentMTLS requires client certificates on agent connections. Not
	// enforced in v0.1 (see README, Later work): agents authenticate with
	// their rotating host token bound to the host keys.
	AgentMTLS bool

	// Box encrypts secret columns. When nil, the key is read from
	// MasterKeyPath (default /etc/5050/master.key).
	Box           *secretbox.Box
	MasterKeyPath string
	// Templates defaults to templates.FS.
	Templates fs.FS
	// Upgrade configures release downloads (9.16); zero values use the
	// upgrade package defaults with InstallDir /opt/5050 and DataDir above.
	Upgrade upgrade.Options
	// ACMECA is "" (production), "staging" or an ACME directory URL.
	ACMECA string
	Log    *slog.Logger
}

// New builds the orchestrator. Call Start on the result to run it.
func New(o Options) (cpapi.Orchestrator, error) {
	if o.DataDir == "" {
		o.DataDir = "/var/lib/5050"
	}
	box := o.Box
	if box == nil {
		p := o.MasterKeyPath
		if p == "" {
			p = "/etc/5050/master.key"
		}
		b, err := secretbox.LoadFile(p)
		if err != nil {
			return nil, fmt.Errorf("The orchestrator could not read the master key at %s: %w", p, err)
		}
		box = b
	}
	if o.DB == nil || o.Config == nil {
		return nil, errors.New("The orchestrator needs the state database and the core config.")
	}
	tfs := o.Templates
	if tfs == nil {
		tfs = templates.FS
	}
	up := o.Upgrade
	if up.DataDir == "" {
		up.DataDir = o.DataDir
	}
	if up.InstallDir == "" {
		up.InstallDir = "/opt/5050"
	}
	up = up.Defaults()
	e, err := engine.New(engine.Options{
		DB:        o.DB,
		Config:    o.Config,
		DataDir:   o.DataDir,
		PublicURL: o.PublicURL,
		LocalURL:  o.LocalURL,
		PodCIDR:   o.PodCIDR,
		MeshCIDR:  o.MeshCIDR,
		Box:       box,
		Templates: tfs,
		Upgrade:   up,
		ACMECA:    o.ACMECA,
		Log:       o.Log,
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}
