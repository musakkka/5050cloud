// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/upgrade/minisign"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// fakeRun records commands and can run a hook per command.
type fakeRun struct {
	mu   sync.Mutex
	cmds []string
	hook func(cmd string) error
}

func (f *fakeRun) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	c := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.cmds = append(f.cmds, c)
	h := f.hook
	f.mu.Unlock()
	if h != nil {
		return nil, h(c)
	}
	return nil, nil
}

func (f *fakeRun) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.cmds, "\n")
}

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, c := range files {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(c)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

type releaseServer struct {
	*httptest.Server
	files map[string][]byte
}

// newRelease serves version signed by key with a tarball holding files.
func newRelease(t *testing.T, key minisign.PrivateKey, version string, files map[string]string) (*releaseServer, []byte) {
	t.Helper()
	tb := tarball(t, files)
	sum := sha256.Sum256(tb)
	name := ServerTarball(version, "amd64")
	sums := fmt.Sprintf("%s  %s\n%s  5050-cli_x.tar.gz\n", hex.EncodeToString(sum[:]), name, strings.Repeat("0", 64))
	sig, err := minisign.Sign(key, []byte(sums), "test")
	if err != nil {
		t.Fatal(err)
	}
	rs := &releaseServer{files: map[string][]byte{
		"/latest.txt":                            []byte(version + "\n"),
		"/" + version + "/checksums.txt":         []byte(sums),
		"/" + version + "/checksums.txt.minisig": []byte(sig),
		"/" + version + "/" + name:               tb,
	}}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := rs.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(rs.Close)
	return rs, tb
}

func keypair(t *testing.T) (minisign.PublicKey, minisign.PrivateKey) {
	t.Helper()
	pub, sec, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, sec
}

func baseOpts(t *testing.T, base string, keys ...minisign.PublicKey) Options {
	t.Helper()
	dir := t.TempDir()
	o := Options{
		ReleaseBase: base, InstallDir: filepath.Join(dir, "opt"), DataDir: filepath.Join(dir, "data"),
		Arch: "amd64", ReleaseFile: "-", Version: "v0.1.0", PollInterval: 5 * time.Millisecond,
		ReadyTimeout: 300 * time.Millisecond, AgentConnectTimeout: 200 * time.Millisecond,
	}
	for _, k := range keys {
		o.PublicKeys = append(o.PublicKeys, k.String())
	}
	return o
}

func TestResolveVerifiesCurrentAndNextKey(t *testing.T) {
	cur, curSec := keypair(t)
	next, nextSec := keypair(t)
	other, otherSec := keypair(t)
	_ = other
	for _, tc := range []struct {
		name string
		sec  minisign.PrivateKey
		ok   bool
	}{{"current", curSec, true}, {"next", nextSec, true}, {"unknown", otherSec, false}} {
		t.Run(tc.name, func(t *testing.T) {
			rs, tb := newRelease(t, tc.sec, "v0.2.0", map[string]string{"5050d": "x"})
			o := baseOpts(t, rs.URL, cur, next)
			r, err := Resolve(context.Background(), o, "")
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "signature") {
					t.Fatalf("want a signature error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(tb)
			if r.Version != "v0.2.0" || r.SHA256 != hex.EncodeToString(sum[:]) || !strings.HasSuffix(r.URL, "/v0.2.0/5050_0.2.0_linux_amd64.tar.gz") {
				t.Fatalf("bad release %+v", r)
			}
		})
	}
}

func TestResolveRejectsTamperedChecksums(t *testing.T) {
	pub, sec := keypair(t)
	rs, _ := newRelease(t, sec, "v0.2.0", map[string]string{"5050d": "x"})
	rs.files["/v0.2.0/checksums.txt"] = append(rs.files["/v0.2.0/checksums.txt"], []byte("00  evil\n")...)
	if _, err := Resolve(context.Background(), baseOpts(t, rs.URL, pub), "v0.2.0"); err == nil {
		t.Fatal("tampered checksums must fail")
	}
	if _, err := Resolve(context.Background(), baseOpts(t, rs.URL, pub), "nope"); err == nil {
		t.Fatal("bad version must fail")
	}
	o := baseOpts(t, rs.URL)
	o.PublicKeys = nil
	embeddedKeys = ""
	if _, err := Resolve(context.Background(), o, "v0.2.0"); err == nil || !strings.Contains(err.Error(), "no release signing key") {
		t.Fatalf("want no key error, got %v", err)
	}
}

func TestStageRejectsBadSHAAndUnsafeTar(t *testing.T) {
	pub, sec := keypair(t)
	rs, _ := newRelease(t, sec, "v0.2.0", map[string]string{"5050d": "x", "5050-agent": "y", "5050": "z"})
	o := baseOpts(t, rs.URL, pub)
	r, err := Resolve(context.Background(), o, "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.SHA256 = strings.Repeat("a", 64)
	if _, err := Stage(context.Background(), o, bad, "5050d"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("want sha error, got %v", err)
	}
	dir, err := Stage(context.Background(), o, r, "5050d", "5050-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "5050-agent")); err != nil {
		t.Fatal(err)
	}
	if again, err := Stage(context.Background(), o, r, "5050d"); err != nil || again != dir {
		t.Fatalf("restage: %v %s", err, again)
	}
	for _, n := range []string{"../x", "/abs"} {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x"))
		_ = tw.Close()
		_ = gz.Close()
		if err := untar(&buf, t.TempDir()); err == nil {
			t.Fatalf("%s must be refused", n)
		}
	}
}

func makeStateDB(t *testing.T, path string, versions ...int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS core_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, v := range versions {
		if _, err := db.Exec(`INSERT OR IGNORE INTO core_migrations VALUES (?, 'm', 0)`, v); err != nil {
			t.Fatal(err)
		}
	}
}

// readyServer answers /readyz with whatever version() says.
func readyServer(t *testing.T, version func() string, agents int) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := version()
		if v == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(api.Readyz{Version: v, Ready: true, Checks: &api.ReadyChecks{DB: true, Migrations: true, Hub: true, Agents: agents}})
	}))
	t.Cleanup(s.Close)
	return s
}

func setupInstall(t *testing.T, o Options) (oldDir, newDir string) {
	t.Helper()
	oldDir = filepath.Join(o.InstallDir, "releases", "v0.1.0")
	newDir = filepath.Join(o.InstallDir, "releases", "v0.2.0")
	for _, d := range []string{oldDir, newDir, o.DataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := swapLink(o.CurrentLink(), oldDir); err != nil {
		t.Fatal(err)
	}
	makeStateDB(t, filepath.Join(o.DataDir, "state.db"), 1)
	return
}

func TestHelperSwitchesWhenReady(t *testing.T) {
	o := baseOpts(t, "http://unused")
	oldDir, newDir := setupInstall(t, o)
	var running atomic.Value
	running.Store("v0.1.0")
	rs := readyServer(t, func() string { return running.Load().(string) }, 2)
	o.ControlURL = rs.URL
	fr := &fakeRun{hook: func(c string) error {
		if c == "systemctl start 5050d.service" {
			t2, _ := linkTarget(o.CurrentLink())
			running.Store(filepath.Base(t2))
		}
		return nil
	}}
	o.Run = fr
	if err := RunHelper(context.Background(), o, HelperArgs{To: "v0.2.0", From: "v0.1.0", ReleaseDir: newDir, Agents: 2}); err != nil {
		t.Fatal(err)
	}
	if got, _ := linkTarget(o.CurrentLink()); got != newDir {
		t.Fatalf("current -> %s", got)
	}
	st, _ := Status(o)
	if st.State != StateDone || st.ToVersion != "v0.2.0" {
		t.Fatalf("status %+v", st)
	}
	if !strings.Contains(fr.all(), "systemctl stop 5050d.service\nsystemctl start 5050d.service") {
		t.Fatalf("commands:\n%s", fr.all())
	}
	if _, err := os.Stat(filepath.Join(o.DataDir, "backups", "pre-upgrade-v0.2.0", "state.db")); err != nil {
		t.Fatal("state copy missing")
	}
	_ = oldDir
}

func TestHelperRollsBackAndRestoresSchema(t *testing.T) {
	o := baseOpts(t, "http://unused")
	oldDir, newDir := setupInstall(t, o)
	var running atomic.Value
	running.Store("v0.1.0")
	rs := readyServer(t, func() string { return running.Load().(string) }, 0)
	o.ControlURL = rs.URL
	o.Run = &fakeRun{hook: func(c string) error {
		if c == "systemctl start 5050d.service" {
			t2, _ := linkTarget(o.CurrentLink())
			if filepath.Base(t2) == "v0.2.0" {
				// The broken new version migrates, then never gets ready.
				makeStateDB(t, filepath.Join(o.DataDir, "state.db"), 1, 2)
				running.Store("")
			} else {
				running.Store("v0.1.0")
			}
		}
		return nil
	}}
	err := RunHelper(context.Background(), o, HelperArgs{To: "v0.2.0", From: "v0.1.0", ReleaseDir: newDir})
	if err == nil {
		t.Fatal("want a rollback")
	}
	if got, _ := linkTarget(o.CurrentLink()); got != oldDir {
		t.Fatalf("current -> %s, want %s", got, oldDir)
	}
	st, _ := Status(o)
	if st.State != StateRolledBack || !strings.Contains(st.Message, "restored") {
		t.Fatalf("status %+v", st)
	}
	if v := schemaVersion(filepath.Join(o.DataDir, "state.db")); v != "core_migrations:1" {
		t.Fatalf("schema %q", v)
	}
	if _, err := os.Stat(filepath.Join(o.DataDir, "adopt-after-restore")); err != nil {
		t.Fatal("adopt marker missing")
	}
}

func TestHelperRollsBackWhenAgentsMissing(t *testing.T) {
	o := baseOpts(t, "http://unused")
	oldDir, newDir := setupInstall(t, o)
	rs := readyServer(t, func() string { return "v0.2.0" }, 1)
	o.ControlURL = rs.URL
	o.Run = &fakeRun{}
	err := RunHelper(context.Background(), o, HelperArgs{To: "v0.2.0", From: "v0.1.0", ReleaseDir: newDir, Agents: 3})
	if err == nil || !strings.Contains(err.Error(), "1 of 3 agents") {
		t.Fatalf("got %v", err)
	}
	if got, _ := linkTarget(o.CurrentLink()); got != oldDir {
		t.Fatalf("current -> %s", got)
	}
	st, _ := Status(o)
	if strings.Contains(st.Message, "restored") {
		t.Fatal("same schema must not restore state")
	}
}

func TestStartControlPlaneLaunchesHelper(t *testing.T) {
	pub, sec := keypair(t)
	rs, _ := newRelease(t, sec, "v0.2.0", map[string]string{"5050d": "x", "5050-agent": "y", "5050": "z"})
	o := baseOpts(t, rs.URL, pub)
	o.AgentsConnected = func() int { return 4 }
	snapped := ""
	o.Snapshot = func(p string) error { snapped = p; return nil }
	fr := &fakeRun{}
	o.Run = fr
	r, err := Resolve(context.Background(), o, "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := StartControlPlane(context.Background(), o, r); err != nil {
		t.Fatal(err)
	}
	c := fr.all()
	if !strings.HasPrefix(c, "systemd-run --unit 5050-upgrade-") || !strings.Contains(c, "5050d upgrade-helper --to v0.2.0 --from v0.1.0") || !strings.Contains(c, "--agents 4") {
		t.Fatalf("command %s", c)
	}
	if snapped == "" {
		t.Fatal("no snapshot")
	}
	st, _ := Status(o)
	if st.State != StateSwitching {
		t.Fatalf("status %+v", st)
	}
	if err := StartControlPlane(context.Background(), o, r); err == nil {
		t.Fatal("a second upgrade while one runs must fail")
	}
	if code := HelperMain([]string{"--bogus"}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestAgentUpgradeAndRevert(t *testing.T) {
	pub, sec := keypair(t)
	rs, _ := newRelease(t, sec, "v0.2.0", map[string]string{"5050-agent": "new"})
	o := baseOpts(t, rs.URL, pub)
	fr := &fakeRun{}
	o.Run = fr
	oldDir := filepath.Join(o.InstallDir, "releases", "v0.1.0")
	_ = os.MkdirAll(oldDir, 0o755)
	if _, err := swapLink(o.CurrentLink(), oldDir); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(context.Background(), o, "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := UpgradeAgent(context.Background(), o, proto.Upgrade{Version: r.Version, URL: r.URL, SHA256: r.SHA256}); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(o.InstallDir, "releases", "v0.2.0")
	if got, _ := linkTarget(o.AgentLink()); got != newDir {
		t.Fatalf("agent -> %s", got)
	}
	if !strings.Contains(fr.all(), "systemctl --no-block restart 5050-agent.service") {
		t.Fatalf("commands %s", fr.all())
	}

	// The new agent starts and never connects: it reverts.
	n := o.Defaults()
	n.Version = "v0.2.0"
	if err := checkPending(context.Background(), n, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	var p pendingAgent
	if err := readJSON(n.pendingPath(), &p); err != nil || p.Boots != 1 {
		t.Fatalf("marker %+v %v", p, err)
	}
	if err := watchPending(context.Background(), n, p, time.Now().Add(50*time.Millisecond), func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if got, _ := linkTarget(o.AgentLink()); got != oldDir {
		t.Fatalf("agent -> %s after revert", got)
	}
	rv, ok := LastAgentRevert(o)
	if !ok || rv.Version != "v0.2.0" {
		t.Fatalf("revert record %+v", rv)
	}
	if _, err := os.Stat(n.pendingPath()); !os.IsNotExist(err) {
		t.Fatal("marker must be gone")
	}
}

func TestAgentPendingConnectsAndCrashLoop(t *testing.T) {
	o := baseOpts(t, "http://unused").Defaults()
	o.Version = "v0.2.0"
	o.Run = &fakeRun{}
	oldDir := filepath.Join(o.InstallDir, "releases", "v0.1.0")
	newDir := filepath.Join(o.InstallDir, "releases", "v0.2.0")
	_ = os.MkdirAll(oldDir, 0o755)
	_ = os.MkdirAll(newDir, 0o755)
	_, _ = swapLink(o.AgentLink(), newDir)

	p := pendingAgent{Version: "v0.2.0", Previous: oldDir}
	_ = writeJSON(o.pendingPath(), p)
	if err := watchPending(context.Background(), o, p, time.Now().Add(time.Second), func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.pendingPath()); !os.IsNotExist(err) {
		t.Fatal("a connected agent clears the marker")
	}

	p.Boots = maxAgentBoots
	_ = writeJSON(o.pendingPath(), p)
	if err := checkPending(context.Background(), o, func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if got, _ := linkTarget(o.AgentLink()); got != oldDir {
		t.Fatalf("crash loop must revert, agent -> %s", got)
	}

	// The old agent finding a marker for another version just removes it.
	_ = writeJSON(o.pendingPath(), pendingAgent{Version: "v9.9.9", Previous: oldDir})
	if err := checkPending(context.Background(), o, func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.pendingPath()); !os.IsNotExist(err) {
		t.Fatal("stale marker must be removed")
	}
	if err := CheckPendingAgentUpgrade(o, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
}

func TestPruneKeepsLinkedReleases(t *testing.T) {
	o := baseOpts(t, "http://unused")
	var dirs []string
	for i := 0; i < 5; i++ {
		d := filepath.Join(o.InstallDir, "releases", fmt.Sprintf("v0.%d.0", i))
		_ = os.MkdirAll(d, 0o755)
		ts := time.Now().Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(d, ts, ts)
		dirs = append(dirs, d)
	}
	_, _ = swapLink(o.CurrentLink(), dirs[0])
	if err := Prune(o, 1); err != nil {
		t.Fatal(err)
	}
	for i, d := range dirs {
		_, err := os.Stat(d)
		keep := i == 0 || i == 4
		if keep != (err == nil) {
			t.Fatalf("%s keep=%v err=%v", d, keep, err)
		}
	}
}

func TestDefaultsReadReleaseFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "release.json")
	_ = os.WriteFile(f, []byte(`{"releaseBase":"http://x/rel/","publicKeys":["K"]}`), 0o600)
	o := Options{ReleaseFile: f}.Defaults()
	if o.ReleaseBase != "http://x/rel" || o.LatestURL != "http://x/rel/latest.txt" || o.PublicKeys[len(o.PublicKeys)-1] != "K" {
		t.Fatalf("%+v", o)
	}
	if st, err := Status(o); err != nil || st.State != StateIdle {
		t.Fatalf("%+v %v", st, err)
	}
}
