// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core"
	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ballastSize is the file deleted to free space when the disk fills (6.4, 6.6).
const ballastSize = 1 << 30

func cmdInit(ctx context.Context, args []string, stdout io.Writer) error {
	fs := newFlags("init")
	cfgPath := configFlag(fs)
	f := &config.File{}
	fs.StringVar(&f.DataDir, "data-dir", env("DATA_DIR", config.DefaultDataDir), "data directory")
	fs.StringVar(&f.Listen, "listen", env("LISTEN", config.DefaultListen), "listen address")
	fs.StringVar(&f.PublicIP, "public-ip", env("PUBLIC_IP", ""), "public IPv4 address of this host")
	fs.StringVar(&f.BaseDomain, "base-domain", env("DOMAIN", ""), "base domain for generated service domains (default <dashed ip>.sslip.io)")
	fs.StringVar(&f.ConsoleDomain, "console-domain", env("CONSOLE_DOMAIN", ""), "console domain (default console-<4 random>.<base domain>)")
	fs.StringVar(&f.ACMEEmail, "acme-email", env("ACME_EMAIL", ""), "email for certificate notices")
	fs.StringVar(&f.ACMECA, "acme-ca", env("ACME_CA", "production"), "ACME directory: staging, production or a URL")
	fs.BoolVar(&f.Private, "private", env("PRIVATE", "") != "", "no inbound ports: public traffic comes through a Cloudflare tunnel, Caddy serves plain HTTP on loopback")
	fs.StringVar(&f.InstallName, "name", env("NAME", ""), "install name")
	fs.StringVar(&f.ReleaseBase, "release-base", env("RELEASE_BASE", ""), "release download base URL")
	keyPath := fs.String("master-key", env("MASTER_KEY", ""), "master key path (default next to config.json)")
	hostKey := fs.String("host-key", env("HOST_KEY", ""), "host key path (default next to config.json)")
	recFile := fs.String("recovery-key-file", env("RECOVERY_KEY_FILE", ""), "write the recovery key to this file (0600)")
	setupFile := fs.String("setup-url-file", env("SETUP_URL_FILE", ""), "write the setup link to this file (0600)")
	joinFile := fs.String("join-token-file", env("JOIN_TOKEN_FILE", ""), "write a join token for this host's agent to this file (0600)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*cfgPath); err == nil {
		return fmt.Errorf("%s already exists; this install is initialised", *cfgPath)
	}
	if f.PublicIP == "" {
		return errors.New("pass --public-ip (the public IPv4 address of this host)")
	}
	dir := filepath.Dir(*cfgPath)
	f.MasterKeyPath, f.HostKeyPath = *keyPath, *hostKey
	if f.MasterKeyPath == "" {
		f.MasterKeyPath = filepath.Join(dir, "master.key")
	}
	if f.HostKeyPath == "" {
		f.HostKeyPath = filepath.Join(dir, "host.key")
	}
	switch f.ACMECA {
	case "https://acme-v02.api.letsencrypt.org/directory":
		f.ACMECA = "production"
	case "https://acme-staging-v02.api.letsencrypt.org/directory":
		f.ACMECA = "staging"
	}
	f.Defaults()
	if err := f.Validate(); err != nil {
		return err
	}
	if _, _, err := config.EnsureKey(f.MasterKeyPath); err != nil {
		return err
	}
	if _, _, err := config.EnsureKey(f.HostKeyPath); err != nil {
		return err
	}
	if err := os.MkdirAll(f.DataDir, 0o700); err != nil {
		return err
	}
	if err := writeBallast(filepath.Join(f.DataDir, "ballast")); err != nil {
		_, _ = fmt.Fprintf(stdout, "Warning: could not create the 1 GB ballast file: %s\n", err)
	}
	if err := config.Save(*cfgPath, f); err != nil {
		return err
	}
	p, err := openWith(ctx, *cfgPath, f, nil, nil)
	if err != nil {
		return err
	}
	defer p.Close()
	tok, err := p.c.CreateSetupToken(ctx)
	if err != nil {
		return err
	}
	setupURL := p.c.ConsoleURL(ctx) + "/setup?token=" + tok
	if *setupFile != "" {
		if err := config.WriteSecret(*setupFile, []byte(setupURL+"\n")); err != nil {
			return err
		}
	}
	key, err := os.ReadFile(f.MasterKeyPath)
	if err != nil {
		return err
	}
	rk := core.RecoveryKeyFor(key)
	if *recFile != "" {
		if err := config.WriteSecret(*recFile, []byte(rk+"\n")); err != nil {
			return err
		}
	}
	if *joinFile != "" {
		o, err := newOrch(p)
		if err != nil {
			return err
		}
		p.c.SetOrchestrator(o)
		jt, err := o.CreateJoinToken(ctx, api.CreateJoinTokenRequest{Roles: []string{"control", "build", "ingress", "app", "data"}, Name: hostName()})
		if err != nil {
			return fmt.Errorf("cannot create the local join token: %w", err)
		}
		if err := config.WriteSecret(*joinFile, []byte(jt.Token+"\n")); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(stdout, "Initialised %s.\nConsole: %s\n", *cfgPath, p.c.ConsoleURL(ctx))
	if *setupFile == "" {
		_, _ = fmt.Fprintf(stdout, "Create the owner account (valid 24 hours, works once): %s\n", setupURL)
	}
	if *recFile == "" {
		_, _ = fmt.Fprintf(stdout, "Recovery key (store it somewhere safe): %s\n", rk)
	}
	return nil
}

func hostName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "control"
	}
	return strings.SplitN(h, ".", 2)[0]
}

func writeBallast(path string) error {
	if st, err := os.Stat(path); err == nil && st.Size() >= ballastSize {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := allocate(f, ballastSize); err == nil {
		return nil
	}
	buf := make([]byte, 1<<20)
	for i := 0; i < ballastSize/len(buf); i++ {
		if _, err := f.Write(buf); err != nil {
			return err
		}
	}
	return f.Sync()
}
