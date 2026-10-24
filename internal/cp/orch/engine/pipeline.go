// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Deploy steps, in order (9.1).
const (
	stepQueued      = "queued"
	stepFetch       = "fetch"
	stepBuild       = "build"
	stepPull        = "pull image"
	stepPush        = "push"
	stepPreDeploy   = "pre_deploy"
	stepStart       = "start"
	stepHealthcheck = "healthcheck"
	stepCutover     = "cutover"
	stepDrain       = "drain"
)

// lane serialises deployments of one service instance: one in flight, at
// most one queued (a newer one supersedes it).
type lane struct {
	key     string
	queued  string
	current string
	cancel  context.CancelFunc
	reason  map[string]string // deployment -> why it was cancelled
	running bool
}

var laneWG sync.WaitGroup

func (o *Orchestrator) laneFor(envID, svcID string) *lane {
	k := envID + "/" + svcID
	l := o.lanes[k]
	if l == nil {
		l = &lane{key: k, reason: map[string]string{}}
		o.lanes[k] = l
	}
	return l
}

func (o *Orchestrator) waitLanes() {
	done := make(chan struct{})
	go func() { laneWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// enqueue puts a QUEUED deployment in its lane.
func (o *Orchestrator) enqueue(d depRow) {
	o.mu.Lock()
	l := o.laneFor(d.EnvID, d.ServiceID)
	superseded := l.queued
	l.queued = d.ID
	start := !l.running
	if start {
		l.running = true
		laneWG.Add(1)
	}
	o.mu.Unlock()
	if superseded != "" && superseded != d.ID {
		o.cancelQueued(superseded, "Superseded by "+shortID(d.ID))
	}
	if start {
		go o.runLane(l)
	}
}

func (o *Orchestrator) runLane(l *lane) {
	defer laneWG.Done()
	for {
		o.mu.Lock()
		id := l.queued
		l.queued = ""
		if id == "" || (o.ctx != nil && o.ctx.Err() != nil) {
			l.running = false
			o.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(o.bgLocked())
		l.current, l.cancel = id, cancel
		o.mu.Unlock()
		o.process(ctx, id)
		cancel()
		o.mu.Lock()
		l.current, l.cancel = "", nil
		delete(l.reason, id)
		o.mu.Unlock()
	}
}

func (o *Orchestrator) bgLocked() context.Context {
	if o.ctx != nil {
		return o.ctx
	}
	return context.Background()
}

func (o *Orchestrator) cancelQueued(id, reason string) {
	ctx := o.bg()
	d, err := o.loadDep(ctx, o.db, id)
	if err != nil || d.Status != api.DepQueued {
		return
	}
	d.Reason = reason
	o.finish(ctx, &d, api.DepCancelled, "")
}

// cancelReason returns why a running deployment's context was cancelled.
func (o *Orchestrator) cancelReason(d depRow) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if l := o.lanes[d.EnvID+"/"+d.ServiceID]; l != nil {
		return l.reason[d.ID]
	}
	return ""
}

// ---------------------------------------------------------------- persistence

func (o *Orchestrator) saveDep(ctx context.Context, d *depRow) error {
	commit := ""
	if d.Commit != nil {
		commit = jsonString(d.Commit)
	}
	cfile := ""
	if d.ConfigFile != nil {
		cfile = jsonString(d.ConfigFile)
	}
	err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_deployments SET status = ?, project_id = ?, config_rev = ?, config = ?, commit_info = ?,
			image = ?, digest = ?, diagnosis = ?, reason = ?, steps = ?, env_enc = ?, is_node = ?, start_cmd_detected = ?,
			source_map = ?, config_file = ?, notices = ?, build_host = ?, started_at = ?, finished_at = ?, active = ? WHERE id = ?`,
			string(d.Status), d.ProjectID, d.ConfigRev, jsonString(d.Config), commit, d.Image, d.Digest, d.Diagnosis, d.Reason,
			jsonString(nonNilSteps(d.Steps)), d.EnvEnc, b2i(d.IsNode), d.StartCmd, jsonString(nonNilMap(d.SourceMap)), cfile,
			jsonString(nonNilStrings(d.Notices)), d.BuildHost, d.StartedAt, d.FinishedAt, b2i(d.Active), d.ID)
		return err
	})
	if err == nil {
		o.publishDep(*d)
		o.notify()
	}
	return err
}

func nonNilSteps(s []api.DeployStep) []api.DeployStep {
	if s == nil {
		return []api.DeployStep{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// step records the state of one pipeline step and saves the deployment.
func (o *Orchestrator) step(ctx context.Context, d *depRow, name, state, detail string) {
	now := o.now().UnixMilli()
	found := false
	for i := range d.Steps {
		if d.Steps[i].Name != name {
			continue
		}
		found = true
		s := &d.Steps[i]
		if state == "running" && s.StartedAt == 0 {
			s.StartedAt = now
		}
		if state == "ok" || state == "failed" || state == "skipped" {
			if s.StartedAt == 0 {
				s.StartedAt = now
			}
			s.FinishedAt = now
		}
		s.State = state
		if detail != "" {
			s.Detail = detail
		}
	}
	if !found {
		s := api.DeployStep{Name: name, State: state, Detail: detail, StartedAt: now}
		if state == "ok" || state == "failed" || state == "skipped" {
			s.FinishedAt = now
		}
		d.Steps = append(d.Steps, s)
	}
	if err := o.saveDep(ctx, d); err != nil {
		o.log.Warn("save deployment step", "deployment", d.ID, "err", err)
	}
}

// failRunningSteps marks steps that were still running as failed.
func (o *Orchestrator) failRunningSteps(d *depRow) {
	now := o.now().UnixMilli()
	for i := range d.Steps {
		if d.Steps[i].State == "running" || d.Steps[i].State == "pending" {
			d.Steps[i].State = "failed"
			d.Steps[i].FinishedAt = now
		}
	}
}

// finish ends a deployment with a terminal status and tells people.
func (o *Orchestrator) finish(ctx context.Context, d *depRow, status api.DeploymentStatus, diagnosis string) {
	ctx = context.WithoutCancel(ctx)
	d.Status = status
	if diagnosis != "" {
		d.Diagnosis = diagnosis
	}
	if d.FinishedAt == 0 {
		d.FinishedAt = o.now().UnixMilli()
	}
	if status != api.DepSuccess && status != api.DepCompleted {
		o.failRunningSteps(d)
	}
	if err := o.saveDep(ctx, d); err != nil {
		o.log.Error("save deployment", "deployment", d.ID, "err", err)
	}
	o.builds.Finish(d.ID)
	label := api.StatusLabel(status, d.Active)
	text := deploymentActivityText(*d, status)
	o.activity(ctx, d.EnvID, d.ServiceID, "deployment", text, d.Actor, d.ID, label)
	switch status {
	case api.DepFailed:
		o.notice(ctx, cpapi.Notice{Event: api.EventDeploymentFailed, ProjectID: d.ProjectID, EnvID: d.EnvID, ServiceID: d.ServiceID,
			Ref: d.ID, Text: firstNonEmpty(d.Diagnosis, "The deployment failed."), Details: map[string]any{"deploymentId": d.ID}})
	case api.DepSuccess:
		if err := o.cfg.FireWebhook(ctx, d.ProjectID, api.HookDeploymentSuccess, o.apiDeployment(*d)); err != nil {
			o.log.Warn("webhook", "err", err)
		}
	}
	if status == api.DepSuccess || status == api.DepFailed || status == api.DepRemoved {
		o.data.ServiceChanged(ctx, d.EnvID, d.ServiceID)
	}
}

func deploymentActivityText(d depRow, status api.DeploymentStatus) string {
	switch status {
	case api.DepSuccess:
		if d.Trigger == "rollback" {
			return "Rolled back to " + shortID(d.RollbackOf)
		}
		return "Deployment " + shortID(d.ID) + " is active"
	case api.DepFailed:
		return "Deployment " + shortID(d.ID) + " failed: " + d.Diagnosis
	case api.DepCrashed:
		return "Deployment " + shortID(d.ID) + " crashed"
	case api.DepCancelled:
		return "Deployment " + shortID(d.ID) + " was removed: " + d.Reason
	case api.DepSkipped:
		return "Deployment " + shortID(d.ID) + " was skipped: " + d.Reason
	case api.DepRemoved:
		return "Deployment " + shortID(d.ID) + " was removed"
	}
	return "Deployment " + shortID(d.ID) + " is " + strings.ToLower(string(status))
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------- the pipeline

// process runs one deployment from QUEUED to a terminal state.
func (o *Orchestrator) process(ctx context.Context, id string) {
	d, err := o.loadDep(ctx, o.db, id)
	if err != nil || d.Status != api.DepQueued {
		return
	}
	if err := o.waitFor(ctx, d); err != nil {
		o.aborted(ctx, &d)
		return
	}
	snap, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID)
	if err != nil || snap.Deleted {
		d.Reason = "The service was deleted before it deployed."
		o.finish(ctx, &d, api.DepCancelled, "")
		return
	}
	inst, err := o.cfg.Install(ctx)
	if err != nil {
		o.finish(ctx, &d, api.DepFailed, "The install settings could not be read. Redeploy to retry.")
		return
	}
	d.StartedAt = o.now().UnixMilli()
	d.ProjectID = snap.ProjectID
	o.step(ctx, &d, stepQueued, "ok", "")

	reuse := d.Image != "" // redeploy and rollback carry their image, config and variables
	if !reuse {
		d.Config, d.ConfigRev = snap.Config, snap.Rev
		provided := providedVars(snap, d.ID, d.Commit, inst)
		env, unresolved, err := o.cfg.ResolveVariables(ctx, d.EnvID, d.ServiceID, provided)
		if err != nil {
			o.finish(ctx, &d, api.DepFailed, "The service's variables could not be resolved. Check them in the Variables tab.")
			return
		}
		o.detectImagePort(ctx, d, d.Config, env, provided)
		for _, u := range unresolved {
			_ = o.builds.Append(d.ID, o.now().UnixMilli(), []string{fmt.Sprintf("Warning: the reference ${{%s}} does not resolve and was set to an empty value.", u)})
		}
		applyVarOverrides(&d.Config, env)
		enc, err := o.sealEnv(d.ID, env)
		if err != nil {
			o.finish(ctx, &d, api.DepFailed, "The variable snapshot could not be encrypted.")
			return
		}
		d.EnvEnc = enc
		o.mu.Lock()
		o.envCache[d.ID] = env
		o.mu.Unlock()
		if err := o.prepareImage(ctx, &d, snap, env, inst); err != nil {
			if ctx.Err() != nil {
				o.aborted(ctx, &d)
				return
			}
			var q *queueTimeout
			if errors.As(err, &q) {
				d.Reason = q.Error()
				o.finish(ctx, &d, api.DepCancelled, "")
				return
			}
			o.finish(ctx, &d, api.DepFailed, err.Error())
			return
		}
	} else {
		o.step(ctx, &d, stepPull, "ok", "Reusing image "+shortID(d.Image))
	}
	if ctx.Err() != nil {
		o.aborted(ctx, &d)
		return
	}
	kind := sched.Kind(d.Config, len(snap.Domains) > 0)
	if kind == api.KindCron {
		d.Status = api.DepSuccess
		d.Reason = "Scheduled"
		o.activate(ctx, &d)
		return
	}
	d.Status = api.DepDeploying
	if err := o.saveDep(ctx, &d); err != nil {
		o.log.Warn("save deployment", "err", err)
	}
	if d.Config.Deploy.PreDeployCommand != "" {
		if err := o.preDeploy(ctx, &d, snap); err != nil {
			if ctx.Err() != nil {
				o.aborted(ctx, &d)
				return
			}
			o.finish(ctx, &d, api.DepFailed, err.Error())
			return
		}
	}
	o.rolloutAndFinish(ctx, &d, snap)
}

// aborted finishes a deployment whose context was cancelled (abort, supersede, shutdown).
func (o *Orchestrator) aborted(ctx context.Context, d *depRow) {
	reason := o.cancelReason(*d)
	if reason == "" {
		// shutdown: leave it for resume
		return
	}
	o.removeReplicasOf(context.WithoutCancel(ctx), d.ID)
	d.Reason = reason
	o.finish(ctx, d, api.DepCancelled, "")
}

// waitFor blocks until the deployments this one waits for (commit
// ordering, 9.1) reached a terminal state.
func (o *Orchestrator) waitFor(ctx context.Context, d depRow) error {
	for _, w := range d.WaitFor {
		for {
			wd, err := o.loadDep(ctx, o.db, w)
			if err != nil || terminal(wd.Status) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-o.changes():
			case <-time.After(o.t.RetryEvery):
			}
		}
	}
	return nil
}

// queueTimeout cancels a deployment that waited too long (9.1).
type queueTimeout struct{ reason string }

func (q *queueTimeout) Error() string { return q.reason }

// prepareImage builds, imports or reuses the image of a deployment.
func (o *Orchestrator) prepareImage(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot, env map[string]string, inst api.Install) error {
	src := d.Config.Source.Type
	if src == "" && d.Config.Source.Image != "" {
		src = "image"
	}
	if src == "" && d.UploadPath != "" {
		src = "upload"
	}
	needBuild := d.Rebuild || d.UploadPath != ""
	var last depRow
	var haveLast bool
	if !needBuild {
		last, haveLast = o.lastImage(ctx, d.EnvID, d.ServiceID, d.ID)
		needBuild = !haveLast
	}
	switch src {
	case "github", "image", "upload":
	case "", "empty":
		if !haveLast {
			return errorf("This service has no source yet. Connect a repo or set an image in Settings > Source.")
		}
	default:
		return errorf("The source type %q is not supported.", src)
	}
	if !needBuild {
		d.Image, d.Digest, d.IsNode, d.StartCmd = last.Image, last.Digest, last.IsNode, last.StartCmd
		d.SourceMap, d.ConfigFile, d.Notices = last.SourceMap, last.ConfigFile, last.Notices
		if d.Commit == nil {
			d.Commit = last.Commit
		}
		if last.ConfigFile != nil {
			merged, err := o.cfg.MergeConfigFile(ctx, d.EnvID, d.ServiceID, commitSHA(d.Commit), nil)
			if err == nil && len(merged.Source) > 0 {
				d.Config = merged.Config
				applyVarOverrides(&d.Config, env)
			}
		}
		o.step(ctx, d, stepPull, "ok", "Reusing the image of "+shortID(last.ID))
		return nil
	}
	return o.build(ctx, d, snap, env, inst, src)
}

func commitSHA(c *api.Commit) string {
	if c == nil {
		return ""
	}
	return c.SHA
}

// lastImage returns the newest deployment of an instance whose image is still retained.
func (o *Orchestrator) lastImage(ctx context.Context, envID, svcID, exclude string) (depRow, bool) {
	if d, ok, _ := o.activeDep(ctx, envID, svcID); ok && d.Image != "" && d.ID != exclude && o.imageRetained(ctx, d) {
		return d, true
	}
	ds, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? AND image != '' AND id != ? AND status IN ('SUCCESS', 'REMOVED', 'CRASHED', 'REMOVING')
		ORDER BY created_at DESC LIMIT 5`, envID, svcID, exclude)
	if err != nil {
		return depRow{}, false
	}
	for _, d := range ds {
		if o.imageRetained(ctx, d) {
			return d, true
		}
	}
	return depRow{}, false
}

func (o *Orchestrator) imageRetained(ctx context.Context, d depRow) bool {
	if d.Image == "" {
		return false
	}
	if !strings.HasPrefix(d.Image, "svc_") {
		return true
	}
	var deleted int64
	err := o.db.QueryRowContext(ctx, `SELECT deleted_at FROM o_images WHERE ref = ? ORDER BY created_at DESC LIMIT 1`, d.Image).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	return err == nil && deleted == 0
}

type buildOutcome struct {
	res proto.BuildResult
}

// build runs a build or image import on a build host and waits for its result.
func (o *Orchestrator) build(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot, env map[string]string, inst api.Install, src string) error {
	d.Status = api.DepBuilding
	o.step(ctx, d, stepFetch, "running", "")
	arch := o.serviceArch(ctx, d.EnvID, snap)
	hostID, err := o.waitBuildHost(ctx, d, arch)
	if err != nil {
		return err
	}
	defer func() {
		o.mu.Lock()
		o.building[hostID]--
		o.pauseCond.Broadcast()
		o.mu.Unlock()
	}()
	host, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return err
	}
	if arch == "" {
		arch = host.Facts.Arch
	}
	if arch == "" {
		arch = "amd64"
	}
	repo := strings.ToLower(d.ServiceID)
	cfg := d.Config
	timeoutMin := cfg.Build.TimeoutMin
	if timeoutMin <= 0 {
		timeoutMin = inst.BuildTimeoutMin
	}
	if timeoutMin <= 0 {
		timeoutMin = 30
	}
	job := proto.BuildJob{
		BuildID: d.ID, ServiceID: d.ServiceID, RootDir: cfg.Source.RootDir, ConfigPath: cfg.Build.ConfigPath,
		Builder: builderName(cfg.Build.Builder), RailpackVersion: cfg.Build.RailpackVersion, DockerfilePath: cfg.Build.DockerfilePath,
		BuildCommand: cfg.Build.BuildCommand, StartCommand: cfg.Deploy.StartCommand, Env: env,
		Image:    o.registryAddr(host) + "/" + repo + ":" + d.ID,
		CacheRef: o.registryAddr(host) + "/" + repo + ":buildcache",
		Platform: "linux/" + arch, NoCache: cfg.Build.NoCache, TimeoutSec: timeoutMin * 60,
	}
	if job.RootDir == "" {
		job.RootDir = "/"
	}
	switch src {
	case "image":
		job.SourceImage = cfg.Source.Image
		if cfg.Source.RegistryCredential != "" {
			u, p, s, err := o.cfg.RegistryCredential(ctx, cfg.Source.RegistryCredential)
			if err != nil {
				return errorf("The registry credential of this service could not be read. Check Settings > Source.")
			}
			job.SourceImageAuth = &proto.RegistryAuth{Server: s, Username: u, Password: p}
		}
	case "github":
		g, commit, err := o.gitSource(ctx, cfg, d.Commit, d.Ref)
		if err != nil {
			return err
		}
		job.Git = g
		d.Commit = commit
	case "upload":
		id := uploadID(d.UploadPath)
		if id == "" {
			return errorf("The uploaded source of this deployment is missing. Run 5050 up again.")
		}
		job.TarballURL = strings.TrimSuffix(o.o.PublicURL, "/") + "/api/agent/v1/uploads/" + id
	}
	d.BuildHost = hostID
	o.step(ctx, d, stepFetch, "ok", "")
	if src == "image" {
		o.step(ctx, d, stepPull, "running", cfg.Source.Image)
	} else {
		o.step(ctx, d, stepBuild, "running", "")
	}
	res, err := o.runBuild(ctx, d, hostID, job)
	if err != nil {
		return err
	}
	if !res.OK {
		diag := res.Diagnosis
		if diag == "" {
			diag = "The build failed. Check the build logs for the error."
		}
		if src == "image" {
			o.step(ctx, d, stepPull, "failed", "")
		} else {
			o.step(ctx, d, stepBuild, "failed", "")
		}
		return errorf("%s", diag)
	}
	if src == "image" {
		o.step(ctx, d, stepPull, "ok", "")
	} else {
		o.step(ctx, d, stepBuild, "ok", "")
	}
	o.step(ctx, d, stepPush, "ok", "")
	d.Image = repo + ":" + d.ID
	d.Digest = res.Digest
	d.IsNode = res.IsNode
	d.StartCmd = res.StartCommand
	d.Notices = append([]string(nil), res.Notices...)
	merged, err := o.cfg.MergeConfigFile(ctx, d.EnvID, d.ServiceID, commitSHA(d.Commit), res.ConfigFile)
	if err != nil {
		return errorf("The config file could not be applied: %s", strings.TrimSuffix(err.Error(), "."))
	}
	if res.ConfigFile != nil {
		d.Config = merged.Config
		applyVarOverrides(&d.Config, env)
		d.SourceMap = merged.Source
		d.ConfigFile = &api.ConfigFileInfo{Path: res.ConfigFile.Path, Format: res.ConfigFile.Format, CommitSHA: commitSHA(d.Commit)}
	}
	d.Notices = append(d.Notices, merged.Notices...)
	now := o.now().UnixMilli()
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO o_images (deployment_id, service_id, ref, digest, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(deployment_id) DO UPDATE SET ref = excluded.ref, digest = excluded.digest`, d.ID, d.ServiceID, d.Image, d.Digest, now)
		return err
	}); err != nil {
		return err
	}
	return o.saveDep(ctx, d)
}

func uploadID(p string) string {
	base := p
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".tar.gz")
	if !validFileID(base) {
		return ""
	}
	return base
}

func builderName(b string) string {
	switch strings.ToLower(b) {
	case "railpack", "dockerfile", "nixpacks":
		return strings.ToLower(b)
	}
	return "auto"
}

// waitBuildHost reserves a build slot, waiting up to the queue timeout.
func (o *Orchestrator) waitBuildHost(ctx context.Context, d *depRow, arch string) (string, error) {
	start := o.now()
	noted := ""
	for {
		hosts, err := o.schedHosts(ctx, d.EnvID)
		if err != nil {
			return "", err
		}
		o.mu.Lock()
		for o.paused > 0 {
			o.pauseCond.Wait()
		}
		id, perr := sched.BuildHost(hosts, o.building, arch)
		if perr == nil {
			o.building[id]++
			o.mu.Unlock()
			return id, nil
		}
		o.mu.Unlock()
		if o.now().Sub(start) >= o.t.QueueTimeout {
			return "", &queueTimeout{reason: perr.Error()}
		}
		if noted != perr.Error() {
			noted = perr.Error()
			o.step(ctx, d, stepFetch, "running", perr.Error())
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(o.t.RetryEvery):
		case <-o.changes():
		}
	}
}

// runBuild sends the build and waits for its result (build results are
// buffered by the agent across reconnects, 7.5).
func (o *Orchestrator) runBuild(ctx context.Context, d *depRow, hostID string, job proto.BuildJob) (proto.BuildResult, error) {
	ch := make(chan buildOutcome, 1)
	o.mu.Lock()
	o.buildWait[d.ID] = ch
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.buildWait, d.ID)
		o.mu.Unlock()
	}()
	if err := o.saveDep(ctx, d); err != nil {
		return proto.BuildResult{}, err
	}
	if err := o.hub.Send(ctx, hostID, proto.TBuild, d.ID, job); err != nil {
		return proto.BuildResult{}, errorf("The build could not be sent to host %s: %s", hostID, strings.TrimSuffix(err.Error(), "."))
	}
	return o.awaitBuild(ctx, d, hostID, ch, time.Duration(job.TimeoutSec)*time.Second+o.t.BuildGrace)
}

func (o *Orchestrator) awaitBuild(ctx context.Context, d *depRow, hostID string, ch chan buildOutcome, limit time.Duration) (proto.BuildResult, error) {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	select {
	case out := <-ch:
		return out.res, nil
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = o.hub.Send(sctx, hostID, proto.TBuildCancel, hub.NewID("req"), proto.BuildLog{BuildID: d.ID})
		cancel()
		return proto.BuildResult{}, ctx.Err()
	case <-deadline.C:
		return proto.BuildResult{}, errorf("The build did not report a result in time and was stopped. Redeploy to retry.")
	}
}

// serviceArch is the architecture a service runs on: its volume host, else
// the first eligible run host (v0.1 keeps one architecture per service).
func (o *Orchestrator) serviceArch(ctx context.Context, envID string, snap cpapi.ServiceSnapshot) string {
	hosts, err := o.schedHosts(ctx, envID)
	if err != nil {
		return ""
	}
	if snap.Volume != nil {
		if vh, _ := o.volumeHost(ctx, snap.Volume.ID); vh != "" {
			for _, h := range hosts {
				if h.ID == vh {
					return h.Arch
				}
			}
		}
	}
	id, err := sched.Place(hosts, nil, o.placeRequest(snap, snap.Config, ""))
	if err != nil {
		return ""
	}
	for _, h := range hosts {
		if h.ID == id {
			return h.Arch
		}
	}
	return ""
}

// ---------------------------------------------------------------- pre-deploy

func (o *Orchestrator) preDeploy(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot) error {
	o.step(ctx, d, stepPreDeploy, "running", "")
	cfg := d.Config
	timeout := cfg.Deploy.PreDeployTimeoutSec
	if timeout <= 0 {
		timeout = 300
	}
	hostID, err := o.PlaceJob(ctx, d.EnvID, cfg.Deploy.Placement.Roles)
	if err != nil {
		o.step(ctx, d, stepPreDeploy, "failed", "")
		return err
	}
	host, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return err
	}
	jobID := hub.NewID("job")
	network, ip, release, err := o.jobNetwork(ctx, d.EnvID, hostID, jobID)
	if err != nil {
		o.step(ctx, d, stepPreDeploy, "failed", "")
		return err
	}
	defer release()
	env, err := o.openEnv(d.ID, d.EnvEnc)
	if err != nil {
		return err
	}
	job := proto.Job{JobID: jobID, Kind: "pre_deploy", Image: o.imageRef(host, *d), Cmd: preDeployArgv(cfg.Deploy.PreDeployCommand),
		Env: env, Network: network, IP: ip, TimeoutSec: timeout, MemLimitMB: cfg.Deploy.MemoryLimitMB, User: cfg.Deploy.RunAsUser,
		Labels: map[string]string{"5050.deployment": d.ID, "5050.service": d.ServiceID, "5050.environment": d.EnvID}}
	var last string
	res, err := o.RunJob(ctx, hostID, job, func(line string) {
		if strings.TrimSpace(line) != "" {
			last = line
		}
		_ = o.builds.Append(d.ID, o.now().UnixMilli(), []string{"[pre-deploy] " + line})
	})
	if err != nil {
		o.step(ctx, d, stepPreDeploy, "failed", "")
		return errorf("The pre-deploy command could not run: %s", strings.TrimSuffix(err.Error(), "."))
	}
	if res.TimedOut {
		o.step(ctx, d, stepPreDeploy, "failed", "")
		return errorf("Pre-deploy command timed out after %d seconds.", timeout)
	}
	if res.ExitCode != 0 {
		if last == "" {
			last = lastLine(res.Output)
		}
		o.step(ctx, d, stepPreDeploy, "failed", "")
		return errorf("%s", preDeployFailed(res.ExitCode, last))
	}
	o.step(ctx, d, stepPreDeploy, "ok", "")
	return nil
}

func preDeployArgv(cmd string) []string {
	argv := startArgv(cmd)
	if len(argv) == 0 {
		return []string{"true"}
	}
	return argv
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// ---------------------------------------------------------------- placement

// schedHosts builds the placement view of every host for an environment.
func (o *Orchestrator) schedHosts(ctx context.Context, envID string) ([]sched.Host, error) {
	rows, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return nil, err
	}
	reserved := map[string]int{}
	reps, err := o.queryReps(ctx, `desired IN ('running', 'stopped')`)
	if err != nil {
		return nil, err
	}
	depRes := map[string]int{}
	for _, r := range reps {
		res, ok := depRes[r.DeploymentID]
		if !ok {
			if d, err := o.loadDep(ctx, o.db, r.DeploymentID); err == nil {
				res = sched.ReservationMB(d.Config, isDatabase(d.Config))
			} else {
				res = 256
			}
			depRes[r.DeploymentID] = res
		}
		reserved[r.HostID] += res
	}
	var out []sched.Host
	for _, h := range rows {
		if h.State == sched.StateUnknown || h.MeshIndex == 0 {
			continue
		}
		state := h.State
		if !o.hub.Connected(h.ID) {
			if state == sched.StateOnline || state == "joining" {
				state = sched.StateOffline
			}
		} else if state == "joining" {
			state = sched.StateOnline
		}
		slots, err := ipam.SlotsUsed(ctx, o.db, h.ID)
		if err != nil {
			return nil, err
		}
		has := map[string]bool{}
		if envID != "" {
			if s, err := ipam.Slot(ctx, o.db, h.ID, envID); err == nil && s > 0 {
				has[envID] = true
			}
		}
		cpu := 0.0
		if hs, ok := o.met.HostSample(h.ID); ok {
			cpu = hs.CPUUsed
		}
		out = append(out, sched.Host{
			ID: h.ID, Name: h.Name, Roles: h.Roles, State: state, Drain: h.Drain, Arch: h.Facts.Arch, CPUs: h.Facts.CPUs,
			MemMB: int(h.Facts.MemBytes >> 20), ReserveMB: h.ReserveMB, ReservedMB: reserved[h.ID], CPUUsed: cpu,
			SlotsUsed: slots, EnvSlots: has, Unreachable: o.unreachable(h.ID),
		})
	}
	return out, nil
}

func (o *Orchestrator) unreachable(hostID string) map[string]bool {
	hs, ok := o.met.HostSample(hostID)
	if !ok || len(hs.MeshHandshakes) == 0 {
		return nil
	}
	now := o.now().UnixMilli()
	out := map[string]bool{}
	for peer, at := range hs.MeshHandshakes {
		if at > 0 && now-at > (3*time.Minute).Milliseconds() && o.hub.Connected(peer) {
			out[peer] = true
		}
	}
	return out
}

func (o *Orchestrator) placeRequest(snap cpapi.ServiceSnapshot, cfg api.ServiceConfig, pinned string) sched.Request {
	roles := cfg.Deploy.Placement.Roles
	if len(roles) == 0 && isDatabase(cfg) {
		roles = []string{"data"}
	}
	return sched.Request{EnvID: snap.EnvID, ServiceID: snap.ServiceID, Roles: roles, Hosts: cfg.Deploy.Placement.Hosts,
		ReservationMB: sched.ReservationMB(cfg, isDatabase(cfg)), PinnedHost: pinned}
}

// placeOpts tunes placeReplicas.
type placeOpts struct {
	exclude map[string]bool
	serving bool
	// deadline for waiting on capacity or eligible hosts (0 = CapacityWait).
	wait      time.Duration
	onPending func(msg string)
}

// placeReplicas places count new replicas of deployment d, one at a time,
// waiting while there is no capacity (Pending, 7.3).
func (o *Orchestrator) placeReplicas(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot, count int, po placeOpts) ([]repRow, error) {
	var placed []repRow
	start := o.now()
	wait := po.wait
	if wait == 0 {
		wait = o.t.CapacityWait
	}
	pinned := ""
	if snap.Volume != nil {
		pinned, _ = o.volumeHost(ctx, snap.Volume.ID)
	}
	for len(placed) < count {
		r, err := o.placeOne(ctx, d, snap, pinned, po)
		if err == nil {
			placed = append(placed, r)
			if snap.Volume != nil && pinned == "" {
				pinned = r.HostID
			}
			continue
		}
		var pe *sched.PinnedError
		var ne *ipam.NoSlotError
		if errors.As(err, &pe) && strings.Contains(pe.Reason, "gone") {
			return placed, err
		}
		if errors.As(err, &ne) && !o.anyOtherHost(ctx, d.EnvID) {
			return placed, err
		}
		if o.now().Sub(start) >= wait {
			return placed, err
		}
		msg := err.Error()
		var ce *sched.NoCapacityError
		if errors.As(err, &ce) {
			msg = ce.Pending()
		}
		if po.onPending != nil {
			po.onPending(msg)
		}
		select {
		case <-ctx.Done():
			return placed, ctx.Err()
		case <-time.After(o.t.RetryEvery):
		case <-o.changes():
		}
	}
	return placed, nil
}

func (o *Orchestrator) anyOtherHost(ctx context.Context, envID string) bool {
	hosts, err := o.schedHosts(ctx, envID)
	if err != nil {
		return false
	}
	for _, h := range hosts {
		if h.Schedulable() && h.SlotsUsed < ipam.MaxSlots {
			return true
		}
	}
	return false
}

func (o *Orchestrator) placeOne(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot, pinned string, po placeOpts) (repRow, error) {
	o.placeMu.Lock()
	defer o.placeMu.Unlock()
	hosts, err := o.schedHosts(ctx, d.EnvID)
	if err != nil {
		return repRow{}, err
	}
	live, err := o.queryReps(ctx, `environment_id = ? AND service_id = ? AND desired IN ('running', 'stopped')`, d.EnvID, d.ServiceID)
	if err != nil {
		return repRow{}, err
	}
	counts := map[string]int{}
	var peers []string
	for _, r := range live {
		counts[r.HostID]++
	}
	envReps, _ := o.queryReps(ctx, `environment_id = ? AND desired = 'running'`, d.EnvID)
	seenPeer := map[string]bool{}
	for _, r := range envReps {
		if !seenPeer[r.HostID] {
			seenPeer[r.HostID] = true
			peers = append(peers, r.HostID)
		}
	}
	req := o.placeRequest(snap, d.Config, pinned)
	req.Exclude = po.exclude
	req.PeerHosts = peers
	req.Arch = o.deploymentArch(ctx, *d)
	if pinned == "" && isDatabase(d.Config) && len(d.Config.Deploy.Placement.Hosts) == 0 {
		// The first non-control host with the data role is the default for databases (9.2).
		if other := nonControlDataHost(hosts, req); other != "" {
			if req.Exclude == nil {
				req.Exclude = map[string]bool{}
			}
			for _, h := range hosts {
				if h.ID != other && h.HasRole("control") {
					req.Exclude[h.ID] = true
				}
			}
		}
	}
	hostID, err := sched.Place(hosts, counts, req)
	if err != nil {
		return repRow{}, err
	}
	host, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return repRow{}, err
	}
	r := repRow{ID: hub.NewID("rep"), DeploymentID: d.ID, ServiceID: d.ServiceID, EnvID: d.EnvID, HostID: hostID,
		CreatedAt: o.now().UnixMilli(), Desired: desiredRunning, Serving: po.serving}
	err = o.st.Write(ctx, func(tx *sql.Tx) error {
		ip, err := ipam.Allocate(ctx, tx, o.plan, hostID, host.Name, host.MeshIndex, d.EnvID, r.ID)
		if err != nil {
			return err
		}
		r.IP = ip
		if snap.Volume != nil && pinned == "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO o_volume_hosts (volume_id, service_id, environment_id, host_id, created_at)
				VALUES (?, ?, ?, ?, ?) ON CONFLICT(volume_id) DO NOTHING`, snap.Volume.ID, d.ServiceID, d.EnvID, hostID, r.CreatedAt); err != nil {
				return err
			}
		}
		return insertRep(ctx, tx, r)
	})
	if err != nil {
		return repRow{}, err
	}
	o.invalidateNet()
	return r, nil
}

func nonControlDataHost(hosts []sched.Host, req sched.Request) string {
	var ids []string
	for _, h := range hosts {
		if h.Schedulable() && h.HasRole("data") && !h.HasRole("control") && h.FreeMB() >= req.ReservationMB {
			ids = append(ids, h.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// deploymentArch is the architecture the image was built for.
func (o *Orchestrator) deploymentArch(ctx context.Context, d depRow) string {
	if d.BuildHost == "" {
		return ""
	}
	h, err := hub.LoadHost(ctx, o.db, d.BuildHost)
	if err != nil {
		return ""
	}
	return h.Facts.Arch
}

// volumeHost returns where a volume lives ("" before its first deploy).
func (o *Orchestrator) volumeHost(ctx context.Context, volumeID string) (string, error) {
	var h string
	err := o.db.QueryRowContext(ctx, `SELECT host_id FROM o_volume_hosts WHERE volume_id = ? AND trashed_at = 0`, volumeID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return h, err
}

// ---------------------------------------------------------------- rollout

// rolloutAndFinish runs the zero-downtime rollout (7.3) and finishes the deployment.
func (o *Orchestrator) rolloutAndFinish(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot) {
	var err error
	if snap.Volume != nil {
		err = o.rolloutVolume(ctx, d, snap)
	} else {
		err = o.rollout(ctx, d, snap)
	}
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		o.aborted(ctx, d)
		return
	}
	if errors.Is(err, errCompleted) {
		o.finish(ctx, d, api.DepCompleted, completedText)
		return
	}
	o.removeReplicasOf(context.WithoutCancel(ctx), d.ID)
	var q *queueTimeout
	if errors.As(err, &q) {
		d.Reason = q.Error()
		o.finish(ctx, d, api.DepCancelled, "")
		return
	}
	o.finish(ctx, d, api.DepFailed, err.Error())
}

func (o *Orchestrator) pendingNote(ctx context.Context, d *depRow) func(string) {
	last := ""
	return func(msg string) {
		if msg == last {
			return
		}
		last = msg
		o.step(ctx, d, stepStart, "running", msg)
	}
}

func (o *Orchestrator) rollout(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot) error {
	n := sched.Replicas(d.Config)
	surge := sched.SurgeSize(d.Config.Deploy.MaxSurge, n)
	o.step(ctx, d, stepStart, "running", "")
	for {
		mine, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'running'`, d.ID)
		if err != nil {
			return err
		}
		if len(mine) >= n {
			break
		}
		wave := surge
		if wave > n-len(mine) {
			wave = n - len(mine)
		}
		placed, err := o.placeReplicas(ctx, d, snap, wave, placeOpts{onPending: o.pendingNote(ctx, d)})
		if len(placed) > 0 {
			o.hub.MarkDirty(hostsOf(placed)...)
		}
		if err != nil {
			return placementDiagnosis(err)
		}
		o.step(ctx, d, stepHealthcheck, "running", "")
		if err := o.waitHealthy(ctx, d); err != nil {
			if errors.Is(err, errCompleted) {
				o.step(ctx, d, stepStart, "ok", "")
				o.step(ctx, d, stepHealthcheck, "ok", "")
				return err
			}
			o.step(ctx, d, stepHealthcheck, "failed", "")
			return err
		}
	}
	o.step(ctx, d, stepStart, "ok", "")
	o.step(ctx, d, stepHealthcheck, "ok", "")
	return o.cutover(ctx, d)
}

func hostsOf(rs []repRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		if !seen[r.HostID] {
			seen[r.HostID] = true
			out = append(out, r.HostID)
		}
	}
	return out
}

// placementDiagnosis turns a placement error into the 9.18 sentence.
func placementDiagnosis(err error) error {
	var ce *sched.NoCapacityError
	if errors.As(err, &ce) {
		return err
	}
	var ne *sched.NoEligibleError
	if errors.As(err, &ne) {
		return &queueTimeout{reason: ne.Error()}
	}
	return err
}

// waitHealthy waits until every running replica of d is healthy, failing on
// a crash, an exit or an unhealthy report (7.3 step 2).
func (o *Orchestrator) waitHealthy(ctx context.Context, d *depRow) error {
	deadline := o.now().Add(time.Duration(healthDeadline(d.Config))*time.Second + o.t.HealthSlack)
	for {
		reps, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'running'`, d.ID)
		if err != nil {
			return err
		}
		all, done := len(reps) > 0, len(reps) > 0
		for _, r := range reps {
			if ranToCompletion(d.Config, r) {
				continue
			}
			done = false
			switch r.State {
			case "healthy":
			case "crashed", "exited", "unhealthy":
				return errorf("%s", o.replicaDiagnosis(ctx, *d, r))
			default:
				all = false
			}
		}
		if done {
			return errCompleted
		}
		if all {
			return nil
		}
		if o.now().After(deadline) {
			port := targetPort(d.Config, nil)
			return errorf("%s", healthTimeout(port, healthDeadline(d.Config)))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-o.changes():
		case <-time.After(time.Second):
		}
	}
}

// cutover switches traffic to d and starts draining the previous deployment.
func (o *Orchestrator) cutover(ctx context.Context, d *depRow) error {
	o.step(ctx, d, stepCutover, "running", "")
	prev, hasPrev, err := o.activeDep(ctx, d.EnvID, d.ServiceID)
	if err != nil {
		return err
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE o_replicas SET serving = 1, draining = 0 WHERE deployment_id = ? AND desired = 'running'`, d.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET serving = 0, draining = 1 WHERE environment_id = ? AND service_id = ?
			AND deployment_id != ? AND desired IN ('running', 'stopped')`, d.EnvID, d.ServiceID, d.ID)
		return err
	}); err != nil {
		return err
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	if slow := o.hub.WaitApplied(ctx, nil, o.t.SwitchWait); len(slow) > 0 {
		o.log.Info("hosts converge later", "hosts", slow, "deployment", d.ID)
	}
	o.step(ctx, d, stepCutover, "ok", "")
	d.Status = api.DepSuccess
	o.activate(context.WithoutCancel(ctx), d)
	if hasPrev && prev.ID != d.ID {
		o.startDrain(prev, d.Config.Deploy.OverlapSec, d.Config.Deploy.DrainingSec)
	}
	// earlier deployments still draining keep draining; stray replicas of
	// older deployments go too
	o.retireOthers(context.WithoutCancel(ctx), *d, prev.ID)
	return nil
}

// activate marks d the active deployment.
func (o *Orchestrator) activate(ctx context.Context, d *depRow) {
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_deployments SET active = 0 WHERE environment_id = ? AND service_id = ? AND id != ? AND active = 1`,
			d.EnvID, d.ServiceID, d.ID)
		return err
	}); err != nil {
		o.log.Error("activate", "err", err)
	}
	d.Active = true
	o.finish(ctx, d, api.DepSuccess, "")
	o.invalidateNet()
	o.hub.MarkDirty()
}

func (o *Orchestrator) retireOthers(ctx context.Context, d depRow, keep string) {
	ds, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? AND id != ? AND id != ? AND status IN ('SUCCESS', 'CRASHED')`,
		d.EnvID, d.ServiceID, d.ID, keep)
	if err != nil {
		return
	}
	for _, x := range ds {
		o.startDrain(x, 0, 0)
	}
}

// startDrain retires a previous deployment in the background (7.3 steps 4
// and 5): it is REMOVING while its replicas drain, then REMOVED.
func (o *Orchestrator) startDrain(prev depRow, overlapSec, drainingSec int) {
	ctx := o.bg()
	prev.Status = api.DepRemoving
	prev.Active = false
	if err := o.saveDep(ctx, &prev); err != nil {
		o.log.Warn("drain", "err", err)
	}
	laneWG.Add(1)
	go func() {
		defer laneWG.Done()
		o.drain(ctx, prev, overlapSec, drainingSec)
	}()
}

func (o *Orchestrator) drain(ctx context.Context, prev depRow, overlapSec, drainingSec int) {
	if drainingSec <= 0 {
		drainingSec = 30
	}
	o.sleep(ctx, o.drainTime(overlapSec+drainingSec))
	if ctx.Err() != nil {
		return
	}
	o.stopReplicas(ctx, prev.ID)
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', draining = 0, serving = 0 WHERE deployment_id = ? AND desired IN ('running', 'stopped')`, prev.ID)
		return err
	}); err != nil {
		o.log.Warn("drain", "err", err)
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	cur, err := o.loadDep(ctx, o.db, prev.ID)
	if err == nil && cur.Status == api.DepRemoving {
		cur.FinishedAt = 0
		o.finish(ctx, &cur, api.DepRemoved, "")
	}
}

// stopReplicas sets a deployment's replicas to stopped and waits until the
// agents report them stopped (or the grace runs out).
func (o *Orchestrator) stopReplicas(ctx context.Context, depID string) {
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'stopped', serving = 0 WHERE deployment_id = ? AND desired = 'running'`, depID)
		return err
	}); err != nil {
		return
	}
	o.invalidateNet()
	reps, _ := o.queryReps(ctx, `deployment_id = ? AND desired = 'stopped'`, depID)
	o.hub.MarkDirty(hostsOf(reps)...)
	deadline := o.now().Add(time.Duration(defaultStopGraceSec)*time.Second + o.t.StopWait)
	for o.now().Before(deadline) {
		reps, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'stopped'`, depID)
		if err != nil {
			return
		}
		done := true
		for _, r := range reps {
			switch r.State {
			case "stopped", "exited", "removed", "crashed":
			default:
				if o.hub.Connected(r.HostID) {
					done = false
				}
			}
		}
		if done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-o.changes():
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// drainTime converts OverlapSec and DrainingSec to a duration.
func (o *Orchestrator) drainTime(sec int) time.Duration { return time.Duration(sec) * o.t.DrainUnit }

func (o *Orchestrator) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// removeReplicasOf takes every replica of a deployment out of the desired state.
func (o *Orchestrator) removeReplicasOf(ctx context.Context, depID string) {
	reps, _ := o.queryReps(ctx, `deployment_id = ? AND desired IN ('running', 'stopped')`, depID)
	if len(reps) == 0 {
		return
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', serving = 0, draining = 0 WHERE deployment_id = ? AND desired IN ('running', 'stopped')`, depID)
		return err
	}); err != nil {
		o.log.Warn("remove replicas", "err", err)
	}
	o.invalidateNet()
	o.hub.MarkDirty(hostsOf(reps)...)
	o.hub.MarkDirty()
}

// rolloutVolume is the rollout of a service with a volume: stop the old
// container, start the new one on the volume's host, and restart the old
// one if the new one fails (7.3).
func (o *Orchestrator) rolloutVolume(ctx context.Context, d *depRow, snap cpapi.ServiceSnapshot) error {
	prev, hasPrev, err := o.activeDep(ctx, d.EnvID, d.ServiceID)
	if err != nil {
		return err
	}
	if hasPrev && prev.ID != d.ID {
		pv, nv := prev.Config.TemplateVersion, d.Config.TemplateVersion
		if isDatabase(d.Config) && pv != "" && nv != "" && pv != nv {
			return errorf("Major version changes need a dump and restore. Use Upgrade to a new major version in the database panel.")
		}
	}
	o.step(ctx, d, stepStart, "running", "Short gap on every deploy: a service with a volume stops before the new version starts.")
	mine, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'running'`, d.ID)
	if err != nil {
		return err
	}
	if len(mine) == 0 {
		if hasPrev && prev.ID != d.ID {
			o.stopReplicas(ctx, prev.ID)
		}
		placed, err := o.placeReplicas(ctx, d, snap, 1, placeOpts{serving: true, onPending: o.pendingNote(ctx, d)})
		if len(placed) > 0 {
			o.hub.MarkDirty(hostsOf(placed)...)
		}
		if err != nil {
			o.restartPrevious(ctx, prev, hasPrev && prev.ID != d.ID)
			return placementDiagnosis(err)
		}
	}
	o.step(ctx, d, stepHealthcheck, "running", "")
	if err := o.waitHealthy(ctx, d); err != nil {
		if errors.Is(err, errCompleted) {
			o.step(ctx, d, stepStart, "ok", "")
			o.step(ctx, d, stepHealthcheck, "ok", "")
			return err
		}
		o.step(ctx, d, stepHealthcheck, "failed", "")
		if ctx.Err() != nil {
			o.removeReplicasOf(context.WithoutCancel(ctx), d.ID)
			o.restartPrevious(context.WithoutCancel(ctx), prev, hasPrev && prev.ID != d.ID)
			return ctx.Err()
		}
		o.removeReplicasOf(ctx, d.ID)
		if hasPrev && prev.ID != d.ID {
			o.restartPrevious(ctx, prev, true)
			return errorf("The new version failed its health check; the previous version was restarted. %s", err.Error())
		}
		return err
	}
	o.step(ctx, d, stepStart, "ok", "")
	o.step(ctx, d, stepHealthcheck, "ok", "")
	o.step(ctx, d, stepCutover, "running", "")
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE o_replicas SET serving = 1 WHERE deployment_id = ? AND desired = 'running'`, d.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', serving = 0, draining = 0 WHERE environment_id = ? AND service_id = ?
			AND deployment_id != ? AND desired IN ('running', 'stopped')`, d.EnvID, d.ServiceID, d.ID)
		return err
	}); err != nil {
		return err
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.step(ctx, d, stepCutover, "ok", "")
	d.Status = api.DepSuccess
	o.activate(context.WithoutCancel(ctx), d)
	if hasPrev && prev.ID != d.ID {
		cur, err := o.loadDep(ctx, o.db, prev.ID)
		if err == nil && !terminalRemoved(cur.Status) {
			cur.Active = false
			cur.FinishedAt = 0
			o.finish(context.WithoutCancel(ctx), &cur, api.DepRemoved, "")
		}
	}
	return nil
}

func terminalRemoved(s api.DeploymentStatus) bool {
	return s == api.DepRemoved || s == api.DepCancelled
}

func (o *Orchestrator) restartPrevious(ctx context.Context, prev depRow, ok bool) {
	if !ok {
		return
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'running', serving = 1 WHERE deployment_id = ? AND desired = 'stopped'`, prev.ID)
		return err
	}); err != nil {
		o.log.Warn("restart previous", "err", err)
	}
	o.invalidateNet()
	o.hub.MarkDirty()
}

// ---------------------------------------------------------------- resume after a restart

// resume re-queues QUEUED deployments in creation order, resumes DEPLOYING
// ones from their replica states, re-attaches running builds and finishes
// drains (9.1, 6.5).
func (o *Orchestrator) resume(ctx context.Context) error {
	ds, err := o.queryDeps(ctx, `status IN ('QUEUED', 'BUILDING', 'DEPLOYING', 'REMOVING') ORDER BY created_at, id`)
	if err != nil {
		return err
	}
	for _, d := range ds {
		switch d.Status {
		case api.DepQueued:
			o.enqueue(d)
		case api.DepDeploying:
			o.resumeLane(d, func(ctx context.Context, d depRow) {
				snap, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID)
				if err != nil || snap.Deleted {
					d.Reason = "The service was deleted before it deployed."
					o.removeReplicasOf(ctx, d.ID)
					o.finish(ctx, &d, api.DepCancelled, "")
					return
				}
				o.rolloutAndFinish(ctx, &d, snap)
			})
		case api.DepBuilding:
			o.resumeLane(d, o.resumeBuild)
		case api.DepRemoving:
			laneWG.Add(1)
			go func(d depRow) {
				defer laneWG.Done()
				o.drain(o.bg(), d, 0, 1)
			}(d)
		}
	}
	return nil
}

// resumeLane runs fn as the lane's current work, ahead of queued deployments.
func (o *Orchestrator) resumeLane(d depRow, fn func(context.Context, depRow)) {
	o.mu.Lock()
	l := o.laneFor(d.EnvID, d.ServiceID)
	if l.running {
		o.mu.Unlock()
		return
	}
	l.running = true
	laneWG.Add(1)
	ctx, cancel := context.WithCancel(o.bgLocked())
	l.current, l.cancel = d.ID, cancel
	o.mu.Unlock()
	go func() {
		fn(ctx, d)
		cancel()
		o.mu.Lock()
		l.current, l.cancel = "", nil
		o.mu.Unlock()
		o.runLane(l) // continues with anything queued meanwhile (runLane calls Done)
	}()
}

// resumeBuild waits for the result of a build that was running when the
// control plane stopped.
func (o *Orchestrator) resumeBuild(ctx context.Context, d depRow) {
	ch := make(chan buildOutcome, 1)
	o.mu.Lock()
	o.buildWait[d.ID] = ch
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.buildWait, d.ID)
		o.mu.Unlock()
	}()
	limit := 30*time.Minute + o.t.BuildGrace
	if d.Config.Build.TimeoutMin > 0 {
		limit = time.Duration(d.Config.Build.TimeoutMin)*time.Minute + o.t.BuildGrace
	}
	if d.StartedAt > 0 {
		limit -= o.now().Sub(time.UnixMilli(d.StartedAt))
	}
	if limit < o.t.BuildGrace {
		limit = o.t.BuildGrace
	}
	t := time.NewTimer(limit)
	defer t.Stop()
	var res proto.BuildResult
	select {
	case out := <-ch:
		res = out.res
	case <-ctx.Done():
		return
	case <-t.C:
		o.finish(ctx, &d, api.DepFailed, "Interrupted by a control plane restart. Redeploy to retry.")
		return
	}
	if !res.OK {
		o.finish(ctx, &d, api.DepFailed, firstNonEmpty(res.Diagnosis, "The build failed. Check the build logs for the error."))
		return
	}
	d.Image = strings.ToLower(d.ServiceID) + ":" + d.ID
	d.Digest, d.IsNode, d.StartCmd = res.Digest, res.IsNode, res.StartCommand
	merged, err := o.cfg.MergeConfigFile(ctx, d.EnvID, d.ServiceID, commitSHA(d.Commit), res.ConfigFile)
	if err == nil && res.ConfigFile != nil {
		d.Config = merged.Config
		d.SourceMap = merged.Source
	}
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO o_images (deployment_id, service_id, ref, digest, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(deployment_id) DO NOTHING`, d.ID, d.ServiceID, d.Image, d.Digest, o.now().UnixMilli())
		return err
	})
	o.step(ctx, &d, stepBuild, "ok", "")
	snap, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID)
	if err != nil || snap.Deleted {
		d.Reason = "The service was deleted before it deployed."
		o.finish(ctx, &d, api.DepCancelled, "")
		return
	}
	d.Status = api.DepDeploying
	_ = o.saveDep(ctx, &d)
	if d.Config.Deploy.PreDeployCommand != "" {
		if err := o.preDeploy(ctx, &d, snap); err != nil {
			o.finish(ctx, &d, api.DepFailed, err.Error())
			return
		}
	}
	o.rolloutAndFinish(ctx, &d, snap)
}

// deliverBuild hands a build result to whoever waits for it.
func (o *Orchestrator) deliverBuild(res proto.BuildResult) bool {
	o.mu.Lock()
	ch := o.buildWait[res.BuildID]
	o.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- buildOutcome{res: res}:
	default:
	}
	return true
}
