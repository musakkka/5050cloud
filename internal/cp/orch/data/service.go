// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
)

// Data is the data services implementation returned by New. Besides the
// frozen Service interface it offers RunRestoreTests and RunRegistryGC for
// operators and tests (see README.md).
type Data struct {
	st      *store
	cfg     cpapi.Config
	run     Runner
	cat     *catalog
	dataDir string
	keys    *keyring
	now     func() time.Time
	log     *slog.Logger
	http    *http.Client

	cron *cronScheduler

	bgMu  sync.Mutex
	bgCtx context.Context
	bgWG  sync.WaitGroup

	mu      sync.Mutex
	busy    map[string]bool // "env/svc" -> a backup, restore or wipe is running
	gcMu    sync.Mutex      // one registry garbage collection at a time
	started bool
}

var _ Service = (*Data)(nil)

// New returns the data services. It applies the data migrations at once, so a
// broken state.db fails at boot rather than on the first backup.
func New(o Options) (Service, error) {
	d, err := newData(o, time.Now)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func newData(o Options, now func() time.Time) (*Data, error) {
	if o.DB == nil {
		return nil, errors.New("The data services need the state database (Options.DB is nil).")
	}
	if o.Config == nil || o.Runner == nil {
		return nil, errors.New("The data services need both the core config (Options.Config) and the runner (Options.Runner).")
	}
	keyFile := os.Getenv("FIFTY_MASTER_KEY_FILE")
	if keyFile == "" {
		keyFile = defaultMasterKeyFile
	}
	d := &Data{
		st:      &store{db: o.DB},
		cfg:     o.Config,
		run:     o.Runner,
		cat:     newCatalog(o.Templates),
		dataDir: o.DataDir,
		keys:    &keyring{cfg: o.Config, file: keyFile},
		now:     now,
		log:     slog.Default().With("component", "data"),
		http:    &http.Client{Timeout: 30 * time.Second},
		bgCtx:   context.Background(),
		busy:    map[string]bool{},
	}
	d.cron = newCronScheduler(d)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := d.st.migrate(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

// Start runs the cron scheduler, backup schedules, retention, backup health,
// the WAL guard bookkeeping, registry garbage collection and the monthly
// restore test until ctx is done (docs/SPEC.md 9.9, 9.13, 9.6).
func (d *Data) Start(ctx context.Context) error {
	d.bgMu.Lock()
	if d.started {
		d.bgMu.Unlock()
		return errors.New("The data services were started twice.")
	}
	d.started = true
	d.bgCtx = ctx
	d.bgMu.Unlock()

	if err := d.cron.boot(ctx); err != nil {
		d.log.Error("cron boot failed", "err", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); d.cron.loop(ctx) }()
	go func() { defer wg.Done(); d.maintenanceLoop(ctx) }()
	<-ctx.Done()
	wg.Wait()
	d.bgWG.Wait()
	return nil
}

// background runs fn after the request that started it has returned, bound to
// the lifetime of Start (or of the process before Start runs).
func (d *Data) background(name string, fn func(ctx context.Context)) {
	d.bgMu.Lock()
	ctx := d.bgCtx
	d.bgWG.Add(1)
	d.bgMu.Unlock()
	go func() {
		defer d.bgWG.Done()
		defer func() {
			if r := recover(); r != nil {
				d.log.Error("background task panicked", "task", name, "panic", r)
			}
		}()
		fn(ctx)
	}()
}

// claim marks a service busy with one long data operation at a time.
func (d *Data) claim(envID, serviceID, what string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := envID + "/" + serviceID
	if d.busy[k] {
		return fmt.Errorf("Another backup, restore or wipe of this service is still running, so %s has to wait until it ends.", what)
	}
	d.busy[k] = true
	return nil
}

func (d *Data) release(envID, serviceID string) {
	d.mu.Lock()
	delete(d.busy, envID+"/"+serviceID)
	d.mu.Unlock()
}

// maintenanceLoop runs the minute-level schedules.
func (d *Data) maintenanceLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	d.maintenanceTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.maintenanceTick(ctx)
		}
	}
}

func (d *Data) maintenanceTick(ctx context.Context) {
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"backup schedules", d.runDueBackups},
		{"pending base backups", d.runPendingBaseBackups},
		{"backup health", d.checkBackupHealth},
		{"monthly restore test", d.maybeRunRestoreTests},
		{"registry maintenance", d.maybeRegistryMaintenance},
	}
	for _, s := range steps {
		if ctx.Err() != nil {
			return
		}
		if err := s.fn(ctx); err != nil && ctx.Err() == nil {
			d.log.Warn("data maintenance step failed", "step", s.name, "err", err)
		}
	}
}

// ServiceChanged is called after a deploy, removal or config change so cron
// and backup schedules follow.
func (d *Data) ServiceChanged(ctx context.Context, envID, serviceID string) {
	snap, err := d.cfg.ServiceInstance(ctx, envID, serviceID)
	if err != nil {
		d.cron.remove(envID, serviceID)
		return
	}
	d.cron.update(ctx, snap)
	if !snap.Deleted && !d.recipe(snap).none() {
		_ = d.st.ensureServiceRow(ctx, envID, serviceID, d.now().UnixMilli())
	}
}

// ---------------------------------------------------------------- helpers

// recipeInfo is what the data services know about one service.
type recipeInfo struct {
	tmpl   *manifest.Template
	svc    *manifest.TemplateService
	recipe *manifest.BackupRecipe
	// data is the Data tab client: "postgres" | "mysql" | "redis" | "mongodb".
	data string
	// volumeOnly is set for a service whose only data is a plain volume.
	volumeOnly bool
}

func (r recipeInfo) none() bool { return r.recipe == nil }

// supported reports whether backups exist for this kind of service.
func (r recipeInfo) supported() bool { return r.recipe != nil }

// recipe finds the backup recipe of a service: its template's, or the generic
// volume recipe for a service with a volume and no template recipe.
func (d *Data) recipe(snap cpapi.ServiceSnapshot) recipeInfo {
	var info recipeInfo
	if snap.Config.Template != "" {
		t, s, err := d.cat.serviceFor(snap.Config.Template, snap.Config.Name, snap.Config.Source.Image)
		if err == nil {
			info.tmpl, info.svc, info.data = t, s, s.Data
			if s.Backup != nil {
				info.recipe = s.Backup
				return info
			}
			if s.Data == "mysql" {
				// MySQL has no backups in v0.1 (docs/SPEC.md 9.13).
				return info
			}
		}
	}
	if snap.Volume != nil {
		info.volumeOnly = true
		info.recipe = &manifest.BackupRecipe{
			Kind:      "volume",
			RunIn:     "job",
			Backup:    []string{"/opt/5050/bin/backup-volume"},
			Restore:   []string{"/opt/5050/bin/restore-volume"},
			Retention: []string{"/opt/5050/bin/retention-restic"},
		}
	}
	return info
}

// instance loads a committed service and refuses deleted ones.
func (d *Data) instance(ctx context.Context, envID, serviceID string) (cpapi.ServiceSnapshot, error) {
	snap, err := d.cfg.ServiceInstance(ctx, envID, serviceID)
	if err != nil {
		return snap, err
	}
	if snap.Deleted {
		return snap, errors.New("This service has been deleted.")
	}
	return snap, nil
}

// imageFor is the image to run data jobs of a service in: the active
// deployment's image when there is one, else the configured image pinned by
// images.json (a restore onto a new host after the old one was lost).
func (d *Data) imageFor(ctx context.Context, snap cpapi.ServiceSnapshot) string {
	if img, _, _, err := d.run.ActiveImage(ctx, snap.EnvID, snap.ServiceID); err == nil && img != "" {
		return img
	}
	return d.cat.Pinned(snap.Config.Source.Image)
}

func (d *Data) activity(ctx context.Context, envID, serviceID, text, actor, ref string) {
	if err := d.cfg.RecordActivity(ctx, envID, api.Activity{
		ID: newID("act"), TS: d.now().UnixMilli(), ServiceID: serviceID,
		Kind: "backup", Text: text, Actor: actor, Ref: ref,
	}); err != nil {
		d.log.Warn("record activity", "err", err)
	}
}

func (d *Data) audit(ctx context.Context, actor, action, target, detail string) {
	if actor == "" {
		actor = "system"
	}
	if err := d.cfg.Audit(ctx, api.AuditEntry{
		ID: newID("aud"), TS: d.now().UnixMilli(), Actor: actor, Action: action, Target: target, Detail: detail,
	}); err != nil {
		d.log.Warn("write audit entry", "action", action, "err", err)
	}
}

// notifyBackupFailed sends backup.failed by webhooks, email and the bell, and
// writes the same sentence to the activity feed.
func (d *Data) notifyBackupFailed(ctx context.Context, snap cpapi.ServiceSnapshot, ref, text string) {
	text = sentence(text)
	d.activity(ctx, snap.EnvID, snap.ServiceID, text, "", ref)
	if err := d.cfg.Notify(ctx, cpapi.Notice{
		Event: api.HookBackupFailed, ProjectID: snap.ProjectID, EnvID: snap.EnvID,
		ServiceID: snap.ServiceID, Ref: ref, Text: text,
		Details: map[string]any{"service": snap.Config.Name, "environment": snap.EnvName},
	}); err != nil {
		d.log.Warn("notify backup.failed", "err", err)
	}
}

// bucket returns the backup settings and whether backups are configured.
func (d *Data) bucket(ctx context.Context) (api.BackupSettings, bool, error) {
	b, err := d.cfg.BackupSettings(ctx)
	if err != nil {
		return b, false, fmt.Errorf("read the backup settings: %w", err)
	}
	return b, bucketReady(b), nil
}

// TestBucket puts, gets and deletes a small object with the right key for each.
func (d *Data) TestBucket(ctx context.Context, s api.BackupSettings) api.TestResult {
	return testBucket(ctx, s, d.http)
}

// jobLabels marks job containers so operators can find them.
func jobLabels(envID, serviceID, kind string) map[string]string {
	return map[string]string{
		"5050.service":     serviceID,
		"5050.environment": envID,
		"5050.job":         kind,
	}
}

// jobError turns a failed job result into one sentence for people.
func jobError(what string, res JobOutcome) string {
	switch {
	case res.Err != nil:
		return fmt.Sprintf("%s could not run: %s", what, sentence(res.Err.Error()))
	case res.Result.TimedOut:
		return fmt.Sprintf("%s did not finish in time and was stopped.", what)
	case res.Result.Error != "":
		return fmt.Sprintf("%s failed: %s", what, sentence(res.Result.Error))
	}
	detail := tailLines(res.Result.Output, 2)
	if detail == "" {
		return fmt.Sprintf("%s failed with exit code %d.", what, res.Result.ExitCode)
	}
	return fmt.Sprintf("%s failed with exit code %d: %s", what, res.Result.ExitCode, sentence(detail))
}
