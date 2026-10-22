// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core"
	coreapi "github.com/musakkka/5050cloud/internal/cp/core/api"
	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/internal/cp/core/fakeorch"
	"github.com/musakkka/5050cloud/internal/cp/core/github"
	"github.com/musakkka/5050cloud/internal/cp/core/mail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
	"github.com/musakkka/5050cloud/templates"
	"github.com/musakkka/5050cloud/web"
)

// plane is an opened control plane: config, key, store and core.
type plane struct {
	path string
	file *config.File
	box  *secretbox.Box
	st   *store.Store
	c    *core.Core
	log  *slog.Logger
}

func (p *plane) Close() { _ = p.st.Close() }

// openPlane loads config.json and opens state.db and the core.
func openPlane(ctx context.Context, path string, mailer func(api.SMTPSettings) mail.Sender, log *slog.Logger) (*plane, error) {
	f, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w (run `5050d init` first)", path, err)
	}
	return openWith(ctx, path, f, mailer, log)
}

func openWith(ctx context.Context, path string, f *config.File, mailer func(api.SMTPSettings) mail.Sender, log *slog.Logger) (*plane, error) {
	if log == nil {
		log = logger()
	}
	f.Defaults()
	if err := f.Validate(); err != nil {
		return nil, err
	}
	box, err := secretbox.LoadFile(f.MasterKeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the master key %s: %w", f.MasterKeyPath, err)
	}
	p := &plane{path: path, file: f, box: box, log: log}
	st, err := store.Open(ctx, f.StatePath(), store.Options{OnReadOnly: func(on bool) {
		if on && p.c != nil {
			go func() {
				_ = p.c.Notify(context.Background(), cpapi.Notice{Event: api.AlertReadOnly, Text: "The control plane disk is nearly full, so it switched to read-only mode."})
			}()
		}
	}})
	if err != nil {
		return nil, err
	}
	p.st = st
	p.c = core.New(core.Options{Store: st, Box: box, File: f, Path: path, MasterKeyPath: f.MasterKeyPath,
		Templates: templates.FS, Mailer: mailer, Git: github.Git{}, Log: log})
	if err := p.c.EnsureInstall(ctx); err != nil {
		_ = st.Close()
		return nil, err
	}
	return p, nil
}

func newOrch(p *plane) (cpapi.Orchestrator, error) {
	return orch.New(orch.Options{
		DB: p.st.Shared, Config: p.c, DataDir: p.file.DataDir,
		PublicURL: p.c.ConsoleURL(context.Background()), LocalURL: "http://" + p.file.Listen,
		PodCIDR: p.file.PodCIDR, MeshCIDR: p.file.MeshCIDR, Box: p.box, MasterKeyPath: p.file.MasterKeyPath,
		Templates: templates.FS, ACMECA: p.file.ACMECA, Log: p.log,
		Upgrade: upgrade.Options{ReleaseBase: p.file.ReleaseBase, LatestURL: p.file.LatestURL, DataDir: p.file.DataDir},
	})
}

// logMailer writes messages to the log instead of sending them (development).
type logMailer struct {
	log *slog.Logger
	mail.Fake
}

func (m *logMailer) Send(ctx context.Context, msg mail.Message) error {
	m.log.Info("email (development server, not sent)", "to", strings.Join(msg.To, ","), "subject", msg.Subject, "text", msg.Text)
	return m.Fake.Send(ctx, msg)
}

func cmdServe(ctx context.Context, args []string, stdout io.Writer) error {
	fs := newFlags("serve")
	cfgPath := configFlag(fs)
	dev := fs.Bool("dev", false, "development server: local data directory, no hosts, emails written to the log")
	dataDir := fs.String("data-dir", env("DATA_DIR", ".5050-dev"), "data directory for --dev")
	listen := fs.String("listen", env("LISTEN", ""), "listen address (default from config.json, 127.0.0.1:5050)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log := logger()
	var p *plane
	var err error
	if *dev {
		p, err = openDev(ctx, *dataDir, *listen, log)
	} else {
		p, err = openPlane(ctx, *cfgPath, nil, log)
	}
	if err != nil {
		return err
	}
	defer p.Close()
	if *listen != "" {
		p.file.Listen = *listen
	}
	var o cpapi.Orchestrator
	if *dev {
		o = fakeorch.New(p.c)
	} else if o, err = newOrch(p); err != nil {
		return err
	}
	p.c.SetOrchestrator(o)
	if err := finishPendingRestore(ctx, p, o); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		if err := o.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errc <- fmt.Errorf("the orchestrator stopped: %w", err)
		}
	}()
	go p.c.Run(ctx)
	go p.st.Run(ctx)
	if !*dev {
		go hourlySnapshots(ctx, p)
	}

	webFS, err := iofs.Sub(web.Dist, "dist")
	if err != nil {
		webFS = nil
	}
	srv := &http.Server{Addr: p.file.Listen, Handler: coreapi.New(p.c, webFS, log),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, c2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer c2()
		_ = srv.Shutdown(sctx)
	}()
	if *dev {
		if st, err := p.c.SetupStatus(ctx); err == nil && st.NeedsSetup {
			if tok, err := p.c.CreateSetupToken(ctx); err == nil {
				_, _ = fmt.Fprintf(stdout, "Development server. Create the owner at: %s/setup?token=%s\n", p.c.ConsoleURL(ctx), tok)
			}
		}
	}
	log.Info("5050d listening", "addr", p.file.Listen, "version", versionString(), "dev", *dev)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		cancel()
		return err
	}
}

// openDev prepares a throwaway install in dataDir for `serve --dev`.
func openDev(ctx context.Context, dataDir, listen string, log *slog.Logger) (*plane, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if listen == "" {
		listen = "127.0.0.1:5050"
	}
	path := filepath.Join(abs, "config.json")
	f, err := config.Load(path)
	if err != nil {
		f = &config.File{DataDir: abs, Listen: listen, InstallName: "5050cloud dev", PublicIP: "127.0.0.1", BaseDomain: "127-0-0-1.sslip.io",
			ConsoleDomain: "console.127-0-0-1.sslip.io", MasterKeyPath: filepath.Join(abs, "master.key"), HostKeyPath: filepath.Join(abs, "host.key"),
			ConsoleURL: "http://" + listen, Insecure: true, NoUpdateCheck: true}
		f.Defaults()
		if err := config.Save(path, f); err != nil {
			return nil, err
		}
	}
	if _, _, err := config.EnsureKey(f.MasterKeyPath); err != nil {
		return nil, err
	}
	m := &logMailer{log: log}
	return openWith(ctx, path, f, func(api.SMTPSettings) mail.Sender { return m }, log)
}

func versionString() string { return version.Version }
