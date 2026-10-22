// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/logs"
)

// Retention of control plane files (9.19, 6.4).
const (
	uploadKeep   = time.Hour          // `5050 up` sources, after their build
	replicaKeep  = 7 * 24 * time.Hour // removed replica rows
	buildLogKeep = 90 * 24 * time.Hour
)

// janitorLoop trims the host log tail, deletes old uploads and build logs,
// forgets long-removed replica rows and checks certificate expiry.
func (o *Orchestrator) janitorLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	certs := time.NewTicker(o.t.CertEvery)
	defer certs.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			o.janitor(ctx)
		case <-certs.C:
			o.certExpiryCheck(ctx)
		}
	}
}

func (o *Orchestrator) janitor(ctx context.Context) {
	now := o.now()
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM o_host_logs WHERE ts < ?`, now.Add(-logs.TailKeep).UnixMilli())
		return err
	}); err != nil && ctx.Err() == nil {
		o.log.Warn("janitor: host logs", "err", err)
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM o_replicas WHERE removed_at > 0 AND removed_at < ?`, now.Add(-replicaKeep).UnixMilli())
		return err
	}); err != nil && ctx.Err() == nil {
		o.log.Warn("janitor: replicas", "err", err)
	}
	o.cleanUploads(ctx, now)
	o.cleanBuildLogs(ctx, now)
}

// cleanUploads deletes `5050 up` tarballs an hour after their build ended.
func (o *Orchestrator) cleanUploads(ctx context.Context, now time.Time) {
	dir := filepath.Join(o.o.DataDir, "uploads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".tar.gz") {
			continue
		}
		id := strings.TrimSuffix(name, ".tar.gz")
		d, err := o.loadDep(ctx, o.db, id)
		info, ierr := e.Info()
		switch {
		case err == nil && d.FinishedAt > 0 && now.Sub(time.UnixMilli(d.FinishedAt)) > uploadKeep:
		case err == nil && d.Image != "" && d.Status != "QUEUED" && d.Status != "BUILDING" && ierr == nil && now.Sub(info.ModTime()) > uploadKeep:
		case err != nil && ierr == nil && now.Sub(info.ModTime()) > 24*time.Hour:
			// an upload no deployment claims
		default:
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// cleanBuildLogs deletes build logs of deployments older than 90 days.
func (o *Orchestrator) cleanBuildLogs(ctx context.Context, now time.Time) {
	ds, err := o.queryDeps(ctx, `created_at < ? AND finished_at > 0 ORDER BY created_at LIMIT 500`, now.Add(-buildLogKeep).UnixMilli())
	if err != nil {
		return
	}
	for _, d := range ds {
		_ = o.builds.Delete(d.ID)
	}
}

func noticeFor(event, ref, text string) cpapi.Notice {
	return cpapi.Notice{Event: event, Ref: ref, Text: text}
}
