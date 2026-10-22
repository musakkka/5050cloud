// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"fmt"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// WAL guard thresholds (docs/SPEC.md 9.13): pg_wal above 1 GB, or a segment
// waiting more than 10 minutes to be archived.
const (
	walGuardBytes = 1 << 30
	walGuardAge   = 10 * time.Minute
)

type walInfo struct {
	walBytes        int64
	oldestSec       int64
	sampledAt       int64
	failingSince    int64
	staleNotifiedAt int64
	firstSeen       int64
}

func (d *Data) walState(ctx context.Context, envID, serviceID string) walInfo {
	var w walInfo
	_ = d.st.db.QueryRowContext(ctx, `SELECT wal_bytes, oldest_unarchived_sec, wal_sampled_at, wal_failing_since,
		stale_notified_at, first_seen_at FROM o_data_services WHERE service_id = ? AND environment_id = ?`,
		serviceID, envID).Scan(&w.walBytes, &w.oldestSec, &w.sampledAt, &w.failingSince, &w.staleNotifiedAt, &w.firstSeen)
	return w
}

// walMessage is the sentence on the database card while archiving fails.
func walMessage(w walInfo) string {
	mins := w.oldestSec / 60
	return fmt.Sprintf("WAL archiving failing for %d min, pg_wal is %s. The volume fills if this continues.", mins, humanBytes(w.walBytes))
}

// walFailing applies the thresholds to one probe sample.
func walFailing(s proto.DBSample) bool {
	return s.WALBytes > walGuardBytes || time.Duration(s.OldestUnarchivedSec)*time.Second > walGuardAge
}

// DBSample feeds the Postgres probe into the WAL guard and backup health.
// Entering the failing state fires backup.failed once; leaving it writes an
// activity line.
func (d *Data) DBSample(ctx context.Context, envID, serviceID string, s proto.DBSample) {
	now := d.now().UnixMilli()
	if err := d.st.ensureServiceRow(ctx, envID, serviceID, now); err != nil {
		return
	}
	prev := d.walState(ctx, envID, serviceID)
	failing := walFailing(s)
	if failing {
		// Only a database that is supposed to archive can fail at it.
		snap, err := d.instance(ctx, envID, serviceID)
		if err != nil {
			return
		}
		info := d.recipe(snap)
		_, configured, _ := d.bucket(ctx)
		sch, _ := d.schedule(ctx, snap, info)
		if !configured || info.recipe == nil || !info.recipe.Continuous || !sch.ContinuousWAL || d.isUnpromotedCopy(ctx, envID, serviceID) {
			failing = false
		}
	}
	since := prev.failingSince
	switch {
	case failing && since == 0:
		since = now - s.OldestUnarchivedSec*1000
		if since > now || since <= 0 {
			since = now
		}
	case !failing:
		since = 0
	}
	_, _ = d.st.exec(ctx, `UPDATE o_data_services SET wal_bytes = ?, oldest_unarchived_sec = ?, wal_sampled_at = ?,
		wal_failing_since = ? WHERE service_id = ? AND environment_id = ?`,
		s.WALBytes, s.OldestUnarchivedSec, now, since, serviceID, envID)

	if failing == (prev.failingSince != 0) {
		return
	}
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return
	}
	if failing {
		w := walInfo{walBytes: s.WALBytes, oldestSec: s.OldestUnarchivedSec}
		d.notifyBackupFailed(ctx, snap, "", snap.Config.Name+": "+walMessage(w))
		return
	}
	d.activity(ctx, envID, serviceID, fmt.Sprintf("WAL archiving of %s caught up.", snap.Config.Name), "", "")
}
