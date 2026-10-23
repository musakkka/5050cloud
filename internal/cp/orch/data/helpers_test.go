// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/internal/cp/orch/data/fakerunner"
	"github.com/musakkka/5050cloud/internal/cp/orch/fakeconfig"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/templates"
)

var _ Runner = fakerunner.New()
var _ VolumePlacer = fakerunner.Placing{}

func init() { pollEvery = 5 * time.Millisecond }

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// testEnv is one data service over a temporary state.db, the fake core and
// the fake runner.
type testEnv struct {
	t      *testing.T
	d      *Data
	db     *sql.DB
	cfg    *fakeconfig.Config
	run    *fakerunner.Runner
	runner Runner
	clock  *clock
	projID string
	envID  string
	dbPath string
}

func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func writeMasterKey(t *testing.T) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(p, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIFTY_MASTER_KEY_FILE", p)
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	writeMasterKey(t)
	e := &testEnv{t: t, cfg: fakeconfig.New(), run: fakerunner.New(),
		clock: &clock{t: time.Date(2026, 10, 21, 10, 0, 30, 0, time.UTC)}}
	e.runner = e.run
	e.dbPath = filepath.Join(t.TempDir(), "state.db")
	e.db = openTestDB(t, e.dbPath)
	e.projID, e.envID = e.cfg.AddProject("shop")
	e.d = e.newData(e.db)
	return e
}

// newData builds another process over the same database (a restart).
func (e *testEnv) newData(db *sql.DB) *Data {
	e.t.Helper()
	d, err := newData(Options{DB: db, Config: e.cfg, Runner: e.runner, Templates: templates.FS, DataDir: e.t.TempDir()}, e.clock.Now)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *testEnv) wait() { e.d.bgWG.Wait() }

func (e *testEnv) ctx() context.Context { return context.Background() }

func (e *testEnv) setBucket() api.BackupSettings {
	b := api.BackupSettings{Endpoint: "http://minio.test:9000", Bucket: "backups", Region: "us-east-1", Prefix: "install-1",
		AccessKeyID: "writer", SecretAccessKey: "writer-secret", RetentionAccessKeyID: "janitor",
		RetentionSecretAccessKey: "janitor-secret", PathStyle: true}
	e.cfg.SetBackupSettings(b)
	return b
}

// templateService returns a template service by template id and service name.
func templateService(t *testing.T, id, name string) manifest.TemplateService {
	t.Helper()
	b, err := templates.FS.ReadFile(id + "/template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var tm manifest.Template
	if err := yaml.UnmarshalStrict(b, &tm); err != nil {
		t.Fatal(err)
	}
	for _, s := range tm.Services {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("template %s has no service %s", id, name)
	return manifest.TemplateService{}
}

// addTemplateService creates a service from a template with plain variable
// values (secrets replaced by fixed test values), a volume and a running replica.
func (e *testEnv) addTemplateService(id, name string, running bool) string {
	e.t.Helper()
	ts := templateService(e.t, id, name)
	svc := e.cfg.AddService(e.envID, ts.Config)
	vars := map[string]string{}
	for k, v := range ts.Variables {
		if strings.Contains(v, "secret(") || strings.Contains(v, "input.") {
			v = "pw-" + strings.ToLower(k)
		}
		vars[k] = v
	}
	e.cfg.SetVariables(e.envID, svc, vars)
	if ts.Volume != nil {
		e.cfg.AddVolume(e.envID, svc, ts.Volume.MountPath)
	}
	if running {
		env, _, _ := e.cfg.ResolveVariables(e.ctx(), e.envID, svc, map[string]string{"FIFTY_PRIVATE_DOMAIN": strings.ToLower(ts.Config.Net.PrivateName) + ".internal"})
		e.run.SetRunning(e.envID, svc, "host_1", fakerunner.Image{Ref: ts.Config.Source.Image, Config: ts.Config, Env: env})
	}
	return svc
}

// insertBackup adds a finished backup row.
func (e *testEnv) insertBackup(svc, kind, status, storeID string, finishedAt time.Time) string {
	e.t.Helper()
	id := newID("bak")
	_, err := e.db.Exec(`INSERT INTO o_backups (id, service_id, environment_id, kind, status, trigger, store_id, started_at, finished_at, size_bytes)
		VALUES (?, ?, ?, ?, ?, 'schedule', ?, ?, ?, 100)`, id, svc, e.envID, kind, status, storeID,
		finishedAt.Add(-time.Minute).UnixMilli(), finishedAt.UnixMilli())
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func countRows(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
