// SPDX-License-Identifier: AGPL-3.0-only

// Package core is the control plane's domain model (WP1, docs/SPEC.md 8.3):
// users and sessions, projects, environments, services and their committed
// config, variables, staged changes and commits, domains, TCP proxies,
// volumes, monitors, settings, GitHub, imports, email, notifications,
// activity and presence. Core implements cpapi.Config for the orchestrator
// and is driven by the HTTP layer in core/api.
//
// Every write goes through the store's single writer (6.5). Secret columns
// are sealed with the master key; their additional data is
// "<table>|<row id>|<column>".
package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/internal/cp/core/events"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/github"
	"github.com/musakkka/5050cloud/internal/cp/core/mail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/core/webhooks"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

// Options builds a Core.
type Options struct {
	Store *store.Store
	Box   *secretbox.Box
	// File is config.json as loaded; Path is where it lives (rewritten when
	// the console domain changes).
	File *config.File
	Path string
	// MasterKeyPath is read for the recovery key and the backup keys.
	MasterKeyPath string
	// Templates is templates.FS.
	Templates fs.FS
	// Mailer builds a sender for the stored SMTP settings; nil uses real SMTP.
	// Tests and the dev server pass a fake.
	Mailer func(s api.SMTPSettings) mail.Sender
	// GitHub is the GitHub API client; Git runs git for public repositories.
	GitHub *github.Client
	Git    github.Git
	// HTTP returns the outbound client for webhooks, the Railway importer
	// and public GitHub calls (SSRF guarded unless private destinations are allowed).
	HTTP func() *http.Client
	Now  func() time.Time
	Log  *slog.Logger
}

// Core implements cpapi.Config and the operations behind the REST API.
type Core struct {
	st        *store.Store
	box       *secretbox.Box
	file      *config.File
	path      string
	keyPath   string
	templates fs.FS
	mailer    func(api.SMTPSettings) mail.Sender
	gh        *github.Client
	git       github.Git
	httpc     func() *http.Client
	now       func() time.Time
	log       *slog.Logger

	orch     cpapi.Orchestrator
	broker   *events.Broker
	hooks    *webhooks.Dispatcher
	presence *presenceTable

	mu      sync.Mutex
	latest  string // latest release seen by the update check
	started time.Time
}

// New returns a Core over an open store.
func New(o Options) *Core {
	c := &Core{
		st: o.Store, box: o.Box, file: o.File, path: o.Path, keyPath: o.MasterKeyPath,
		templates: o.Templates, mailer: o.Mailer, gh: o.GitHub, git: o.Git,
		httpc: o.HTTP, now: o.Now, log: o.Log,
		broker: events.New(), presence: newPresence(),
	}
	if c.file == nil {
		c.file = &config.File{}
		c.file.Defaults()
	}
	if c.keyPath == "" {
		c.keyPath = c.file.MasterKeyPath
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.log == nil {
		c.log = slog.Default()
	}
	if c.httpc == nil {
		allow := c.file.AllowPrivateDestinations
		cl := webhooks.NewClient(allow, 30*time.Second)
		c.httpc = func() *http.Client { return cl }
	}
	if c.gh == nil {
		c.gh = github.NewClient(c.file.GitHubAPI, c.httpc())
	}
	if c.mailer == nil {
		c.mailer = func(s api.SMTPSettings) mail.Sender {
			return mail.SMTP{Host: s.Host, Port: s.Port, TLSMode: s.TLSMode, Username: s.Username, Password: s.Password, From: s.From}
		}
	}
	c.hooks = webhooks.New(c.st, c.box, c.httpc, c.log)
	c.hooks.AlertSecret = c.alertSecret
	c.started = c.now()
	return c
}

// SetOrchestrator wires the other half. It must be called before serving.
func (c *Core) SetOrchestrator(o cpapi.Orchestrator) { c.orch = o }

// Orchestrator returns the wired orchestrator.
func (c *Core) Orchestrator() cpapi.Orchestrator { return c.orch }

// Store returns the database.
func (c *Core) Store() *store.Store { return c.st }

// Broker returns the event fan-out.
func (c *Core) Broker() *events.Broker { return c.broker }

// Hooks returns the webhook dispatcher.
func (c *Core) Hooks() *webhooks.Dispatcher { return c.hooks }

// File returns config.json as loaded.
func (c *Core) File() *config.File { return c.file }

// Now is the core clock in Unix milliseconds.
func (c *Core) Now() int64 { return c.now().UnixMilli() }

// Run starts background work: webhook deliveries, the public repo poller,
// domain DNS checks, presence expiry and housekeeping. It blocks until ctx
// is done.
func (c *Core) Run(ctx context.Context) {
	var wg sync.WaitGroup
	run := func(f func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); f(ctx) }()
	}
	run(c.hooks.Run)
	run(c.pollPublicRepos)
	run(c.checkDomainsLoop)
	run(c.housekeeping)
	run(c.updateCheck)
	wg.Wait()
}

func (c *Core) housekeeping(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := c.Now()
		c.presence.expire(now, func(envID string) { c.publishPresence(envID) })
		_ = c.st.Exec(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
		_ = c.st.Exec(ctx, `DELETE FROM name_aliases WHERE expires_at < ?`, now)
		_ = c.st.Exec(ctx, `DELETE FROM cli_auth WHERE expires_at < ?`, now-int64(time.Hour/time.Millisecond))
		_ = c.st.Exec(ctx, `DELETE FROM password_resets WHERE expires_at < ?`, now-int64(24*time.Hour/time.Millisecond))
		_ = c.hooks.Prune(ctx)
	}
}

// ---------------------------------------------------------------- settings

// getSetting decodes the JSON value of key into v; missing keys leave v alone.
func (c *Core) getSetting(ctx context.Context, q store.Querier, key string, v any) (secret []byte, ok bool, err error) {
	var raw string
	var enc []byte
	err = q.QueryRowContext(ctx, `SELECT value, secret_enc FROM settings WHERE key = ?`, key).Scan(&raw, &enc)
	if store.NoRows(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if v != nil && raw != "" {
		if err := json.Unmarshal([]byte(raw), v); err != nil {
			return nil, false, fail.Wrapf(err, "settings %s", key)
		}
	}
	if len(enc) > 0 {
		secret, err = c.box.Open(enc, settingAAD(key))
		if err != nil {
			return nil, false, fail.Internal(fail.Wrapf(err, "open settings %s", key))
		}
	}
	return secret, true, nil
}

func settingAAD(key string) string { return "settings|" + key + "|secret_enc" }

// putSetting writes key; secret nil keeps the stored secret, empty clears it.
func (c *Core) putSetting(ctx context.Context, tx *sql.Tx, key string, v any, secret []byte) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if secret == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
		return err
	}
	var enc []byte
	if len(secret) > 0 {
		enc, err = c.box.Seal(secret, settingAAD(key))
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value, secret_enc) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, secret_enc = excluded.secret_enc`, key, string(b), enc)
	return err
}

// installSettings is the "install" settings row.
type installSettings struct {
	Name             string   `json:"name"`
	BaseDomain       string   `json:"baseDomain"`
	ConsoleDomain    string   `json:"consoleDomain"`
	OldConsoleDomain string   `json:"oldConsoleDomain,omitempty"`
	OldConsoleUntil  int64    `json:"oldConsoleUntil,omitempty"`
	ACMEEmail        string   `json:"acmeEmail,omitempty"`
	PublicIP         string   `json:"publicIp"`
	TrustedProxies   []string `json:"trustedProxies"`
	RegistryCapPct   int      `json:"registryCapPct"`
	BuildTimeoutMin  int      `json:"buildTimeoutMin"`
	RequireTOTP      bool     `json:"requireTotp"`
	AllowPrivate     bool     `json:"allowPrivateDestinations,omitempty"`
	// SetupDone is set once the owner exists.
	SetupDone bool `json:"setupDone"`
}

func (c *Core) installRow(ctx context.Context, q store.Querier) (installSettings, error) {
	s := installSettings{
		Name: c.file.InstallName, BaseDomain: c.file.BaseDomain, ConsoleDomain: c.file.ConsoleDomain,
		ACMEEmail: c.file.ACMEEmail, PublicIP: c.file.PublicIP, RegistryCapPct: 30, BuildTimeoutMin: 30,
		TrustedProxies: []string{}, AllowPrivate: c.file.AllowPrivateDestinations,
	}
	if _, _, err := c.getSetting(ctx, q, "install", &s); err != nil {
		return s, err
	}
	if s.TrustedProxies == nil {
		s.TrustedProxies = []string{}
	}
	return s, nil
}

func (c *Core) railwayAliases(ctx context.Context, q store.Querier) bool {
	on := true
	_, _, _ = c.getSetting(ctx, q, "railway_aliases", &on)
	return on
}

// EnsureInstall seeds the settings rows from config.json at first start.
func (c *Core) EnsureInstall(ctx context.Context) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		_, ok, err := c.getSetting(ctx, tx, "install", nil)
		if err != nil || ok {
			return err
		}
		s, err := c.installRow(ctx, tx)
		if err != nil {
			return err
		}
		var users int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
			return err
		}
		s.SetupDone = users > 0
		return c.putSetting(ctx, tx, "install", s, nil)
	})
}

// ConsoleURL is https://<console domain> (or the development override).
func (c *Core) ConsoleURL(ctx context.Context) string {
	if c.file.ConsoleURL != "" {
		return c.file.ConsoleBase()
	}
	s, err := c.installRow(ctx, c.st.Reader)
	if err != nil || s.ConsoleDomain == "" {
		return c.file.ConsoleBase()
	}
	return "https://" + s.ConsoleDomain
}

// ConsoleHosts are the host names the console answers on (the current one and,
// for 24 hours after a change, the old one).
func (c *Core) ConsoleHosts(ctx context.Context) []string {
	s, err := c.installRow(ctx, c.st.Reader)
	if err != nil {
		return nil
	}
	out := []string{s.ConsoleDomain}
	if s.OldConsoleDomain != "" && s.OldConsoleUntil > c.Now() {
		out = append(out, s.OldConsoleDomain)
	}
	return out
}

// TrustedProxies are the CIDRs Caddy trusts for forwarded headers.
func (c *Core) TrustedProxies(ctx context.Context) []string {
	s, err := c.installRow(ctx, c.st.Reader)
	if err != nil {
		return nil
	}
	return s.TrustedProxies
}

// AllowPrivateDestinations reports the SSRF guard override.
func (c *Core) AllowPrivateDestinations(ctx context.Context) bool {
	s, err := c.installRow(ctx, c.st.Reader)
	return err == nil && s.AllowPrivate
}

// ReadOnly reports read-only mode (6.6).
func (c *Core) ReadOnly() bool { return c.st.ReadOnly() }

// Install implements cpapi.Config and GET /install.
func (c *Core) Install(ctx context.Context) (api.Install, error) {
	s, err := c.installRow(ctx, c.st.Reader)
	if err != nil {
		return api.Install{}, err
	}
	in := api.Install{
		Name: s.Name, Version: version.Version, ConsoleURL: c.ConsoleURL(ctx), BaseDomain: s.BaseDomain,
		PublicIP: s.PublicIP, ACMEEmail: s.ACMEEmail, TrustedProxies: s.TrustedProxies,
		RegistryCapPct: s.RegistryCapPct, BuildTimeoutMin: s.BuildTimeoutMin,
		RailwayAliases: c.railwayAliases(ctx, c.st.Reader), RequireTOTP: s.RequireTOTP,
		ReadOnly: c.st.ReadOnly(),
	}
	c.mu.Lock()
	if c.latest != "" && newerVersion(c.latest, version.Version) {
		in.LatestVersion = c.latest
	}
	c.mu.Unlock()
	app, _ := c.githubApp(ctx)
	in.GitHubApp = app != nil
	b, _ := c.backupSettings(ctx, c.st.Reader)
	in.BackupsReady = b.Endpoint != "" && b.Bucket != "" && b.AccessKeyID != "" && b.SecretAccessKey != ""
	sm, _ := c.smtpSettings(ctx, c.st.Reader)
	in.SMTPReady = sm.Host != "" && sm.From != ""
	if c.orch != nil {
		if rs, err := c.orch.AdoptStatus(ctx); err == nil {
			in.Restore = rs
		}
	}
	return in, nil
}

// UpdateInstall is PATCH /install.
func (c *Core) UpdateInstall(ctx context.Context, a Actor, req api.UpdateInstallRequest) (api.Install, error) {
	var consoleChanged bool
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		s, err := c.installRow(ctx, tx)
		if err != nil {
			return err
		}
		if req.Name != nil {
			n := trim(*req.Name)
			if n == "" || len(n) > 64 {
				return fail.Validation("name", "The install name must be 1 to 64 characters.")
			}
			s.Name = n
		}
		if req.BaseDomain != nil {
			d := lower(trim(*req.BaseDomain))
			if !config.ValidDomain(d) {
				return fail.Validation("baseDomain", "The base domain %q is not a valid domain name.", d)
			}
			s.BaseDomain = d
		}
		if req.ConsoleDomain != nil {
			d := lower(trim(*req.ConsoleDomain))
			if !config.ValidDomain(d) {
				return fail.Validation("consoleDomain", "The console domain %q is not a valid domain name.", d)
			}
			if d != s.ConsoleDomain {
				s.OldConsoleDomain, s.OldConsoleUntil = s.ConsoleDomain, c.Now()+day
				s.ConsoleDomain = d
				consoleChanged = true
			}
		}
		if req.ACMEEmail != nil {
			e := trim(*req.ACMEEmail)
			if e != "" && !validEmail(e) {
				return fail.Validation("acmeEmail", "The ACME email %q is not a valid email address.", e)
			}
			s.ACMEEmail = e
		}
		if req.TrustedProxies != nil {
			list, err := cleanCIDRs("trustedProxies", *req.TrustedProxies)
			if err != nil {
				return err
			}
			s.TrustedProxies = list
		}
		if req.RegistryCapPct != nil {
			if *req.RegistryCapPct < 5 || *req.RegistryCapPct > 90 {
				return fail.Validation("registryCapPct", "The registry size cap must be between 5 and 90 percent of the disk.")
			}
			s.RegistryCapPct = *req.RegistryCapPct
		}
		if req.BuildTimeoutMin != nil {
			if *req.BuildTimeoutMin < 1 || *req.BuildTimeoutMin > 240 {
				return fail.Validation("buildTimeoutMin", "The build timeout must be between 1 and 240 minutes.")
			}
			s.BuildTimeoutMin = *req.BuildTimeoutMin
		}
		if req.RequireTOTP != nil {
			s.RequireTOTP = *req.RequireTOTP
		}
		if req.RailwayAliases != nil {
			if err := c.putSetting(ctx, tx, "railway_aliases", *req.RailwayAliases, nil); err != nil {
				return err
			}
		}
		return c.putSetting(ctx, tx, "install", s, nil)
	})
	if err != nil {
		return api.Install{}, err
	}
	if consoleChanged && c.path != "" {
		s, _ := c.installRow(ctx, c.st.Reader)
		f := *c.file
		f.ConsoleDomain = s.ConsoleDomain
		if err := config.Save(c.path, &f); err != nil {
			c.log.Warn("cannot rewrite config.json after a console domain change", "err", err)
		} else {
			c.file.ConsoleDomain = s.ConsoleDomain
		}
	}
	in, err := c.Install(ctx)
	if err == nil {
		c.broker.Publish(api.Event{Type: "install", Install: &in})
		if c.orch != nil {
			c.orch.NetworkChanged(ctx, "")
		}
	}
	return in, err
}

const (
	minute = int64(time.Minute / time.Millisecond)
	hour   = int64(time.Hour / time.Millisecond)
	day    = 24 * hour
)
