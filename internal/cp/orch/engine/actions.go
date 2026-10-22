// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ApplyCommit applies a commit plan (9.1, 9.5): deletes and scale changes at
// once, then one deployment per created or updated service, ordered so a
// service waits for the services it references.
func (o *Orchestrator) ApplyCommit(ctx context.Context, plan cpapi.CommitPlan) ([]api.Deployment, error) {
	out := []api.Deployment{}
	var deploy []cpapi.PlanItem
	for _, it := range plan.Items {
		switch it.Action {
		case "delete":
			if err := o.deleteService(ctx, plan.EnvID, it.ServiceID, plan.Actor); err != nil {
				return out, err
			}
		case "scale":
			if err := o.scale(ctx, plan.EnvID, it.ServiceID, plan.Actor); err != nil {
				return out, err
			}
		case "create", "update":
			if plan.Deploy {
				deploy = append(deploy, it)
			} else {
				o.data.ServiceChanged(ctx, plan.EnvID, it.ServiceID)
			}
		default:
			return out, errorf("The commit plan has an unknown action %q.", it.Action)
		}
	}
	o.NetworkChanged(ctx, plan.EnvID)
	if len(deploy) == 0 {
		return out, nil
	}
	snaps, err := o.cfg.ServiceInstances(ctx, plan.EnvID)
	if err != nil {
		return out, err
	}
	byName := map[string]string{}
	refs := map[string][]string{}
	for _, s := range snaps {
		byName[s.Config.Name] = s.ServiceID
		if s.PrivateName != "" {
			byName[s.PrivateName] = s.ServiceID
		}
	}
	ids := make([]string, 0, len(deploy))
	for _, it := range deploy {
		ids = append(ids, it.ServiceID)
	}
	for _, s := range snaps {
		for _, n := range s.References {
			if id := byName[n]; id != "" {
				refs[s.ServiceID] = append(refs[s.ServiceID], id)
			}
		}
	}
	deps := sched.DeployDeps(ids, refs)
	created := map[string]string{} // service -> deployment
	// Create in an order where every dependency already has its deployment id.
	order := topo(ids, deps)
	for _, sid := range order {
		it := itemFor(deploy, sid)
		var wait []string
		for _, dep := range deps[sid] {
			if id := created[dep]; id != "" {
				wait = append(wait, id)
			}
		}
		d, err := o.createDeployment(ctx, depInput{
			EnvID: plan.EnvID, ServiceID: sid, Trigger: "commit", Actor: plan.Actor, Message: plan.Message,
			Rebuild: it.Rebuild || it.Action == "create", WaitFor: wait,
		})
		if err != nil {
			return out, err
		}
		created[sid] = d.ID
		out = append(out, o.apiDeployment(d))
	}
	return out, nil
}

func itemFor(items []cpapi.PlanItem, sid string) cpapi.PlanItem {
	for _, it := range items {
		if it.ServiceID == sid {
			return it
		}
	}
	return cpapi.PlanItem{ServiceID: sid}
}

// topo orders ids so each comes after its deps (deps never contain cycles).
func topo(ids []string, deps map[string][]string) []string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	done := map[string]bool{}
	var out []string
	var visit func(string, int)
	visit = func(s string, depth int) {
		if done[s] || depth > len(ids) {
			return
		}
		for _, d := range deps[s] {
			visit(d, depth+1)
		}
		if !done[s] {
			done[s] = true
			out = append(out, s)
		}
	}
	for _, s := range sorted {
		visit(s, 0)
	}
	return out
}

// depInput describes a new deployment.
type depInput struct {
	EnvID, ServiceID, Trigger, Actor, Message string
	Commit                                    *api.Commit
	Ref                                       string
	UploadPath                                string
	Rebuild                                   bool
	WaitFor                                   []string
	// Reuse copies image, config and variables from this deployment (redeploy, rollback).
	Reuse      *depRow
	RollbackOf string
	Skipped    string // reason for a SKIPPED record
}

// createDeployment inserts a QUEUED deployment and puts it in its lane.
func (o *Orchestrator) createDeployment(ctx context.Context, in depInput) (depRow, error) {
	snap, err := o.cfg.ServiceInstance(ctx, in.EnvID, in.ServiceID)
	if err != nil {
		return depRow{}, errorf("This service does not exist in this environment.")
	}
	if snap.Deleted {
		return depRow{}, errorf("This service was deleted.")
	}
	if snap.Volume != nil && sched.Replicas(snap.Config) > 1 {
		return depRow{}, errorf("A service with a volume can only run 1 replica.")
	}
	now := o.now().UnixMilli()
	d := depRow{
		ID: hub.NewID("dep"), EnvID: in.EnvID, ServiceID: in.ServiceID, ProjectID: snap.ProjectID, Status: api.DepQueued,
		Trigger: in.Trigger, Actor: in.Actor, Message: in.Message, ConfigRev: snap.Rev, Config: snap.Config, Commit: in.Commit,
		Ref: in.Ref, Rebuild: in.Rebuild, WaitFor: in.WaitFor, RollbackOf: in.RollbackOf, CreatedAt: now,
		Steps: []api.DeployStep{{Name: stepQueued, State: "running", StartedAt: now}},
	}
	if in.UploadPath != "" {
		p, err := o.adoptUpload(in.UploadPath, d.ID)
		if err != nil {
			return depRow{}, err
		}
		d.UploadPath = p
		d.Rebuild = true
	}
	if in.Reuse != nil {
		src := *in.Reuse
		env, err := o.openEnv(src.ID, src.EnvEnc)
		if err != nil {
			return depRow{}, err
		}
		cp := make(map[string]string, len(env))
		for k, v := range env {
			cp[k] = v
		}
		for _, k := range []string{"FIFTY_DEPLOYMENT_ID", "RAILWAY_DEPLOYMENT_ID", "FIFTY_SNAPSHOT_ID", "RAILWAY_SNAPSHOT_ID"} {
			if _, ok := cp[k]; ok {
				cp[k] = d.ID
			}
		}
		enc, err := o.sealEnv(d.ID, cp)
		if err != nil {
			return depRow{}, err
		}
		d.EnvEnc, d.Config, d.ConfigRev = enc, src.Config, src.ConfigRev
		d.Image, d.Digest, d.IsNode, d.StartCmd = src.Image, src.Digest, src.IsNode, src.StartCmd
		d.SourceMap, d.ConfigFile, d.Notices, d.Commit, d.BuildHost = src.SourceMap, src.ConfigFile, src.Notices, src.Commit, src.BuildHost
		o.mu.Lock()
		o.envCache[d.ID] = cp
		o.mu.Unlock()
	}
	if in.Skipped != "" {
		d.Status, d.Reason, d.FinishedAt = api.DepSkipped, in.Skipped, now
		d.Steps = []api.DeployStep{{Name: stepQueued, State: "skipped", StartedAt: now, FinishedAt: now}}
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error { return insertDep(ctx, tx, d) }); err != nil {
		return depRow{}, err
	}
	o.publishDep(d)
	if d.Status == api.DepSkipped {
		o.activity(ctx, d.EnvID, d.ServiceID, "deployment", deploymentActivityText(d, d.Status), d.Actor, d.ID, api.LabelSkipped)
		return d, nil
	}
	o.enqueue(d)
	return d, nil
}

// adoptUpload moves a `5050 up` tarball into the orchestrator's uploads directory.
func (o *Orchestrator) adoptUpload(path, depID string) (string, error) {
	dst := filepath.Join(o.o.DataDir, "uploads", depID+".tar.gz")
	if err := os.Rename(path, dst); err == nil {
		return dst, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return "", errorf("The uploaded source could not be read. Run 5050 up again.")
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", errorf("The uploaded source could not be stored: %s", sentenceOf(err))
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return "", errorf("The uploaded source could not be stored: %s", sentenceOf(err))
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	_ = os.Remove(path)
	return dst, nil
}

// Deploy starts a deployment from the committed config (push, CLI, UI).
func (o *Orchestrator) Deploy(ctx context.Context, in cpapi.DeployInput) (api.Deployment, error) {
	trigger := in.Trigger
	if trigger == "" {
		trigger = "ui"
	}
	skipped := ""
	if in.Skipped {
		skipped = "No changed files matched the watch paths"
	}
	rebuild := in.Rebuild || in.Commit != nil || in.UploadPath != "" || in.Ref != ""
	d, err := o.createDeployment(ctx, depInput{EnvID: in.EnvID, ServiceID: in.ServiceID, Trigger: trigger, Actor: in.Actor,
		Message: in.Message, Commit: in.Commit, Ref: in.Ref, UploadPath: in.UploadPath, Rebuild: rebuild, Skipped: skipped})
	if err != nil {
		return api.Deployment{}, err
	}
	return o.apiDeployment(d), nil
}

// Redeploy makes a new deployment from the same image, config snapshot and
// variable snapshot. When the image is gone, it rebuilds the same commit.
func (o *Orchestrator) Redeploy(ctx context.Context, deploymentID, actor string) (api.Deployment, error) {
	src, err := o.loadDep(ctx, o.db, deploymentID)
	if err != nil {
		return api.Deployment{}, err
	}
	in := depInput{EnvID: src.EnvID, ServiceID: src.ServiceID, Trigger: "redeploy", Actor: actor, Message: "Redeploy of " + shortID(src.ID)}
	if src.Image != "" && o.imageRetained(ctx, src) {
		in.Reuse = &src
	} else if src.Commit != nil && src.Commit.SHA != "" {
		in.Commit, in.Ref, in.Rebuild = src.Commit, src.Commit.SHA, true
	} else {
		in.Rebuild = true
	}
	d, err := o.createDeployment(ctx, in)
	if err != nil {
		return api.Deployment{}, err
	}
	o.audit(ctx, actor, "deployment.redeploy", d.ID, "from "+src.ID)
	return o.apiDeployment(d), nil
}

// Rollback redeploys an older deployment's image and variables; the
// service's committed config and variables do not change (9.6).
func (o *Orchestrator) Rollback(ctx context.Context, deploymentID, actor string) (api.Deployment, error) {
	src, err := o.loadDep(ctx, o.db, deploymentID)
	if err != nil {
		return api.Deployment{}, err
	}
	if src.Image == "" || !o.imageRetained(ctx, src) {
		return api.Deployment{}, errorf("The image of deployment %s is no longer kept, so it cannot be rolled back to. Redeploy from its commit instead.", shortID(src.ID))
	}
	d, err := o.createDeployment(ctx, depInput{EnvID: src.EnvID, ServiceID: src.ServiceID, Trigger: "rollback", Actor: actor,
		Message: "Rollback to " + shortID(src.ID), Reuse: &src, RollbackOf: src.ID})
	if err != nil {
		return api.Deployment{}, err
	}
	o.audit(ctx, actor, "deployment.rollback", d.ID, "to "+src.ID)
	return o.apiDeployment(d), nil
}

// Abort cancels a queued or building deployment, or stops the new replicas
// of a deploying one; the old deployment keeps serving.
func (o *Orchestrator) Abort(ctx context.Context, deploymentID, actor string) (api.Deployment, error) {
	d, err := o.loadDep(ctx, o.db, deploymentID)
	if err != nil {
		return api.Deployment{}, err
	}
	reason := "Aborted"
	if actor != "" {
		reason = "Aborted by " + actor
	}
	switch d.Status {
	case api.DepQueued:
		o.mu.Lock()
		l := o.lanes[d.EnvID+"/"+d.ServiceID]
		if l != nil && l.queued == d.ID {
			l.queued = ""
		}
		var cancel context.CancelFunc
		if l != nil && l.current == d.ID {
			l.reason[d.ID] = reason
			cancel = l.cancel
		}
		o.mu.Unlock()
		if cancel != nil {
			cancel()
			o.waitTerminal(ctx, d.ID, 10*time.Second)
		} else {
			d.Reason = reason
			o.finish(ctx, &d, api.DepCancelled, "")
		}
	case api.DepBuilding, api.DepDeploying:
		o.mu.Lock()
		l := o.lanes[d.EnvID+"/"+d.ServiceID]
		var cancel context.CancelFunc
		if l != nil && l.current == d.ID {
			l.reason[d.ID] = reason
			cancel = l.cancel
		}
		o.mu.Unlock()
		if cancel == nil {
			o.removeReplicasOf(ctx, d.ID)
			d.Reason = reason
			o.finish(ctx, &d, api.DepCancelled, "")
		} else {
			cancel()
			o.waitTerminal(ctx, d.ID, 15*time.Second)
		}
	default:
		return api.Deployment{}, errorf("Only a queued, building or deploying deployment can be aborted; this one is %s.", strings.ToLower(api.StatusLabel(d.Status, d.Active)))
	}
	o.audit(ctx, actor, "deployment.abort", d.ID, "")
	cur, err := o.loadDep(ctx, o.db, d.ID)
	if err != nil {
		return api.Deployment{}, err
	}
	return o.apiDeployment(cur), nil
}

func (o *Orchestrator) waitTerminal(ctx context.Context, id string, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		d, err := o.loadDep(ctx, o.db, id)
		if err != nil || terminal(d.Status) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-o.changes():
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Remove stops the active (or crashed) deployment; the service then shows
// "No deployment".
func (o *Orchestrator) Remove(ctx context.Context, deploymentID, actor string) (api.Deployment, error) {
	d, err := o.loadDep(ctx, o.db, deploymentID)
	if err != nil {
		return api.Deployment{}, err
	}
	if d.Status != api.DepSuccess && d.Status != api.DepCrashed {
		return api.Deployment{}, errorf("Only an active or crashed deployment can be removed; this one is %s.", strings.ToLower(api.StatusLabel(d.Status, d.Active)))
	}
	d.Active = false
	d.Status = api.DepRemoving
	if err := o.saveDep(ctx, &d); err != nil {
		return api.Deployment{}, err
	}
	o.retire(d, d.Config.Deploy.DrainingSec)
	o.audit(ctx, actor, "deployment.remove", d.ID, "")
	return o.apiDeployment(d), nil
}

// retire drains a deployment's replicas off the routes and removes them.
func (o *Orchestrator) retire(d depRow, drainingSec int) {
	ctx := o.bg()
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET serving = 0, draining = 1 WHERE deployment_id = ? AND desired IN ('running', 'stopped')`, d.ID)
		return err
	})
	o.invalidateNet()
	o.hub.MarkDirty()
	laneWG.Add(1)
	go func() {
		defer laneWG.Done()
		o.drain(ctx, d, 0, drainingSec)
	}()
}

// Restart replaces the active deployment's containers one replica at a time
// (a hard restart without downtime when there are several replicas).
func (o *Orchestrator) Restart(ctx context.Context, deploymentID, actor string) (api.Deployment, error) {
	d, err := o.loadDep(ctx, o.db, deploymentID)
	if err != nil {
		return api.Deployment{}, err
	}
	if !d.Active || (d.Status != api.DepSuccess && d.Status != api.DepCrashed) {
		return api.Deployment{}, errorf("Only the active or a crashed deployment can be restarted.")
	}
	old, err := o.queryReps(ctx, `deployment_id = ? AND desired IN ('running', 'stopped') ORDER BY created_at, id`, d.ID)
	if err != nil {
		return api.Deployment{}, err
	}
	o.audit(ctx, actor, "deployment.restart", d.ID, "")
	o.activity(ctx, d.EnvID, d.ServiceID, "deployment", "Restarting deployment "+shortID(d.ID), actor, d.ID, "")
	laneWG.Add(1)
	go func() {
		defer laneWG.Done()
		o.restartReplicas(o.bg(), d, old)
	}()
	return o.apiDeployment(d), nil
}

func (o *Orchestrator) restartReplicas(ctx context.Context, d depRow, old []repRow) {
	snap, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID)
	if err != nil {
		return
	}
	for _, r := range old {
		var nr repRow
		var err error
		if snap.Volume != nil {
			// One container per volume: stop the old one, then start the new one on the volume's host.
			o.stopOne(ctx, r)
			o.markRemoved(ctx, r.ID)
			pinned, _ := o.volumeHost(ctx, snap.Volume.ID)
			nr, err = o.placeOne(ctx, &d, snap, pinned, placeOpts{serving: true})
		} else {
			nr, err = o.placeOne(ctx, &d, snap, r.HostID, placeOpts{serving: true})
			if err != nil {
				// The host is full or gone: place anywhere.
				nr, err = o.placeOne(ctx, &d, snap, "", placeOpts{serving: true})
			}
		}
		if err != nil {
			o.log.Warn("restart replica", "replica", r.ID, "err", err)
			o.activity(ctx, d.EnvID, d.ServiceID, "alert", "A replica could not be restarted: "+strings.TrimSuffix(err.Error(), "."), "", d.ID, "")
			continue
		}
		o.hub.MarkDirty(nr.HostID)
		if !o.waitReplicaHealthy(ctx, nr.ID, time.Duration(healthDeadline(d.Config))*time.Second+o.t.HealthSlack) {
			o.log.Warn("restarted replica did not become healthy", "replica", nr.ID)
		}
		if snap.Volume == nil {
			o.dropReplica(ctx, r, d.Config.Deploy.DrainingSec)
		}
	}
	cur, err := o.loadDep(ctx, o.db, d.ID)
	if err == nil && cur.Status == api.DepCrashed {
		cur.Status, cur.Diagnosis = api.DepSuccess, ""
		_ = o.saveDep(ctx, &cur)
	}
	o.activity(ctx, d.EnvID, d.ServiceID, "deployment", "Restarted deployment "+shortID(d.ID), "", d.ID, "")
}

// stopOne stops one replica and waits for the agent.
func (o *Orchestrator) stopOne(ctx context.Context, r repRow) {
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'stopped', serving = 0 WHERE id = ?`, r.ID)
		return err
	})
	o.invalidateNet()
	o.hub.MarkDirty(r.HostID)
	deadline := time.Now().Add(time.Duration(defaultStopGraceSec)*time.Second + o.t.StopWait)
	for time.Now().Before(deadline) && o.hub.Connected(r.HostID) {
		rs, err := o.queryReps(ctx, `id = ?`, r.ID)
		if err != nil || len(rs) == 0 {
			return
		}
		switch rs[0].State {
		case "stopped", "exited", "removed", "crashed":
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-o.changes():
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// dropReplica drains one replica and removes it after the draining time.
func (o *Orchestrator) dropReplica(ctx context.Context, r repRow, drainingSec int) {
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET serving = 0, draining = 1 WHERE id = ? AND desired IN ('running', 'stopped')`, r.ID)
		return err
	})
	o.invalidateNet()
	o.hub.MarkDirty()
	if drainingSec <= 0 {
		drainingSec = 30
	}
	o.sleep(ctx, o.drainTime(drainingSec))
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', serving = 0, draining = 0 WHERE id = ? AND desired IN ('running', 'stopped')`, r.ID)
		return err
	})
	o.invalidateNet()
	o.hub.MarkDirty()
}

func (o *Orchestrator) waitReplicaHealthy(ctx context.Context, id string, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		rs, err := o.queryReps(ctx, `id = ?`, id)
		if err != nil || len(rs) == 0 {
			return false
		}
		switch rs[0].State {
		case "healthy":
			return true
		case "crashed", "exited":
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-o.changes():
		case <-time.After(time.Second):
		}
	}
	return false
}

// scale changes the replica count of the active deployment in place (9.1):
// new replicas join routes when healthy, removed ones drain.
func (o *Orchestrator) scale(ctx context.Context, envID, svcID, actor string) error {
	snap, err := o.cfg.ServiceInstance(ctx, envID, svcID)
	if err != nil {
		return err
	}
	if snap.Volume != nil && sched.Replicas(snap.Config) > 1 {
		return errorf("A service with a volume can only run 1 replica.")
	}
	d, ok, err := o.activeDep(ctx, envID, svcID)
	if err != nil {
		return err
	}
	want := sched.Replicas(snap.Config)
	if !ok {
		return nil // nothing runs yet; the next deployment uses the new count
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		cfg := d.Config
		cfg.Deploy.Replicas = want
		_, err := tx.ExecContext(ctx, `UPDATE o_deployments SET config = ? WHERE id = ?`, jsonString(cfg), d.ID)
		return err
	}); err != nil {
		return err
	}
	d.Config.Deploy.Replicas = want
	o.activity(ctx, envID, svcID, "deployment", fmt.Sprintf("Scaled to %d %s", want, plural(want, "replica", "replicas")), actor, d.ID, "")
	laneWG.Add(1)
	go func() {
		defer laneWG.Done()
		o.reconcileCount(o.bg(), d, snap)
	}()
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// reconcileCount adds or drains replicas of an active deployment until its count matches.
func (o *Orchestrator) reconcileCount(ctx context.Context, d depRow, snap cpapi.ServiceSnapshot) {
	o.mu.Lock()
	key := "scale:" + d.EnvID + "/" + d.ServiceID
	if o.alerted[key] {
		o.mu.Unlock()
		return
	}
	o.alerted[key] = true
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.alerted, key)
		o.mu.Unlock()
	}()
	want := sched.Replicas(d.Config)
	live, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'running' AND draining = 0 ORDER BY created_at, id`, d.ID)
	if err != nil {
		return
	}
	switch {
	case len(live) < want:
		placed, err := o.placeReplicas(ctx, &d, snap, want-len(live), placeOpts{serving: true})
		if len(placed) > 0 {
			o.hub.MarkDirty(hostsOf(placed)...)
		}
		if err != nil {
			o.activity(ctx, d.EnvID, d.ServiceID, "alert", "Scaling stopped: "+strings.TrimSuffix(err.Error(), "."), "", d.ID, "")
		}
	case len(live) > want:
		// Drain the newest replicas on the most crowded hosts first.
		extra := pickExtra(live, len(live)-want)
		for _, r := range extra {
			o.dropReplica(ctx, r, d.Config.Deploy.DrainingSec)
		}
	}
}

// pickExtra chooses n replicas to remove: from the hosts with the most
// replicas of the service, newest first.
func pickExtra(live []repRow, n int) []repRow {
	counts := map[string]int{}
	for _, r := range live {
		counts[r.HostID]++
	}
	cand := append([]repRow(nil), live...)
	var out []repRow
	for len(out) < n && len(cand) > 0 {
		sort.SliceStable(cand, func(i, j int) bool {
			if counts[cand[i].HostID] != counts[cand[j].HostID] {
				return counts[cand[i].HostID] > counts[cand[j].HostID]
			}
			if cand[i].CreatedAt != cand[j].CreatedAt {
				return cand[i].CreatedAt > cand[j].CreatedAt
			}
			return cand[i].ID > cand[j].ID
		})
		out = append(out, cand[0])
		counts[cand[0].HostID]--
		cand = cand[1:]
	}
	return out
}

// deleteService removes every replica of a deleted service instance and
// moves its volume to the trash (9.12).
func (o *Orchestrator) deleteService(ctx context.Context, envID, svcID, actor string) error {
	deps, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? AND status NOT IN ('REMOVED', 'CANCELLED', 'FAILED', 'SKIPPED', 'COMPLETED')`, envID, svcID)
	if err != nil {
		return err
	}
	o.mu.Lock()
	if l := o.lanes[envID+"/"+svcID]; l != nil {
		if l.queued != "" {
			l.reason[l.queued] = "The service was deleted."
		}
		l.queued = ""
		if l.cancel != nil {
			l.reason[l.current] = "The service was deleted."
			l.cancel()
		}
	}
	o.mu.Unlock()
	for _, d := range deps {
		o.removeReplicasOf(ctx, d.ID)
		switch d.Status {
		case api.DepQueued, api.DepBuilding, api.DepDeploying:
			d.Reason = "The service was deleted."
			o.finish(ctx, &d, api.DepCancelled, "")
		default:
			d.Active = false
			d.FinishedAt = 0
			o.finish(ctx, &d, api.DepRemoved, "")
		}
	}
	now := o.now().UnixMilli()
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_volume_hosts SET trashed_at = ? WHERE environment_id = ? AND service_id = ? AND trashed_at = 0`, now, envID, svcID)
		return err
	}); err != nil {
		return err
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.data.ServiceChanged(ctx, envID, svcID)
	o.activity(ctx, envID, svcID, "deployment", "Service deleted", actor, "", "")
	return nil
}

// MoveReplica places a replica on another host and removes the old one
// (used for "Move now" on an offline singleton).
func (o *Orchestrator) MoveReplica(ctx context.Context, replicaID, hostID, actor string) error {
	rs, err := o.queryReps(ctx, `id = ?`, replicaID)
	if err != nil {
		return err
	}
	if len(rs) == 0 || !rs[0].live() {
		return errorf("This replica does not exist or was already removed.")
	}
	r := rs[0]
	if r.HostID == hostID {
		return errorf("The replica already runs on that host.")
	}
	d, err := o.loadDep(ctx, o.db, r.DeploymentID)
	if err != nil {
		return err
	}
	snap, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID)
	if err != nil {
		return err
	}
	if snap.Volume != nil {
		return errorf("A replica of a service with a volume cannot move: in v0.1 volumes stay on their host.")
	}
	cfg := d.Config
	if hostID != "" {
		cfg.Deploy.Placement.Hosts = []string{hostID}
	}
	dd := d
	dd.Config = cfg
	nr, err := o.placeOne(ctx, &dd, snap, "", placeOpts{serving: r.Serving, exclude: map[string]bool{r.HostID: true}})
	if err != nil {
		return err
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', serving = 0, draining = 0 WHERE id = ?`, r.ID)
		return err
	}); err != nil {
		return err
	}
	o.invalidateNet()
	o.hub.MarkDirty(r.HostID, nr.HostID)
	o.hub.MarkDirty()
	o.audit(ctx, actor, "replica.move", r.ID, "to "+nr.HostID)
	o.activity(ctx, d.EnvID, d.ServiceID, "deployment", "Moved replica "+shortID(r.ID)+" to another host", actor, d.ID, "")
	return nil
}

func (o *Orchestrator) audit(ctx context.Context, actor, action, target, detail string) {
	if actor == "" {
		actor = "system"
	}
	if err := o.cfg.Audit(ctx, api.AuditEntry{ID: hub.NewID("aud"), TS: o.now().UnixMilli(), Actor: actor, Action: action, Target: target, Detail: detail}); err != nil {
		o.log.Warn("audit", "action", action, "err", err)
	}
}

// markRemoved takes one replica out of the desired state.
func (o *Orchestrator) markRemoved(ctx context.Context, id string) {
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', serving = 0, draining = 0 WHERE id = ?`, id)
		return err
	})
	o.invalidateNet()
	o.hub.MarkDirty()
}
