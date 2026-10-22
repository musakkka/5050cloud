// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// VolumePlacer is an optional interface of Runner. When the runner
// implements it, a restored copy's volume is created on its host before the
// copy first starts, so the copy never starts empty. Without it the copy is
// deployed once, stopped, wiped and restored (see README.md).
type VolumePlacer interface {
	// PlaceVolume records that the volume of a service that has never been
	// deployed lives on hostID, or picks a host by the service's placement
	// when hostID is "", and returns the host.
	PlaceVolume(ctx context.Context, envID, serviceID, hostID string) (string, error)
}

// ServiceImageSetter is an optional interface of cpapi.Config, needed by
// Upgrade to a new major version: the copy must run the new major's image.
type ServiceImageSetter interface {
	// SetServiceImage commits a new image and template version for a
	// service at once, without deploying it.
	SetServiceImage(ctx context.Context, envID, serviceID, image, templateVersion, actor string) error
}

// Restore states in o_data_restores.
const (
	restoreRunning  = "restoring"
	restoreDone     = "restored"
	restoreFailed   = "failed"
	restorePromoted = "promoted"
)

// Service names are at most 32 characters (docs/SPEC.md 18.1, changes).
const maxServiceName = 32

// copyName builds "<name><suffix>", shortening the name so the result fits.
func copyName(name, suffix string) string {
	keep := maxServiceName - len(suffix)
	if keep < 1 {
		keep = 1
	}
	r := []rune(name)
	if len(r) > keep {
		r = r[:keep]
	}
	return strings.TrimRight(string(r), " -") + suffix
}

// pgTimeLayout is the recovery_target_time format restore-postgres expects.
const pgTimeLayout = "2006-01-02 15:04:05.000-07"

// isUnpromotedCopy reports whether a service is a restored copy (or a major
// upgrade copy) that has not been promoted: its WAL archiving stays off so it
// never writes into the source's archive.
func (d *Data) isUnpromotedCopy(ctx context.Context, envID, serviceID string) bool {
	var state string
	err := d.st.db.QueryRowContext(ctx, `SELECT state FROM o_data_restores WHERE service_id = ? AND environment_id = ?`,
		serviceID, envID).Scan(&state)
	return err == nil && state != restorePromoted
}

type restoreRow struct {
	ServiceID, EnvID, SourceID, SourceEnvID, Kind, BackupID, State, Error string
	TargetTime, RecoveredTo                                               int64
}

func (d *Data) restoreRow(ctx context.Context, serviceID string) (restoreRow, error) {
	var r restoreRow
	err := d.st.db.QueryRowContext(ctx, `SELECT service_id, environment_id, source_service_id, source_environment_id, kind,
		backup_id, state, error, target_time, recovered_to FROM o_data_restores WHERE service_id = ?`, serviceID).Scan(
		&r.ServiceID, &r.EnvID, &r.SourceID, &r.SourceEnvID, &r.Kind, &r.BackupID, &r.State, &r.Error, &r.TargetTime, &r.RecoveredTo)
	return r, err
}

func (d *Data) setRestoreState(ctx context.Context, serviceID, state, errText string, recoveredTo int64) {
	_, _ = d.st.exec(context.WithoutCancel(ctx), `UPDATE o_data_restores SET state = ?, error = ?, recovered_to = CASE WHEN ? > 0 THEN ? ELSE recovered_to END,
		updated_at = ? WHERE service_id = ?`, state, errText, recoveredTo, recoveredTo, d.now().UnixMilli(), serviceID)
}

// pickBackup chooses the backup a restore starts from.
func (d *Data) pickBackup(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, req api.RestoreRequest) (backupRow, int64, error) {
	if req.BackupID != "" && req.PointInTime != 0 {
		return backupRow{}, 0, errors.New("Pick either a backup or a point in time, not both.")
	}
	if req.BackupID != "" {
		r, err := d.backupByID(ctx, req.BackupID)
		if err != nil {
			return r, 0, err
		}
		if r.ServiceID != snap.ServiceID || r.EnvID != snap.EnvID {
			return r, 0, errors.New("That backup belongs to another service.")
		}
		if r.Status != "ok" || r.StoreID == "" {
			return r, 0, errors.New("Only a finished, good backup can be restored.")
		}
		return r, 0, nil
	}
	rows, err := d.listBackups(ctx, snap.EnvID, snap.ServiceID, 10000)
	if err != nil {
		return backupRow{}, 0, err
	}
	if req.PointInTime != 0 {
		if info.recipe.Kind != "postgres_wal" {
			return backupRow{}, 0, errors.New("Point-in-time restore is only available for Postgres. Pick a backup instead.")
		}
		if req.PointInTime > d.now().UnixMilli() {
			return backupRow{}, 0, errors.New("That point in time is in the future.")
		}
		h := d.health(ctx, snap, info, true)
		if h.RecoverableTo > 0 && req.PointInTime > h.RecoverableTo {
			return backupRow{}, 0, fmt.Errorf("The latest point you can restore to is %s UTC.",
				time.UnixMilli(h.RecoverableTo).UTC().Format("2006-01-02 15:04:05"))
		}
		// The newest base backup that finished before the target; WAL replay
		// covers the rest.
		for _, r := range rows {
			if r.Status == "ok" && r.StoreID != "" && r.FinishedAt <= req.PointInTime {
				return r, req.PointInTime, nil
			}
		}
		return backupRow{}, 0, errors.New("No base backup finished before that time, so the copy cannot reach it. Pick a later time.")
	}
	for _, r := range rows {
		if r.Status == "ok" && r.StoreID != "" {
			return r, 0, nil
		}
	}
	return backupRow{}, 0, errors.New("This service has no good backup to restore yet.")
}

// Restore creates "<name>-restored-YYYYMMDD-HHMM" with a new volume and
// restores a backup (or, for Postgres, a point in time) into it. The source
// is never touched. The restore runs in the background; the copy's activity
// feed and the Backups tab show its progress.
func (d *Data) Restore(ctx context.Context, envID, serviceID string, req api.RestoreRequest, actor string) (api.RestoreResponse, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return api.RestoreResponse{}, err
	}
	info := d.recipe(snap)
	if !info.supported() {
		return api.RestoreResponse{}, unsupportedError(info)
	}
	b, configured, err := d.bucket(ctx)
	if err != nil {
		return api.RestoreResponse{}, err
	}
	if !configured {
		return api.RestoreResponse{}, errors.New("Set a backup bucket in Settings > Backups before restoring.")
	}
	bk, target, err := d.pickBackup(ctx, snap, info, req)
	if err != nil {
		return api.RestoreResponse{}, err
	}
	targetEnv := req.TargetEnvironment
	if targetEnv == "" {
		targetEnv = envID
	}
	if targetEnv != envID {
		if _, _, err := d.cfg.Environment(ctx, targetEnv); err != nil {
			return api.RestoreResponse{}, fmt.Errorf("The target environment cannot be found: %w", err)
		}
	}
	name := copyName(snap.Config.Name, "-restored-"+d.now().UTC().Format("20060102-1504"))
	newID, err := d.cfg.CloneServiceForRestore(ctx, envID, serviceID, name, targetEnv)
	if err != nil {
		return api.RestoreResponse{}, fmt.Errorf("The copy %s could not be created: %w", name, err)
	}
	now := d.now().UnixMilli()
	if _, err := d.st.exec(ctx, `INSERT INTO o_data_restores (service_id, environment_id, source_service_id, source_environment_id,
		kind, backup_id, target_time, state, created_at, updated_at) VALUES (?, ?, ?, ?, 'restore', ?, ?, ?, ?, ?)`,
		newID, targetEnv, serviceID, envID, bk.ID, target, restoreRunning, now, now); err != nil {
		return api.RestoreResponse{}, err
	}
	recovered := target
	if recovered == 0 {
		recovered = bk.FinishedAt
	}
	what := "the backup of " + time.UnixMilli(bk.FinishedAt).UTC().Format("2006-01-02 15:04") + " UTC"
	if target != 0 {
		what = time.UnixMilli(target).UTC().Format("2006-01-02 15:04:05") + " UTC"
	}
	d.activity(ctx, envID, serviceID, fmt.Sprintf("Restoring %s of %s into %s.", what, snap.Config.Name, name), actor, newID)
	d.background("restore "+newID, func(bg context.Context) {
		d.performRestore(bg, snap, info, b, bk, target, targetEnv, newID, req.TargetHost, actor)
	})
	return api.RestoreResponse{ServiceID: newID, RecoveredTo: recovered}, nil
}

// performRestore restores into the copy and starts it.
func (d *Data) performRestore(ctx context.Context, src cpapi.ServiceSnapshot, info recipeInfo, b api.BackupSettings, bk backupRow, target int64, envID, copyID, targetHost, actor string) {
	fail := func(err error) {
		msg := sentence(err.Error())
		d.setRestoreState(ctx, copyID, restoreFailed, msg, 0)
		d.notifyBackupFailed(ctx, src, bk.ID, fmt.Sprintf("Restoring %s into a copy failed. %s", src.Config.Name, msg))
	}
	cp, err := d.instance(ctx, envID, copyID)
	if err != nil {
		fail(err)
		return
	}
	if cp.Volume == nil {
		fail(errors.New("The copy has no volume to restore into"))
		return
	}
	if err := d.claim(envID, copyID, "the restore"); err != nil {
		fail(err)
		return
	}
	defer d.release(envID, copyID)

	host, create, err := d.prepareCopyVolume(ctx, cp, targetHost, actor)
	if err != nil {
		fail(err)
		return
	}
	out := d.restoreJob(ctx, restoreSpec{
		src: src, info: info, bucket: b, backup: bk, target: target, into: cp, host: host,
		volume: volumeName(cp.Volume.ID), createVolume: create, wipe: !create,
	})
	if !out.ok() {
		fail(errors.New(jobError("The restore", out)))
		return
	}
	recovered := recoveredTo(out.Result.Output, bk.FinishedAt)
	d.setRestoreState(ctx, copyID, restoreDone, "", recovered)
	if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: envID, ServiceID: copyID, Trigger: "restore", Actor: actor,
		Message: "Restored from " + src.Config.Name}); err != nil {
		fail(fmt.Errorf("The data was restored, but the copy could not be started: %w", err))
		return
	}
	d.activity(ctx, envID, copyID, fmt.Sprintf("%s recovered to %s UTC. Promote it to replace %s.",
		cp.Config.Name, time.UnixMilli(recovered).UTC().Format("2006-01-02 15:04:05"), src.Config.Name), actor, copyID)
}

// prepareCopyVolume puts the copy's volume on a host before the restore. It
// reports whether the restore job must create the volume (it does not exist
// yet) or must wipe it (the copy was started once to create it).
func (d *Data) prepareCopyVolume(ctx context.Context, cp cpapi.ServiceSnapshot, targetHost, actor string) (string, bool, error) {
	if host, _ := d.run.VolumeHost(ctx, cp.EnvID, cp.ServiceID); host != "" {
		if err := d.run.StopService(ctx, cp.EnvID, cp.ServiceID, actor); err != nil {
			return "", false, fmt.Errorf("The copy could not be stopped for the restore: %w", err)
		}
		return host, false, nil
	}
	if vp, ok := d.run.(VolumePlacer); ok {
		host, err := vp.PlaceVolume(ctx, cp.EnvID, cp.ServiceID, targetHost)
		if err != nil {
			return "", false, fmt.Errorf("No host can hold the copy's volume: %w", err)
		}
		return host, true, nil
	}
	// The runner cannot place a volume without a deployment: start the copy
	// once (archiving stays off in copies), then stop it and restore over it.
	if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: cp.EnvID, ServiceID: cp.ServiceID, Trigger: "restore", Actor: actor,
		Message: "Creating the volume of the restored copy"}); err != nil {
		return "", false, fmt.Errorf("The copy could not be deployed to create its volume: %w", err)
	}
	host, err := d.waitVolumeHost(ctx, cp.EnvID, cp.ServiceID, 15*time.Minute)
	if err != nil {
		return "", false, err
	}
	if err := d.run.StopService(ctx, cp.EnvID, cp.ServiceID, actor); err != nil {
		return "", false, fmt.Errorf("The copy could not be stopped for the restore: %w", err)
	}
	return host, false, nil
}

// pollEvery is how often waits poll the runner; tests shorten it.
var pollEvery = 2 * time.Second

func (d *Data) waitVolumeHost(ctx context.Context, envID, serviceID string, limit time.Duration) (string, error) {
	deadline := time.Now().Add(limit)
	for {
		if host, _ := d.run.VolumeHost(ctx, envID, serviceID); host != "" {
			if _, _, err := d.run.ActiveReplica(ctx, envID, serviceID); err == nil {
				return host, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("The copy did not start within %d minutes, so its volume could not be prepared.", int(limit.Minutes()))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// restoreSpec describes one run of a template's restore recipe.
type restoreSpec struct {
	src    cpapi.ServiceSnapshot // the service whose backup is restored
	info   recipeInfo
	bucket api.BackupSettings
	backup backupRow
	target int64                 // Postgres point in time (ms), 0 = end of the archive
	into   cpapi.ServiceSnapshot // the service the data lands in (variables)
	host   string
	volume string
	// createVolume asks the agent to create the volume for the job; wipe
	// empties a volume the copy created on its first start.
	createVolume bool
	wipe         bool
	// check runs the template's check against the restored data (monthly test).
	check bool
	kind  string // job kind, default "restore"
}

// restoreJob runs the restore recipe in a new container with the target
// volume mounted at the template's mount path.
func (d *Data) restoreJob(ctx context.Context, rs restoreSpec) JobOutcome {
	keys, err := d.keys.get(ctx)
	if err != nil {
		return JobOutcome{Err: err}
	}
	r := rs.info.recipe
	env := copyEnv(backupEnv(rs.bucket, keys, rs.src.ServiceID, false), map[string]string{"BACKUP_ID": rs.backup.StoreID})
	if rs.target > 0 {
		env["RESTORE_TARGET_TIME"] = time.UnixMilli(rs.target).UTC().Format(pgTimeLayout)
	}
	if rs.wipe {
		env["FIFTY_RESTORE_WIPE"] = "1"
	}
	kind := rs.kind
	if kind == "" {
		kind = "restore"
	}
	job := proto.Job{Kind: kind, TimeoutSec: restoreTimeoutSec, Labels: jobLabels(rs.into.EnvID, rs.into.ServiceID, kind)}
	if rs.createVolume {
		job.Volumes = []string{rs.volume}
	}
	cmd := append([]string(nil), r.Restore...)
	if r.RunIn == "service" && rs.info.svc != nil {
		// The template's own image, files and tools, with the copy's variables.
		rendered, err := d.renderService(ctx, rs.into, rs.info.tmpl, rs.info.svc, proto.Facts{}, 0, true)
		if err != nil {
			return JobOutcome{Err: err}
		}
		job.Image = d.imageFor(ctx, rs.src)
		job.Files = rendered.Files
		job.Mounts = rendered.Mounts
		mount := rs.info.svc.Volume
		target := "/data"
		if mount != nil && mount.MountPath != "" {
			target = mount.MountPath
		}
		job.Mounts = append(job.Mounts, proto.Mount{Kind: "volume", Source: rs.volume, Target: target})
		job.Env = copyEnv(d.serviceEnv(ctx, rs.into), rendered.Env, env)
		if rs.info.data == "postgres" {
			job.Env["PGDATA"] = pgData(rs.info.svc)
			job.Env["FIFTY_ARCHIVE"] = "off"
		}
		if rs.check {
			cmd = append(cmd, "--check")
			if rs.info.data == "postgres" && rs.info.svc.Probe != nil {
				cmd = append(cmd, rs.info.svc.Probe.Check...)
			}
		}
	} else {
		image, err := d.cat.HelperImage()
		if err != nil {
			return JobOutcome{Err: err}
		}
		files, err := d.sharedFiles("restic-env", "restore-volume")
		if err != nil {
			return JobOutcome{Err: err}
		}
		job.Image, job.Files, job.Env = image, files, env
		job.Mounts = []proto.Mount{{Kind: "volume", Source: rs.volume, Target: "/data"}, toolMount("restic")}
		cmd = []string{toolDir + "restore-volume"}
		if rs.check {
			cmd = append(cmd, "--check")
		}
	}
	job.Cmd = cmd
	return d.runJob(ctx, rs.host, job)
}

// recoveredTo reads "recoveredTo" from the restore summary, or falls back.
func recoveredTo(output string, fallback int64) int64 {
	var sum struct {
		RecoveredTo string `json:"recoveredTo"`
	}
	if lastJSONLine(output, &sum) != nil || sum.RecoveredTo == "" {
		return fallback
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999-07", "2006-01-02 15:04:05-07", "2006-01-02 15:04:05.999999-07:00", time.RFC3339Nano} {
		if t, err := time.Parse(layout, sum.RecoveredTo); err == nil {
			return t.UnixMilli()
		}
	}
	return fallback
}

// ---------------------------------------------------------------- promote

// Promote gives a restored copy the original's name, so ${{Name.VAR}}
// references resolve to it; the original becomes "<name>-old-<stamp>" and
// stops, dependents redeploy, and archiving turns on in the promoted copy
// followed by a base backup (docs/SPEC.md 9.13).
func (d *Data) Promote(ctx context.Context, envID, serviceID string, req api.PromoteRequest, actor string) error {
	orig, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return err
	}
	if req.RestoredServiceID == "" || req.RestoredServiceID == serviceID {
		return errors.New("Pick the restored copy to promote.")
	}
	rr, err := d.restoreRow(ctx, req.RestoredServiceID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("That service is not a restored copy of this one.")
	}
	if err != nil {
		return err
	}
	if rr.SourceID != serviceID {
		return errors.New("That service is a copy of another service.")
	}
	if rr.EnvID != envID {
		return errors.New("Only a copy in the same environment can be promoted; this one was restored into another environment.")
	}
	switch rr.State {
	case restoreRunning:
		return errors.New("The copy is still being restored. Promote it once the restore has finished.")
	case restoreFailed:
		return errors.New("The restore of this copy failed, so it cannot be promoted. Restore again.")
	case restorePromoted:
		return errors.New("This copy has already been promoted.")
	}
	cp, err := d.instance(ctx, envID, req.RestoredServiceID)
	if err != nil {
		return err
	}
	if err := d.claim(envID, serviceID, "promote"); err != nil {
		return err
	}
	defer d.release(envID, serviceID)

	oldName := copyName(orig.Config.Name, "-old-"+d.now().UTC().Format("20060102-1504"))
	if err := d.cfg.SwapServiceNames(ctx, envID, cp.ServiceID, orig.ServiceID, oldName); err != nil {
		return fmt.Errorf("The names could not be swapped: %w", err)
	}
	d.setRestoreState(ctx, cp.ServiceID, restorePromoted, "", 0)
	d.copySchedule(ctx, orig, cp)
	if err := d.run.StopService(ctx, envID, orig.ServiceID, actor); err != nil {
		d.log.Warn("stop the original after promote", "service", orig.ServiceID, "err", err)
	}
	var errs []error
	if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: envID, ServiceID: cp.ServiceID, Trigger: "restore", Actor: actor,
		Message: "Promoted to " + orig.Config.Name}); err != nil {
		errs = append(errs, fmt.Errorf("redeploy the promoted copy: %w", err))
	} else if d.recipe(cp).recipe != nil {
		d.markPendingBaseBackup(ctx, envID, cp.ServiceID)
	}
	snaps, err := d.cfg.ServiceInstances(ctx, envID)
	if err == nil {
		for _, s := range snaps {
			if s.Deleted || s.ServiceID == cp.ServiceID || s.ServiceID == orig.ServiceID || !contains(s.References, orig.Config.Name) {
				continue
			}
			if _, _, err := d.run.ActiveReplica(ctx, envID, s.ServiceID); err != nil {
				continue // not running: it picks the new values up on its next deploy
			}
			if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: envID, ServiceID: s.ServiceID, Trigger: "redeploy", Actor: actor,
				Message: orig.Config.Name + " was promoted from a restored copy"}); err != nil {
				errs = append(errs, fmt.Errorf("redeploy %s: %w", s.Config.Name, err))
			}
		}
	}
	d.ServiceChanged(ctx, envID, cp.ServiceID)
	d.ServiceChanged(ctx, envID, orig.ServiceID)
	d.activity(ctx, envID, cp.ServiceID, fmt.Sprintf("Promoted the restored copy to %s; the original is now %s and stopped.",
		orig.Config.Name, oldName), actor, cp.ServiceID)
	if len(errs) > 0 {
		return fmt.Errorf("The copy was promoted, but some services could not be redeployed: %w", errors.Join(errs...))
	}
	return nil
}

// copySchedule gives the promoted copy the original's backup schedule.
func (d *Data) copySchedule(ctx context.Context, from, to cpapi.ServiceSnapshot) {
	sch, _, ok, err := d.scheduleRow(ctx, from.EnvID, from.ServiceID)
	if err != nil || !ok {
		return
	}
	_, _ = d.SetBackupSchedule(ctx, to.EnvID, to.ServiceID, sch)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- major upgrade

// MajorUpgrade dumps the source and loads the dump into a new copy running
// the chosen major version, then offers Promote (docs/SPEC.md 9.13).
func (d *Data) MajorUpgrade(ctx context.Context, envID, serviceID string, req api.MajorUpgradeRequest, actor string) (api.RestoreResponse, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return api.RestoreResponse{}, err
	}
	info := d.recipe(snap)
	if info.tmpl == nil || info.svc == nil || len(info.tmpl.MajorVersions) == 0 {
		return api.RestoreResponse{}, errors.New("This service has no major versions to choose from.")
	}
	if info.recipe == nil || len(info.recipe.Dump) == 0 || len(info.recipe.Load) == 0 {
		if info.data == "mysql" {
			return api.RestoreResponse{}, errors.New("Upgrading MySQL to a new major version is not supported yet.")
		}
		return api.RestoreResponse{}, errors.New("This template has no dump and restore recipe for a major version upgrade.")
	}
	if !contains(info.tmpl.MajorVersions, req.Version) {
		return api.RestoreResponse{}, fmt.Errorf("%q is not a version this template offers; pick one of %s.", req.Version, strings.Join(info.tmpl.MajorVersions, ", "))
	}
	current := snap.Config.TemplateVersion
	if current == "" {
		current = info.tmpl.MajorVersions[0]
	}
	if req.Version == current {
		return api.RestoreResponse{}, fmt.Errorf("%s already runs version %s.", snap.Config.Name, req.Version)
	}
	image := d.releaseImage(info, req.Version)
	if image == "" {
		return api.RestoreResponse{}, fmt.Errorf("No image is pinned for version %s.", req.Version)
	}
	setter, ok := d.cfg.(ServiceImageSetter)
	if !ok {
		return api.RestoreResponse{}, errors.New("This control plane cannot change the image of a copy yet, so a major version upgrade is not available.")
	}
	if _, _, err := d.run.ActiveReplica(ctx, envID, serviceID); err != nil {
		return api.RestoreResponse{}, fmt.Errorf("%s must be running to be dumped. Start it, then try again.", snap.Config.Name)
	}
	name := copyName(snap.Config.Name, "-v"+strings.ReplaceAll(req.Version, ".", "-")+"-"+d.now().UTC().Format("20060102-1504"))
	newID, err := d.cfg.CloneServiceForRestore(ctx, envID, serviceID, name, envID)
	if err != nil {
		return api.RestoreResponse{}, fmt.Errorf("The copy %s could not be created: %w", name, err)
	}
	if err := setter.SetServiceImage(ctx, envID, newID, image, req.Version, actor); err != nil {
		return api.RestoreResponse{}, fmt.Errorf("The copy was created but its image could not be set to %s: %w", image, err)
	}
	now := d.now().UnixMilli()
	if _, err := d.st.exec(ctx, `INSERT INTO o_data_restores (service_id, environment_id, source_service_id, source_environment_id,
		kind, state, created_at, updated_at) VALUES (?, ?, ?, ?, 'major_upgrade', ?, ?, ?)`,
		newID, envID, serviceID, envID, restoreRunning, now, now); err != nil {
		return api.RestoreResponse{}, err
	}
	dep, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: envID, ServiceID: newID, Trigger: "restore", Actor: actor,
		Message: "Version " + req.Version + " copy of " + snap.Config.Name})
	if err != nil {
		d.setRestoreState(ctx, newID, restoreFailed, sentence(err.Error()), 0)
		return api.RestoreResponse{}, fmt.Errorf("The copy could not be deployed: %w", err)
	}
	d.activity(ctx, envID, serviceID, fmt.Sprintf("Copying %s into %s on version %s.", snap.Config.Name, name, req.Version), actor, newID)
	d.background("major upgrade "+newID, func(bg context.Context) {
		d.performMajorUpgrade(bg, snap, info, envID, newID, actor)
	})
	return api.RestoreResponse{ServiceID: newID, Deployment: dep, RecoveredTo: now}, nil
}

// releaseImage is the newest image of a major version: images.json releases
// first, then the template's own map.
func (d *Data) releaseImage(info recipeInfo, version string) string {
	if f, err := d.cat.imagesFile(); err == nil {
		if img := f.Releases[info.tmpl.ID][version]; img != "" {
			return img
		}
	}
	return info.svc.Images[version]
}

func (d *Data) performMajorUpgrade(ctx context.Context, src cpapi.ServiceSnapshot, info recipeInfo, envID, copyID, actor string) {
	fail := func(err error) {
		msg := sentence(err.Error())
		d.setRestoreState(ctx, copyID, restoreFailed, msg, 0)
		d.notifyBackupFailed(ctx, src, copyID, fmt.Sprintf("Upgrading %s to a new major version failed. %s", src.Config.Name, msg))
	}
	if _, err := d.waitVolumeHost(ctx, envID, copyID, 15*time.Minute); err != nil {
		fail(err)
		return
	}
	cp, err := d.instance(ctx, envID, copyID)
	if err != nil {
		fail(err)
		return
	}
	// Wait for the new server to accept connections.
	if info.svc.Probe != nil && len(info.svc.Probe.Health) > 0 {
		deadline := time.Now().Add(10 * time.Minute)
		for {
			if out := d.execInService(ctx, cp, "maintenance", info.svc.Probe.Health, nil, "", nil, shortTimeoutSec); out.ok() {
				break
			}
			if time.Now().After(deadline) {
				fail(errors.New("The new version did not become ready within 10 minutes"))
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollEvery):
			}
		}
	}
	hostID, err := d.run.PlaceJob(ctx, envID, nil)
	if err != nil {
		fail(fmt.Errorf("No host can run the dump and load: %w", err))
		return
	}
	network, ip, release, err := d.run.JobNetwork(ctx, envID, hostID)
	if err != nil {
		fail(fmt.Errorf("The dump job could not join the private network: %w", err))
		return
	}
	defer release()
	// Dump from the source and load into the copy in one job, piped, using
	// the new major's client tools (they read older servers).
	script := shellJoin(info.recipe.Dump) + " | " + shellJoin(info.recipe.Load)
	env := copyEnv(d.serviceEnv(ctx, src), map[string]string{
		"SOURCE_HOST": privateDomain(src), "TARGET_HOST": privateDomain(cp),
	})
	if info.data == "postgres" {
		env["PGPASSWORD"] = pick(env, "POSTGRES_PASSWORD", "PGPASSWORD")
	}
	script = "set -o pipefail; " + script
	image, _, _, err := d.run.ActiveImage(ctx, envID, copyID)
	if err != nil || image == "" {
		image = d.releaseImage(info, cp.Config.TemplateVersion)
	}
	out := d.runJob(ctx, hostID, proto.Job{
		Kind: "maintenance", Image: d.cat.Pinned(image), Cmd: []string{"bash", "-c", script}, Env: env,
		Network: network, IP: ip, TimeoutSec: restoreTimeoutSec, Labels: jobLabels(envID, copyID, "major_upgrade"),
	})
	if !out.ok() {
		fail(errors.New(jobError("The dump and restore", out)))
		return
	}
	d.setRestoreState(ctx, copyID, restoreDone, "", d.now().UnixMilli())
	d.activity(ctx, envID, copyID, fmt.Sprintf("%s holds a copy of %s on version %s. Promote it to switch over.",
		cp.Config.Name, src.Config.Name, cp.Config.TemplateVersion), actor, copyID)
}
