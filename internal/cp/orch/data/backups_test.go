// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestBackupRecipesPerTemplate(t *testing.T) {
	cases := []struct {
		tmpl, svc string
		kind      string
		inService bool
		cmd       string
		tool      string
	}{
		{"postgres", "Postgres", "postgres_wal", true, "/opt/5050/bin/backup-postgres", ""},
		{"pgvector", "Postgres", "postgres_wal", true, "/opt/5050/bin/backup-postgres", ""},
		{"postgis", "Postgres", "postgres_wal", true, "/opt/5050/bin/backup-postgres", ""},
		{"redis", "Redis", "redis_rdb", true, "/opt/5050/bin/backup-redis", ""},
		{"mongodb", "MongoDB", "mongo_dump", true, "/opt/5050/bin/backup-mongo", ""},
		{"uptime-kuma", "Uptime Kuma", "volume", false, "/opt/5050/bin/backup-volume", "restic"},
		{"minio", "MinIO", "volume", false, "/opt/5050/bin/backup-volume", "restic"},
		{"plausible", "ClickHouse", "volume", false, "/opt/5050/bin/backup-volume", "restic"},
	}
	for _, c := range cases {
		t.Run(c.tmpl+"/"+c.svc, func(t *testing.T) {
			e := newEnv(t)
			e.setBucket()
			svc := e.addTemplateService(c.tmpl, c.svc, true)
			b, err := e.d.BackupNow(e.ctx(), e.envID, svc, "usr_1")
			if err != nil {
				t.Fatal(err)
			}
			if b.Kind != c.kind || b.Status != "running" || b.Trigger != "manual" {
				t.Fatalf("backup = %+v", b)
			}
			e.wait()
			jobs := e.run.Jobs()
			if len(jobs) != 1 {
				t.Fatalf("%d jobs", len(jobs))
			}
			j := jobs[0].Job
			if c.inService != (j.Kind == "exec" && j.ReplicaID != "") {
				t.Fatalf("job kind %s replica %q, want in service = %v", j.Kind, j.ReplicaID, c.inService)
			}
			if j.Cmd[0] != c.cmd {
				t.Fatalf("cmd = %v", j.Cmd)
			}
			if j.Env["AWS_ACCESS_KEY_ID"] != "writer" || j.Env["BACKUP_S3_PREFIX"] != "install-1/"+svc || (j.Env["RESTIC_PASSWORD"] == "") != (c.kind == "postgres_wal") {
				t.Fatalf("backup env = %v", j.Env)
			}
			if c.kind == "postgres_wal" && (j.User != "postgres" || j.Env["WALG_S3_PREFIX"] != "s3://backups/install-1/"+svc+"/wal-g" || len(j.Env["WALG_LIBSODIUM_KEY"]) != 64) {
				t.Fatalf("postgres env = %v user %q", j.Env, j.User)
			}
			if !c.inService {
				var ro, tool bool
				for _, m := range j.Mounts {
					ro = ro || (m.Kind == "volume" && m.ReadOnly && m.Target == "/data")
					tool = tool || (m.Kind == "tool" && m.Source == c.tool)
				}
				if !ro || !tool || len(j.Files) != 2 || j.Image == "" || !strings.Contains(j.Image, "@sha256:") {
					t.Fatalf("volume job = %+v", j)
				}
			}
			resp, err := e.d.Backups(e.ctx(), e.envID, svc)
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Backups) != 1 || resp.Backups[0].Status != "ok" || resp.Backups[0].SizeBytes != 1024 {
				t.Fatalf("backups = %+v", resp.Backups)
			}
			if !resp.Health.Configured || !resp.Health.Supported || resp.Health.Stale {
				t.Fatalf("health = %+v", resp.Health)
			}
		})
	}
}

func TestBackupFailureNotifiesAndMarksStale(t *testing.T) {
	e := newEnv(t)
	e.setBucket()
	svc := e.addTemplateService("redis", "Redis", true)
	e.run.OnJob = func(string, proto.Job) (proto.JobResult, error) {
		return proto.JobResult{ExitCode: 1, Output: "Fatal: unable to save snapshot\nrestic could not store the RDB file; the lines above say why.\n"}, nil
	}
	if _, err := e.d.BackupNow(e.ctx(), e.envID, svc, ""); err != nil {
		t.Fatal(err)
	}
	e.wait()
	resp, _ := e.d.Backups(e.ctx(), e.envID, svc)
	if resp.Backups[0].Status != "failed" || !strings.Contains(resp.Backups[0].Error, "exit code 1") {
		t.Fatalf("backup = %+v", resp.Backups[0])
	}
	if !resp.Health.Stale || !strings.HasPrefix(resp.Health.Message, "The last backup failed") {
		t.Fatalf("health = %+v", resp.Health)
	}
	n := e.cfg.Notices()
	if len(n) != 1 || n[0].Event != api.HookBackupFailed || !strings.Contains(n[0].Text, "The backup of Redis failed") {
		t.Fatalf("notices = %+v", n)
	}
}

func TestBackupPreconditions(t *testing.T) {
	e := newEnv(t)
	pg := e.addTemplateService("postgres", "Postgres", true)
	if _, err := e.d.BackupNow(e.ctx(), e.envID, pg, ""); err == nil || !strings.Contains(err.Error(), "Settings > Backups") {
		t.Fatalf("no bucket: %v", err)
	}
	resp, _ := e.d.Backups(e.ctx(), e.envID, pg)
	if resp.Health.Configured || resp.Health.Message != "Backups off: set a bucket in Settings > Backups." {
		t.Fatalf("health without a bucket = %+v", resp.Health)
	}
	e.setBucket()
	my := e.addTemplateService("mysql", "MySQL", true)
	if _, err := e.d.BackupNow(e.ctx(), e.envID, my, ""); err == nil || !strings.Contains(err.Error(), "Backups not supported yet") {
		t.Fatalf("mysql: %v", err)
	}
	resp, _ = e.d.Backups(e.ctx(), e.envID, my)
	if resp.Health.Supported || resp.Health.Message != "Backups not supported yet." {
		t.Fatalf("mysql health = %+v", resp.Health)
	}
	web := e.cfg.AddService(e.envID, api.ServiceConfig{Name: "web"})
	if _, err := e.d.BackupNow(e.ctx(), e.envID, web, ""); err == nil || !strings.Contains(err.Error(), "no data to back up") {
		t.Fatalf("web: %v", err)
	}
	// One long operation per service at a time.
	block := make(chan struct{})
	e.run.OnJob = func(string, proto.Job) (proto.JobResult, error) {
		<-block
		return proto.JobResult{Output: `{"ok":true,"sizeBytes":1,"id":"base_000000010000000000000003"}`}, nil
	}
	if _, err := e.d.BackupNow(e.ctx(), e.envID, pg, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.BackupNow(e.ctx(), e.envID, pg, ""); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("second backup: %v", err)
	}
	close(block)
	e.wait()
}

func TestBackupScheduleValidationAndDue(t *testing.T) {
	e := newEnv(t)
	e.setBucket()
	pg := e.addTemplateService("postgres", "Postgres", true)
	ctx := e.ctx()
	for _, bad := range []api.BackupSchedule{
		{Enabled: true, Cron: "every day"},
		{Enabled: true, Timezone: "Nowhere/Land"},
		{Enabled: true, RetainDays: 5000},
	} {
		if _, err := e.d.SetBackupSchedule(ctx, e.envID, pg, bad); err == nil {
			t.Errorf("schedule %+v accepted", bad)
		}
	}
	got, err := e.d.SetBackupSchedule(ctx, e.envID, pg, api.BackupSchedule{Enabled: true, Cron: "30 2 * * *", ContinuousWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Timezone != "UTC" || got.RetainDays != 7 {
		t.Fatalf("defaults not filled: %+v", got)
	}
	// Not due before 02:30 the next day; due after.
	e.clock.Set(time.Date(2026, 10, 22, 2, 29, 0, 0, time.UTC))
	if err := e.d.runDueBackups(ctx); err != nil {
		t.Fatal(err)
	}
	e.wait()
	if n := len(e.run.Jobs()); n != 0 {
		t.Fatalf("%d backups before the schedule", n)
	}
	e.clock.Set(time.Date(2026, 10, 22, 2, 31, 0, 0, time.UTC))
	if err := e.d.runDueBackups(ctx); err != nil {
		t.Fatal(err)
	}
	e.wait()
	if err := e.d.runDueBackups(ctx); err != nil { // the same minute again: no second backup
		t.Fatal(err)
	}
	e.wait()
	if n := len(e.run.Jobs()); n != 1 {
		t.Fatalf("%d backups at the scheduled time, want 1", n)
	}
	// Turning archiving off redeploys the database.
	if _, err := e.d.SetBackupSchedule(ctx, e.envID, pg, api.BackupSchedule{Enabled: true, Cron: "30 2 * * *", ContinuousWAL: false}); err != nil {
		t.Fatal(err)
	}
	if d := e.run.Deploys(); len(d) != 1 || d[0].Message != "Continuous WAL archiving turned off" {
		t.Fatalf("deploys = %+v", d)
	}
}

func TestBackupHealthStaleAfter26Hours(t *testing.T) {
	e := newEnv(t)
	e.setBucket()
	pg := e.addTemplateService("postgres", "Postgres", true)
	e.d.ServiceChanged(e.ctx(), e.envID, pg)
	e.insertBackup(pg, "postgres_wal", "ok", "base_000000010000000000000002", e.clock.Now())
	h := func() api.BackupHealth {
		r, err := e.d.Backups(e.ctx(), e.envID, pg)
		if err != nil {
			t.Fatal(err)
		}
		return r.Health
	}
	if got := h(); got.Stale {
		t.Fatalf("fresh backup is stale: %+v", got)
	}
	e.clock.Add(27 * time.Hour)
	got := h()
	if !got.Stale || got.Message != "The last good backup is 27 hours old." {
		t.Fatalf("health = %+v", got)
	}
	// The health check notifies once per stale episode.
	e.clock.Set(time.Date(2026, 10, 22, 13, 20, 0, 0, time.UTC))
	for i := 0; i < 2; i++ {
		if err := e.d.checkBackupHealth(e.ctx()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(e.cfg.Notices()); n != 1 {
		t.Fatalf("%d notices, want 1", n)
	}
}

func TestWALGuard(t *testing.T) {
	e := newEnv(t)
	e.setBucket()
	pg := e.addTemplateService("postgres", "Postgres", true)
	ctx := e.ctx()
	cases := []struct {
		s    proto.DBSample
		fail bool
	}{
		{proto.DBSample{WALBytes: 512 << 20, OldestUnarchivedSec: 30}, false},
		{proto.DBSample{WALBytes: 1<<30 + 1, OldestUnarchivedSec: 0}, true},
		{proto.DBSample{WALBytes: 100 << 20, OldestUnarchivedSec: 601}, true},
		{proto.DBSample{WALBytes: 100 << 20, OldestUnarchivedSec: 600}, false},
	}
	for _, c := range cases {
		if walFailing(c.s) != c.fail {
			t.Errorf("walFailing(%+v) = %v", c.s, !c.fail)
		}
	}
	// Entering the failing state notifies once with the card sentence.
	e.d.DBSample(ctx, e.envID, pg, proto.DBSample{WALBytes: 1503238553, OldestUnarchivedSec: 720})
	e.d.DBSample(ctx, e.envID, pg, proto.DBSample{WALBytes: 1503238553, OldestUnarchivedSec: 780})
	n := e.cfg.Notices()
	if len(n) != 1 || !strings.Contains(n[0].Text, "WAL archiving failing for 12 min, pg_wal is 1.4 GB. The volume fills if this continues.") {
		t.Fatalf("notices = %+v", n)
	}
	r, _ := e.d.Backups(ctx, e.envID, pg)
	if r.Health.WALArchiveOK == nil || *r.Health.WALArchiveOK || !r.Health.Stale || r.Health.WALBytes != 1503238553 {
		t.Fatalf("health = %+v", r.Health)
	}
	// Recovery writes an activity line and clears the state.
	e.d.DBSample(ctx, e.envID, pg, proto.DBSample{WALBytes: 64 << 20, OldestUnarchivedSec: 5})
	acts := e.cfg.Activities()
	if !strings.Contains(acts[len(acts)-1].Text, "caught up") {
		t.Fatalf("activities = %+v", acts)
	}
	r, _ = e.d.Backups(ctx, e.envID, pg)
	if r.Health.WALArchiveOK == nil || !*r.Health.WALArchiveOK {
		t.Fatalf("health after recovery = %+v", r.Health)
	}
}

func TestWALGuardIgnoresDatabasesThatDoNotArchive(t *testing.T) {
	e := newEnv(t)
	pg := e.addTemplateService("postgres", "Postgres", true)
	// No bucket: archiving is off, a large pg_wal is not an archiving failure.
	e.d.DBSample(e.ctx(), e.envID, pg, proto.DBSample{WALBytes: 2 << 30, OldestUnarchivedSec: 3600})
	if n := len(e.cfg.Notices()); n != 0 {
		t.Fatalf("%d notices without a bucket", n)
	}
}

func TestRetentionUsesRetentionKeyAndKeepsLocked(t *testing.T) {
	e := newEnv(t)
	b := e.setBucket()
	kuma := e.addTemplateService("uptime-kuma", "Uptime Kuma", true)
	now := e.clock.Now()
	old1 := e.insertBackup(kuma, "volume", "ok", "aaaaaaaa11111111", now.Add(-10*24*time.Hour))
	old2 := e.insertBackup(kuma, "volume", "ok", "bbbbbbbb22222222", now.Add(-9*24*time.Hour))
	recent := e.insertBackup(kuma, "volume", "ok", "cccccccc33333333", now.Add(-24*time.Hour))
	if _, err := e.d.LockBackup(e.ctx(), old2, true); err != nil {
		t.Fatal(err)
	}
	snap, _ := e.d.instance(e.ctx(), e.envID, kuma)
	if err := e.d.applyRetention(e.ctx(), snap, e.d.recipe(snap), b); err != nil {
		t.Fatal(err)
	}
	jobs := e.run.JobsOfKind("retention")
	if len(jobs) != 1 {
		t.Fatalf("%d retention jobs", len(jobs))
	}
	j := jobs[0]
	if j.Host != "host_1" || j.Job.Env["AWS_ACCESS_KEY_ID"] != "janitor" || j.Job.Env["FORGET_IDS"] != "aaaaaaaa11111111" {
		t.Fatalf("retention job = %+v", j)
	}
	left, _ := e.d.Backups(e.ctx(), e.envID, kuma)
	ids := map[string]bool{}
	for _, x := range left.Backups {
		ids[x.ID] = true
	}
	if ids[old1] || !ids[old2] || !ids[recent] {
		t.Fatalf("left = %v", ids)
	}
	// Without a retention key nothing expires.
	b.RetentionAccessKeyID, b.RetentionSecretAccessKey = "", ""
	if err := e.d.applyRetention(e.ctx(), snap, e.d.recipe(snap), b); err != nil {
		t.Fatal(err)
	}
	if n := len(e.run.JobsOfKind("retention")); n != 1 {
		t.Fatalf("retention ran without the retention key")
	}
}

func TestWALGRetentionAndDelete(t *testing.T) {
	e := newEnv(t)
	b := e.setBucket()
	pg := e.addTemplateService("postgres", "Postgres", true)
	if _, err := e.d.SetBackupSchedule(e.ctx(), e.envID, pg, api.BackupSchedule{Enabled: true, RetainDays: 2, ContinuousWAL: true}); err != nil {
		t.Fatal(err)
	}
	now := e.clock.Now()
	for i := 0; i < 4; i++ {
		e.insertBackup(pg, "postgres_wal", "ok", "base_00000001000000000000000"+string(rune('2'+i)), now.Add(-time.Duration(4-i)*24*time.Hour))
	}
	snap, _ := e.d.instance(e.ctx(), e.envID, pg)
	if err := e.d.applyRetention(e.ctx(), snap, e.d.recipe(snap), b); err != nil {
		t.Fatal(err)
	}
	j := e.run.JobsOfKind("retention")[0].Job
	if strings.Join(j.Cmd, " ") != "/opt/5050/bin/wal-g delete retain FULL 2 --confirm" || j.Env["AWS_SECRET_ACCESS_KEY"] != "janitor-secret" {
		t.Fatalf("wal-g retention = %v %v", j.Cmd, j.Env["AWS_SECRET_ACCESS_KEY"])
	}
	r, _ := e.d.Backups(e.ctx(), e.envID, pg)
	if len(r.Backups) != 2 {
		t.Fatalf("%d backups left, want 2", len(r.Backups))
	}
	// Delete marks the bucket with the retention key; a locked one is refused.
	if _, err := e.d.LockBackup(e.ctx(), r.Backups[0].ID, true); err != nil {
		t.Fatal(err)
	}
	mark := e.run.JobsOfKind("maintenance")
	if len(mark) != 1 || strings.Join(mark[0].Job.Cmd, " ") != "/opt/5050/bin/wal-g backup-mark "+"base_000000010000000000000005" || mark[0].Job.Env["AWS_ACCESS_KEY_ID"] != "writer" {
		t.Fatalf("mark = %+v", mark)
	}
	if err := e.d.DeleteBackup(e.ctx(), r.Backups[0].ID); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("delete locked: %v", err)
	}
	if err := e.d.DeleteBackup(e.ctx(), r.Backups[1].ID); err != nil {
		t.Fatal(err)
	}
	del := e.run.JobsOfKind("retention")
	if got := strings.Join(del[len(del)-1].Job.Cmd, " "); got != "/opt/5050/bin/wal-g delete target base_000000010000000000000004 --confirm" {
		t.Fatalf("delete cmd = %s", got)
	}
}

func TestBackupSettingsChangedRedeploysArchivingDatabases(t *testing.T) {
	e := newEnv(t)
	e.setBucket()
	pg := e.addTemplateService("postgres", "Postgres", true)
	e.addTemplateService("redis", "Redis", true)
	e.addTemplateService("postgres", "Postgres", false) // not running: picks it up later
	if err := e.d.BackupSettingsChanged(e.ctx()); err != nil {
		t.Fatal(err)
	}
	d := e.run.Deploys()
	if len(d) != 1 || d[0].ServiceID != pg {
		t.Fatalf("deploys = %+v", d)
	}
	// The base backup follows once the database runs again.
	e.clock.Add(time.Minute)
	if err := e.d.runPendingBaseBackups(e.ctx()); err != nil {
		t.Fatal(err)
	}
	e.wait()
	if n := len(e.run.Jobs()); n != 1 {
		t.Fatalf("%d base backups, want 1", n)
	}
}
