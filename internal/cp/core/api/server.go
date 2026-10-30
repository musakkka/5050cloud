// SPDX-License-Identifier: AGPL-3.0-only

// Package api serves the REST API of docs/SPEC.md section 11 over core.Core
// and the orchestrator, plus /healthz, /readyz, the agent endpoints and the
// console single-page app. Every route of pkg/api.Routes is mounted with
// its role and audit action; the table is the contract.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core"
	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/pkg/api"
)

// CookieName is the session cookie. On HTTPS it is SecureCookieName instead: the
// __Host- prefix stops an app on a sibling subdomain from planting its own.
const (
	CookieName       = "5050_session"
	SecureCookieName = "__Host-5050_session"
)

// cookieName is the name for this install; the prefix needs Secure.
func (s *Server) cookieName() string {
	if s.C.File().Insecure {
		return CookieName
	}
	return SecureCookieName
}

// Login attempts allowed in 15 minutes. The per-account count is the one an
// attacker with many addresses meets. It also means enough of them can keep the
// real owner out for a while, so it is set well above what a person retyping
// passwords would reach.
const (
	loginPerAddressAccount = 10
	loginPerAddress        = 100
	loginPerAccount        = 50
)

// maxBody is the largest JSON body read; maxPublicBody is for routes open to
// anyone, which only ever take a few short fields.
const (
	maxBody       = 4 << 20
	maxPublicBody = 64 << 10
)

// consoleCSP limits the console to its own scripts and refuses framing, which is
// what the CLI approval page needs against clickjacking. form-action allows https
// because connecting a GitHub App posts a form to GitHub.
const consoleCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self' https:; frame-ancestors 'none'"

// Server is the HTTP handler.
type Server struct {
	C   *core.Core
	Web fs.FS // the built console (web.Dist sub "dist"); nil serves no SPA
	Log *slog.Logger

	mux          *http.ServeMux
	login        *auth.Limiter // per address and account
	loginAddress *auth.Limiter
	loginAccount *auth.Limiter
	forgot       *auth.Limiter
	handlers     map[string]handler
}

// Ctx is one request as a handler sees it.
type Ctx struct {
	W     http.ResponseWriter
	R     *http.Request
	A     core.Actor
	Route api.Route
	// Target names what an audited action touched (filled by handlers when better than the path).
	Target string
	Detail string
	s      *Server
}

type handler func(x *Ctx) (any, error)

// New builds the server and mounts every route.
func New(c *core.Core, web fs.FS, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{C: c, Web: web, Log: log, mux: http.NewServeMux(),
		login: auth.NewLimiter(loginPerAddressAccount, 15*time.Minute), loginAddress: auth.NewLimiter(loginPerAddress, 15*time.Minute),
		loginAccount: auth.NewLimiter(loginPerAccount, 15*time.Minute), forgot: auth.NewLimiter(5, time.Hour)}
	s.handlers = s.table()
	for _, rt := range api.Routes {
		h, ok := s.handlers[rt.Method+" "+rt.Path]
		if !ok {
			panic("api: no handler for " + rt.Method + " " + rt.Path)
		}
		p := "/api/v1" + rt.Path
		if rt.NoPrefix {
			p = rt.Path
		}
		s.mux.Handle(rt.Method+" "+p, s.wrap(rt, h))
	}
	s.mux.Handle("/api/agent/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o := s.C.Orchestrator()
		if o == nil {
			writeErr(w, fail.Unavailable("The orchestrator is not running."))
			return
		}
		o.AgentHandler().ServeHTTP(w, r)
	}))
	s.mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, fail.NotFound("There is no API route %s %s.", r.Method, r.URL.Path))
	}))
	s.mux.Handle("/", http.HandlerFunc(s.spa))
	return s
}

// Handlers reports whether a route has a handler (route-table test).
func (s *Server) Handlers() map[string]bool {
	out := map[string]bool{}
	for k := range s.handlers {
		out[k] = true
	}
	return out
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", consoleCSP)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) wrap(rt api.Route, h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x := &Ctx{W: w, R: r, Route: rt, s: s}
		x.A.IP = auth.ClientIP(r)
		mutating := r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutating && s.C.ReadOnly() && rt.Path != "/auth/logout" {
			writeErr(w, fail.Unavailable("The control plane is in read-only mode because its disk is nearly full. Free some space; it switches back on its own."))
			return
		}
		if rt.Role != api.Public {
			a, err := s.authenticate(r)
			if err != nil {
				writeErr(w, err)
				return
			}
			a.IP = x.A.IP
			x.A = a
			if mutating && a.TokenName == "" && !sameOrigin(r) {
				writeErr(w, fail.Forbidden("This request came from another site and was refused."))
				return
			}
			if err := s.authorize(x); err != nil {
				writeErr(w, err)
				return
			}
		} else {
			// A login or setup posted from another site would sign the victim in
			// as the attacker. Agents, the CLI and webhooks send no Origin.
			if mutating && !sameOrigin(r) {
				writeErr(w, fail.Forbidden("This request came from another site and was refused."))
				return
			}
			if a, err := s.authenticate(r); err == nil {
				a.IP = x.A.IP
				x.A = a
			}
		}
		out, err := h(x)
		if err != nil {
			if errors.Is(err, errHandled) {
				return
			}
			writeErr(w, err)
			return
		}
		if rt.Audit != "" && !rt.NoAudit && rt.Stream != "ws" {
			s.audit(x)
		}
		switch v := out.(type) {
		case nil:
			w.WriteHeader(http.StatusNoContent)
		case handled:
		case rawBody:
			w.Header().Set("Content-Type", v.ctype)
			_, _ = w.Write(v.data)
		default:
			writeJSON(w, http.StatusOK, out)
		}
	})
}

func (s *Server) audit(x *Ctx) { s.auditAs(x, x.Route.Audit) }

func (s *Server) auditAs(x *Ctx, action string) {
	t := x.Target
	if t == "" {
		t = x.R.URL.Path
	}
	label := x.A.Label()
	if label == "anonymous" && x.Route.Role == api.Public {
		label = "anonymous"
	}
	if err := s.C.Audit(context.WithoutCancel(x.R.Context()), api.AuditEntry{Actor: label, Action: action, Target: t, IP: x.A.IP, Detail: x.Detail}); err != nil {
		s.Log.Warn("cannot write an audit entry", "action", action, "err", err)
	}
}

// errHandled marks a response the handler already wrote.
var errHandled = errors.New("handled")

type handled struct{}

type rawBody struct {
	ctype string
	data  []byte
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (s *Server) authenticate(r *http.Request) (core.Actor, error) {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); h != "" {
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return core.Actor{}, fail.Unauthorized("Use an Authorization: Bearer <token> header.")
		}
		return s.C.TokenActor(ctx, strings.TrimSpace(tok))
	}
	if ck, err := r.Cookie(s.cookieName()); err == nil && ck.Value != "" {
		return s.C.SessionActor(ctx, ck.Value)
	}
	return core.Actor{}, fail.Unauthorized("Log in first.")
}

// authorize applies the route role on the install or on the project the path names.
func (s *Server) authorize(x *Ctx) error {
	want := core.LevelOf(x.Route.Role)
	ctx := x.R.Context()
	envID, projectID, err := s.scope(x)
	if err != nil {
		return err
	}
	if x.A.TokenScope == "environment" {
		if envID == "" || envID != x.A.TokenEnv {
			anywhere := x.Route.Method == http.MethodGet && (x.Route.Path == "/me" || x.Route.Path == "/install" || x.Route.Path == "/version")
			if !anywhere {
				return fail.Forbidden("This token only works for its environment.")
			}
		}
	}
	lvl := core.InstallLevel(x.A)
	if projectID != "" {
		if lvl, err = s.C.ProjectLevel(ctx, x.A, projectID); err != nil {
			return err
		}
		if lvl == core.None {
			return fail.NotFound("The project does not exist or you have no access to it.")
		}
	}
	if lvl < want {
		return fail.Forbidden("Your role does not allow this. Ask an admin.")
	}
	return nil
}

// scope finds the environment and project a request touches.
func (s *Server) scope(x *Ctx) (envID, projectID string, err error) {
	ctx := x.R.Context()
	r := x.R
	if eid := r.PathValue("eid"); eid != "" {
		env, prj, err := s.C.Environment(ctx, eid)
		if err != nil {
			return "", "", err
		}
		return env.ID, prj.ID, nil
	}
	if pid := r.PathValue("pid"); pid != "" {
		if _, err := s.C.Project(ctx, pid); err != nil {
			return "", "", err
		}
		return "", pid, nil
	}
	if e := r.URL.Query().Get("environment"); e != "" && (strings.HasPrefix(x.Route.Path, "/logs") || x.Route.Path == "/events") {
		env, prj, err := s.C.Environment(ctx, e)
		if err != nil {
			return "", "", err
		}
		return env.ID, prj.ID, nil
	}
	var env string
	switch {
	case r.PathValue("did") != "":
		o := s.C.Orchestrator()
		if o == nil {
			return "", "", fail.Unavailable("The orchestrator is not running.")
		}
		d, err := o.Deployment(ctx, r.PathValue("did"))
		if err != nil {
			return "", "", err
		}
		env = d.EnvironmentID
	case strings.HasPrefix(x.Route.Path, "/domains/"):
		env, err = s.C.DomainEnv(ctx, r.PathValue("id"))
	case strings.HasPrefix(x.Route.Path, "/monitors/"):
		env, err = s.C.MonitorEnv(ctx, r.PathValue("id"))
	case strings.HasPrefix(x.Route.Path, "/commits/"):
		env, err = s.C.CommitEnv(ctx, r.PathValue("id"))
	case strings.HasPrefix(x.Route.Path, "/webhooks/"):
		pid, err := s.C.WebhookProject(ctx, r.PathValue("id"))
		return "", pid, err
	}
	if err != nil {
		return "", "", err
	}
	if env != "" {
		return env, s.C.EnvProject(ctx, env), nil
	}
	return "", "", nil
}

// ---------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code, body := fail.ToAPI(err)
	writeJSON(w, code, body)
}

// decode reads a JSON body into v.
func (x *Ctx) decode(v any) error {
	limit := int64(maxBody)
	if x.Route.Role == api.Public {
		limit = maxPublicBody
	}
	body := http.MaxBytesReader(x.W, x.R.Body, limit)
	b, err := io.ReadAll(body)
	if err != nil {
		return fail.Validation("body", "The request body is too large or could not be read.")
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fail.Validation("body", "The request body is not valid JSON: %s.", strings.TrimSuffix(err.Error(), "."))
	}
	return nil
}

func (x *Ctx) text(limit int64) ([]byte, error) {
	b, err := io.ReadAll(http.MaxBytesReader(x.W, x.R.Body, limit))
	if err != nil {
		return nil, fail.Validation("body", "The request body is too large or could not be read.")
	}
	return b, nil
}

func (x *Ctx) p(name string) string { return x.R.PathValue(name) }
func (x *Ctx) q(name string) string { return x.R.URL.Query().Get(name) }

func (x *Ctx) qint(name string, def int) int {
	if v, err := strconv.Atoi(x.q(name)); err == nil {
		return v
	}
	return def
}

func (x *Ctx) qint64(name string) int64 {
	v, _ := strconv.ParseInt(x.q(name), 10, 64)
	return v
}

func (x *Ctx) orch() (orchestrator, error) {
	o := x.s.C.Orchestrator()
	if o == nil {
		return nil, fail.Unavailable("The orchestrator is not running.")
	}
	return o, nil
}

// timeRange reads range= (1h, 6h, 1d, 7d, 30d) or from=/until= (Unix ms).
func (x *Ctx) timeRange() (int64, int64) {
	now := x.s.C.Now()
	from, until := x.qint64("from"), x.qint64("until")
	if until == 0 {
		until = now
	}
	if from == 0 {
		d := map[string]int64{"1h": 3600e3, "6h": 6 * 3600e3, "1d": 86400e3, "24h": 86400e3, "7d": 7 * 86400e3, "30d": 30 * 86400e3}[x.q("range")]
		if d == 0 {
			d = 3600e3
		}
		from = until - d
	}
	return from, until
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, userID string) error {
	v, err := s.C.NewSession(r.Context(), userID, auth.ClientIP(r), r.UserAgent())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: v, Path: "/", HttpOnly: true, Secure: !s.C.File().Insecure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(core.SessionTTL / 1000)})
	return nil
}

// ---------------------------------------------------------------- SPA

func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	if s.Web == nil {
		http.Error(w, "The console is not built into this binary.", http.StatusNotFound)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p != "" {
		if f, err := s.Web.Open(p); err == nil {
			st, err := f.Stat()
			_ = f.Close()
			if err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeFileFS(w, r, s.Web, p)
				return
			}
		}
	}
	b, err := fs.ReadFile(s.Web, "index.html")
	if err != nil {
		http.Error(w, "The console is not built into this binary.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
