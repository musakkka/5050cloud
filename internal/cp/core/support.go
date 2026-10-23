// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/github"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/mail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

func versionString() string { return version.Version }

// ---------------------------------------------------------------- presence

// presenceTable keeps who looks at which environment canvas (9.24).
type presenceTable struct {
	mu sync.Mutex
	m  map[string]map[string]api.Presence // env -> user -> presence
}

func newPresence() *presenceTable { return &presenceTable{m: map[string]map[string]api.Presence{}} }

const presenceTTL = 60 * 1000

func (p *presenceTable) touch(envID string, pr api.Presence) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m[envID] == nil {
		p.m[envID] = map[string]api.Presence{}
	}
	p.m[envID][pr.UserID] = pr
}

func (p *presenceTable) list(envID string) []api.Presence {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []api.Presence{}
	for _, pr := range p.m[envID] {
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

func (p *presenceTable) expire(now int64, changed func(envID string)) {
	p.mu.Lock()
	var envs []string
	for env, users := range p.m {
		n := len(users)
		for id, pr := range users {
			if now-pr.LastSeen > presenceTTL {
				delete(users, id)
			}
		}
		if len(users) != n {
			envs = append(envs, env)
		}
		if len(users) == 0 {
			delete(p.m, env)
		}
	}
	p.mu.Unlock()
	for _, e := range envs {
		changed(e)
	}
}

func (c *Core) publishPresence(envID string) {
	c.broker.Publish(api.Event{Type: "presence", EnvironmentID: envID, Presence: c.presence.list(envID)})
}

// Presence records a heartbeat from a viewer of an environment.
func (c *Core) Presence(ctx context.Context, a Actor, envID string, req api.PresenceRequest) error {
	if _, _, err := c.Environment(ctx, envID); err != nil {
		return err
	}
	name := a.Name
	if name == "" {
		name = a.Email
	}
	c.presence.touch(envID, api.Presence{UserID: a.UserID, Name: name, Cursor: req.Cursor, LastSeen: c.Now()})
	c.publishPresence(envID)
	return nil
}

// ---------------------------------------------------------------- settings rows

// alertSecret signs install alert deliveries; created on first use.
func (c *Core) alertSecret() string {
	ctx := context.Background()
	sec, ok, err := c.getSetting(ctx, c.st.Reader, "alert_secret", nil)
	if err == nil && ok && len(sec) > 0 {
		return string(sec)
	}
	s := auth.NewToken(32)
	_ = c.st.Write(ctx, func(tx *sql.Tx) error {
		cur, ok, err := c.getSetting(ctx, tx, "alert_secret", nil)
		if err == nil && ok && len(cur) > 0 {
			s = string(cur)
			return nil
		}
		return c.putSetting(ctx, tx, "alert_secret", map[string]any{}, []byte(s))
	})
	return s
}

func (c *Core) githubApp(ctx context.Context) (*github.App, error) {
	sec, ok, err := c.getSetting(ctx, c.st.Reader, "github_app", nil)
	if err != nil || !ok || len(sec) == 0 {
		return nil, err
	}
	var app github.App
	if err := json.Unmarshal(sec, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

type backupSecrets struct {
	Secret          string `json:"secret,omitempty"`
	RetentionSecret string `json:"retentionSecret,omitempty"`
}

func (c *Core) backupSettings(ctx context.Context, q store.Querier) (api.BackupSettings, error) {
	var s api.BackupSettings
	sec, _, err := c.getSetting(ctx, q, "backups", &s)
	if err != nil {
		return s, err
	}
	if len(sec) > 0 {
		var bs backupSecrets
		_ = json.Unmarshal(sec, &bs)
		s.SecretAccessKey, s.RetentionSecretAccessKey = bs.Secret, bs.RetentionSecret
	}
	return s, nil
}

// BackupSettings implements cpapi.Config (both secret keys included).
func (c *Core) BackupSettings(ctx context.Context) (api.BackupSettings, error) {
	return c.backupSettings(ctx, c.st.Reader)
}

// BackupSettingsMasked is GET /settings/backups: secrets are never returned.
func (c *Core) BackupSettingsMasked(ctx context.Context) (api.BackupSettings, error) {
	s, err := c.backupSettings(ctx, c.st.Reader)
	if s.SecretAccessKey != "" {
		s.SecretAccessKey = "•••"
	}
	if s.RetentionSecretAccessKey != "" {
		s.RetentionSecretAccessKey = "•••"
	}
	return s, err
}

// PutBackupSettings is PUT /settings/backups. A masked or empty secret keeps the stored one.
func (c *Core) PutBackupSettings(ctx context.Context, in api.BackupSettings) (api.BackupSettings, error) {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		cur, err := c.backupSettings(ctx, tx)
		if err != nil {
			return err
		}
		bs := backupSecrets{Secret: cur.SecretAccessKey, RetentionSecret: cur.RetentionSecretAccessKey}
		if in.SecretAccessKey != "" && in.SecretAccessKey != "•••" {
			bs.Secret = in.SecretAccessKey
		}
		if in.RetentionSecretAccessKey != "" && in.RetentionSecretAccessKey != "•••" {
			bs.RetentionSecret = in.RetentionSecretAccessKey
		}
		in.SecretAccessKey, in.RetentionSecretAccessKey = "", ""
		in.LastStateBackup, in.LitestreamLagMs, in.LitestreamPosition = cur.LastStateBackup, cur.LitestreamLagMs, cur.LitestreamPosition
		b, _ := json.Marshal(bs)
		return c.putSetting(ctx, tx, "backups", in, b)
	})
	if err != nil {
		return api.BackupSettings{}, err
	}
	if c.orch != nil {
		if err := c.orch.BackupSettingsChanged(ctx); err != nil {
			c.log.Warn("backup settings changed but data services were not redeployed", "err", err)
		}
	}
	return c.BackupSettingsMasked(ctx)
}

// FillBackupSecrets replaces masked secrets in a test request with the stored ones.
func (c *Core) FillBackupSecrets(ctx context.Context, in api.BackupSettings) api.BackupSettings {
	cur, _ := c.backupSettings(ctx, c.st.Reader)
	if in.SecretAccessKey == "" || in.SecretAccessKey == "•••" {
		in.SecretAccessKey = cur.SecretAccessKey
	}
	if in.RetentionSecretAccessKey == "" || in.RetentionSecretAccessKey == "•••" {
		in.RetentionSecretAccessKey = cur.RetentionSecretAccessKey
	}
	return in
}

func (c *Core) smtpSettings(ctx context.Context, q store.Querier) (api.SMTPSettings, error) {
	s := api.SMTPSettings{Port: 587, TLSMode: "starttls"}
	sec, _, err := c.getSetting(ctx, q, "smtp", &s)
	if err != nil {
		return s, err
	}
	s.Password = string(sec)
	return s, nil
}

// SMTPSettingsMasked is GET /settings/smtp.
func (c *Core) SMTPSettingsMasked(ctx context.Context) (api.SMTPSettings, error) {
	s, err := c.smtpSettings(ctx, c.st.Reader)
	if s.Password != "" {
		s.Password = "•••"
	}
	return s, err
}

// PutSMTPSettings is PUT /settings/smtp.
func (c *Core) PutSMTPSettings(ctx context.Context, in api.SMTPSettings) (api.SMTPSettings, error) {
	switch in.TLSMode {
	case "", "starttls", "tls", "none":
	default:
		return api.SMTPSettings{}, fail.Validation("tlsMode", "The TLS mode must be starttls, tls or none.")
	}
	if in.TLSMode == "" {
		in.TLSMode = "starttls"
	}
	if in.Port == 0 {
		in.Port = 587
	}
	if in.Port < 1 || in.Port > 65535 {
		return api.SMTPSettings{}, fail.Validation("port", "The port must be between 1 and 65535.")
	}
	if in.From != "" && !validEmail(in.From) {
		return api.SMTPSettings{}, fail.Validation("from", "The from address %q is not a valid email address.", in.From)
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var secret []byte // nil keeps
		if in.Password != "" && in.Password != "•••" {
			secret = []byte(in.Password)
		}
		in.Password = ""
		return c.putSetting(ctx, tx, "smtp", in, secret)
	})
	if err != nil {
		return api.SMTPSettings{}, err
	}
	return c.SMTPSettingsMasked(ctx)
}

// TestSMTP sends a test email with the stored settings.
func (c *Core) TestSMTP(ctx context.Context, to string) api.TestResult {
	s, err := c.smtpSettings(ctx, c.st.Reader)
	if err != nil || s.Host == "" {
		return api.TestResult{Error: "Email is not set up yet: save the SMTP settings first."}
	}
	if !validEmail(to) {
		return api.TestResult{Error: "The test address is not a valid email address."}
	}
	in, _ := c.installRow(ctx, c.st.Reader)
	subj, text := mail.Test(in.Name)
	if err := c.mailer(s).Send(ctx, mail.Message{To: []string{to}, Subject: subj, Text: text}); err != nil {
		return api.TestResult{Error: sentence(err.Error())}
	}
	return api.TestResult{OK: true}
}

// Alerts is GET /settings/alerts.
func (c *Core) Alerts(ctx context.Context) (api.InstallNotifications, error) {
	s := api.InstallNotifications{Emails: []string{}, Events: []string{
		api.AlertHostOffline, api.AlertDiskHigh, api.AlertDiskCritical, api.AlertBackupStale,
		api.AlertUpgradeRolledBack, api.AlertCertFailing, api.AlertVersionSkew, api.AlertHostDuplicate, api.AlertReadOnly,
	}}
	_, _, err := c.getSetting(ctx, c.st.Reader, "alerts", &s)
	if s.Emails == nil {
		s.Emails = []string{}
	}
	return s, err
}

// PutAlerts is PUT /settings/alerts.
func (c *Core) PutAlerts(ctx context.Context, in api.InstallNotifications) (api.InstallNotifications, error) {
	for _, e := range in.Emails {
		if !validEmail(e) {
			return in, fail.Validation("emails", "%q is not a valid email address.", e)
		}
	}
	if in.WebhookURL != "" && !strings.HasPrefix(in.WebhookURL, "https://") && !strings.HasPrefix(in.WebhookURL, "http://") {
		return in, fail.Validation("webhookUrl", "The webhook URL must start with https:// or http://.")
	}
	if in.Emails == nil {
		in.Emails = []string{}
	}
	if in.Events == nil {
		in.Events = []string{}
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error { return c.putSetting(ctx, tx, "alerts", in, nil) })
	return in, err
}

// ---------------------------------------------------------------- events

func (c *Core) publishChanges(ctx context.Context, envID string) {
	n := c.changesCount(ctx, envID)
	c.broker.Publish(api.Event{Type: "changes", EnvironmentID: envID, ChangesCount: &n})
}

func (c *Core) publishService(ctx context.Context, svc api.Service) {
	s := svc
	c.broker.Publish(api.Event{Type: "service", EnvironmentID: svc.EnvironmentID, ServiceID: svc.ID, Service: &s})
}

// Publish implements cpapi.Config.
func (c *Core) Publish(e api.Event) { c.broker.Publish(e) }

func (c *Core) changesCount(ctx context.Context, envID string) int {
	var n int
	_ = c.st.Reader.QueryRowContext(ctx, `SELECT count(*) FROM changes WHERE environment_id = ?`, envID).Scan(&n)
	return n
}

// ---------------------------------------------------------------- canvas positions

func (c *Core) positions(ctx context.Context, q store.Querier, envID string) (map[string][2]float64, error) {
	out := map[string][2]float64{}
	var raw string
	err := q.QueryRowContext(ctx, `SELECT positions FROM canvas_positions WHERE environment_id = ?`, envID).Scan(&raw)
	if store.NoRows(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func (c *Core) setPosition(ctx context.Context, tx *sql.Tx, envID, sid string, p [2]float64) error {
	pos, err := c.positions(ctx, tx, envID)
	if err != nil {
		return err
	}
	pos[sid] = p
	return c.savePositions(ctx, tx, envID, pos)
}

func (c *Core) savePositions(ctx context.Context, tx *sql.Tx, envID string, pos map[string][2]float64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO canvas_positions (environment_id, positions) VALUES (?, ?)
		ON CONFLICT(environment_id) DO UPDATE SET positions = excluded.positions`, envID, jsonString(pos))
	return err
}

// SavePositions is PUT /environments/{eid}/positions.
func (c *Core) SavePositions(ctx context.Context, a Actor, envID string, req api.SavePositionsRequest) error {
	if len(req.Positions) > 500 {
		return fail.Validation("positions", "At most 500 positions can be saved at once.")
	}
	if _, _, err := c.Environment(ctx, envID); err != nil {
		return err
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		pos := map[string][2]float64{}
		if len(req.Positions) > 0 {
			cur, err := c.positions(ctx, tx, envID)
			if err != nil {
				return err
			}
			pos = cur
			for k, v := range req.Positions {
				pos[k] = v
			}
		}
		return c.savePositions(ctx, tx, envID, pos)
	})
	if err == nil {
		c.broker.Publish(api.Event{Type: "positions", EnvironmentID: envID, Positions: req.Positions, Origin: a.SessionHash})
	}
	return err
}

// Groups lists canvas groups.
func (c *Core) Groups(ctx context.Context, q store.Querier, envID string) ([]api.Group, error) {
	return store.All(ctx, q, func(r *sql.Rows) (api.Group, error) {
		var g api.Group
		var svcs string
		err := r.Scan(&g.ID, &g.Name, &g.Icon, &g.Parent, &svcs)
		_ = json.Unmarshal([]byte(svcs), &g.Services)
		if g.Services == nil {
			g.Services = []string{}
		}
		return g, err
	}, `SELECT id, name, icon, parent, services FROM groups WHERE environment_id = ? ORDER BY id`, envID)
}

// PutGroups replaces the canvas groups of an environment.
func (c *Core) PutGroups(ctx context.Context, a Actor, envID string, in []api.Group) ([]api.Group, error) {
	if _, _, err := c.Environment(ctx, envID); err != nil {
		return nil, err
	}
	for i := range in {
		if in[i].ID == "" {
			in[i].ID = ids.New(ids.Group)
		}
		in[i].Name = cleanName(in[i].Name)
		if in[i].Name == "" || len(in[i].Name) > 64 {
			return nil, fail.Validation("name", "A group name must be 1 to 64 characters.")
		}
		if in[i].Services == nil {
			in[i].Services = []string{}
		}
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE environment_id = ?`, envID); err != nil {
			return err
		}
		for _, g := range in {
			if _, err := tx.ExecContext(ctx, `INSERT INTO groups (id, environment_id, name, icon, parent, services) VALUES (?, ?, ?, ?, ?, ?)`,
				g.ID, envID, g.Name, g.Icon, g.Parent, jsonString(g.Services)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.broker.Publish(api.Event{Type: "groups", EnvironmentID: envID, Groups: in, Origin: a.SessionHash})
	return in, nil
}

// ---------------------------------------------------------------- activity

func (c *Core) insertActivity(ctx context.Context, tx *sql.Tx, envID string, a api.Activity) error {
	if a.ID == "" {
		a.ID = ids.New(ids.Activity)
	}
	if a.TS == 0 {
		a.TS = c.Now()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO activity (id, environment_id, service_id, ts, kind, text, actor, ref, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, envID, a.ServiceID, a.TS, a.Kind, a.Text, a.Actor, a.Ref, a.Status)
	if err == nil {
		act := a
		c.broker.Publish(api.Event{Type: "activity", EnvironmentID: envID, ServiceID: a.ServiceID, Activity: &act})
	}
	return err
}

// RecordActivity implements cpapi.Config.
func (c *Core) RecordActivity(ctx context.Context, envID string, a api.Activity) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		if err := c.insertActivity(ctx, tx, envID, a); err != nil {
			return err
		}
		c.touchProject(ctx, tx, envID)
		return nil
	})
}

// Activity is GET /environments/{eid}/activity.
func (c *Core) Activity(ctx context.Context, envID, service, kind, before string, limit int) (api.Page[api.Activity], error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, ts, service_id, kind, text, actor, ref, status FROM activity WHERE environment_id = ?`
	args := []any{envID}
	if service != "" {
		q += ` AND service_id = ?`
		args = append(args, service)
	}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)
	items, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.Activity, error) {
		var a api.Activity
		return a, r.Scan(&a.ID, &a.TS, &a.ServiceID, &a.Kind, &a.Text, &a.Actor, &a.Ref, &a.Status)
	}, q, args...)
	if err != nil {
		return api.Page[api.Activity]{}, err
	}
	p := api.Page[api.Activity]{Items: items}
	if len(items) > limit {
		p.Items = items[:limit]
		p.Next = items[limit-1].ID
	}
	if p.Items == nil {
		p.Items = []api.Activity{}
	}
	return p, nil
}

// ---------------------------------------------------------------- audit

// Audit implements cpapi.Config.
func (c *Core) Audit(ctx context.Context, e api.AuditEntry) error {
	if e.ID == "" {
		e.ID = ids.New(ids.Audit)
	}
	if e.TS == 0 {
		e.TS = c.Now()
	}
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO audit_log (id, ts, actor, action, target, ip, detail) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.TS, e.Actor, e.Action, e.Target, e.IP, e.Detail)
		return err
	})
}

// AuditLog is GET /audit.
func (c *Core) AuditLog(ctx context.Context, before string, limit int) (api.Page[api.AuditEntry], error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, ts, actor, action, target, ip, detail FROM audit_log`
	args := []any{}
	if before != "" {
		q += ` WHERE id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)
	items, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.AuditEntry, error) {
		var e api.AuditEntry
		return e, r.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Target, &e.IP, &e.Detail)
	}, q, args...)
	if err != nil {
		return api.Page[api.AuditEntry]{}, err
	}
	p := api.Page[api.AuditEntry]{Items: items}
	if len(items) > limit {
		p.Items = items[:limit]
		p.Next = items[limit-1].ID
	}
	if p.Items == nil {
		p.Items = []api.AuditEntry{}
	}
	return p, nil
}

// ---------------------------------------------------------------- imports

func (c *Core) saveImport(ctx context.Context, imp api.Import) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO imports (id, kind, project_id, environment_id, status, error, report, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET status = excluded.status, error = excluded.error, report = excluded.report, project_id = excluded.project_id, environment_id = excluded.environment_id`,
			imp.ID, imp.Kind, imp.ProjectID, imp.EnvironmentID, imp.Status, imp.Error, jsonString(imp.Report), imp.CreatedAt)
		return err
	})
}

// Import is GET /imports/{id}.
func (c *Core) Import(ctx context.Context, id string) (api.Import, error) {
	var imp api.Import
	var rep string
	err := c.st.Reader.QueryRowContext(ctx, `SELECT id, kind, project_id, environment_id, status, error, report, created_at FROM imports WHERE id = ?`, id).
		Scan(&imp.ID, &imp.Kind, &imp.ProjectID, &imp.EnvironmentID, &imp.Status, &imp.Error, &rep, &imp.CreatedAt)
	if store.NoRows(err) {
		return imp, fail.NotFound("The import %s does not exist.", id)
	}
	_ = json.Unmarshal([]byte(rep), &imp.Report)
	if imp.Report.Landed == nil {
		imp.Report.Landed = []api.ImportItem{}
	}
	if imp.Report.Skipped == nil {
		imp.Report.Skipped = []api.ImportItem{}
	}
	return imp, err
}

// ---------------------------------------------------------------- commits

func (c *Core) recordCommitDeployments(ctx context.Context, commitID string, deps []api.Deployment) {
	if commitID == "" || len(deps) == 0 {
		return
	}
	list := make([]string, 0, len(deps))
	for _, d := range deps {
		list = append(list, d.ID)
	}
	_ = c.st.Exec(ctx, `UPDATE commits SET deployments = ? WHERE id = ?`, jsonString(list), commitID)
}

// ---------------------------------------------------------------- GitHub

// GitHubCloneToken implements cpapi.Config: an installation token for
// repositories the GitHub App can read, "" for public repositories.
func (c *Core) GitHubCloneToken(ctx context.Context, repo string) (string, error) {
	ref, ok := github.ParseRepo(repo)
	if !ok || ref.URL != "" {
		return "", nil
	}
	app, err := c.githubApp(ctx)
	if err != nil || app == nil {
		return "", err
	}
	insts, err := c.gh.Installations(ctx, *app)
	if err != nil {
		return "", err
	}
	owner := strings.SplitN(ref.FullName, "/", 2)[0]
	for _, in := range insts {
		if strings.EqualFold(in.Account, owner) {
			// Limited to this one repository, read-only contents.
			return c.gh.RepoToken(ctx, *app, in.ID, strings.SplitN(ref.FullName, "/", 2)[1])
		}
	}
	return "", nil
}

// pollPublicRepos checks public repositories (no App webhook) every 2
// minutes and deploys services with auto deploy on when their branch moves (9.14).
func (c *Core) pollPublicRepos(ctx context.Context) {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if c.orch == nil || c.st.ReadOnly() {
			continue
		}
		insts, err := c.AllInstances(ctx)
		if err != nil {
			continue
		}
		for _, in := range insts {
			src := in.Config.Source
			if src.Type != "github" || (src.AutoDeploy != nil && !*src.AutoDeploy) {
				continue
			}
			ref, ok := github.ParseRepo(src.Repo)
			if !ok || ref.URL == "" {
				continue
			}
			sha, _, err := c.git.LsRemote(ctx, ref.CloneURL(), src.Branch)
			if err != nil || sha == "" {
				continue
			}
			var last string
			err = c.st.Reader.QueryRowContext(ctx, `SELECT sha FROM github_polls WHERE service_id = ? AND environment_id = ?`, in.ServiceID, in.EnvID).Scan(&last)
			if err != nil && !store.NoRows(err) {
				continue
			}
			_ = c.st.Exec(ctx, `INSERT INTO github_polls (service_id, environment_id, sha, checked_at) VALUES (?, ?, ?, ?)
				ON CONFLICT(service_id, environment_id) DO UPDATE SET sha = excluded.sha, checked_at = excluded.checked_at`, in.ServiceID, in.EnvID, sha, c.Now())
			if last == "" || last == sha {
				continue
			}
			if _, err := c.orch.Deploy(ctx, cpapi.DeployInput{EnvID: in.EnvID, ServiceID: in.ServiceID, Trigger: "github", Actor: "github", Ref: sha, Rebuild: true}); err != nil {
				c.log.Warn("cannot deploy a new commit found by polling", "service", in.ServiceID, "err", err)
			}
		}
	}
}

// updateCheck fetches the latest release version every 6 hours.
func (c *Core) updateCheck(ctx context.Context) {
	if c.file.NoUpdateCheck {
		return
	}
	url := c.file.LatestURL
	if url == "" {
		url = "https://get.5050cloud.com/latest"
	}
	check := func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		resp, err := c.httpc().Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		v := strings.TrimSpace(string(b))
		if _, ok := semver(v); ok {
			c.mu.Lock()
			c.latest = v
			c.mu.Unlock()
		}
	}
	check()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
