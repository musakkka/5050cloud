// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core"
	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/internal/cp/core/fakeorch"
	"github.com/musakkka/5050cloud/internal/cp/core/mail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/templates"
)

type env struct {
	t    *testing.T
	srv  *httptest.Server
	c    *core.Core
	orch *fakeorch.Orch
	mail *mail.Fake
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	f := &config.File{DataDir: dir, Listen: "127.0.0.1:0", PublicIP: "203.0.113.7", MasterKeyPath: filepath.Join(dir, "master.key"),
		HostKeyPath: filepath.Join(dir, "host.key"), Insecure: true, NoUpdateCheck: true}
	f.Defaults()
	box, _, err := config.EnsureKey(f.MasterKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), f.StatePath(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	fm := &mail.Fake{}
	c := core.New(core.Options{Store: st, Box: box, File: f, MasterKeyPath: f.MasterKeyPath, Templates: templates.FS,
		Mailer: func(api.SMTPSettings) mail.Sender { return fm }})
	if err := c.EnsureInstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	o := fakeorch.New(c)
	c.SetOrchestrator(o)
	srv := httptest.NewServer(New(c, nil, nil))
	t.Cleanup(srv.Close)
	f.ConsoleURL = srv.URL
	return &env{t: t, srv: srv, c: c, orch: o, mail: fm}
}

type client struct {
	e     *env
	h     *http.Client
	token string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, h: &http.Client{Jar: jar}}
}

// do sends a JSON request and decodes the answer into out; it fails the test on an unexpected status.
func (cl *client) do(method, path string, in, out any, want int) {
	cl.e.t.Helper()
	var body io.Reader
	if in != nil {
		if s, ok := in.(string); ok {
			body = strings.NewReader(s)
		} else {
			b, _ := json.Marshal(in)
			body = bytes.NewReader(b)
		}
	}
	req, _ := http.NewRequest(method, cl.e.srv.URL+path, body)
	req.Header.Set("Content-Type", "application/json")
	if cl.token != "" {
		req.Header.Set("Authorization", "Bearer "+cl.token)
	}
	resp, err := cl.h.Do(req)
	if err != nil {
		cl.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		cl.e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, b)
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			cl.e.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
}

func TestEveryRouteHasAHandler(t *testing.T) {
	e := newEnv(t)
	s := New(e.c, nil, nil)
	h := s.Handlers()
	for _, r := range api.Routes {
		if !h[r.Method+" "+r.Path] {
			t.Errorf("no handler for %s %s", r.Method, r.Path)
		}
		if r.Method != http.MethodGet && r.Audit == "" && !r.NoAudit {
			t.Errorf("%s %s has no audit action", r.Method, r.Path)
		}
		if r.Role == "" {
			t.Errorf("%s %s has no role", r.Method, r.Path)
		}
	}
	if len(h) != len(api.Routes) {
		t.Errorf("%d handlers for %d routes", len(h), len(api.Routes))
	}
}

func TestRolesAreEnforced(t *testing.T) {
	e := newEnv(t)
	anon := e.client()
	anon.do("GET", "/api/v1/projects", nil, nil, 401)
	anon.do("GET", "/api/v1/install", nil, nil, 401)
	anon.do("GET", "/healthz", nil, nil, 200)
	var ss api.SetupStatus
	anon.do("GET", "/api/v1/setup", nil, &ss, 200)
	if !ss.NeedsSetup {
		t.Fatal("a fresh install needs setup")
	}
	anon.do("POST", "/api/v1/setup", api.SetupRequest{Token: "WRONG", Email: "o@example.com", Password: "a long password"}, nil, 401)
}

func TestScriptedFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client()

	// Setup.
	tok, err := e.c.CreateSetupToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var me api.Me
	owner.do("POST", "/api/v1/setup", api.SetupRequest{Token: tok, Email: "owner@example.com", Name: "Owner", Password: "correct horse battery"}, &me, 200)
	if me.User.Role != api.RoleOwner {
		t.Fatalf("setup role %q", me.User.Role)
	}
	owner.do("POST", "/api/v1/setup", api.SetupRequest{Token: tok, Email: "x@example.com", Password: "correct horse battery"}, nil, 409)

	// 2FA, then log out and back in with a code.
	var ts api.TOTPSetupResponse
	owner.do("POST", "/api/v1/me/2fa/setup", nil, &ts, 200)
	var te api.TOTPEnableResponse
	owner.do("POST", "/api/v1/me/2fa/enable", api.TOTPEnableRequest{Code: auth.TOTPCode(ts.Secret, time.Now())}, &te, 200)
	if len(te.RecoveryCodes) == 0 {
		t.Fatal("no recovery codes")
	}
	owner.do("POST", "/api/v1/auth/logout", nil, nil, 204)
	owner.do("GET", "/api/v1/me", nil, nil, 401)
	var lr api.LoginResponse
	owner.do("POST", "/api/v1/auth/login", api.LoginRequest{Email: "owner@example.com", Password: "correct horse battery"}, &lr, 200)
	if !lr.NeedTOTP || lr.Ticket == "" {
		t.Fatalf("login did not ask for a code: %+v", lr)
	}
	owner.do("POST", "/api/v1/auth/login/totp", api.TOTPLoginRequest{Ticket: lr.Ticket, Code: auth.TOTPCode(ts.Secret, time.Now())}, &me, 200)

	// Project and an image service deployed at once.
	var prj api.Project
	owner.do("POST", "/api/v1/projects", api.CreateProjectRequest{Name: "shop"}, &prj, 200)
	eid := prj.Environments[0].ID
	var cs api.CreateServiceResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/services", api.CreateServiceRequest{Name: "db", DeployNow: true,
		Source: api.SourceConfig{Type: "image", Image: "postgres:17"}, Variables: map[string]string{"POSTGRES_PASSWORD": "${{secret(16)}}"}}, &cs, 200)
	dbID := cs.Services[0].ID
	if len(cs.Deployments) != 1 {
		t.Fatalf("Deploy Now made %d deployments", len(cs.Deployments))
	}

	// A staged service with a reference to db.
	owner.do("POST", "/api/v1/environments/"+eid+"/services", api.CreateServiceRequest{Name: "api", Source: api.SourceConfig{Type: "image", Image: "nginx:1.27"}}, &cs, 200)
	apiID := cs.Services[0].ID
	var vr api.VariablesResponse
	owner.do("PUT", "/api/v1/environments/"+eid+"/services/"+apiID+"/variables", api.SetVariablesRequest{Set: map[string]string{
		"DB_HOST": "${{db.FIFTY_PRIVATE_DOMAIN}}", "DB_PASSWORD": "${{db.POSTGRES_PASSWORD}}", "MODE": "prod"}}, &vr, 200)

	// Rename db: staged, and the commit rewrites the reference.
	name := "database"
	owner.do("PATCH", "/api/v1/environments/"+eid+"/services/"+dbID+"/info", api.UpdateServiceInfoRequest{Name: &name}, nil, 200)
	var ch api.ChangesResponse
	owner.do("GET", "/api/v1/environments/"+eid+"/changes", nil, &ch, 200)
	var rename *api.Change
	for i := range ch.Changes {
		if ch.Changes[i].Kind == "service.rename" {
			rename = &ch.Changes[i]
		}
	}
	if rename == nil || len(rename.Rewrites) == 0 {
		t.Fatalf("the rename lists no rewrites: %+v", ch.Changes)
	}
	var cr api.CommitResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/changes/commit", api.CommitRequest{Message: "Add api", Deploy: true}, &cr, 200)
	plan, ok := e.orch.LastPlan()
	if !ok || plan.CommitID != cr.CommitID || !plan.Deploy {
		t.Fatalf("fakeorch got plan %+v for commit %s", plan, cr.CommitID)
	}
	actions := map[string]string{}
	for _, it := range plan.Items {
		actions[it.ServiceID] = it.Action
	}
	if actions[apiID] != "create" || actions[dbID] != "update" {
		t.Fatalf("plan actions %+v", actions)
	}
	owner.do("GET", "/api/v1/environments/"+eid+"/changes", nil, &ch, 200)
	if len(ch.Changes) != 0 {
		t.Fatalf("%d changes left after the commit", len(ch.Changes))
	}

	// Reveal resolves the rewritten reference and is audited.
	var rv api.RevealResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/services/"+apiID+"/variables/reveal", api.RevealRequest{Names: []string{"DB_HOST", "DB_PASSWORD"}, Resolved: true}, &rv, 200)
	if rv.Values["DB_HOST"] != "database.internal" || len(rv.Values["DB_PASSWORD"]) != 16 {
		t.Fatalf("reveal %+v", rv.Values)
	}
	env, _, err := e.c.ResolveVariables(ctx, eid, apiID, map[string]string{"FIFTY_DEPLOYMENT_ID": "dep_x"})
	if err != nil || env["DB_HOST"] != "database.internal" || env["RAILWAY_PRIVATE_DOMAIN"] != "api.internal" || env["FIFTY_DEPLOYMENT_ID"] != "dep_x" {
		t.Fatalf("ResolveVariables %v %v", env, err)
	}
	var audit api.Page[api.AuditEntry]
	owner.do("GET", "/api/v1/audit", nil, &audit, 200)
	if !hasAction(audit.Items, "variables.reveal") || !hasAction(audit.Items, "changes.commit") {
		t.Fatalf("audit log lacks reveal or commit: %+v", audit.Items)
	}

	// API tokens.
	var tr api.CreateTokenResponse
	owner.do("POST", "/api/v1/tokens", api.CreateTokenRequest{Name: "ci", Scope: "account"}, &tr, 200)
	bot := e.client()
	bot.token = tr.Secret
	bot.do("GET", "/api/v1/me", nil, &me, 200)
	if me.User.Email != "owner@example.com" {
		t.Fatalf("token user %q", me.User.Email)
	}
	bot.do("GET", "/api/v1/environments/"+eid+"/canvas", nil, nil, 200)

	// CLI auth.
	cli := e.client()
	var start api.CLIAuthStart
	cli.do("POST", "/api/v1/cli/auth/start", nil, &start, 200)
	var poll api.CLIAuthPollResponse
	cli.do("POST", "/api/v1/cli/auth/poll", api.CLIAuthPoll{PollToken: start.PollToken}, &poll, 200)
	if poll.Status != "pending" {
		t.Fatalf("poll before approval: %q", poll.Status)
	}
	owner.do("POST", "/api/v1/cli/auth/approve", api.CLIAuthApprove{Code: start.Code}, nil, 204)
	cli.do("POST", "/api/v1/cli/auth/poll", api.CLIAuthPoll{PollToken: start.PollToken}, &poll, 200)
	if poll.Status != "approved" || poll.Token == "" {
		t.Fatalf("poll after approval: %+v", poll)
	}

	// Invite link.
	var inv api.Invite
	owner.do("POST", "/api/v1/invites", api.CreateInviteRequest{Role: api.RoleMember}, &inv, 200)
	itok := inv.URL[strings.LastIndex(inv.URL, "/")+1:]
	guest := e.client()
	var pv api.InvitePreview
	guest.do("GET", "/api/v1/invites/"+itok, nil, &pv, 200)
	if !pv.Valid {
		t.Fatalf("invite preview %+v", pv)
	}
	guest.do("POST", "/api/v1/invites/accept", api.AcceptInviteRequest{Token: itok, Email: "member@example.com", Name: "Mem", Password: "another long password"}, &me, 200)
	if me.User.Role != api.RoleMember {
		t.Fatalf("invited role %q", me.User.Role)
	}
	guest.do("GET", "/api/v1/audit", nil, nil, 403)
	guest.do("DELETE", "/api/v1/environments/"+eid+"/services/"+apiID, api.ConfirmRequest{Confirm: "api"}, nil, 403)

	// Forgot password with the fake mailer.
	owner.do("PUT", "/api/v1/settings/smtp", api.SMTPSettings{Host: "smtp.example.com", Port: 587, From: "noreply@example.com"}, nil, 200)
	anon := e.client()
	anon.do("POST", "/api/v1/auth/forgot", api.ForgotPasswordRequest{Email: "member@example.com"}, nil, 204)
	m, ok := e.mail.Last("member@example.com")
	if !ok {
		t.Fatal("no reset email")
	}
	rt := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(m.Text)
	if rt == nil {
		t.Fatalf("no token in %q", m.Text)
	}
	anon.do("POST", "/api/v1/auth/reset", api.ResetPasswordRequest{Token: rt[1], Password: "a brand new password"}, &me, 200)
	anon.do("POST", "/api/v1/auth/login", api.LoginRequest{Email: "member@example.com", Password: "a brand new password"}, &lr, 200)

	// Templates and domains.
	var tds api.TemplateDeployResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/templates/redis/deploy", api.TemplateDeployRequest{}, &tds, 200)
	if len(tds.Services) != 1 || tds.Services[0].Config.Template != "redis" {
		t.Fatalf("redis template %+v", tds.Services)
	}
	var dom api.Domain
	owner.do("POST", "/api/v1/environments/"+eid+"/services/"+apiID+"/domains", api.CreateDomainRequest{Kind: "generated"}, &dom, 200)
	if !strings.HasSuffix(dom.Domain, ".203-0-113-7.sslip.io") {
		t.Fatalf("generated domain %q", dom.Domain)
	}
	var cv api.Canvas
	owner.do("GET", "/api/v1/environments/"+eid+"/canvas", nil, &cv, 200)
	if len(cv.Services) != 3 || len(cv.Edges) == 0 {
		t.Fatalf("canvas has %d services and %d edges", len(cv.Services), len(cv.Edges))
	}
	var exp []byte
	{
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/v1/environments/"+eid+"/export", nil)
		resp, err := owner.h.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		exp, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if !bytes.Contains(exp, []byte("database:")) {
		t.Fatalf("export lacks the service: %s", exp)
	}
}

func hasAction(list []api.AuditEntry, action string) bool {
	for _, a := range list {
		if a.Action == action {
			return true
		}
	}
	return false
}
