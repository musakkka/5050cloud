// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
)

// apiDeployment converts a deployment row to the API shape.
func (o *Orchestrator) apiDeployment(d depRow) api.Deployment {
	steps := d.Steps
	if steps == nil {
		steps = []api.DeployStep{}
	}
	out := api.Deployment{
		ID: d.ID, ServiceID: d.ServiceID, EnvironmentID: d.EnvID, Status: d.Status,
		Label: api.StatusLabel(d.Status, d.Active), Active: d.Active, Diagnosis: d.Diagnosis, Reason: d.Reason,
		Trigger: d.Trigger, Actor: d.Actor, Commit: d.Commit, Digest: d.Digest, RollbackOf: d.RollbackOf,
		Message: d.Message, Steps: steps, CreatedAt: d.CreatedAt, StartedAt: d.StartedAt, FinishedAt: d.FinishedAt,
	}
	if d.Image != "" {
		out.Image = o.publicImageRef(d)
	}
	return out
}

// publicImageRef is the image reference shown in the UI.
func (o *Orchestrator) publicImageRef(d depRow) string {
	if !strings.HasPrefix(d.Image, "svc_") {
		return d.Image
	}
	return o.plan.MeshAddr(1).String() + ":" + registryPort + "/" + d.Image
}

func apiEvent(kind, envID, svcID string, d *api.Deployment) api.Event {
	return api.Event{Type: kind, EnvironmentID: envID, ServiceID: svcID, Deployment: d}
}

func apiActivity(svcID, kind, text, actor, ref, status string, now time.Time) api.Activity {
	return api.Activity{TS: now.UnixMilli(), ServiceID: svcID, Kind: kind, Text: text, Actor: actor, Ref: ref, Status: status}
}

// withExtras fills the fields that need more than the row: restarts and canRollback.
func (o *Orchestrator) withExtras(ctx context.Context, d depRow) api.Deployment {
	out := o.apiDeployment(d)
	out.CanRollback = !d.Active && d.Image != "" && o.imageRetained(ctx, d) &&
		(d.Status == api.DepSuccess || d.Status == api.DepRemoved || d.Status == api.DepCrashed || d.Status == api.DepRemoving)
	reps, err := o.queryReps(ctx, `deployment_id = ?`, d.ID)
	if err == nil {
		for _, r := range reps {
			out.Restarts += r.Restarts
		}
	}
	return out
}

// Deployments lists a service instance's deployments, newest first. query
// filters by id, status, label, trigger, actor, message or commit.
func (o *Orchestrator) Deployments(ctx context.Context, envID, serviceID string, limit int, query string) ([]api.Deployment, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	fetch := limit
	q := strings.ToLower(strings.TrimSpace(query))
	if q != "" {
		fetch = 1000
	}
	ds, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, envID, serviceID, fetch)
	if err != nil {
		return nil, err
	}
	out := []api.Deployment{}
	for _, d := range ds {
		if q != "" && !depMatches(d, q) {
			continue
		}
		out = append(out, o.withExtras(ctx, d))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func depMatches(d depRow, q string) bool {
	fields := []string{d.ID, string(d.Status), api.StatusLabel(d.Status, d.Active), d.Trigger, d.Actor, d.Message, d.Diagnosis, d.Reason}
	if d.Commit != nil {
		fields = append(fields, d.Commit.SHA, d.Commit.Message, d.Commit.Author, d.Commit.Branch)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// Deployment returns one deployment with what it ran with.
func (o *Orchestrator) Deployment(ctx context.Context, deploymentID string) (api.DeploymentDetail, error) {
	d, err := o.loadDep(ctx, o.db, deploymentID)
	if err != nil {
		return api.DeploymentDetail{}, err
	}
	det := api.DeploymentDetail{Deployment: o.withExtras(ctx, d), Config: d.Config, VariableNames: []string{},
		Source: d.SourceMap, ConfigFile: d.ConfigFile, Notices: d.Notices, Replicas: []api.Replica{}}
	if env, err := o.openEnv(d.ID, d.EnvEnc); err == nil {
		for k := range env {
			det.VariableNames = append(det.VariableNames, k)
		}
		sort.Strings(det.VariableNames)
	}
	reps, err := o.queryReps(ctx, `deployment_id = ? ORDER BY created_at, id`, d.ID)
	if err != nil {
		return det, err
	}
	names := o.hostNames(ctx)
	single := sched.Singleton(d.Config, sched.Kind(d.Config, false))
	for _, r := range reps {
		if !r.live() && r.RemovedAt > 0 {
			continue
		}
		det.Replicas = append(det.Replicas, o.apiReplica(r, names, single))
	}
	return det, nil
}

func (o *Orchestrator) hostNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return out
	}
	for _, h := range hosts {
		out[h.ID] = h.Name
	}
	return out
}

func (o *Orchestrator) apiReplica(r repRow, names map[string]string, singleton bool) api.Replica {
	state := r.State
	if state == "" {
		state = "pending"
	}
	out := api.Replica{ID: r.ID, DeploymentID: r.DeploymentID, HostID: r.HostID, HostName: names[r.HostID], State: state,
		Restarts: r.Restarts, StartedAt: r.StartedAt, Singleton: singleton}
	if r.live() && !o.hub.Connected(r.HostID) {
		if singleton {
			out.Note = "Host offline: singleton not moved. Move now"
		} else {
			out.Note = "Host offline."
		}
	}
	return out
}

// BuildLogs writes stored build lines, then live ones while following.
func (o *Orchestrator) BuildLogs(ctx context.Context, deploymentID string, follow bool, emit func([]api.LogLine) error) error {
	if _, err := o.loadDep(ctx, o.db, deploymentID); err != nil {
		return err
	}
	if follow {
		// A client that connects right after an upload can arrive before the
		// build log exists; wait for it while the deployment is queued or
		// building instead of ending the stream empty.
		for {
			n := 0
			err := o.builds.Follow(ctx, deploymentID, func(l []api.LogLine) error { n += len(l); return emit(l) })
			if err != nil || n > 0 || ctx.Err() != nil {
				return err
			}
			d, err := o.loadDep(ctx, o.db, deploymentID)
			if err != nil || (d.Status != api.DepQueued && d.Status != api.DepWaiting && d.Status != api.DepBuilding) {
				return err
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	lines, err := o.builds.Read(deploymentID)
	if err != nil {
		return err
	}
	if lines == nil {
		lines = []api.LogLine{}
	}
	return emit(lines)
}
