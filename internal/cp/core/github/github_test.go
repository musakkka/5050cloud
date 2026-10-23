// SPDX-License-Identifier: AGPL-3.0-only

package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSignature(t *testing.T) {
	body := []byte(`{"zen":"x"}`)
	sig := Sign("s3cret", body)
	if !VerifySignature("s3cret", body, sig) {
		t.Fatal("valid signature rejected")
	}
	for _, bad := range []string{"", "sha1=abc", "sha256=zz", Sign("other", body)} {
		if VerifySignature("s3cret", body, bad) {
			t.Fatalf("bad signature %q accepted", bad)
		}
	}
	if VerifySignature("", body, sig) {
		t.Fatal("an empty secret must never verify")
	}
}

func TestParsePush(t *testing.T) {
	body := `{"ref":"refs/heads/main","before":"0000000000000000000000000000000000000000","after":"abc","created":true,"forced":false,
	"repository":{"full_name":"acme/api","default_branch":"main","private":true},
	"commits":[{"added":["src/a.go"],"modified":["README.md"],"removed":[]},{"added":["src/a.go"],"modified":[],"removed":["old.txt"]}],
	"head_commit":{"id":"abc","message":"Fix it","url":"https://github.com/acme/api/commit/abc","timestamp":"2026-10-01T10:00:00Z","author":{"name":"Ada","username":"ada"}}}`
	p, err := ParsePush([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch != "main" || p.Repo != "acme/api" || !p.FirstPush() || !p.Private || p.Head.Author != "ada" || p.Head.Time == 0 {
		t.Fatalf("push = %+v", p)
	}
	if !reflect.DeepEqual(p.Files, []string{"src/a.go", "README.md", "old.txt"}) {
		t.Fatalf("files = %v", p.Files)
	}
	tag, _ := ParsePush([]byte(`{"ref":"refs/tags/v1","before":"1","head_commit":{"author":{"name":"N"}}}`))
	if tag.Branch != "" || tag.FirstPush() || tag.Head.Author != "N" {
		t.Fatalf("tag push = %+v", tag)
	}
	if _, err := ParsePush([]byte("{")); err == nil {
		t.Fatal("bad JSON accepted")
	}
}

func TestWatchPaths(t *testing.T) {
	cases := []struct {
		patterns []string
		files    []string
		want     bool
	}{
		{nil, []string{"x"}, true},
		{[]string{"/src/**"}, []string{"src/a/b.go"}, true},
		{[]string{"/src/**"}, []string{"docs/a.md"}, false},
		{[]string{"src/**/*.go"}, []string{"src/a/b.go"}, true},
		{[]string{"*.md"}, []string{"docs/deep/README.md"}, true},
		{[]string{"**", "!**/*.md"}, []string{"docs/README.md"}, false},
		{[]string{"**", "!**/*.md"}, []string{"docs/README.md", "main.go"}, true},
		{[]string{"/api"}, []string{"api/server.go"}, true},
		{[]string{"/api/"}, []string{"apiv2/server.go"}, false},
		{[]string{"/src/?.go"}, []string{"src/a.go"}, true},
		{[]string{"# comment", ""}, []string{"a"}, false},
		{[]string{"/"}, []string{"anything"}, true},
		{[]string{"src/[a"}, []string{"src/a"}, false},
		{[]string{"/a/**/z"}, []string{"a/z"}, true},
		{[]string{"/a/**/z"}, []string{"a/b/c/z"}, true},
		{[]string{"/a/b"}, []string{"a"}, false},
	}
	for _, c := range cases {
		if got := MatchWatch(c.patterns, c.files); got != c.want {
			t.Errorf("MatchWatch(%v, %v) = %v", c.patterns, c.files, got)
		}
	}
}

func TestParseRepo(t *testing.T) {
	cases := map[string]string{
		"acme/api":                              "acme/api",
		"https://github.com/acme/api":           "acme/api",
		"https://github.com/acme/api.git":       "acme/api",
		"http://www.github.com/acme/api/":       "acme/api",
		"github.com/acme/my.repo":               "acme/my.repo",
		"https://github.com/acme/api/tree/main": "acme/api",
	}
	for in, want := range cases {
		r, ok := ParseRepo(in)
		if !ok || r.FullName != want {
			t.Errorf("ParseRepo(%q) = %+v %v", in, r, ok)
		}
	}
	for _, bad := range []string{"", "acme", "https://gitlab.com/a/b", "a/b/c", "../x", "a b/c", "https://github.com/a"} {
		if _, ok := ParseRepo(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	r, _ := ParseRepo("https://github.com/acme/api")
	if r.CloneURL() != "https://github.com/acme/api.git" {
		t.Fatal(r.CloneURL())
	}
	r2, _ := ParseRepo("acme/api")
	if r2.URL != "" || r2.CloneURL() != "https://github.com/acme/api.git" {
		t.Fatal("owner/name has no URL")
	}
	if !SameRepo("ACME/api", "https://github.com/acme/api.git") || SameRepo("a/b", "a/c") || SameRepo("", "a/b") {
		t.Fatal("SameRepo")
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScanNode(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"package.json":                        `{"name":"shop","scripts":{"start":"node server.js"},"workspaces":["apps/*","packages/*"],"packageManager":"pnpm@9"}`,
		".env.example":                        "PORT=4000\nDATABASE_URL=postgres://localhost/dev\n",
		".env":                                "SECRET_KEY=real\n",
		"sub/.env":                            "IGNORED=1\n",
		"railway.json":                        `{"deploy":{"startCommand":"node server.js","healthcheckPath":"/h"}}`,
		"nixpacks.toml":                       "[start]\ncmd = \"node x.js\"\n",
		"apps/web/package.json":               `{"name":"web","scripts":{"start":"next start"}}`,
		"apps/admin/package.json":             `{"name":"admin","scripts":{"dev":"vite"}}`,
		"packages/eslint-config/package.json": `{"name":"eslint-config","main":"index.js"}`,
		"packages/ui/package.json":            `{"name":"ui"}`,
		"node_modules/x/package.json":         `{"name":"x","main":"y"}`,
		".git/config":                         "x",
	})
	res, err := Scan(context.Background(), DirRepo{Root: dir}, "/")
	if err != nil {
		t.Fatal(err)
	}
	if res.Builder != "railpack" || res.Provider != "node" || res.StartCommand != "pnpm start" || res.SuggestedPort != 4000 {
		t.Fatalf("scan = %+v", res)
	}
	if !reflect.DeepEqual(res.EnvNames, []string{"DATABASE_URL", "PORT", "SECRET_KEY"}) {
		t.Fatalf("env names = %v", res.EnvNames)
	}
	if _, leaked := res.EnvExamples["SECRET_KEY"]; leaked || res.EnvExamples["PORT"] != "4000" {
		t.Fatalf("examples = %v", res.EnvExamples)
	}
	if len(res.ConfigFiles) != 2 || res.ConfigFiles[0].Path != "railway.json" || !reflect.DeepEqual(res.ConfigFiles[0].Sets, []string{"deploy.healthcheckPath", "deploy.startCommand"}) || res.ConfigFiles[1].Sets[0] != "deploy.startCommand" {
		t.Fatalf("config files = %+v", res.ConfigFiles)
	}
	var pk []string
	for _, p := range res.Packages {
		pk = append(pk, p.Name+"@"+p.RootDir)
	}
	if !reflect.DeepEqual(pk, []string{"admin@/apps/admin", "web@/apps/web"}) {
		t.Fatalf("packages = %v", pk)
	}
	if res.Packages[1].StartCommand != "npm start" || res.Packages[1].WatchPaths[0] != "/apps/web/**" {
		t.Fatalf("package = %+v", res.Packages[1])
	}
}

func TestScanOther(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"svc/Dockerfile":            "FROM alpine\nEXPOSE 9000\n",
		"svc/railway.toml":          "[build]\nbuilder = \"DOCKERFILE\"\n",
		"svc/.env.sample":           "A=1\n",
		"go.mod":                    "module x\n",
		"pnpm-workspace.yaml":       "packages:\n  - 'svc/apps/*'\n",
		"svc/apps/api/package.json": `{"name":"api","scripts":{"start":"x"}}`,
		"svc/package.json":          `{"name":"svc","main":"index.js"}`,
		"svc/yarn.lock":             "",
	})
	res, err := Scan(context.Background(), DirRepo{Root: dir}, "/svc")
	if err != nil {
		t.Fatal(err)
	}
	if res.Builder != "dockerfile" || !res.Dockerfile || res.SuggestedPort != 9000 || res.StartCommand != "node index.js" {
		t.Fatalf("scan = %+v", res)
	}
	if len(res.ConfigFiles) != 1 || res.ConfigFiles[0].Path != "svc/railway.toml" {
		t.Fatalf("configs = %+v", res.ConfigFiles)
	}
	if len(res.Packages) != 0 {
		t.Fatalf("packages outside the root glob must not match: %+v", res.Packages)
	}
	for provider, file := range map[string]string{"go": "go.mod", "python": "requirements.txt", "rust": "Cargo.toml", "staticfile": "index.html"} {
		d := writeTree(t, map[string]string{file: "x"})
		r, err := Scan(context.Background(), DirRepo{Root: d}, "")
		if err != nil || r.Provider != provider || r.SuggestedPort != defaultPort(provider) {
			t.Errorf("%s: %+v %v", file, r, err)
		}
	}
	if defaultPort("unknown") != 0 {
		t.Fatal("defaultPort")
	}
	if _, err := Scan(context.Background(), DirRepo{Root: "/does/not/exist"}, ""); err != nil {
		t.Fatal("a missing directory scans as empty")
	}
	if parsePackageJSON([]byte("{")) != nil {
		t.Fatal("bad package.json")
	}
	p := parsePackageJSON([]byte(`{"workspaces":{"packages":["a/*"]},"scripts":{"start":"x"},"packageManager":"bun@1"}`))
	if !reflect.DeepEqual(p.Workspaces, []string{"a/*"}) || startCommand(p, nil, "") != "bun run start" {
		t.Fatalf("workspaces object: %+v", p)
	}
	if packageManager(&packageJSON{PackageManager: "yarn@4"}, nil, "") != "yarn" || packageManager(&packageJSON{}, []string{"bun.lock"}, "") != "bun" || packageManager(&packageJSON{}, []string{"pnpm-lock.yaml"}, "") != "pnpm" {
		t.Fatal("packageManager")
	}
	if !IsEnvFile(".env.staging") || IsEnvFile("env") {
		t.Fatal("IsEnvFile")
	}
}

// fakeGitHub serves the endpoints the client uses.
func fakeGitHub(t *testing.T, limited *bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token-" + r.PathValue("id"), "expires_at": time.Now().Add(time.Hour)})
	})
	mux.HandleFunc("GET /app/installations", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":7,"account":{"login":"acme"}}]`))
	})
	mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[{"full_name":"acme/api","private":true,"default_branch":"main","pushed_at":"2026-10-01T00:00:00Z"}]}`))
	})
	mux.HandleFunc("GET /repos/acme/api/branches", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"main"},{"name":"dev"}]`))
	})
	mux.HandleFunc("GET /repos/acme/api", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"default_branch":"main","private":true}`))
	})
	mux.HandleFunc("GET /repos/acme/api/git/trees/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if *limited {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		_, _ = w.Write([]byte(`{"tree":[{"path":"package.json","type":"blob"},{"path":"src","type":"tree"}]}`))
	})
	mux.HandleFunc("GET /repos/acme/api/contents/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.raw" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"name":"api","scripts":{"start":"node a.js"}}`))
	})
	mux.HandleFunc("GET /repos/acme/api/commits/{ref}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sha":"abc123","html_url":"https://github.com/acme/api/commit/abc123","commit":{"message":"m","author":{"name":"Ada","date":"2026-10-01T00:00:00Z"}},"author":{"login":"ada"}}`))
	})
	mux.HandleFunc("GET /repos/acme/api/compare/{spec}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[{"filename":"a.go"}]}`))
	})
	mux.HandleFunc("POST /app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("code") != "good" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"slug":"5050cloud-test","name":"5050cloud-test","client_id":"cid","client_secret":"cs","webhook_secret":"ws","pem":"PEM","owner":{"login":"acme"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testApp(t *testing.T) App {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return App{ID: 42, Slug: "x", PEM: string(p)}
}

func TestClient(t *testing.T) {
	limited := false
	srv := fakeGitHub(t, &limited)
	c := NewClient(srv.URL, nil)
	ctx := context.Background()
	app := testApp(t)
	jwt, err := AppJWT(app, time.Now())
	if err != nil || strings.Count(jwt, ".") != 2 {
		t.Fatalf("jwt = %q %v", jwt, err)
	}
	tok, err := c.InstallationToken(ctx, app, 7)
	if err != nil || tok != "inst-token-7" {
		t.Fatalf("token = %q %v", tok, err)
	}
	if again, _ := c.InstallationToken(ctx, app, 7); again != tok {
		t.Fatal("token not cached")
	}
	inst, err := c.Installations(ctx, app)
	if err != nil || len(inst) != 1 || inst[0].Account != "acme" {
		t.Fatalf("installations = %v %v", inst, err)
	}
	repos, err := c.Repos(ctx, tok, 7)
	if err != nil || len(repos) != 1 || !repos[0].Private || repos[0].Installation != 7 {
		t.Fatalf("repos = %v %v", repos, err)
	}
	br, err := c.Branches(ctx, tok, "acme/api")
	if err != nil || !reflect.DeepEqual(br, []string{"main", "dev"}) {
		t.Fatalf("branches = %v %v", br, err)
	}
	def, priv, err := c.RepoInfo(ctx, "", "acme/api")
	if err != nil || def != "main" || !priv {
		t.Fatal("RepoInfo")
	}
	cm, err := c.CommitInfo(ctx, tok, "acme/api", "main")
	if err != nil || cm.SHA != "abc123" || cm.Author != "ada" {
		t.Fatalf("commit = %+v %v", cm, err)
	}
	files, err := c.CompareFiles(ctx, tok, "acme/api", "a", "b")
	if err != nil || files[0] != "a.go" {
		t.Fatal("CompareFiles")
	}
	created, err := c.ConvertManifest(ctx, "good")
	if err != nil || created.ID != 42 || created.WebhookSecret != "ws" || created.Owner != "acme" {
		t.Fatalf("manifest = %+v %v", created, err)
	}
	if _, err := c.ConvertManifest(ctx, "bad"); !NotFound(err) {
		t.Fatalf("expected 404, got %v", err)
	}
	res, err := ScanRemote(ctx, c, Git{}, RepoRef{FullName: "acme/api"}, tok, "main", "/")
	if err != nil || res.StartCommand != "npm start" {
		t.Fatalf("api scan = %+v %v", res, err)
	}
	limited = true
	if _, err := ScanRemote(ctx, c, Git{Bin: "/nonexistent/git"}, RepoRef{FullName: "acme/api"}, "", "main", "/"); err == nil {
		t.Fatal("expected the clone fallback to run and fail without git")
	}
	if _, err := (&APIRepo{Client: c, Repo: "acme/api", Ref: "main"}).Files(ctx); !RateLimited(err) {
		t.Fatalf("expected rate limited, got %v", err)
	}
	if _, err := AppJWT(App{PEM: "nope"}, time.Now()); err == nil {
		t.Fatal("bad PEM accepted")
	}
	if _, err := parseKey(string(pem.EncodeToMemory(&pem.Block{Type: "X", Bytes: []byte("junk")}))); err == nil {
		t.Fatal("junk key accepted")
	}
}

func TestManifest(t *testing.T) {
	m := Manifest("My Install!", "https://console.example.com", "/new")
	if m["name"] != "5050cloud-my-install" {
		t.Fatalf("name = %v", m["name"])
	}
	hook := m["hook_attributes"].(map[string]any)
	if hook["url"] != "https://console.example.com/api/v1/github/webhook" || m["redirect_url"] != "https://console.example.com/api/v1/github/app/callback" {
		t.Fatalf("manifest = %v", m)
	}
	if FormURL("", "s") != "https://github.com/settings/apps/new?state=s" || !strings.Contains(FormURL("acme", "s"), "/organizations/acme/") {
		t.Fatal("FormURL")
	}
	if InstallURL("x") != "https://github.com/apps/x/installations/new" || slugify("!!") != "install" {
		t.Fatal("InstallURL / slugify")
	}
	long := Manifest(strings.Repeat("a", 60), "https://c", "")
	if len(long["name"].(string)) > 34 {
		t.Fatal("app names are capped")
	}
}

func TestGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	src := writeTree(t, map[string]string{"package.json": `{"scripts":{"start":"x"}}`})
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	g := Git{}
	url := "file://" + src
	sha, br, err := g.LsRemote(ctx, url, "")
	if err != nil || len(sha) != 40 || br != "main" {
		t.Fatalf("ls-remote HEAD = %q %q %v", sha, br, err)
	}
	sha2, _, err := g.LsRemote(ctx, url, "main")
	if err != nil || sha2 != sha {
		t.Fatalf("ls-remote main = %q %v", sha2, err)
	}
	if _, _, err := g.LsRemote(ctx, url, "nope"); !errors.Is(err, ErrNoBranch) {
		t.Fatalf("missing branch: %v", err)
	}
	heads, err := g.Heads(ctx, url)
	if err != nil || !reflect.DeepEqual(heads, []string{"main"}) {
		t.Fatalf("heads = %v %v", heads, err)
	}
	dir, err := g.ShallowClone(ctx, url, "main")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(dir)) }()
	res, err := Scan(ctx, DirRepo{Root: dir}, "")
	if err != nil || res.Provider != "node" {
		t.Fatalf("clone scan = %+v %v", res, err)
	}
	if _, err := g.ShallowClone(ctx, "file:///does/not/exist", ""); err == nil {
		t.Fatal("clone of a missing repo must fail")
	}
	if _, _, err := g.LsRemote(ctx, "file:///does/not/exist", ""); err == nil {
		t.Fatal("ls-remote of a missing repo must fail")
	}
	if _, err := g.Heads(ctx, "file:///does/not/exist"); err == nil {
		t.Fatal("heads of a missing repo must fail")
	}
	if firstLineOf("a\nb") != "a" {
		t.Fatal("firstLineOf")
	}
}

func TestRepoTokenIsScopedToOneRepository(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "repo-token", "expires_at": time.Now().Add(time.Hour)})
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, nil)
	tok, err := c.RepoToken(context.Background(), testApp(t), 7, "app")
	if err != nil || tok != "repo-token" {
		t.Fatalf("token = %q %v", tok, err)
	}
	if repos, _ := got["repositories"].([]any); len(repos) != 1 || repos[0] != "app" {
		t.Errorf("repositories = %v, want only app", got["repositories"])
	}
	if perms, _ := got["permissions"].(map[string]any); len(perms) != 1 || perms["contents"] != "read" {
		t.Errorf("permissions = %v, want contents:read only", got["permissions"])
	}
}
