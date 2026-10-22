// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// staleAfter is how old the last good backup may be before health turns
// stale (docs/SPEC.md 9.13).
const staleAfter = 26 * time.Hour

// cronParser accepts 5-field crontab lines and @hourly, @daily, @weekly,
// @monthly (docs/SPEC.md 9.9).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func defaultSchedule(info recipeInfo) api.BackupSchedule {
	return api.BackupSchedule{
		// Databases are backed up by default; a plain volume is opt-in.
		Enabled:       info.recipe != nil && !info.volumeOnly,
		Cron:          "0 3 * * *",
		Timezone:      "UTC",
		RetainDays:    7,
		ContinuousWAL: info.recipe != nil && info.recipe.Continuous,
	}
}

// scheduleRow reads the stored schedule of a service.
func (d *Data) scheduleRow(ctx context.Context, envID, serviceID string) (api.BackupSchedule, int64, bool, error) {
	var raw string
	var last int64
	err := d.st.db.QueryRowContext(ctx, `SELECT schedule, last_run_at FROM o_backup_schedules
		WHERE service_id = ? AND environment_id = ?`, serviceID, envID).Scan(&raw, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return api.BackupSchedule{}, 0, false, nil
	}
	if err != nil {
		return api.BackupSchedule{}, 0, false, err
	}
	var s api.BackupSchedule
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return api.BackupSchedule{}, 0, false, fmt.Errorf("the stored backup schedule cannot be read: %w", err)
	}
	return s, last, true, nil
}

// schedule returns the stored schedule or the default.
func (d *Data) schedule(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo) (api.BackupSchedule, error) {
	s, _, ok, err := d.scheduleRow(ctx, snap.EnvID, snap.ServiceID)
	if err != nil || !ok {
		return defaultSchedule(info), err
	}
	return s, nil
}

// normalizeSchedule fills defaults and validates.
func normalizeSchedule(s api.BackupSchedule, info recipeInfo) (api.BackupSchedule, cron.Schedule, *time.Location, error) {
	if s.Cron == "" {
		s.Cron = "0 3 * * *"
	}
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	if s.RetainDays == 0 {
		s.RetainDays = 7
	}
	if s.RetainDays < 1 || s.RetainDays > 3650 {
		return s, nil, nil, fmt.Errorf("Keep backups for 1 to 3650 days, not %d.", s.RetainDays)
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return s, nil, nil, fmt.Errorf("%q is not a time zone name such as UTC or Europe/Berlin.", s.Timezone)
	}
	sched, err := cronParser.Parse(s.Cron)
	if err != nil {
		return s, nil, nil, fmt.Errorf("%q is not a valid schedule: use five fields such as 0 3 * * * or @daily.", s.Cron)
	}
	if info.recipe == nil || !info.recipe.Continuous {
		s.ContinuousWAL = false
	}
	return s, sched, loc, nil
}

// SetBackupSchedule validates and stores a schedule. Turning continuous WAL
// archiving on or off redeploys the database so the setting takes effect.
func (d *Data) SetBackupSchedule(ctx context.Context, envID, serviceID string, s api.BackupSchedule) (api.BackupSchedule, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return api.BackupSchedule{}, err
	}
	info := d.recipe(snap)
	if !info.supported() {
		return api.BackupSchedule{}, unsupportedError(info)
	}
	old, err := d.schedule(ctx, snap, info)
	if err != nil {
		return api.BackupSchedule{}, err
	}
	s, _, _, err = normalizeSchedule(s, info)
	if err != nil {
		return api.BackupSchedule{}, err
	}
	raw, _ := json.Marshal(s)
	if _, err := d.st.exec(ctx, `INSERT INTO o_backup_schedules (service_id, environment_id, schedule, last_run_at)
		VALUES (?, ?, ?, ?) ON CONFLICT (service_id, environment_id) DO UPDATE SET schedule = excluded.schedule`,
		serviceID, envID, string(raw), d.now().UnixMilli()); err != nil {
		return api.BackupSchedule{}, err
	}
	_ = d.st.ensureServiceRow(ctx, envID, serviceID, d.now().UnixMilli())
	if info.recipe.Continuous && old.ContinuousWAL != s.ContinuousWAL {
		msg := "Continuous WAL archiving turned on"
		if !s.ContinuousWAL {
			msg = "Continuous WAL archiving turned off"
		}
		if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: envID, ServiceID: serviceID, Trigger: "redeploy", Message: msg}); err != nil {
			return s, fmt.Errorf("The schedule was saved, but redeploying %s to apply the archiving change failed: %w", snap.Config.Name, err)
		}
		if s.ContinuousWAL {
			d.markPendingBaseBackup(ctx, envID, serviceID)
		}
	}
	return s, nil
}

func unsupportedError(info recipeInfo) error {
	if info.data == "mysql" {
		return errors.New("Backups not supported yet: MySQL backups are not part of v0.1. The docs show a mysqldump cron service you can add.")
	}
	return errors.New("This service has no data to back up: it has no volume and its template has no backup recipe.")
}

// ---------------------------------------------------------------- list and health

const backupColumns = `id, service_id, kind, status, size_bytes, trigger, locked, store_id, error, started_at, finished_at, verified_at, environment_id`

type backupRow struct {
	api.Backup
	StoreID string
	EnvID   string
}

func scanBackup(sc interface{ Scan(...any) error }) (backupRow, error) {
	var r backupRow
	var locked int
	err := sc.Scan(&r.ID, &r.ServiceID, &r.Kind, &r.Status, &r.SizeBytes, &r.Trigger, &locked,
		&r.StoreID, &r.Error, &r.StartedAt, &r.FinishedAt, &r.VerifiedAt, &r.EnvID)
	r.Locked = locked != 0
	return r, err
}

func (d *Data) backupByID(ctx context.Context, id string) (backupRow, error) {
	r, err := scanBackup(d.st.db.QueryRowContext(ctx, `SELECT `+backupColumns+` FROM o_backups WHERE id = ? AND deleted_at = 0`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("The backup %s does not exist or was deleted.", id)
	}
	return r, err
}

func (d *Data) listBackups(ctx context.Context, envID, serviceID string, limit int) ([]backupRow, error) {
	rows, err := d.st.db.QueryContext(ctx, `SELECT `+backupColumns+` FROM o_backups
		WHERE environment_id = ? AND service_id = ? AND deleted_at = 0 AND trigger != 'restore_test'
		ORDER BY started_at DESC LIMIT ?`, envID, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backupRow
	for rows.Next() {
		r, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Backups is the Backups tab: the list, the schedule and the health.
func (d *Data) Backups(ctx context.Context, envID, serviceID string) (api.BackupsResponse, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return api.BackupsResponse{}, err
	}
	info := d.recipe(snap)
	resp := api.BackupsResponse{Backups: []api.Backup{}}
	if info.supported() {
		rows, err := d.listBackups(ctx, envID, serviceID, 200)
		if err != nil {
			return resp, err
		}
		for _, r := range rows {
			resp.Backups = append(resp.Backups, r.Backup)
		}
		if resp.Schedule, err = d.schedule(ctx, snap, info); err != nil {
			return resp, err
		}
	}
	_, configured, _ := d.bucket(ctx)
	resp.Health = d.health(ctx, snap, info, configured)
	return resp, nil
}

// health computes the backup health of one service (docs/SPEC.md 9.13).
func (d *Data) health(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, configured bool) api.BackupHealth {
	h := api.BackupHealth{Configured: configured, Supported: info.supported()}
	if !h.Supported {
		if info.data == "mysql" {
			h.Message = "Backups not supported yet."
		}
		return h
	}
	if !configured {
		h.Message = "Backups off: set a bucket in Settings > Backups."
		return h
	}
	now := d.now()
	var lastOK, firstSeen int64
	_ = d.st.db.QueryRowContext(ctx, `SELECT COALESCE(max(finished_at), 0) FROM o_backups
		WHERE environment_id = ? AND service_id = ? AND status = 'ok' AND deleted_at = 0 AND trigger != 'restore_test'`,
		snap.EnvID, snap.ServiceID).Scan(&lastOK)
	h.LastOK = lastOK
	var lastStatus, lastErr string
	_ = d.st.db.QueryRowContext(ctx, `SELECT status, error FROM o_backups
		WHERE environment_id = ? AND service_id = ? AND status != 'running' AND trigger != 'restore_test'
		ORDER BY started_at DESC LIMIT 1`, snap.EnvID, snap.ServiceID).Scan(&lastStatus, &lastErr)
	if lastStatus == "failed" {
		h.LastError = lastErr
	}
	ws := d.walState(ctx, snap.EnvID, snap.ServiceID)
	firstSeen = ws.firstSeen
	sch, _ := d.schedule(ctx, snap, info)

	stale := false
	switch {
	case lastOK > 0 && now.Sub(time.UnixMilli(lastOK)) > staleAfter:
		stale = true
		h.Message = fmt.Sprintf("The last good backup is %s old.", ageText(now.Sub(time.UnixMilli(lastOK))))
	case lastOK == 0 && firstSeen > 0 && now.Sub(time.UnixMilli(firstSeen)) > staleAfter && sch.Enabled:
		stale = true
		h.Message = "There is no good backup yet."
	}
	if h.LastError != "" {
		stale = true
		h.Message = "The last backup failed: " + h.LastError
	}
	h.RecoverableTo = lastOK
	if info.recipe.Continuous && sch.ContinuousWAL {
		okArchive := ws.failingSince == 0
		h.WALArchiveOK = &okArchive
		h.WALBytes = ws.walBytes
		if okArchive && lastOK > 0 && ws.sampledAt > 0 {
			// Archiving is healthy: everything up to the last archive_timeout
			// (60 s) before the latest probe can be recovered.
			h.RecoverableTo = max(lastOK, ws.sampledAt-60_000)
		}
		if !okArchive {
			stale = true
			h.Message = walMessage(ws)
			if lastOK > 0 {
				h.RecoverableTo = max(lastOK, ws.failingSince)
			}
		}
	}
	h.Stale = stale
	return h
}

func ageText(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d hours", int(d.Hours()))
}

// checkBackupHealth runs every 10 minutes and fires backup.failed once per
// stale episode caused by old or failed backups (the WAL guard notifies for
// archiving itself).
func (d *Data) checkBackupHealth(ctx context.Context) error {
	if d.now().Minute()%10 != 0 {
		return nil
	}
	_, configured, err := d.bucket(ctx)
	if err != nil || !configured {
		return err
	}
	snaps, err := d.cfg.AllInstances(ctx)
	if err != nil {
		return err
	}
	for _, snap := range snaps {
		info := d.recipe(snap)
		if !info.supported() {
			continue
		}
		sch, _ := d.schedule(ctx, snap, info)
		if !sch.Enabled {
			continue
		}
		_ = d.st.ensureServiceRow(ctx, snap.EnvID, snap.ServiceID, d.now().UnixMilli())
		h := d.health(ctx, snap, info, true)
		walFailing := h.WALArchiveOK != nil && !*h.WALArchiveOK
		ws := d.walState(ctx, snap.EnvID, snap.ServiceID)
		switch {
		case h.Stale && !walFailing && ws.staleNotifiedAt == 0:
			d.notifyBackupFailed(ctx, snap, "", fmt.Sprintf("Backups of %s are stale: %s", snap.Config.Name, h.Message))
			_, _ = d.st.exec(ctx, `UPDATE o_data_services SET stale_notified_at = ? WHERE service_id = ? AND environment_id = ?`,
				d.now().UnixMilli(), snap.ServiceID, snap.EnvID)
		case !h.Stale && ws.staleNotifiedAt != 0:
			d.activity(ctx, snap.EnvID, snap.ServiceID, fmt.Sprintf("Backups of %s are healthy again.", snap.Config.Name), "", "")
			_, _ = d.st.exec(ctx, `UPDATE o_data_services SET stale_notified_at = 0 WHERE service_id = ? AND environment_id = ?`,
				snap.ServiceID, snap.EnvID)
		}
	}
	return nil
}

// ---------------------------------------------------------------- taking backups

// BackupNow starts a backup and returns its row (status "running").
func (d *Data) BackupNow(ctx context.Context, envID, serviceID, actor string) (api.Backup, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return api.Backup{}, err
	}
	return d.startBackup(ctx, snap, d.recipe(snap), "manual", actor)
}

// startBackup checks the preconditions, records the row and runs the backup
// in the background.
func (d *Data) startBackup(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, trigger, actor string) (api.Backup, error) {
	if !info.supported() {
		return api.Backup{}, unsupportedError(info)
	}
	b, configured, err := d.bucket(ctx)
	if err != nil {
		return api.Backup{}, err
	}
	if !configured {
		return api.Backup{}, errors.New("Set a backup bucket in Settings > Backups before taking a backup.")
	}
	if err := validateBucket(b); err != nil {
		return api.Backup{}, err
	}
	if err := d.claim(snap.EnvID, snap.ServiceID, "this backup"); err != nil {
		return api.Backup{}, err
	}
	row := api.Backup{
		ID: newID("bak"), ServiceID: snap.ServiceID, Kind: info.recipe.Kind, Status: "running",
		Trigger: trigger, StartedAt: d.now().UnixMilli(),
	}
	if _, err := d.st.exec(ctx, `INSERT INTO o_backups (id, service_id, environment_id, kind, status, trigger, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, row.ID, row.ServiceID, snap.EnvID, row.Kind, row.Status, row.Trigger, row.StartedAt); err != nil {
		d.release(snap.EnvID, snap.ServiceID)
		return api.Backup{}, err
	}
	d.background("backup "+row.ID, func(bg context.Context) {
		defer d.release(snap.EnvID, snap.ServiceID)
		d.performBackup(bg, snap, info, b, row, actor)
	})
	return row, nil
}

type backupSummary struct {
	OK        bool   `json:"ok"`
	SizeBytes int64  `json:"sizeBytes"`
	ID        string `json:"id"`
}

func (d *Data) performBackup(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, b api.BackupSettings, row api.Backup, actor string) {
	out := d.backupJob(ctx, snap, info, b)
	var sum backupSummary
	errText := ""
	switch {
	case !out.ok():
		errText = jobError("The backup", out)
	case lastJSONLine(out.Result.Output, &sum) != nil || !sum.OK || sum.ID == "":
		errText = "The backup finished without reporting where it was stored."
	}
	finished := d.now().UnixMilli()
	if errText != "" {
		_, _ = d.st.exec(ctx, `UPDATE o_backups SET status = 'failed', error = ?, finished_at = ? WHERE id = ?`, errText, finished, row.ID)
		d.notifyBackupFailed(ctx, snap, row.ID, fmt.Sprintf("The backup of %s failed. %s", snap.Config.Name, errText))
		return
	}
	_, _ = d.st.exec(ctx, `UPDATE o_backups SET status = 'ok', size_bytes = ?, store_id = ?, finished_at = ?, error = '' WHERE id = ?`,
		sum.SizeBytes, sum.ID, finished, row.ID)
	_, _ = d.st.exec(ctx, `UPDATE o_data_services SET stale_notified_at = 0 WHERE service_id = ? AND environment_id = ?`,
		snap.ServiceID, snap.EnvID)
	if row.Trigger == "manual" {
		d.activity(ctx, snap.EnvID, snap.ServiceID, fmt.Sprintf("Backed up %s (%s).", snap.Config.Name, humanBytes(sum.SizeBytes)), actor, row.ID)
	}
	if row.Trigger == "schedule" || row.Trigger == "manual" {
		if err := d.applyRetention(ctx, snap, info, b); err != nil {
			d.log.Warn("backup retention", "service", snap.ServiceID, "err", err)
		}
	}
}

// backupJob runs the recipe's backup command where it belongs.
func (d *Data) backupJob(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, b api.BackupSettings) JobOutcome {
	keys, err := d.keys.get(ctx)
	if err != nil {
		return JobOutcome{Err: err}
	}
	env := backupEnv(b, keys, snap.ServiceID, false)
	r := info.recipe
	if r.RunIn == "service" {
		user := ""
		svcEnv := d.serviceEnv(ctx, snap)
		if r.Kind == "postgres_wal" {
			user = "postgres"
			svcEnv["PGDATA"] = pgData(info.svc)
		}
		return d.execInService(ctx, snap, "backup", r.Backup, copyEnv(svcEnv, env), user, nil, backupTimeoutSec)
	}
	// A job with the volume mounted read-only on the volume's host.
	if snap.Volume == nil {
		return JobOutcome{Err: errors.New("the service has no volume")}
	}
	host, err := d.run.VolumeHost(ctx, snap.EnvID, snap.ServiceID)
	if err != nil || host == "" {
		return JobOutcome{Err: errors.New("the volume has not been created on a host yet")}
	}
	image, err := d.cat.HelperImage()
	if err != nil {
		return JobOutcome{Err: err}
	}
	files, err := d.sharedFiles("restic-env", "backup-volume")
	if err != nil {
		return JobOutcome{Err: err}
	}
	job := proto.Job{
		Kind: "backup", Image: image, Cmd: []string{toolDir + "backup-volume"}, Env: env,
		Mounts: []proto.Mount{
			{Kind: "volume", Source: volumeName(snap.Volume.ID), Target: "/data", ReadOnly: true},
			toolMount("restic"),
		},
		Files: files, TimeoutSec: backupTimeoutSec, Labels: jobLabels(snap.EnvID, snap.ServiceID, "backup"),
	}
	if network, ip, release, err := d.run.JobNetwork(ctx, snap.EnvID, host); err == nil {
		defer release()
		job.Network, job.IP = network, ip
	}
	return d.runJob(ctx, host, job)
}

// runDueBackups starts scheduled backups whose time has come.
func (d *Data) runDueBackups(ctx context.Context) error {
	_, configured, err := d.bucket(ctx)
	if err != nil || !configured {
		return err
	}
	snaps, err := d.cfg.AllInstances(ctx)
	if err != nil {
		return err
	}
	now := d.now()
	for _, snap := range snaps {
		info := d.recipe(snap)
		if !info.supported() {
			continue
		}
		_ = d.st.ensureServiceRow(ctx, snap.EnvID, snap.ServiceID, now.UnixMilli())
		sch, last, found, err := d.scheduleRow(ctx, snap.EnvID, snap.ServiceID)
		if err != nil {
			continue
		}
		if !found {
			sch = defaultSchedule(info)
		}
		if !sch.Enabled {
			continue
		}
		_, cs, loc, err := normalizeSchedule(sch, info)
		if err != nil {
			continue
		}
		base := last
		if base == 0 {
			base = d.walState(ctx, snap.EnvID, snap.ServiceID).firstSeen
		}
		if base == 0 {
			base = now.UnixMilli()
		}
		next := cs.Next(time.UnixMilli(base).In(loc))
		if now.Before(next) {
			continue
		}
		raw, _ := json.Marshal(sch)
		// Record the run first: a restart right after this never repeats it.
		if _, err := d.st.exec(ctx, `INSERT INTO o_backup_schedules (service_id, environment_id, schedule, last_run_at)
			VALUES (?, ?, ?, ?) ON CONFLICT (service_id, environment_id) DO UPDATE SET last_run_at = excluded.last_run_at`,
			snap.ServiceID, snap.EnvID, string(raw), now.UnixMilli()); err != nil {
			return err
		}
		if _, err := d.startBackup(ctx, snap, info, "schedule", ""); err != nil {
			d.log.Warn("scheduled backup did not start", "service", snap.ServiceID, "err", err)
			if !strings.Contains(err.Error(), "still running") {
				d.notifyBackupFailed(ctx, snap, "", fmt.Sprintf("The scheduled backup of %s did not start: %s", snap.Config.Name, err.Error()))
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- base backups after changes

func (d *Data) markPendingBaseBackup(ctx context.Context, envID, serviceID string) {
	_ = d.st.ensureServiceRow(ctx, envID, serviceID, d.now().UnixMilli())
	_, _ = d.st.exec(ctx, `UPDATE o_data_services SET pending_base_backup = ? WHERE service_id = ? AND environment_id = ?`,
		d.now().UnixMilli(), serviceID, envID)
}

// runPendingBaseBackups takes the base backup that follows Promote, turning
// archiving on, or a bucket change, once the database runs again.
func (d *Data) runPendingBaseBackups(ctx context.Context) error {
	rows, err := d.st.db.QueryContext(ctx, `SELECT environment_id, service_id, pending_base_backup FROM o_data_services WHERE pending_base_backup > 0`)
	if err != nil {
		return err
	}
	type pending struct {
		env, svc string
		since    int64
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.env, &p.svc, &p.since); err == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	now := d.now()
	for _, p := range list {
		if now.Sub(time.UnixMilli(p.since)) < 20*time.Second {
			continue
		}
		clear := func() {
			_, _ = d.st.exec(ctx, `UPDATE o_data_services SET pending_base_backup = 0 WHERE service_id = ? AND environment_id = ?`, p.svc, p.env)
		}
		snap, err := d.instance(ctx, p.env, p.svc)
		if err != nil {
			clear()
			continue
		}
		if _, _, err := d.run.ActiveReplica(ctx, p.env, p.svc); err != nil {
			if now.Sub(time.UnixMilli(p.since)) > time.Hour {
				clear()
				d.notifyBackupFailed(ctx, snap, "", fmt.Sprintf("The base backup of %s could not start because it has not been running for an hour.", snap.Config.Name))
			}
			continue
		}
		if _, err := d.startBackup(ctx, snap, d.recipe(snap), "schedule", ""); err != nil {
			if strings.Contains(err.Error(), "still running") {
				continue
			}
			d.log.Warn("pending base backup", "service", p.svc, "err", err)
		}
		clear()
	}
	return nil
}

// BackupSettingsChanged redeploys the databases that archive continuously so
// they pick up the new credentials, then takes a base backup of each.
func (d *Data) BackupSettingsChanged(ctx context.Context) error {
	_, configured, err := d.bucket(ctx)
	if err != nil {
		return err
	}
	snaps, err := d.cfg.AllInstances(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, snap := range snaps {
		info := d.recipe(snap)
		if !info.supported() || !info.recipe.Continuous {
			continue
		}
		if _, _, err := d.run.ActiveReplica(ctx, snap.EnvID, snap.ServiceID); err != nil {
			continue // not running; it picks the settings up on its next deploy
		}
		if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: snap.EnvID, ServiceID: snap.ServiceID,
			Trigger: "redeploy", Message: "Backup settings changed"}); err != nil {
			errs = append(errs, fmt.Errorf("redeploy %s: %w", snap.Config.Name, err))
			continue
		}
		if configured {
			d.markPendingBaseBackup(ctx, snap.EnvID, snap.ServiceID)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("Some databases could not be redeployed with the new backup settings: %w", errors.Join(errs...))
	}
	return nil
}

// ---------------------------------------------------------------- retention, lock and delete

// applyRetention removes backups older than the schedule keeps, except
// locked ones, with the retention key on the control host.
func (d *Data) applyRetention(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, b api.BackupSettings) error {
	if !retentionReady(b) {
		return nil // only the retention key may delete; without it nothing expires
	}
	sch, err := d.schedule(ctx, snap, info)
	if err != nil {
		return err
	}
	rows, err := d.listBackups(ctx, snap.EnvID, snap.ServiceID, 10000)
	if err != nil {
		return err
	}
	var ok []backupRow
	for _, r := range rows {
		if r.Status == "ok" && r.StoreID != "" {
			ok = append(ok, r)
		}
	}
	var expire []backupRow
	if info.recipe.Kind == "postgres_wal" {
		// wal-g keeps the newest RetainDays full backups plus locked ones.
		kept := 0
		for _, r := range ok {
			if r.Locked {
				continue
			}
			kept++
			if kept > sch.RetainDays {
				expire = append(expire, r)
			}
		}
		if len(expire) == 0 {
			return nil
		}
		argv, err := renderArgs(info.recipe.Retention, map[string]any{"RetainDays": sch.RetainDays})
		if err != nil {
			return err
		}
		out := d.controlJob(ctx, snap, "retention", d.imageFor(ctx, snap), argv, nil, []string{"wal-g"}, nil, true)
		if !out.ok() {
			return errors.New(jobError("Backup retention", out))
		}
	} else {
		cutoff := d.now().Add(-time.Duration(sch.RetainDays) * 24 * time.Hour).UnixMilli()
		for i, r := range ok {
			if i == 0 || r.Locked || r.FinishedAt >= cutoff {
				continue // the newest good backup always stays
			}
			expire = append(expire, r)
		}
		if len(expire) == 0 {
			return nil
		}
		if out := d.resticForget(ctx, snap, expire); !out.ok() {
			return errors.New(jobError("Backup retention", out))
		}
	}
	now := d.now().UnixMilli()
	for _, r := range expire {
		_, _ = d.st.exec(ctx, `UPDATE o_backups SET deleted_at = ? WHERE id = ?`, now, r.ID)
	}
	return nil
}

// resticForget forgets and prunes snapshots with the retention key.
func (d *Data) resticForget(ctx context.Context, snap cpapi.ServiceSnapshot, rows []backupRow) JobOutcome {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if !isHexID(r.StoreID) {
			return JobOutcome{Err: fmt.Errorf("the stored snapshot id %q is not a restic id", r.StoreID)}
		}
		ids = append(ids, r.StoreID)
	}
	image, err := d.cat.HelperImage()
	if err != nil {
		return JobOutcome{Err: err}
	}
	files, err := d.sharedFiles("restic-env", "retention-restic")
	if err != nil {
		return JobOutcome{Err: err}
	}
	return d.controlJob(ctx, snap, "retention", image, []string{toolDir + "retention-restic"},
		map[string]string{"FORGET_IDS": strings.Join(ids, " ")}, []string{"restic"}, files, true)
}

// controlJob runs a job on the control host (retention and backup marks). The
// retention key is used only when retention is set.
func (d *Data) controlJob(ctx context.Context, snap cpapi.ServiceSnapshot, kind, image string, cmd []string, extra map[string]string, tools []string, files []proto.FileSpec, retention bool) JobOutcome {
	b, _, err := d.bucket(ctx)
	if err != nil {
		return JobOutcome{Err: err}
	}
	keys, err := d.keys.get(ctx)
	if err != nil {
		return JobOutcome{Err: err}
	}
	host, err := d.run.ControlHost(ctx)
	if err != nil {
		return JobOutcome{Err: err}
	}
	mounts := make([]proto.Mount, 0, len(tools))
	for _, t := range tools {
		mounts = append(mounts, toolMount(t))
	}
	return d.runJob(ctx, host, proto.Job{
		Kind: kind, Image: image, Cmd: cmd, Env: copyEnv(backupEnv(b, keys, snap.ServiceID, retention), extra),
		Mounts: mounts, Files: files, TimeoutSec: backupTimeoutSec, Labels: jobLabels(snap.EnvID, snap.ServiceID, kind),
	})
}

func isHexID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func isWalgName(s string) bool {
	if !strings.HasPrefix(s, "base_") || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || r == 'D') {
			return false
		}
	}
	return true
}

// LockBackup keeps a backup out of retention. A locked WAL-G backup is also
// marked permanent in the bucket, so WAL-G's own retention skips it.
func (d *Data) LockBackup(ctx context.Context, backupID string, locked bool) (api.Backup, error) {
	r, err := d.backupByID(ctx, backupID)
	if err != nil {
		return api.Backup{}, err
	}
	if r.Status != "ok" {
		return api.Backup{}, errors.New("Only a finished, good backup can be locked.")
	}
	if r.Kind == "postgres_wal" && r.Locked != locked {
		snap, err := d.instance(ctx, r.EnvID, r.ServiceID)
		if err != nil {
			return api.Backup{}, err
		}
		if !isWalgName(r.StoreID) {
			return api.Backup{}, fmt.Errorf("The stored backup name %q is not a WAL-G backup name.", r.StoreID)
		}
		argv := []string{toolDir + "wal-g", "backup-mark", r.StoreID}
		if !locked {
			argv = []string{toolDir + "wal-g", "backup-mark", "-i", r.StoreID}
		}
		if out := d.controlJob(ctx, snap, "maintenance", d.imageFor(ctx, snap), argv, nil, []string{"wal-g"}, nil, false); !out.ok() {
			return api.Backup{}, errors.New(jobError("Marking the backup in the bucket", out))
		}
	}
	v := 0
	if locked {
		v = 1
	}
	if _, err := d.st.exec(ctx, `UPDATE o_backups SET locked = ? WHERE id = ?`, v, backupID); err != nil {
		return api.Backup{}, err
	}
	r.Locked = locked
	return r.Backup, nil
}

// DeleteBackup removes one backup from the bucket with the retention key.
func (d *Data) DeleteBackup(ctx context.Context, backupID string) error {
	r, err := d.backupByID(ctx, backupID)
	if err != nil {
		return err
	}
	if r.Locked {
		return errors.New("This backup is locked. Unlock it before deleting it.")
	}
	if r.Status == "running" {
		return errors.New("This backup is still running. Wait until it ends, then delete it.")
	}
	if r.Status == "ok" && r.StoreID != "" {
		b, _, err := d.bucket(ctx)
		if err != nil {
			return err
		}
		if !retentionReady(b) {
			return errors.New("Deleting a backup needs the retention key, which only retention jobs use. Add it in Settings > Backups.")
		}
		snap, err := d.instance(ctx, r.EnvID, r.ServiceID)
		if err != nil {
			return err
		}
		var out JobOutcome
		if r.Kind == "postgres_wal" {
			if !isWalgName(r.StoreID) {
				return fmt.Errorf("The stored backup name %q is not a WAL-G backup name.", r.StoreID)
			}
			out = d.controlJob(ctx, snap, "retention", d.imageFor(ctx, snap),
				[]string{toolDir + "wal-g", "delete", "target", r.StoreID, "--confirm"}, nil, []string{"wal-g"}, nil, true)
		} else {
			out = d.resticForget(ctx, snap, []backupRow{r})
		}
		if !out.ok() {
			return errors.New(jobError("Deleting the backup", out))
		}
	}
	_, err = d.st.exec(ctx, `UPDATE o_backups SET deleted_at = ? WHERE id = ?`, d.now().UnixMilli(), backupID)
	return err
}
