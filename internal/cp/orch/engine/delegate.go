// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Data routes forward to WP2b's data.Service (internal/cp/orch/README.md).

func (o *Orchestrator) CronRuns(ctx context.Context, envID, serviceID string, limit int) ([]api.CronRun, error) {
	return o.data.CronRuns(ctx, envID, serviceID, limit)
}

func (o *Orchestrator) RunCronNow(ctx context.Context, envID, serviceID, actor string) (api.CronRun, error) {
	return o.data.RunCronNow(ctx, envID, serviceID, actor)
}

func (o *Orchestrator) Backups(ctx context.Context, envID, serviceID string) (api.BackupsResponse, error) {
	return o.data.Backups(ctx, envID, serviceID)
}

func (o *Orchestrator) BackupNow(ctx context.Context, envID, serviceID, actor string) (api.Backup, error) {
	return o.data.BackupNow(ctx, envID, serviceID, actor)
}

func (o *Orchestrator) SetBackupSchedule(ctx context.Context, envID, serviceID string, s api.BackupSchedule) (api.BackupSchedule, error) {
	return o.data.SetBackupSchedule(ctx, envID, serviceID, s)
}

func (o *Orchestrator) LockBackup(ctx context.Context, backupID string, locked bool) (api.Backup, error) {
	return o.data.LockBackup(ctx, backupID, locked)
}

func (o *Orchestrator) DeleteBackup(ctx context.Context, backupID string) error {
	return o.data.DeleteBackup(ctx, backupID)
}

func (o *Orchestrator) Restore(ctx context.Context, envID, serviceID string, req api.RestoreRequest, actor string) (api.RestoreResponse, error) {
	return o.data.Restore(ctx, envID, serviceID, req, actor)
}

func (o *Orchestrator) Promote(ctx context.Context, envID, serviceID string, req api.PromoteRequest, actor string) error {
	return o.data.Promote(ctx, envID, serviceID, req, actor)
}

func (o *Orchestrator) MajorUpgrade(ctx context.Context, envID, serviceID string, req api.MajorUpgradeRequest, actor string) (api.RestoreResponse, error) {
	return o.data.MajorUpgrade(ctx, envID, serviceID, req, actor)
}

func (o *Orchestrator) RegenerateCredentials(ctx context.Context, envID, serviceID, actor string) (api.CommitResponse, error) {
	return o.data.RegenerateCredentials(ctx, envID, serviceID, actor)
}

func (o *Orchestrator) TestBucket(ctx context.Context, s api.BackupSettings) api.TestResult {
	return o.data.TestBucket(ctx, s)
}

func (o *Orchestrator) BackupSettingsChanged(ctx context.Context) error {
	return o.data.BackupSettingsChanged(ctx)
}

func (o *Orchestrator) DataSummary(ctx context.Context, envID, serviceID string) (api.DataResponse, error) {
	return o.data.DataSummary(ctx, envID, serviceID)
}

func (o *Orchestrator) Extensions(ctx context.Context, envID, serviceID string) ([]api.Extension, error) {
	return o.data.Extensions(ctx, envID, serviceID)
}

func (o *Orchestrator) EnableExtension(ctx context.Context, envID, serviceID, name, actor string) (api.Extension, error) {
	return o.data.EnableExtension(ctx, envID, serviceID, name, actor)
}

func (o *Orchestrator) RedisKeys(ctx context.Context, envID, serviceID, match, cursor string) (api.RedisKeysPage, error) {
	return o.data.RedisKeys(ctx, envID, serviceID, match, cursor)
}

func (o *Orchestrator) RevealRedisKey(ctx context.Context, envID, serviceID, key string) (api.RedisRevealResponse, error) {
	return o.data.RevealRedisKey(ctx, envID, serviceID, key)
}

func (o *Orchestrator) WipeVolume(ctx context.Context, envID, serviceID, actor string) error {
	return o.data.WipeVolume(ctx, envID, serviceID, actor)
}

// VolumeRuntime is what the volume panel needs: host, usage and free space.
func (o *Orchestrator) VolumeRuntime(ctx context.Context, envID, serviceID string) (cpapi.VolumeRuntime, error) {
	snap, err := o.cfg.ServiceInstance(ctx, envID, serviceID)
	if err != nil {
		return cpapi.VolumeRuntime{}, err
	}
	if snap.Volume == nil {
		return cpapi.VolumeRuntime{}, errorf("This service has no volume.")
	}
	hostID, err := o.volumeHost(ctx, snap.Volume.ID)
	if err != nil || hostID == "" {
		return cpapi.VolumeRuntime{}, err
	}
	vr := cpapi.VolumeRuntime{HostID: hostID}
	h, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		vr.HostLost = true
		return vr, nil //nolint:nilerr // a lost host is reported through HostLost
	}
	vr.HostName = h.Name
	vr.HostLost = h.State == sched.StateLost
	disk := o.hostDataDisk(ctx, hostID)
	vr.HostTotalBytes = disk.Total
	vr.HostFreeBytes = disk.Total - disk.Used
	if pts, err := o.met.Raw(ctx, envID, serviceID, "disk", o.now().UnixMilli()-15*60*1000, o.now().UnixMilli()+1); err == nil && len(pts) > 0 {
		vr.UsedBytes = int64(pts[len(pts)-1].Value)
	}
	return vr, nil
}
