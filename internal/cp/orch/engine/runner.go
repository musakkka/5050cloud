// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/data"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// jobTracker matches job_log and job_result messages to waiting callers.
// It lives above the socket: agents keep results across a reconnect (7.5),
// so a job survives a network blip.
type jobTracker struct {
	mu      sync.Mutex
	waiting map[string]*jobWait
}

type jobWait struct {
	logf func(string)
	done chan proto.JobResult
}

func newJobTracker() *jobTracker { return &jobTracker{waiting: map[string]*jobWait{}} }

func (t *jobTracker) add(id string, logf func(string)) *jobWait {
	w := &jobWait{logf: logf, done: make(chan proto.JobResult, 1)}
	t.mu.Lock()
	t.waiting[id] = w
	t.mu.Unlock()
	return w
}

func (t *jobTracker) remove(id string) {
	t.mu.Lock()
	delete(t.waiting, id)
	t.mu.Unlock()
}

func (t *jobTracker) log(jl proto.JobLog) {
	t.mu.Lock()
	w := t.waiting[jl.JobID]
	t.mu.Unlock()
	if w == nil || w.logf == nil {
		return
	}
	for _, l := range jl.Lines {
		w.logf(l)
	}
}

func (t *jobTracker) result(jr proto.JobResult) {
	t.mu.Lock()
	w := t.waiting[jr.JobID]
	t.mu.Unlock()
	if w == nil {
		return
	}
	select {
	case w.done <- jr:
	default:
	}
}

// jobGrace is how long a job result may arrive after the job's timeout.
const jobGrace = 2 * time.Minute

// RunJob dispatches a job to a host and waits for its result (data.Runner).
func (o *Orchestrator) RunJob(ctx context.Context, hostID string, job proto.Job, logf func(line string)) (proto.JobResult, error) {
	if job.JobID == "" {
		job.JobID = hub.NewID("job")
	}
	host, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return proto.JobResult{}, err
	}
	job.Image = o.localImage(host, job.Image)
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	w := o.jobs.add(job.JobID, logf)
	defer o.jobs.remove(job.JobID)
	if err := o.hub.Send(ctx, hostID, proto.TJob, job.JobID, job); err != nil {
		return proto.JobResult{}, errorf("The job could not be sent to host %s: %s", host.Name, sentenceOf(err))
	}
	limit := time.Duration(job.TimeoutSec)*time.Second + jobGrace
	if job.TimeoutSec <= 0 {
		limit = time.Hour + jobGrace
	}
	t := time.NewTimer(limit)
	defer t.Stop()
	select {
	case res := <-w.done:
		return res, nil
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = o.hub.Send(sctx, hostID, proto.TJobCancel, hub.NewID("req"), proto.JobResult{JobID: job.JobID})
		cancel()
		return proto.JobResult{}, ctx.Err()
	case <-t.C:
		return proto.JobResult{}, errorf("Host %s did not report the result of job %s in time.", host.Name, job.JobID)
	}
}

// localImage rewrites an internal registry reference for the host that pulls it.
func (o *Orchestrator) localImage(h hub.HostRow, image string) string {
	for _, prefix := range []string{"127.0.0.1:" + registryPort + "/", o.plan.MeshAddr(1).String() + ":" + registryPort + "/"} {
		if strings.HasPrefix(image, prefix) {
			return o.registryAddr(h) + "/" + strings.TrimPrefix(image, prefix)
		}
	}
	return image
}

// PlaceJob picks an online host with the roles that can take envID (data.Runner).
func (o *Orchestrator) PlaceJob(ctx context.Context, envID string, roles []string) (string, error) {
	hosts, err := o.schedHosts(ctx, envID)
	if err != nil {
		return "", err
	}
	id, err := sched.Place(hosts, nil, sched.Request{EnvID: envID, Roles: roles})
	if err != nil {
		return "", err
	}
	return id, nil
}

// jobNetwork reserves an address for a job on a host and makes sure the
// environment's network exists there.
func (o *Orchestrator) jobNetwork(ctx context.Context, envID, hostID, owner string) (string, string, func(), error) {
	host, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return "", "", nil, err
	}
	var ip string
	created := false
	err = o.st.Write(ctx, func(tx *sql.Tx) error {
		slot, err := ipam.Slot(ctx, tx, hostID, envID)
		if err != nil {
			return err
		}
		created = slot == 0
		ip, err = ipam.Allocate(ctx, tx, o.plan, hostID, host.Name, host.MeshIndex, envID, owner)
		return err
	})
	if err != nil {
		return "", "", nil, err
	}
	if created {
		o.hub.MarkDirty(hostID)
		o.hub.WaitApplied(ctx, []string{hostID}, o.t.SwitchWait)
	}
	release := func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = o.st.Write(rctx, func(tx *sql.Tx) error { return ipam.Release(rctx, tx, owner) })
	}
	return "5050-" + envID, ip, release, nil
}

// runner adapts the orchestrator to data.Runner.
type runner struct{ o *Orchestrator }

var _ data.Runner = runner{}

func (r runner) RunJob(ctx context.Context, hostID string, job proto.Job, logf func(string)) (proto.JobResult, error) {
	return r.o.RunJob(ctx, hostID, job, logf)
}

func (r runner) PlaceJob(ctx context.Context, envID string, roles []string) (string, error) {
	return r.o.PlaceJob(ctx, envID, roles)
}

func (r runner) ActiveReplica(ctx context.Context, envID, serviceID string) (string, string, error) {
	return r.o.activeReplica(ctx, envID, serviceID)
}

func (r runner) VolumeHost(ctx context.Context, envID, serviceID string) (string, error) {
	snap, err := r.o.cfg.ServiceInstance(ctx, envID, serviceID)
	if err != nil {
		return "", err
	}
	if snap.Volume == nil {
		return "", nil
	}
	return r.o.volumeHost(ctx, snap.Volume.ID)
}

func (r runner) ControlHost(ctx context.Context) (string, error) { return r.o.controlHost(ctx) }

func (r runner) JobNetwork(ctx context.Context, envID, hostID string) (string, string, func(), error) {
	return r.o.jobNetwork(ctx, envID, hostID, hub.NewID("job"))
}

func (r runner) Deploy(ctx context.Context, in cpapi.DeployInput) (api.Deployment, error) {
	return r.o.Deploy(ctx, in)
}

func (r runner) StopService(ctx context.Context, envID, serviceID, actor string) error {
	return r.o.stopService(ctx, envID, serviceID, actor)
}

func (r runner) PauseBuilds(ctx context.Context) (func(), error) { return r.o.pauseBuilds(ctx) }

func (r runner) RegistryJob(ctx context.Context, cmd []string, env map[string]string) (proto.JobResult, error) {
	host, err := r.o.controlHost(ctx)
	if err != nil {
		return proto.JobResult{}, err
	}
	// The agent runs the registry as the container "5050-registry"; an exec
	// job with that replica id runs inside it (8.1).
	job := proto.Job{JobID: hub.NewID("job"), Kind: "exec", ReplicaID: registryContainer, Cmd: cmd, Env: env, TimeoutSec: 3600,
		Labels: map[string]string{}}
	return r.o.RunJob(ctx, host, job, nil)
}

func (r runner) ActiveImage(ctx context.Context, envID, serviceID string) (string, api.ServiceConfig, map[string]string, error) {
	d, ok, err := r.o.activeDep(ctx, envID, serviceID)
	if err != nil {
		return "", api.ServiceConfig{}, nil, err
	}
	if !ok || d.Image == "" {
		return "", api.ServiceConfig{}, nil, errorf("This service has no active deployment yet. Deploy it first.")
	}
	env, err := r.o.openEnv(d.ID, d.EnvEnc)
	if err != nil {
		return "", api.ServiceConfig{}, nil, err
	}
	cp := make(map[string]string, len(env))
	for k, v := range env {
		cp[k] = v
	}
	ctrl, err := r.o.controlHost(ctx)
	if err != nil {
		return "", api.ServiceConfig{}, nil, err
	}
	h, err := hub.LoadHost(ctx, r.o.db, ctrl)
	if err != nil {
		return "", api.ServiceConfig{}, nil, err
	}
	// RunJob rewrites this reference for the host that runs the job.
	return r.o.imageRef(h, d), d.Config, cp, nil
}

// registryContainer is the replica id the agent's registry container answers to.
const registryContainer = "registry"

// controlHost returns the host with mesh index 1.
func (o *Orchestrator) controlHost(ctx context.Context) (string, error) {
	var id string
	err := o.db.QueryRowContext(ctx, `SELECT id FROM o_hosts WHERE mesh_index = 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errorf("The control host has not joined yet.")
	}
	return id, err
}

// activeReplica returns a running replica of the active deployment, healthy first.
func (o *Orchestrator) activeReplica(ctx context.Context, envID, serviceID string) (string, string, error) {
	d, ok, err := o.activeDep(ctx, envID, serviceID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", errorf("This service has no active deployment. Deploy it first.")
	}
	reps, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'running' ORDER BY created_at, id`, d.ID)
	if err != nil {
		return "", "", err
	}
	var fallback *repRow
	for i := range reps {
		r := reps[i]
		if !o.hub.Connected(r.HostID) {
			continue
		}
		if r.State == "healthy" {
			return r.HostID, r.ID, nil
		}
		if fallback == nil && (r.State == "running" || r.State == "unhealthy" || r.State == "starting") {
			fallback = &reps[i]
		}
	}
	if fallback != nil {
		return fallback.HostID, fallback.ID, nil
	}
	return "", "", errorf("No replica of this service is running on an online host.")
}

// stopService stops every replica of a service without a new deployment.
func (o *Orchestrator) stopService(ctx context.Context, envID, serviceID, actor string) error {
	deps, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? AND id IN
		(SELECT deployment_id FROM o_replicas WHERE desired = 'running')`, envID, serviceID)
	if err != nil {
		return err
	}
	for _, d := range deps {
		o.stopReplicas(ctx, d.ID)
	}
	if len(deps) > 0 {
		o.activity(ctx, envID, serviceID, "deployment", "Service stopped", actor, "", "")
	}
	return nil
}

// pauseBuilds pauses the build queue and waits for running builds (and
// their pushes) to end (9.6).
func (o *Orchestrator) pauseBuilds(ctx context.Context) (func(), error) {
	o.mu.Lock()
	o.paused++
	o.mu.Unlock()
	var once sync.Once
	resume := func() {
		once.Do(func() {
			o.mu.Lock()
			o.paused--
			o.pauseCond.Broadcast()
			o.mu.Unlock()
			o.notify()
		})
	}
	for {
		o.mu.Lock()
		busy := 0
		for _, n := range o.building {
			busy += n
		}
		o.mu.Unlock()
		if busy == 0 {
			return resume, nil
		}
		select {
		case <-ctx.Done():
			resume()
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
