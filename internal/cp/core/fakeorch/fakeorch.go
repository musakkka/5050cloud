// SPDX-License-Identifier: AGPL-3.0-only

// Package fakeorch is an in-memory cpapi.Orchestrator for `5050d serve
// --dev` and core tests (docs/SPEC.md 19, WP1 milestone 1). It never runs
// containers: every deployment it creates is recorded with the plan that
// caused it and reported SUCCESS at once, and every other call answers
// typed, empty data. It is never wired in a production serve.
package fakeorch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Orch records what the core asked for.
type Orch struct {
	Config cpapi.Config

	mu      sync.Mutex
	Plans   []cpapi.CommitPlan
	Deploys []cpapi.DeployInput
	deps    map[string]api.Deployment
	order   []string
	Network []string // environments passed to NetworkChanged
}

var _ cpapi.Orchestrator = (*Orch)(nil)

// New returns an empty fake.
func New(cfg cpapi.Config) *Orch { return &Orch{Config: cfg, deps: map[string]api.Deployment{}} }

// ErrDev is returned by calls that need real hosts.
var ErrDev = errors.New("the development server has no hosts; this needs a real install")

func now() int64 { return time.Now().UnixMilli() }

func (o *Orch) record(envID, sid, trigger, actor, msg string) api.Deployment {
	t := now()
	d := api.Deployment{ID: ids.New("dep"), ServiceID: sid, EnvironmentID: envID, Status: api.DepSuccess, Active: true,
		Trigger: trigger, Actor: actor, Message: msg, Steps: []api.DeployStep{}, CreatedAt: t, StartedAt: t, FinishedAt: t, CanRollback: true}
	for id, x := range o.deps {
		if x.ServiceID == sid && x.EnvironmentID == envID && x.Active {
			x.Active = false
			x.Status = api.DepRemoved
			x.Label = api.StatusLabel(x.Status, false)
			o.deps[id] = x
		}
	}
	d.Label = api.StatusLabel(d.Status, true)
	o.deps[d.ID] = d
	o.order = append(o.order, d.ID)
	return d
}

func (o *Orch) Start(ctx context.Context) error   { <-ctx.Done(); return nil }
func (o *Orch) Ready(context.Context) (bool, int) { return true, 0 }
func (o *Orch) AgentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, ErrDev.Error(), http.StatusServiceUnavailable)
	})
}

func (o *Orch) Hosts(context.Context) ([]api.Host, error) { return []api.Host{}, nil }
func (o *Orch) CreateJoinToken(context.Context, api.CreateJoinTokenRequest) (api.JoinToken, error) {
	return api.JoinToken{}, ErrDev
}
func (o *Orch) UpdateHost(context.Context, string, api.UpdateHostRequest) (api.Host, error) {
	return api.Host{}, ErrDev
}
func (o *Orch) SetMaintenance(context.Context, string, bool, string) (api.Host, error) {
	return api.Host{}, ErrDev
}
func (o *Orch) RemoveHost(context.Context, string, bool) error { return ErrDev }
func (o *Orch) ClearHostConflicts(context.Context, string) (api.Host, error) {
	return api.Host{}, ErrDev
}
func (o *Orch) HostLost(context.Context, string, string) error          { return ErrDev }
func (o *Orch) Sync(context.Context, string) error                      { return nil }
func (o *Orch) Adopt(context.Context, int64) error                      { return nil }
func (o *Orch) AdoptStatus(context.Context) (*api.RestoreStatus, error) { return nil, nil }
func (o *Orch) UnknownReplicas(context.Context) ([]api.UnknownReplica, error) {
	return []api.UnknownReplica{}, nil
}
func (o *Orch) AdoptReplica(context.Context, string, string) error         { return ErrDev }
func (o *Orch) RemoveUnknownReplica(context.Context, string, string) error { return ErrDev }
func (o *Orch) ReadoptHost(context.Context, string, string) (api.Host, error) {
	return api.Host{}, ErrDev
}
func (o *Orch) FinishRestore(context.Context, string) error               { return nil }
func (o *Orch) MoveReplica(context.Context, string, string, string) error { return ErrDev }
func (o *Orch) HostMetrics(context.Context, string, int64, int64) (api.MetricsResponse, error) {
	return emptyMetrics(), nil
}

// ApplyCommit records the plan and one SUCCESS deployment per create or update.
func (o *Orch) ApplyCommit(_ context.Context, p cpapi.CommitPlan) ([]api.Deployment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Plans = append(o.Plans, p)
	out := []api.Deployment{}
	for _, it := range p.Items {
		if it.Action == "delete" {
			for id, x := range o.deps {
				if x.ServiceID == it.ServiceID && x.EnvironmentID == p.EnvID && x.Active {
					x.Active, x.Status = false, api.DepRemoved
					o.deps[id] = x
				}
			}
			continue
		}
		if it.Action == "scale" {
			continue
		}
		out = append(out, o.record(p.EnvID, it.ServiceID, "commit", p.Actor, p.Message))
	}
	return out, nil
}

// LastPlan returns the most recent commit plan.
func (o *Orch) LastPlan() (cpapi.CommitPlan, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.Plans) == 0 {
		return cpapi.CommitPlan{}, false
	}
	return o.Plans[len(o.Plans)-1], true
}

func (o *Orch) Deploy(_ context.Context, in cpapi.DeployInput) (api.Deployment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Deploys = append(o.Deploys, in)
	if in.Skipped {
		d := api.Deployment{ID: ids.New("dep"), ServiceID: in.ServiceID, EnvironmentID: in.EnvID, Status: api.DepSkipped, Trigger: in.Trigger,
			Actor: in.Actor, Message: in.Message, Commit: in.Commit, Steps: []api.DeployStep{}, CreatedAt: now()}
		d.Label = api.StatusLabel(d.Status, false)
		o.deps[d.ID] = d
		o.order = append(o.order, d.ID)
		return d, nil
	}
	d := o.record(in.EnvID, in.ServiceID, in.Trigger, in.Actor, in.Message)
	d.Commit = in.Commit
	o.deps[d.ID] = d
	return d, nil
}

func (o *Orch) again(id, trigger, actor string) (api.Deployment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	src, ok := o.deps[id]
	if !ok {
		return api.Deployment{}, fail.NotFound("The deployment %s does not exist.", id)
	}
	d := o.record(src.EnvironmentID, src.ServiceID, trigger, actor, src.Message)
	if trigger == "rollback" {
		d.RollbackOf = id
		o.deps[d.ID] = d
	}
	return d, nil
}

func (o *Orch) Redeploy(_ context.Context, id, actor string) (api.Deployment, error) {
	return o.again(id, "redeploy", actor)
}
func (o *Orch) Rollback(_ context.Context, id, actor string) (api.Deployment, error) {
	return o.again(id, "rollback", actor)
}
func (o *Orch) Restart(_ context.Context, id, actor string) (api.Deployment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	d, ok := o.deps[id]
	if !ok {
		return d, fail.NotFound("The deployment %s does not exist.", id)
	}
	d.Restarts++
	o.deps[id] = d
	return d, nil
}
func (o *Orch) Abort(ctx context.Context, id, actor string) (api.Deployment, error) {
	return o.setStatus(id, api.DepCancelled)
}
func (o *Orch) Remove(ctx context.Context, id, actor string) (api.Deployment, error) {
	return o.setStatus(id, api.DepRemoved)
}

func (o *Orch) setStatus(id string, st api.DeploymentStatus) (api.Deployment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	d, ok := o.deps[id]
	if !ok {
		return d, fail.NotFound("The deployment %s does not exist.", id)
	}
	d.Status, d.Active = st, false
	d.Label = api.StatusLabel(st, false)
	o.deps[id] = d
	return d, nil
}

func (o *Orch) Deployments(_ context.Context, envID, sid string, limit int, _ string) ([]api.Deployment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := []api.Deployment{}
	for i := len(o.order) - 1; i >= 0; i-- {
		d := o.deps[o.order[i]]
		if d.EnvironmentID == envID && d.ServiceID == sid {
			out = append(out, d)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (o *Orch) Deployment(_ context.Context, id string) (api.DeploymentDetail, error) {
	o.mu.Lock()
	d, ok := o.deps[id]
	o.mu.Unlock()
	if !ok {
		return api.DeploymentDetail{}, fail.NotFound("The deployment %s does not exist.", id)
	}
	dd := api.DeploymentDetail{Deployment: d, VariableNames: []string{}, Replicas: []api.Replica{}}
	if o.Config != nil {
		if s, err := o.Config.ServiceInstance(context.Background(), d.EnvironmentID, d.ServiceID); err == nil {
			dd.Config = s.Config
		}
		if env, _, err := o.Config.ResolveVariables(context.Background(), d.EnvironmentID, d.ServiceID, nil); err == nil {
			for k := range env {
				dd.VariableNames = append(dd.VariableNames, k)
			}
			sort.Strings(dd.VariableNames)
		}
	}
	return dd, nil
}

func (o *Orch) BuildLogs(context.Context, string, bool, func([]api.LogLine) error) error { return nil }

func (o *Orch) Runtime(_ context.Context, envID string) (map[string]cpapi.Runtime, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := map[string]cpapi.Runtime{}
	for _, d := range o.deps {
		if d.EnvironmentID == envID && d.Active {
			out[d.ServiceID] = cpapi.Runtime{Status: api.StatusOnline, StatusText: "Online (development server)", ActiveDeployment: d.ID}
		}
	}
	return out, nil
}

func (o *Orch) NetworkChanged(_ context.Context, envID string) {
	o.mu.Lock()
	o.Network = append(o.Network, envID)
	o.mu.Unlock()
}
func (o *Orch) DomainCertStatus(context.Context, string) (string, int64, error) {
	return "issuing", 0, nil
}
func (o *Orch) RetryCertificate(context.Context, string) error { return nil }
func (o *Orch) Logs(context.Context, cpapi.LogsQuery) (api.LogsResponse, error) {
	return api.LogsResponse{Lines: []api.LogLine{}}, nil
}
func (o *Orch) TailLogs(ctx context.Context, _ cpapi.LogsQuery, _ func([]api.LogLine) error) error {
	<-ctx.Done()
	return nil
}
func (o *Orch) Metrics(context.Context, string, string, int64, int64, bool) (api.MetricsResponse, error) {
	return emptyMetrics(), nil
}
func (o *Orch) DBMetrics(context.Context, string, string, int64, int64) (api.DBMetricsResponse, error) {
	return api.DBMetricsResponse{Step: 60000, Series: map[string][][2]float64{}, TopQueries: []api.QueryStat{}}, nil
}
func (o *Orch) Monitors(context.Context, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (o *Orch) Usage(_ context.Context, _ string, from, until int64) (api.UsageResponse, error) {
	return api.UsageResponse{From: from, Until: until, Services: []api.UsageRow{}, Total: api.UsageRow{Name: "Total"}}, nil
}
func (o *Orch) Shell(context.Context, string, string, string, string, []string, int, int, io.ReadWriter, <-chan [2]int) (int, error) {
	return 0, ErrDev
}
func (o *Orch) Tunnel(context.Context, string, string, int, io.ReadWriteCloser) error { return ErrDev }
func (o *Orch) VolumeRuntime(context.Context, string, string) (cpapi.VolumeRuntime, error) {
	return cpapi.VolumeRuntime{}, nil
}
func (o *Orch) WipeVolume(context.Context, string, string, string) error { return ErrDev }
func (o *Orch) CronRuns(context.Context, string, string, int) ([]api.CronRun, error) {
	return []api.CronRun{}, nil
}
func (o *Orch) RunCronNow(context.Context, string, string, string) (api.CronRun, error) {
	return api.CronRun{}, ErrDev
}
func (o *Orch) Backups(context.Context, string, string) (api.BackupsResponse, error) {
	return api.BackupsResponse{Backups: []api.Backup{}, Schedule: api.BackupSchedule{Cron: "0 3 * * *", Timezone: "UTC", RetainDays: 7}}, nil
}
func (o *Orch) BackupNow(context.Context, string, string, string) (api.Backup, error) {
	return api.Backup{}, ErrDev
}
func (o *Orch) SetBackupSchedule(_ context.Context, _, _ string, s api.BackupSchedule) (api.BackupSchedule, error) {
	return s, nil
}
func (o *Orch) LockBackup(context.Context, string, bool) (api.Backup, error) {
	return api.Backup{}, ErrDev
}
func (o *Orch) DeleteBackup(context.Context, string) error { return ErrDev }
func (o *Orch) Restore(context.Context, string, string, api.RestoreRequest, string) (api.RestoreResponse, error) {
	return api.RestoreResponse{}, ErrDev
}
func (o *Orch) Promote(context.Context, string, string, api.PromoteRequest, string) error {
	return ErrDev
}
func (o *Orch) MajorUpgrade(context.Context, string, string, api.MajorUpgradeRequest, string) (api.RestoreResponse, error) {
	return api.RestoreResponse{}, ErrDev
}
func (o *Orch) RegenerateCredentials(context.Context, string, string, string) (api.CommitResponse, error) {
	return api.CommitResponse{}, ErrDev
}
func (o *Orch) TestBucket(context.Context, api.BackupSettings) api.TestResult {
	return api.TestResult{Error: "The development server does not reach buckets."}
}
func (o *Orch) BackupSettingsChanged(context.Context) error { return nil }
func (o *Orch) DataSummary(context.Context, string, string) (api.DataResponse, error) {
	return api.DataResponse{Tables: []api.DataTable{}}, nil
}
func (o *Orch) Extensions(context.Context, string, string) ([]api.Extension, error) {
	return []api.Extension{}, nil
}
func (o *Orch) EnableExtension(context.Context, string, string, string, string) (api.Extension, error) {
	return api.Extension{}, ErrDev
}
func (o *Orch) RedisKeys(context.Context, string, string, string, string) (api.RedisKeysPage, error) {
	return api.RedisKeysPage{Keys: []api.RedisKey{}}, nil
}
func (o *Orch) RevealRedisKey(context.Context, string, string, string) (api.RedisRevealResponse, error) {
	return api.RedisRevealResponse{}, ErrDev
}
func (o *Orch) Upgrade(context.Context, api.UpgradeRequest, string) (api.UpgradeStatus, error) {
	return api.UpgradeStatus{}, ErrDev
}
func (o *Orch) UpgradeStatus(context.Context) (api.UpgradeStatus, error) {
	return api.UpgradeStatus{State: "idle"}, nil
}

func emptyMetrics() api.MetricsResponse {
	return api.MetricsResponse{Step: 60000, Series: map[string][][2]float64{}, Deployments: []api.DeployMarker{}}
}
