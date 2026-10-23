// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

type tarEntry struct {
	name, link, body string
	typ              byte
}

func makeTar(t testing.TB, gz bool, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w io.Writer = &buf
	var zw *gzip.Writer
	if gz {
		zw = gzip.NewWriter(&buf)
		w = zw
	}
	tw := tar.NewWriter(w)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: e.typ, Mode: 0o644}
		if e.typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if e.typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if e.typ == tar.TypeXGlobalHeader {
			h = &tar.Header{Typeflag: e.typ, PAXRecords: map[string]string{"comment": "x"}}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func TestExtractArchiveSafe(t *testing.T) {
	dir := t.TempDir()
	data := makeTar(t, true,
		tarEntry{name: "./", typ: tar.TypeDir},
		tarEntry{name: "src/", typ: tar.TypeDir},
		tarEntry{name: "src/index.js", body: "console.log(1)", typ: tar.TypeReg},
		tarEntry{name: "link-inside", link: "src/index.js", typ: tar.TypeSymlink},
		tarEntry{name: "src/up", link: "../link-inside", typ: tar.TypeSymlink},
		tarEntry{name: "dangling", link: "does/not/exist", typ: tar.TypeSymlink},
		tarEntry{name: "hard", link: "src/index.js", typ: tar.TypeLink},
		tarEntry{name: "pax", typ: tar.TypeXGlobalHeader},
	)
	if err := extractArchive(bytes.NewReader(data), dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "src", "up"))
	if err != nil || string(got) != "console.log(1)" {
		t.Errorf("link chain: %q %v", got, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "hard")); string(got) != "console.log(1)" {
		t.Errorf("hard link: %q", got)
	}
	// Plain tar works too.
	if err := extractArchive(bytes.NewReader(makeTar(t, false, tarEntry{name: "a.txt", body: "a", typ: tar.TypeReg})), t.TempDir()); err != nil {
		t.Errorf("plain tar: %v", err)
	}
}

func TestExtractArchiveRejects(t *testing.T) {
	cases := map[string][]tarEntry{
		"dot dot":            {{name: "../evil", body: "x", typ: tar.TypeReg}},
		"nested dot dot":     {{name: "a/../../evil", body: "x", typ: tar.TypeReg}},
		"absolute":           {{name: "/etc/evil", body: "x", typ: tar.TypeReg}},
		"windows drive":      {{name: "C:/evil", body: "x", typ: tar.TypeReg}},
		"backslash dot dot":  {{name: `a\..\..\evil`, body: "x", typ: tar.TypeReg}},
		"absolute symlink":   {{name: "l", link: "/etc/passwd", typ: tar.TypeSymlink}},
		"escaping symlink":   {{name: "a/l", link: "../../etc", typ: tar.TypeSymlink}},
		"write through link": {{name: "d", link: ".", typ: tar.TypeSymlink}, {name: "d/x", body: "x", typ: tar.TypeReg}},
		"link via link": {
			{name: "s", link: ".", typ: tar.TypeSymlink},
			{name: "t", link: "s/..", typ: tar.TypeSymlink},
		},
		"later link changes earlier target": {
			{name: "t", link: "q/..", typ: tar.TypeSymlink},
			{name: "q", link: ".", typ: tar.TypeSymlink},
		},
		"hard link outside":    {{name: "h", link: "../x", typ: tar.TypeLink}},
		"hard link to nothing": {{name: "h", link: "missing", typ: tar.TypeLink}},
		"device":               {{name: "dev", typ: tar.TypeChar}},
		"fifo":                 {{name: "fifo", typ: tar.TypeFifo}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "src")
			err := extractArchive(bytes.NewReader(makeTar(t, true, entries...)), dir)
			if err == nil {
				t.Fatal("the archive was accepted")
			}
			if !strings.HasSuffix(err.Error(), ".") {
				t.Errorf("error is not a sentence: %q", err)
			}
			if _, err := os.Lstat(filepath.Join(root, "evil")); err == nil {
				t.Errorf("a file was written outside the target")
			}
		})
	}
}

func TestExtractArchiveNotTar(t *testing.T) {
	err := extractArchive(strings.NewReader("this is not an archive at all, just text that is long enough"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a valid tar file") {
		t.Errorf("err = %v", err)
	}
	var gz bytes.Buffer
	gz.Write([]byte{0x1f, 0x8b, 0, 0})
	err = extractArchive(&gz, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Errorf("err = %v", err)
	}
}

// FuzzExtractArchive: whatever the archive, nothing lands outside the
// target and every link in the result resolves inside it.
func FuzzExtractArchive(f *testing.F) {
	f.Add(makeTar(f, false, tarEntry{name: "a", body: "x", typ: tar.TypeReg}))
	f.Add(makeTar(f, false, tarEntry{name: "../a", body: "x", typ: tar.TypeReg}))
	f.Add(makeTar(f, false, tarEntry{name: "l", link: "..", typ: tar.TypeSymlink}, tarEntry{name: "l/x", body: "x", typ: tar.TypeReg}))
	f.Add(makeTar(f, true, tarEntry{name: "s", link: ".", typ: tar.TypeSymlink}, tarEntry{name: "t", link: "s/../..", typ: tar.TypeSymlink}))
	f.Fuzz(func(t *testing.T, data []byte) {
		root := t.TempDir()
		dir := filepath.Join(root, "in")
		sentinel := filepath.Join(root, "outside")
		if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := extractArchive(bytes.NewReader(data), dir)
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.Name() != "in" && e.Name() != "outside" {
				t.Fatalf("an entry %s was written outside the target", e.Name())
			}
		}
		if got, _ := os.ReadFile(sentinel); string(got) != "keep" {
			t.Fatal("a file outside the target changed")
		}
		if err != nil {
			return
		}
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil //nolint:nilerr // a vanished entry is not a finding
			}
			if info.Mode()&os.ModeSymlink == 0 {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			if resolveInside(dir, filepath.ToSlash(rel)) != nil {
				t.Fatalf("symlink %s resolves outside the target", rel)
			}
			return nil
		})
	})
}

func TestFetchTarball(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"package.json": "{}"})
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write(archive)
		case "/big":
			w.Header().Set("Content-Length", "600000000")
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "gone", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	b := testBuilder(t, nil, nil, func(o *Options) { o.HostToken = func() string { return "host-token-1" } })
	log, _ := collect()
	dir := filepath.Join(t.TempDir(), "src")
	if err := b.fetchTarball(context.Background(), srv.URL+"/ok", dir, log); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer host-token-1" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		t.Errorf("not extracted: %v", err)
	}
	if err := b.fetchTarball(context.Background(), srv.URL+"/big", t.TempDir(), log); !errors.Is(err, errArchiveTooLarge) {
		t.Errorf("big: %v", err)
	}
	if err := b.fetchTarball(context.Background(), srv.URL+"/missing", t.TempDir(), log); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing: %v", err)
	}
	if err := b.fetchTarball(context.Background(), "ftp://x/y", t.TempDir(), log); err == nil {
		t.Errorf("ftp must be refused")
	}
}

func TestCountingReaderLimit(t *testing.T) {
	r := &countingReader{r: strings.NewReader(strings.Repeat("x", 100)), limit: 10}
	_, err := io.ReadAll(r)
	if !errors.Is(err, errReadLimit) {
		t.Errorf("err = %v", err)
	}
}

func TestFetchGitArgvAndEnv(t *testing.T) {
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, nil)
	src := &proto.GitSource{URL: "https://github.com/acme/app.git", Token: "ghs_installationtoken123", Commit: strings.Repeat("a", 40), Branch: "main"}
	log, lines := collect()
	if err := b.fetchGit(context.Background(), src, filepath.Join(t.TempDir(), "src"), log); err != nil {
		t.Fatal(err)
	}
	calls := r.find(func(cmdSpec) bool { return true })
	if len(calls) < 4 {
		t.Fatalf("calls = %d", len(calls))
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + src.Token))
	for _, c := range calls {
		joined := strings.Join(c.Args, " ")
		if strings.Contains(joined, src.Token) || strings.Contains(joined, basic) {
			t.Errorf("the token is on argv: %s", joined)
		}
		if v, _ := envValue(c.Env, "GIT_CONFIG_VALUE_0"); v != "Authorization: Basic "+basic {
			t.Errorf("token header missing from env of %s", joined)
		}
		if v, _ := envValue(c.Env, "GIT_TERMINAL_PROMPT"); v != "0" {
			t.Errorf("prompts must be off")
		}
	}
	fetch := strings.Join(calls[2].Args, " ")
	if !strings.Contains(fetch, "fetch --quiet --no-tags --depth 1 origin "+src.Commit) {
		t.Errorf("fetch argv = %s", fetch)
	}
	if strings.Contains(strings.Join(lines(), "\n"), src.Token) {
		t.Errorf("the token is in the log")
	}
}

func TestFetchGitFallsBackToBranch(t *testing.T) {
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, _ func(string)) (bool, error) {
		j := strings.Join(c.Args, " ")
		if strings.Contains(j, "--depth 1 origin") {
			return true, &exitError{Cmd: "git", Code: 128, Last: "fatal: remote error: upload-pack: not our ref"}
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, nil)
	src := &proto.GitSource{URL: "https://example.com/r.git", Commit: strings.Repeat("b", 40), Branch: "dev"}
	log, lines := collect()
	if err := b.fetchGit(context.Background(), src, filepath.Join(t.TempDir(), "src"), log); err != nil {
		t.Fatal(err)
	}
	if len(r.find(func(c cmdSpec) bool {
		return strings.Contains(strings.Join(c.Args, " "), "--depth 50 origin refs/heads/dev")
	})) != 1 {
		t.Errorf("no branch fallback")
	}
	if !strings.Contains(strings.Join(lines(), "\n"), "fetching the branch dev instead") {
		t.Errorf("log = %v", lines())
	}
}

func TestFetchGitRejectsBadInput(t *testing.T) {
	b := testBuilder(t, &fakeRunner{}, nil, nil)
	log, _ := collect()
	cases := []proto.GitSource{
		{URL: "ssh://git@github.com/a/b", Commit: strings.Repeat("a", 40)},
		{URL: "https://user:pass@github.com/a/b", Commit: strings.Repeat("a", 40)},
		{URL: "https://github.com/a/b", Commit: "main; rm -rf /"},
	}
	for _, c := range cases {
		if err := b.fetchGit(context.Background(), &c, t.TempDir(), log); err == nil || !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("%+v: err = %v", c, err)
		}
	}
}

// TestFetchGitReal fetches from a real smart-HTTP server (git http-backend)
// that requires the token, so the extraHeader path is proven end to end.
func TestFetchGitReal(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	out, err := exec.Command(gitBin, "--exec-path").Output()
	if err != nil {
		t.Skip("git --exec-path failed")
	}
	backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend is not available")
	}
	base := t.TempDir()
	work := filepath.Join(base, "work")
	run := func(dir string, args ...string) string {
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_NOSYSTEM=1", "HOME="+base)
		o, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, o)
		}
		return strings.TrimSpace(string(o))
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(work, "init", "-q", "-b", "main")
	writeTree(t, work, map[string]string{"package.json": `{"name":"x"}`})
	run(work, "add", ".")
	run(work, "commit", "-q", "-m", "one")
	sha := run(work, "rev-parse", "HEAD")
	run(base, "clone", "-q", "--bare", work, filepath.Join(base, "repo.git"))
	run(filepath.Join(base, "repo.git"), "config", "uploadpack.allowReachableSHA1InWant", "true")
	h := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + base, "GIT_HTTP_EXPORT_ALL=1"}}
	const token = "ghs_realtoken_abc"
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	b := newBuilder(Options{DataDir: t.TempDir(), Git: gitBin}, execRunner{}, newFakeEngine())
	dir := filepath.Join(t.TempDir(), "w", "src")
	log, _ := collect()
	src := &proto.GitSource{URL: srv.URL + "/repo.git", Token: token, Commit: sha, Branch: "main"}
	if err := b.fetchGit(context.Background(), src, dir, log); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "package.json")); string(got) != `{"name":"x"}` {
		t.Errorf("checkout content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Errorf(".git must be removed from the build context")
	}
	src.Token = "wrong"
	if err := b.fetchGit(context.Background(), src, filepath.Join(t.TempDir(), "w", "src"), log); err == nil {
		t.Errorf("a wrong token must fail")
	}
}

func TestFetchGitTokenIsScopedAndNeedsHTTPS(t *testing.T) {
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, nil)
	log, _ := collect()
	src := &proto.GitSource{URL: "https://github.com/acme/app.git", Token: "ghs_scoped", Commit: strings.Repeat("a", 40), Branch: "main"}
	if err := b.fetchGit(context.Background(), src, filepath.Join(t.TempDir(), "src"), log); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.find(func(cmdSpec) bool { return true }) {
		if v, _ := envValue(c.Env, "GIT_CONFIG_KEY_0"); v != "http.https://github.com/acme/app.git.extraHeader" {
			t.Errorf("the token header is not scoped to the repository: %q", v)
		}
	}
	// A token never travels in clear text to a remote host.
	plain := &proto.GitSource{URL: "http://github.com/acme/app.git", Token: "ghs_scoped", Commit: strings.Repeat("a", 40)}
	if err := b.fetchGit(context.Background(), plain, filepath.Join(t.TempDir(), "src"), log); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http with a token: err = %v", err)
	}
	// Submodules are limited to https.
	dir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("[submodule \"x\"]\n\tpath = x\n\turl = https://example.com/x.git\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r2 := &fakeRunner{}
	b2 := testBuilder(t, r2, nil, nil)
	if err := b2.fetchGit(context.Background(), src, dir, log); err != nil {
		t.Fatal(err)
	}
	sub := r2.find(func(c cmdSpec) bool { return strings.Contains(strings.Join(c.Args, " "), "submodule update") })
	if len(sub) != 1 || !strings.Contains(strings.Join(sub[0].Args, " "), "protocol.allow=never -c protocol.https.allow=always") {
		t.Errorf("submodule update is not restricted to https: %+v", sub)
	}
}
