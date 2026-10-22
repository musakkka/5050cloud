// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/pkg/api"
)

// depRow is one o_deployments record.
type depRow struct {
	ID         string
	EnvID      string
	ServiceID  string
	ProjectID  string
	Status     api.DeploymentStatus
	Trigger    string
	Actor      string
	Message    string
	ConfigRev  int64
	Config     api.ServiceConfig
	Commit     *api.Commit
	Image      string // repository path inside the internal registry ("svc_x:dep_y"), or "" before the build
	Digest     string
	RollbackOf string
	Diagnosis  string
	Reason     string
	Steps      []api.DeployStep
	EnvEnc     []byte
	IsNode     bool
	StartCmd   string // detected by the build
	UploadPath string
	Rebuild    bool
	SourceMap  map[string]string
	ConfigFile *api.ConfigFileInfo
	Notices    []string
	BuildHost  string
	Ref        string
	WaitFor    []string
	CreatedAt  int64
	StartedAt  int64
	FinishedAt int64
	Active     bool
}

const depCols = `id, environment_id, service_id, project_id, status, trigger, actor, message, config_rev, config, commit_info,
	image, digest, rollback_of, diagnosis, reason, steps, env_enc, is_node, start_cmd_detected, upload_path, rebuild,
	source_map, config_file, notices, build_host, ref, wait_for, created_at, started_at, finished_at, active`

func scanDep(sc interface{ Scan(...any) error }) (depRow, error) {
	var d depRow
	var cfg, commit, steps, src, cfile, notices, wait string
	var isNode, rebuild, active int
	var status string
	err := sc.Scan(&d.ID, &d.EnvID, &d.ServiceID, &d.ProjectID, &status, &d.Trigger, &d.Actor, &d.Message, &d.ConfigRev, &cfg, &commit,
		&d.Image, &d.Digest, &d.RollbackOf, &d.Diagnosis, &d.Reason, &steps, &d.EnvEnc, &isNode, &d.StartCmd, &d.UploadPath, &rebuild,
		&src, &cfile, &notices, &d.BuildHost, &d.Ref, &wait, &d.CreatedAt, &d.StartedAt, &d.FinishedAt, &active)
	if err != nil {
		return d, err
	}
	d.Status = api.DeploymentStatus(status)
	_ = json.Unmarshal([]byte(cfg), &d.Config)
	if commit != "" {
		var c api.Commit
		if json.Unmarshal([]byte(commit), &c) == nil {
			d.Commit = &c
		}
	}
	_ = json.Unmarshal([]byte(steps), &d.Steps)
	_ = json.Unmarshal([]byte(src), &d.SourceMap)
	if cfile != "" {
		var f api.ConfigFileInfo
		if json.Unmarshal([]byte(cfile), &f) == nil {
			d.ConfigFile = &f
		}
	}
	_ = json.Unmarshal([]byte(notices), &d.Notices)
	_ = json.Unmarshal([]byte(wait), &d.WaitFor)
	d.IsNode, d.Rebuild, d.Active = isNode != 0, rebuild != 0, active != 0
	return d, nil
}

func jsonString(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func insertDep(ctx context.Context, tx *sql.Tx, d depRow) error {
	commit := ""
	if d.Commit != nil {
		commit = jsonString(d.Commit)
	}
	cfile := ""
	if d.ConfigFile != nil {
		cfile = jsonString(d.ConfigFile)
	}
	if d.Steps == nil {
		d.Steps = []api.DeployStep{}
	}
	if d.WaitFor == nil {
		d.WaitFor = []string{}
	}
	if d.Notices == nil {
		d.Notices = []string{}
	}
	if d.SourceMap == nil {
		d.SourceMap = map[string]string{}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO o_deployments (`+depCols+`) VALUES
		(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.EnvID, d.ServiceID, d.ProjectID, string(d.Status), d.Trigger, d.Actor, d.Message, d.ConfigRev, jsonString(d.Config), commit,
		d.Image, d.Digest, d.RollbackOf, d.Diagnosis, d.Reason, jsonString(d.Steps), d.EnvEnc, b2i(d.IsNode), d.StartCmd, d.UploadPath, b2i(d.Rebuild),
		jsonString(d.SourceMap), cfile, jsonString(d.Notices), d.BuildHost, d.Ref, jsonString(d.WaitFor), d.CreatedAt, d.StartedAt, d.FinishedAt, b2i(d.Active))
	return err
}

func (o *Orchestrator) loadDep(ctx context.Context, db hub.DB, id string) (depRow, error) {
	d, err := scanDep(db.QueryRowContext(ctx, `SELECT `+depCols+` FROM o_deployments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, errNoDeployment
	}
	return d, err
}

var errNoDeployment = errorf("This deployment does not exist.")

func (o *Orchestrator) queryDeps(ctx context.Context, where string, args ...any) ([]depRow, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT `+depCols+` FROM o_deployments WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []depRow
	for rows.Next() {
		d, err := scanDep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// activeDep returns the active deployment of a service instance.
func (o *Orchestrator) activeDep(ctx context.Context, envID, svcID string) (depRow, bool, error) {
	ds, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? AND active = 1 ORDER BY created_at DESC LIMIT 1`, envID, svcID)
	if err != nil || len(ds) == 0 {
		return depRow{}, false, err
	}
	return ds[0], true, nil
}

// repRow is one o_replicas record.
type repRow struct {
	ID           string
	DeploymentID string
	ServiceID    string
	EnvID        string
	HostID       string
	IP           string
	State        string
	ContainerID  string
	Restarts     int
	ExitCode     *int
	OOM          bool
	Diagnosis    string
	Listening    []int
	LoopbackOnly []int
	StartedAt    int64
	UpdatedAt    int64
	CreatedAt    int64
	Desired      string
	Serving      bool
	Draining     bool
	Adopted      bool
	RemovedAt    int64
}

const repCols = `id, deployment_id, service_id, environment_id, host_id, ip, state, container_id, restarts, exit_code, oom,
	diagnosis, listening_ports, loopback_only, started_at, updated_at, created_at, desired, serving, draining, adopted, removed_at`

func scanRep(sc interface{ Scan(...any) error }) (repRow, error) {
	var r repRow
	var exit sql.NullInt64
	var oom, serving, draining, adopted int
	var lp, lo string
	err := sc.Scan(&r.ID, &r.DeploymentID, &r.ServiceID, &r.EnvID, &r.HostID, &r.IP, &r.State, &r.ContainerID, &r.Restarts, &exit, &oom,
		&r.Diagnosis, &lp, &lo, &r.StartedAt, &r.UpdatedAt, &r.CreatedAt, &r.Desired, &serving, &draining, &adopted, &r.RemovedAt)
	if err != nil {
		return r, err
	}
	if exit.Valid {
		v := int(exit.Int64)
		r.ExitCode = &v
	}
	_ = json.Unmarshal([]byte(lp), &r.Listening)
	_ = json.Unmarshal([]byte(lo), &r.LoopbackOnly)
	r.OOM, r.Serving, r.Draining, r.Adopted = oom != 0, serving != 0, draining != 0, adopted != 0
	return r, nil
}

func (o *Orchestrator) queryReps(ctx context.Context, where string, args ...any) ([]repRow, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT `+repCols+` FROM o_replicas WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []repRow
	for rows.Next() {
		r, err := scanRep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func insertRep(ctx context.Context, tx *sql.Tx, r repRow) error {
	if r.Desired == "" {
		r.Desired = desiredRunning
	}
	if r.State == "" {
		r.State = "pending"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO o_replicas (`+repCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0, '', '[]', '[]', 0, ?, ?, ?, ?, ?, ?, 0)`,
		r.ID, r.DeploymentID, r.ServiceID, r.EnvID, r.HostID, r.IP, r.State, r.ContainerID, r.Restarts,
		r.CreatedAt, r.CreatedAt, r.Desired, b2i(r.Serving), b2i(r.Draining), b2i(r.Adopted))
	return err
}

// Replica desired states.
const (
	desiredRunning = "running"
	desiredStopped = "stopped"
	desiredRemoved = "removed" // left out of the desired state; the row stays until the agent confirms
)

// live reports whether a replica is still part of what should exist.
func (r repRow) live() bool { return r.Desired == desiredRunning || r.Desired == desiredStopped }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func closeRows(r *sql.Rows) { _ = r.Close() }

func shortID(id string) string {
	if i := strings.IndexByte(id, '_'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 7 {
		return id[len(id)-7:]
	}
	return id
}

func terminal(s api.DeploymentStatus) bool {
	switch s {
	case api.DepSuccess, api.DepFailed, api.DepCrashed, api.DepCompleted, api.DepSkipped, api.DepRemoved, api.DepCancelled:
		return true
	}
	return false
}
