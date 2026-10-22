// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/pkg/api"
)

// fake is a small stand-in for 5050d that records the calls it gets.
type fake struct {
	mu       sync.Mutex
	calls    []string
	bodies   map[string]string
	depState api.DeploymentStatus
	upload   []byte
}

func (f *fake) record(r *http.Request, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, k)
	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	f.bodies[k] = string(body)
}

func (f *fake) called(k string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == k {
			return true
		}
	}
	return false
}

const tok = "5050_testtoken"

var (
	fProject = api.Project{ID: "prj_1", Name: "shop", Environments: []api.Environment{{ID: "env_p", Name: "production"}, {ID: "env_s", Name: "staging"}}}
	fService = api.Service{ID: "svc_web", EnvironmentID: "env_p", Config: api.ServiceConfig{Name: "web"}, Status: api.StatusOnline,
		ActiveDeployment: "dep_0", Domains: []api.Domain{{ID: "dom_1", Domain: "web-shop.example.test", Port: 8080}}}
	fDB = api.Service{ID: "svc_pg", EnvironmentID: "env_p", Config: api.ServiceConfig{Name: "Postgres"}, Kind: api.KindDatabase}
)

func (f *fake) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.Me{User: api.User{Email: "ada@example.test", Role: api.RoleOwner}})
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) { js(w, []api.Project{fProject}) })
	mux.HandleFunc("GET /api/v1/environments/env_p/canvas", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.Canvas{Services: []api.Service{fService, fDB}})
	})
	mux.HandleFunc("PUT /api/v1/environments/env_p/services/svc_web/variables", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.VariablesResponse{})
	})
	mux.HandleFunc("POST /api/v1/environments/env_p/changes/commit", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.CommitResponse{CommitID: "cmt_1", Deployments: []api.Deployment{{ID: "dep_2"}}})
	})
	mux.HandleFunc("POST /api/v1/environments/env_p/services/svc_web/uploads", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/gzip" {
			http.Error(w, "bad type", 400)
		}
		js(w, api.UploadResponse{DeploymentID: "dep_1"})
	})
	mux.HandleFunc("GET /api/v1/deployments/dep_1/build-logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: logs\ndata: [{\"id\":\"1\",\"msg\":\"Step 1/3 building\"}]\n\n")
		fmt.Fprint(w, "data: [{\"id\":\"2\",\"msg\":\"Build done\"}]\n\n")
	})
	mux.HandleFunc("GET /api/v1/deployments/dep_1", func(w http.ResponseWriter, r *http.Request) {
		d := api.DeploymentDetail{Deployment: api.Deployment{ID: "dep_1", Status: f.depState}}
		if f.depState == api.DepFailed {
			d.Diagnosis = "Your app listens on 3000 but the target port is 8080."
		}
		js(w, d)
	})
	mux.HandleFunc("GET /api/v1/logs", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.LogsResponse{Lines: []api.LogLine{{Msg: "listening on 8080"}}})
	})
	mux.HandleFunc("GET /api/v1/environments/env_p/services/svc_web", func(w http.ResponseWriter, r *http.Request) { js(w, fService) })
	mux.HandleFunc("GET /api/v1/environments/env_p/services/svc_pg/connect-info", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.ConnectInfo{Client: "psql", PrivateVariable: "DATABASE_URL"})
	})
	mux.HandleFunc("POST /api/v1/environments/env_p/services/svc_pg/variables/reveal", func(w http.ResponseWriter, r *http.Request) {
		js(w, api.RevealResponse{Values: map[string]string{"DATABASE_URL": "postgresql://postgres:pw@postgres.internal:5432/railway"}})
	})
	mux.HandleFunc("GET /api/v1/environments/env_p/services/svc_pg/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("port") != "5432" {
			http.Error(w, "wrong port", 400)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		nc := websocket.NetConn(r.Context(), c, websocket.MessageBinary)
		_, _ = io.Copy(nc, nc) // echo
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			w.WriteHeader(401)
			js(w, api.Error{Error: "Log in first.", Code: api.CodeUnauthorized})
			return
		}
		var body []byte
		if strings.HasSuffix(r.URL.Path, "/uploads") {
			f.upload, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(nil))
		} else if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		f.record(r, body)
		mux.ServeHTTP(w, r)
	})
}

type harness struct {
	t   *testing.T
	f   *fake
	srv *httptest.Server
	dir string
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, f: &fake{depState: api.DepSuccess}, dir: t.TempDir()}
	h.srv = httptest.NewServer(h.f.handler(t))
	t.Cleanup(h.srv.Close)
	t.Setenv("FIFTY_CONFIG", filepath.Join(h.dir, "config.json"))
	t.Setenv("FIFTY_URL", "")
	t.Setenv("FIFTY_TOKEN", "")
	return h
}

func (h *harness) run(name string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	a := &App{In: strings.NewReader(""), Out: &out, Err: &errb, Cwd: h.dir}
	code := a.Run(name, args)
	return code, out.String(), errb.String()
}

func (h *harness) must(args ...string) string {
	h.t.Helper()
	code, out, errs := h.run("5050", args...)
	if code != 0 {
		h.t.Fatalf("5050 %v: exit %d\n%s%s", args, code, out, errs)
	}
	return out
}

func TestLoginLinkStatus(t *testing.T) {
	h := newHarness(t)
	if code, _, errs := h.run("5050", "status"); code == 0 || !strings.Contains(errs, "not logged in") {
		t.Fatalf("status before login: %d %s", code, errs)
	}
	if code, _, errs := h.run("5050", "login", h.srv.URL, "--token", "wrong"); code == 0 || !strings.Contains(errs, "refused") {
		t.Fatalf("bad token accepted: %s", errs)
	}
	out := h.must("login", h.srv.URL, "--token", tok)
	if !strings.Contains(out, "ada@example.test") {
		t.Fatal(out)
	}
	h.must("link", "-p", "shop", "-e", "production", "-s", "web")
	out = h.must("status")
	for _, want := range []string{"shop", "production", "web", "online", "https://web-shop.example.test"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	var ps []api.Project
	if err := json.Unmarshal([]byte(h.must("list", "--json")), &ps); err != nil || len(ps) != 1 {
		t.Errorf("list --json: %v %v", ps, err)
	}
}

func TestVariablesSetCommits(t *testing.T) {
	h := newHarness(t)
	h.must("login", h.srv.URL, "--token", tok)
	h.must("link", "-p", "shop", "-s", "web")
	h.must("variables", "set", "A=1", "B=two")
	k := "PUT /api/v1/environments/env_p/services/svc_web/variables"
	var req api.SetVariablesRequest
	if err := json.Unmarshal([]byte(h.f.bodies[k]), &req); err != nil || req.Set["A"] != "1" || req.Set["B"] != "two" {
		t.Fatalf("variables body %q", h.f.bodies[k])
	}
	var cr api.CommitRequest
	_ = json.Unmarshal([]byte(h.f.bodies["POST /api/v1/environments/env_p/changes/commit"]), &cr)
	if !cr.Deploy {
		t.Error("set did not commit with deploy")
	}
	// The railway alias maps --set to the same calls.
	h.f.bodies = nil
	if code, _, errs := h.run("railway", "variables", "--set", "C=3", "--skip-deploys"); code != 0 {
		t.Fatalf("railway variables --set: %s", errs)
	}
	_ = json.Unmarshal([]byte(h.f.bodies["POST /api/v1/environments/env_p/changes/commit"]), &cr)
	if cr.Deploy || !strings.Contains(h.f.bodies[k], `"C":"3"`) {
		t.Errorf("railway alias: %v", h.f.bodies)
	}
	if code, _, errs := h.run("railway", "docs"); code != 1 || !strings.Contains(errs, "railway docs is not available in 5050cloud") {
		t.Errorf("unavailable verb: %d %s", code, errs)
	}
}

func TestUp(t *testing.T) {
	h := newHarness(t)
	h.must("login", h.srv.URL, "--token", tok)
	h.must("link", "-p", "shop", "-s", "web")
	writeTree(t, h.dir, map[string]string{"server.js": "x", ".gitignore": "secret.env\n", "secret.env": "K=V"})
	out := h.must("up", "-m", "first")
	for _, want := range []string{"Step 1/3 building", "Build done", "listening on 8080", "Deployed: https://web-shop.example.test"} {
		if !strings.Contains(out, want) {
			t.Errorf("up output lacks %q:\n%s", want, out)
		}
	}
	names := strings.Join(listing(t, h.f.upload), " ")
	if !strings.Contains(names, "server.js") || strings.Contains(names, "secret.env") {
		t.Errorf("uploaded %s", names)
	}
	h.f.depState = api.DepFailed
	code, _, errs := h.run("5050", "up")
	if code != 1 || !strings.Contains(errs, "listens on 3000") {
		t.Errorf("failed up: %d %s", code, errs)
	}
}

func TestConnectTunnel(t *testing.T) {
	h := newHarness(t)
	h.must("login", h.srv.URL, "--token", tok)
	h.must("link", "-p", "shop")
	cl, _, _, _ := (&App{Cwd: h.dir}).client()
	tg := &Target{C: cl, Env: fProject.Environments[0], Service: &fDB}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tn, err := (&App{Err: io.Discard}).startTunnel(ctx, tg, 0, 5432)
	if err != nil {
		t.Fatal(err)
	}
	defer tn.close()
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tn.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("tunnel echo %q %v", buf, err)
	}
}

func TestConnectURLs(t *testing.T) {
	u, port, err := rewriteURL("postgresql://postgres:pw@postgres.internal:5432/railway", "psql", 6000)
	if err != nil || port != 5432 || u != "postgresql://postgres:pw@127.0.0.1:6000/railway" {
		t.Fatalf("%s %d %v", u, port, err)
	}
	_, port, _ = rewriteURL("redis://default:pw@redis.internal/0", "redis-cli", 1)
	if port != 6379 {
		t.Errorf("redis default port %d", port)
	}
	name, argv, env, _ := clientCommand("psql", u, []string{"-c", "select 1"})
	if name != "psql" || argv[0] != "postgresql://postgres@127.0.0.1:6000/railway" || argv[2] != "select 1" || env[0] != "PGPASSWORD=pw" {
		t.Errorf("psql: %v %v", argv, env)
	}
	_, argv, env, _ = clientCommand("mysql", "mysql://root:pw@127.0.0.1:6001/app", nil)
	if strings.Join(argv, " ") != "-h 127.0.0.1 -P 6001 -u root app" || env[0] != "MYSQL_PWD=pw" {
		t.Errorf("mysql: %v %v", argv, env)
	}
	_, argv, env, _ = clientCommand("redis-cli", "redis://default:pw@127.0.0.1:6002", nil)
	if strings.Join(argv, " ") != "-u redis://127.0.0.1:6002" || env[0] != "REDISCLI_AUTH=pw" {
		t.Errorf("redis: %v %v", argv, env)
	}
}

func TestDotPatch(t *testing.T) {
	p, _ := dotPatch("deploy.replicas", "3")
	b, _ := json.Marshal(p)
	if string(b) != `{"deploy":{"replicas":3}}` {
		t.Error(string(b))
	}
	p, _ = dotPatch("deploy.startCommand", "npm start")
	b, _ = json.Marshal(p)
	if string(b) != `{"deploy":{"startCommand":"npm start"}}` {
		t.Error(string(b))
	}
}

func TestHTTPFilter(t *testing.T) {
	got := httpFilter("@level:error", "get", ">=500", "/api v1", "r1")
	if got != `@level:error @method:GET @httpStatus:>=500 @path:"/api v1" @requestId:r1` {
		t.Error(got)
	}
}

func TestSSE(t *testing.T) {
	var got []string
	_ = readSSE(strings.NewReader(": hi\nevent: a\ndata: 1\ndata: 2\n\ndata: 3\n"), func(e string, d []byte) bool {
		got = append(got, e+"="+string(d))
		return true
	})
	if strings.Join(got, ",") != "a=1\n2,=3" {
		t.Errorf("%q", got)
	}
}
