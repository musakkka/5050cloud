// SPDX-License-Identifier: AGPL-3.0-only

// Package fakerunner is an in-memory data.Runner for tests of the data
// services and of the orchestrator code that drives them (docs/SPEC.md 18.1).
// It records every call in order, answers jobs through a handler the test
// sets, and keeps a simple model of running services and volume hosts:
// Deploy starts a service on DefaultHost (and places its volume there),
// StopService stops it.
package fakerunner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// ErrNotRunning is returned by ActiveReplica for a stopped service.
var ErrNotRunning = errors.New("the service has no running replica")

// Job is one recorded RunJob call.
type Job struct {
	Host string
	Job  proto.Job
}

// Image is what ActiveImage returns for a service.
type Image struct {
	Ref    string
	Config api.ServiceConfig
	Env    map[string]string
}

// Runner implements data.Runner. Use New; set the hooks before the code under test runs.
type Runner struct {
	mu sync.Mutex

	// DefaultHost is where Deploy starts services and PlaceJob places jobs.
	DefaultHost string
	// ControlHostID is the control host.
	ControlHostID string
	// AutoStart makes Deploy mark the service running (default true).
	AutoStart bool

	// OnJob answers RunJob; the default answers exit 0 with a backup summary line.
	OnJob func(host string, job proto.Job) (proto.JobResult, error)
	// OnRegistryJob answers RegistryJob; the default answers exit 0.
	OnRegistryJob func(cmd []string, env map[string]string) (proto.JobResult, error)
	// OnDeploy runs after a recorded Deploy (for example to fail it).
	OnDeploy func(in cpapi.DeployInput) error
	// PauseErr makes PauseBuilds fail.
	PauseErr error

	calls    []string
	jobs     []Job
	deploys  []cpapi.DeployInput
	running  map[string]string // env/svc -> host
	volumes  map[string]string // env/svc -> host
	images   map[string]Image
	seq      int
	networks int
}

// New returns a runner with one host, "host_1", that is also the control host.
func New() *Runner {
	return &Runner{
		DefaultHost: "host_1", ControlHostID: "host_1", AutoStart: true,
		running: map[string]string{}, volumes: map[string]string{}, images: map[string]Image{},
	}
}

func key(env, svc string) string { return env + "/" + svc }

func (r *Runner) record(format string, args ...any) {
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------- setup and inspection

// SetRunning marks a service running on host with its active image.
func (r *Runner) SetRunning(envID, svcID, host string, img Image) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[key(envID, svcID)] = host
	r.volumes[key(envID, svcID)] = host
	r.images[key(envID, svcID)] = img
}

// SetVolumeHost places a service's volume on host without running it.
func (r *Runner) SetVolumeHost(envID, svcID, host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.volumes[key(envID, svcID)] = host
}

// SetImage sets the active image of a service.
func (r *Runner) SetImage(envID, svcID string, img Image) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.images[key(envID, svcID)] = img
}

// Running reports whether a service is running.
func (r *Runner) Running(envID, svcID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[key(envID, svcID)]
	return ok
}

// Calls returns every call in order ("PauseBuilds", "RegistryJob registry garbage-collect ...").
func (r *Runner) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// Jobs returns every RunJob call.
func (r *Runner) Jobs() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Job(nil), r.jobs...)
}

// JobsOfKind returns the RunJob calls of one job kind.
func (r *Runner) JobsOfKind(kind string) []Job {
	var out []Job
	for _, j := range r.Jobs() {
		if j.Job.Kind == kind {
			out = append(out, j)
		}
	}
	return out
}

// Deploys returns every Deploy call.
func (r *Runner) Deploys() []cpapi.DeployInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]cpapi.DeployInput(nil), r.deploys...)
}

// ---------------------------------------------------------------- data.Runner

func (r *Runner) RunJob(_ context.Context, hostID string, job proto.Job, logf func(string)) (proto.JobResult, error) {
	r.mu.Lock()
	r.record("RunJob %s %s %s", job.Kind, hostID, strings.Join(job.Cmd, " "))
	r.jobs = append(r.jobs, Job{Host: hostID, Job: job})
	h := r.OnJob
	r.mu.Unlock()
	if h == nil {
		res := proto.JobResult{JobID: job.JobID, Output: `{"ok":true,"sizeBytes":1024,"id":"0123456789abcdef"}` + "\n"}
		if logf != nil {
			logf(res.Output)
		}
		return res, nil
	}
	res, err := h(hostID, job)
	if res.JobID == "" {
		res.JobID = job.JobID
	}
	if logf != nil && res.Output != "" {
		for _, l := range strings.Split(strings.TrimRight(res.Output, "\n"), "\n") {
			logf(l)
		}
	}
	return res, err
}

func (r *Runner) PlaceJob(_ context.Context, envID string, roles []string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("PlaceJob %s %s", envID, strings.Join(roles, ","))
	if r.DefaultHost == "" {
		return "", errors.New("no host is online")
	}
	return r.DefaultHost, nil
}

func (r *Runner) ActiveReplica(_ context.Context, envID, serviceID string) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.running[key(envID, serviceID)]
	if !ok {
		return "", "", ErrNotRunning
	}
	return h, "rep_" + serviceID, nil
}

func (r *Runner) VolumeHost(_ context.Context, envID, serviceID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.volumes[key(envID, serviceID)], nil
}

func (r *Runner) ControlHost(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ControlHostID == "" {
		return "", errors.New("there is no control host")
	}
	return r.ControlHostID, nil
}

func (r *Runner) JobNetwork(_ context.Context, envID, hostID string) (string, string, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.networks++
	ip := fmt.Sprintf("10.130.0.%d", 100+r.networks%100)
	r.record("JobNetwork %s %s", envID, hostID)
	return "5050-" + envID, ip, func() {
		r.mu.Lock()
		r.record("ReleaseNetwork %s", ip)
		r.mu.Unlock()
	}, nil
}

func (r *Runner) Deploy(_ context.Context, in cpapi.DeployInput) (api.Deployment, error) {
	r.mu.Lock()
	r.seq++
	r.record("Deploy %s %s", in.ServiceID, in.Trigger)
	r.deploys = append(r.deploys, in)
	dep := api.Deployment{ID: fmt.Sprintf("dep_%026d", r.seq), ServiceID: in.ServiceID, Status: api.DepQueued, Trigger: in.Trigger}
	if r.AutoStart {
		host := r.DefaultHost
		if h := r.volumes[key(in.EnvID, in.ServiceID)]; h != "" {
			host = h
		}
		r.running[key(in.EnvID, in.ServiceID)] = host
		r.volumes[key(in.EnvID, in.ServiceID)] = host
		if _, ok := r.images[key(in.EnvID, in.ServiceID)]; !ok {
			r.images[key(in.EnvID, in.ServiceID)] = Image{Ref: "image-of-" + in.ServiceID, Env: map[string]string{}}
		}
	}
	h := r.OnDeploy
	r.mu.Unlock()
	if h != nil {
		if err := h(in); err != nil {
			return api.Deployment{}, err
		}
	}
	return dep, nil
}

func (r *Runner) StopService(_ context.Context, envID, serviceID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("StopService %s", serviceID)
	delete(r.running, key(envID, serviceID))
	return nil
}

func (r *Runner) PauseBuilds(context.Context) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record("PauseBuilds")
	if r.PauseErr != nil {
		return nil, r.PauseErr
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.record("ResumeBuilds")
			r.mu.Unlock()
		})
	}, nil
}

func (r *Runner) RegistryJob(_ context.Context, cmd []string, env map[string]string) (proto.JobResult, error) {
	r.mu.Lock()
	if len(cmd) == 0 {
		var kv []string
		for k, v := range env {
			kv = append(kv, k+"="+v)
		}
		r.record("RegistryRestart %s", strings.Join(kv, " "))
	} else {
		first := cmd
		if len(cmd) > 3 && cmd[0] == "sh" {
			first = cmd[:2] // scripts are long; record the shape
		}
		r.record("RegistryJob %s", strings.Join(first, " "))
	}
	h := r.OnRegistryJob
	r.mu.Unlock()
	if h == nil {
		return proto.JobResult{}, nil
	}
	return h(cmd, env)
}

func (r *Runner) ActiveImage(_ context.Context, envID, serviceID string) (string, api.ServiceConfig, map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	img, ok := r.images[key(envID, serviceID)]
	if !ok {
		return "", api.ServiceConfig{}, nil, errors.New("the service has no successful deployment")
	}
	env := map[string]string{}
	for k, v := range img.Env {
		env[k] = v
	}
	return img.Ref, img.Config, env, nil
}

// ---------------------------------------------------------------- optional volume placement

// Placing is a Runner that also implements data.VolumePlacer.
type Placing struct {
	*Runner
}

// PlaceVolume records the volume on hostID, or on DefaultHost when it is "".
func (p Placing) PlaceVolume(_ context.Context, envID, serviceID, hostID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if hostID == "" {
		hostID = p.DefaultHost
	}
	p.record("PlaceVolume %s %s", serviceID, hostID)
	p.volumes[key(envID, serviceID)] = hostID
	return hostID, nil
}
