// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/logs"
	"github.com/musakkka/5050cloud/internal/cp/orch/monitors"
	"github.com/musakkka/5050cloud/internal/cp/orch/routes"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/internal/cp/orch/usage"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// ---------------------------------------------------------------- logs

// logSource gives the log fan-out its hosts and agents.
type logSource struct{ o *Orchestrator }

var _ logs.Source = logSource{}

func (s logSource) Services(ctx context.Context, envID string) ([]string, error) {
	snaps, err := s.o.cfg.ServiceInstances(ctx, envID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(snaps))
	for _, x := range snaps {
		out = append(out, x.ServiceID)
	}
	return out, nil
}

// Hosts returns the hosts that ran the services in the window, plus online
// ingress hosts (they hold the HTTP logs).
func (s logSource) Hosts(ctx context.Context, envID string, serviceIDs []string, deploymentID string, since, until int64) ([]logs.HostRef, error) {
	q := `SELECT DISTINCT host_id FROM o_replicas WHERE environment_id = ?`
	args := []any{envID}
	if deploymentID != "" {
		q += ` AND deployment_id = ?`
		args = append(args, deploymentID)
	} else if len(serviceIDs) > 0 {
		q += ` AND service_id IN (` + placeholders(len(serviceIDs)) + `)`
		for _, id := range serviceIDs {
			args = append(args, id)
		}
	}
	if since > 0 {
		q += ` AND (removed_at = 0 OR removed_at >= ?)`
		args = append(args, since)
	}
	if until > 0 {
		q += ` AND created_at <= ?`
		args = append(args, until)
	}
	rows, err := s.o.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			closeRows(rows)
			return nil, err
		}
		ids[id] = true
	}
	closeRows(rows)
	hosts, err := hub.LoadHosts(ctx, s.o.db)
	if err != nil {
		return nil, err
	}
	var out []logs.HostRef
	for _, h := range hosts {
		if h.State == sched.StateLost || h.State == sched.StateUnknown {
			continue
		}
		if ids[h.ID] || (h.HasRole("ingress") && s.o.hub.Connected(h.ID)) {
			out = append(out, logs.HostRef{ID: h.ID, Name: h.Name})
		}
	}
	return out, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (s logSource) Builds(ctx context.Context, envID string, serviceIDs []string, deploymentID string, since, until int64) ([]string, error) {
	q := `environment_id = ?`
	args := []any{envID}
	if deploymentID != "" {
		q += ` AND id = ?`
		args = append(args, deploymentID)
	} else if len(serviceIDs) > 0 {
		q += ` AND service_id IN (` + placeholders(len(serviceIDs)) + `)`
		for _, id := range serviceIDs {
			args = append(args, id)
		}
	}
	if until > 0 {
		q += ` AND created_at <= ?`
		args = append(args, until)
	}
	if since > 0 {
		q += ` AND (finished_at = 0 OR finished_at >= ?)`
		args = append(args, since)
	}
	ds, err := s.o.queryDeps(ctx, q+` ORDER BY created_at DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out, nil
}

func (s logSource) Online(hostID string) bool { return s.o.hub.Connected(hostID) }

func (s logSource) QueryHost(ctx context.Context, hostID string, q proto.LogsQuery) (proto.LogsResult, error) {
	env, err := s.o.hub.Call(ctx, hostID, proto.TLogsQuery, "", q, nil)
	if err != nil {
		return proto.LogsResult{}, err
	}
	if env.T == proto.TAck {
		var ack proto.Ack
		_ = json.Unmarshal(env.D, &ack)
		return proto.LogsResult{}, errorf("%s", firstNonEmpty(ack.Error, "The host refused the log query."))
	}
	var res proto.LogsResult
	if err := json.Unmarshal(env.D, &res); err != nil {
		return proto.LogsResult{}, err
	}
	return res, nil
}

func (s logSource) Subscribe(ctx context.Context, hostID string, sub proto.LogsSubscribe, fn func(proto.LogLines)) error {
	return s.o.hub.Subscribe(ctx, hostID, sub, fn)
}

func (s logSource) Tail(ctx context.Context, hostID string, services map[string]bool, since, until int64) ([]api.LogLine, error) {
	return logs.ReadTail(ctx, s.o.db, hostID, services, since, until)
}

// Logs answers a log query across hosts.
func (o *Orchestrator) Logs(ctx context.Context, q cpapi.LogsQuery) (api.LogsResponse, error) {
	resp, err := o.fan.Query(ctx, q)
	if err != nil {
		return api.LogsResponse{}, errorf("The log filter is not valid: %s", sentenceOf(err))
	}
	return resp, nil
}

// TailLogs streams live lines until ctx is done.
func (o *Orchestrator) TailLogs(ctx context.Context, q cpapi.LogsQuery, emit func([]api.LogLine) error) error {
	return o.fan.Tail(ctx, q, emit)
}

// ---------------------------------------------------------------- metrics

// Metrics answers the service Metrics tab with deployment markers.
func (o *Orchestrator) Metrics(ctx context.Context, envID, serviceID string, fromMs, untilMs int64, perReplica bool) (api.MetricsResponse, error) {
	fromMs, untilMs = o.window(fromMs, untilMs)
	resp, err := o.met.ServiceMetrics(ctx, envID, serviceID, fromMs, untilMs, perReplica)
	if err != nil {
		return resp, err
	}
	ds, err := o.queryDeps(ctx, `environment_id = ? AND service_id = ? AND started_at > 0 AND created_at <= ? AND (finished_at = 0 OR finished_at >= ?)
		AND status IN ('SUCCESS', 'REMOVING', 'REMOVED', 'CRASHED', 'DEPLOYING') ORDER BY created_at`, envID, serviceID, untilMs, fromMs)
	if err == nil {
		for _, d := range ds {
			ts := d.FinishedAt
			if ts == 0 || d.Status == api.DepRemoved || d.Status == api.DepRemoving {
				ts = d.StartedAt
			}
			if ts < fromMs || ts > untilMs {
				continue
			}
			resp.Deployments = append(resp.Deployments, api.DeployMarker{TS: ts, DeploymentID: d.ID, Label: api.StatusLabel(d.Status, d.Active), Commit: d.Commit})
		}
	}
	if d, ok, _ := o.activeDep(ctx, envID, serviceID); ok {
		n := float64(sched.Replicas(d.Config))
		if d.Config.Deploy.MemoryLimitMB > 0 {
			resp.MemLimit = int64(d.Config.Deploy.MemoryLimitMB) << 20 * int64(n)
		}
		resp.CPULimit = d.Config.Deploy.CPULimit * n
	}
	return resp, nil
}

// DBMetrics answers the database panel's Metrics tab.
func (o *Orchestrator) DBMetrics(ctx context.Context, envID, serviceID string, fromMs, untilMs int64) (api.DBMetricsResponse, error) {
	fromMs, untilMs = o.window(fromMs, untilMs)
	return o.met.DBMetrics(ctx, envID, serviceID, fromMs, untilMs)
}

// ---------------------------------------------------------------- monitors

// monitorSource converts metric points into each monitor's unit.
type monitorSource struct{ o *Orchestrator }

var _ monitors.Source = monitorSource{}

func (s monitorSource) Values(ctx context.Context, m api.Monitor, from, until int64) ([]monitors.Point, error) {
	pts, err := s.o.met.Raw(ctx, m.EnvironmentID, m.ServiceID, m.Metric, from, until)
	if err != nil {
		return nil, err
	}
	out := make([]monitors.Point, 0, len(pts))
	switch m.Metric {
	case "cpu":
		limit := s.o.cpuCapacity(ctx, m.EnvironmentID, m.ServiceID)
		if limit <= 0 {
			return nil, nil
		}
		for _, p := range pts {
			out = append(out, monitors.Point{TS: p.TS, Value: p.Value / limit * 100})
		}
	case "memory":
		for _, p := range pts {
			if p.Limit <= 0 {
				continue
			}
			out = append(out, monitors.Point{TS: p.TS, Value: p.Value / p.Limit * 100})
		}
	case "disk":
		gb := strings.EqualFold(m.Unit, "gb")
		total := s.o.volumeFSTotal(ctx, m.EnvironmentID, m.ServiceID)
		for _, p := range pts {
			if gb {
				out = append(out, monitors.Point{TS: p.TS, Value: p.Value / float64(1<<30)})
			} else if total > 0 {
				out = append(out, monitors.Point{TS: p.TS, Value: p.Value / total * 100})
			}
		}
	case "http_5xx_rate":
		for _, p := range pts {
			if p.Limit <= 0 {
				continue // no requests in that window
			}
			out = append(out, monitors.Point{TS: p.TS, Value: p.Value * 100})
		}
	}
	return out, nil
}

func (s monitorSource) ProjectID(ctx context.Context, envID string) string {
	e, _, err := s.o.cfg.Environment(ctx, envID)
	if err != nil {
		return ""
	}
	return e.ProjectID
}

// cpuCapacity is the vCPU a service may use: its limit times replicas, or
// the CPUs of the hosts it runs on.
func (o *Orchestrator) cpuCapacity(ctx context.Context, envID, svcID string) float64 {
	d, ok, err := o.activeDep(ctx, envID, svcID)
	if err != nil || !ok {
		return 0
	}
	reps, err := o.queryReps(ctx, `deployment_id = ? AND desired = 'running'`, d.ID)
	if err != nil || len(reps) == 0 {
		return 0
	}
	if d.Config.Deploy.CPULimit > 0 {
		return d.Config.Deploy.CPULimit * float64(len(reps))
	}
	total := 0.0
	for _, r := range reps {
		if h, err := hub.LoadHost(ctx, o.db, r.HostID); err == nil && h.Facts.CPUs > 0 {
			total += float64(h.Facts.CPUs)
		}
	}
	return total
}

// volumeFSTotal is the size of the filesystem the service's volume lives on.
func (o *Orchestrator) volumeFSTotal(ctx context.Context, envID, svcID string) float64 {
	snap, err := o.cfg.ServiceInstance(ctx, envID, svcID)
	if err != nil || snap.Volume == nil {
		return 0
	}
	hostID, _ := o.volumeHost(ctx, snap.Volume.ID)
	if hostID == "" {
		return 0
	}
	return float64(o.hostDataDisk(ctx, hostID).Total)
}

// hostDataDisk returns /data when present, else /.
func (o *Orchestrator) hostDataDisk(ctx context.Context, hostID string) proto.Disk {
	var disks []proto.Disk
	if hs, ok := o.met.HostSample(hostID); ok {
		disks = hs.Disks
	}
	if len(disks) == 0 {
		if h, err := hub.LoadHost(ctx, o.db, hostID); err == nil {
			disks = h.Facts.Disks
		}
	}
	var root proto.Disk
	for _, d := range disks {
		if d.Mount == "/data" {
			return d
		}
		if d.Mount == "/" {
			root = d
		}
	}
	if root.Total == 0 && len(disks) > 0 {
		return disks[0]
	}
	return root
}

// Monitors returns the live state of a service's monitors, by monitor id.
func (o *Orchestrator) Monitors(ctx context.Context, envID, serviceID string) (map[string]string, error) {
	out := o.mon.ServiceStates(envID, serviceID)
	ms, err := o.cfg.Monitors(ctx)
	if err != nil {
		return out, err
	}
	for _, m := range ms {
		if m.EnvironmentID == envID && m.ServiceID == serviceID {
			if _, ok := out[m.ID]; !ok || out[m.ID] == "" {
				st := m.State
				if st == "" {
					st = monitors.StateOK
				}
				out[m.ID] = st
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- usage

type usageSource struct{ o *Orchestrator }

func (s usageSource) Measure(ctx context.Context, svc usage.Service, from, until int64) (usage.Measured, error) {
	u, err := s.o.met.UsageFor(ctx, svc.EnvID, svc.ServiceID, from, until)
	if err != nil {
		return usage.Measured{}, err
	}
	m := usage.Measured{VCPUHours: u.VCPUHours, MemGBHours: u.MemGBHours, EgressGB: u.EgressGB, VolumeGBMonths: u.VolumeGBMonths}
	if b, err := s.o.data.Backups(ctx, svc.EnvID, svc.ServiceID); err == nil {
		for _, x := range b.Backups {
			if x.Status == "ok" {
				m.BackupBytes += x.SizeBytes
			}
		}
	}
	return m, nil
}

// Usage computes Project Settings > Usage for a window.
func (o *Orchestrator) Usage(ctx context.Context, projectID string, fromMs, untilMs int64) (api.UsageResponse, error) {
	now := o.now().UnixMilli()
	if untilMs <= 0 || untilMs > now {
		untilMs = now
	}
	if fromMs <= 0 || fromMs >= untilMs {
		fromMs = untilMs - 30*24*3600*1000
	}
	snaps, err := o.cfg.AllInstances(ctx)
	if err != nil {
		return api.UsageResponse{}, err
	}
	var svcs []usage.Service
	for _, s := range snaps {
		if s.ProjectID == projectID {
			svcs = append(svcs, usage.Service{EnvID: s.EnvID, ServiceID: s.ServiceID, Name: s.Config.Name})
		}
	}
	return usage.Compute(ctx, usageSource{o}, svcs, fromMs, untilMs, usage.Railway)
}

// ---------------------------------------------------------------- runtime (cards)

// Runtime returns the card and panel view of every service instance of an environment.
func (o *Orchestrator) Runtime(ctx context.Context, envID string) (map[string]cpapi.Runtime, error) {
	snaps, err := o.cfg.ServiceInstances(ctx, envID)
	if err != nil {
		return nil, err
	}
	out, _, _, err := o.netState(ctx)
	if err != nil {
		return nil, err
	}
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return nil, err
	}
	byID := map[string]hub.HostRow{}
	names := map[string]string{}
	for _, h := range hosts {
		byID[h.ID] = h
		names[h.ID] = h.Name
	}
	res := map[string]cpapi.Runtime{}
	for _, s := range snaps {
		res[s.ServiceID] = o.runtimeOf(ctx, s, out, byID, names)
	}
	return res, nil
}

func (o *Orchestrator) runtimeOf(ctx context.Context, s cpapi.ServiceSnapshot, out routes.Output, hosts map[string]hub.HostRow, names map[string]string) cpapi.Runtime {
	rt := cpapi.Runtime{Status: api.StatusNoDeploy, Replicas: []api.Replica{}, ListeningPorts: []int{}, OutboundIPs: []string{}, Processes: 1}
	latest, _ := o.queryDeps(ctx, `environment_id = ? AND service_id = ? ORDER BY created_at DESC LIMIT 1`, s.EnvID, s.ServiceID)
	active, hasActive, _ := o.activeDep(ctx, s.EnvID, s.ServiceID)
	if hasActive {
		rt.ActiveDeployment = active.ID
	}
	kind := sched.Kind(s.Config, len(s.Domains) > 0)
	single := sched.Singleton(s.Config, kind)
	reps, _ := o.queryReps(ctx, `environment_id = ? AND service_id = ? AND desired IN ('running', 'stopped') ORDER BY created_at, id`, s.EnvID, s.ServiceID)
	ports := map[int]bool{}
	ips := map[string]bool{}
	for _, r := range reps {
		rt.Replicas = append(rt.Replicas, o.apiReplica(r, names, single))
		if hasActive && r.DeploymentID == active.ID {
			for _, p := range r.Listening {
				ports[p] = true
			}
		}
		if h, ok := hosts[r.HostID]; ok {
			ip := firstNonEmpty(h.Facts.EgressIP, h.PublicIP)
			if ip != "" {
				ips[ip] = true
			}
		}
	}
	for p := range ports {
		rt.ListeningPorts = append(rt.ListeningPorts, p)
	}
	sort.Ints(rt.ListeningPorts)
	for ip := range ips {
		rt.OutboundIPs = append(rt.OutboundIPs, ip)
	}
	sort.Strings(rt.OutboundIPs)
	cfg := s.Config
	if hasActive {
		cfg = active.Config
	}
	cpus := 0
	for _, r := range reps {
		if h, ok := hosts[r.HostID]; ok {
			cpus = h.Facts.CPUs
			break
		}
	}
	if kind == api.KindWeb || cfg.Deploy.Processes != "" && !strings.EqualFold(cfg.Deploy.Processes, "auto") {
		rt.Processes = sched.Processes(cfg, cpus)
	}
	wrap := hasActive && sched.ClusterWrap(active.IsNode, startArgv(firstNonEmpty(cfg.Deploy.StartCommand, active.StartCmd)))
	rt.ProcessesText = sched.ProcessesText(rt.Processes, wrap, targetPort(cfg, nil))
	rt.MonitorFiring = o.mon.Firing(s.EnvID, s.ServiceID)
	if s.Volume != nil {
		if vh, _ := o.volumeHost(ctx, s.Volume.ID); vh != "" {
			if h, ok := hosts[vh]; ok && h.State == sched.StateLost {
				rt.VolumeHostLost = true
			}
			if pts, err := o.met.Raw(ctx, s.EnvID, s.ServiceID, "disk", o.now().Add(-10*60e9).UnixMilli(), o.now().UnixMilli()+1); err == nil && len(pts) > 0 {
				rt.VolumeUsedBytes = int64(pts[len(pts)-1].Value)
			}
		}
	}

	key := routes.Key(s.EnvID, s.ServiceID)
	switch {
	case rt.VolumeHostLost:
		rt.Status, rt.StatusText = api.StatusHostLost, "This volume was on a host that is gone. Restore it from a backup."
	case len(latest) > 0 && latest[0].Status == api.DepQueued:
		rt.Status, rt.StatusText = api.StatusPending, firstNonEmpty(stepDetail(latest[0], stepFetch), "Initializing")
	case len(latest) > 0 && latest[0].Status == api.DepBuilding:
		rt.Status, rt.StatusText = api.StatusBuilding, firstNonEmpty(currentStep(latest[0]), "Building")
	case len(latest) > 0 && latest[0].Status == api.DepDeploying:
		rt.Status, rt.StatusText = api.StatusDeploying, firstNonEmpty(stepDetail(latest[0], stepStart), currentStep(latest[0]), "Deploying")
		if strings.HasPrefix(rt.StatusText, "Pending") {
			rt.Status = api.StatusPending
		}
	case hasActive && active.Status == api.DepCrashed:
		rt.Status, rt.StatusText = api.StatusCrashed, active.Diagnosis
	case hasActive && kind == api.KindCron:
		rt.Status, rt.StatusText = api.StatusOnline, "Scheduled"
	case hasActive && out.Panic[key]:
		rt.Status, rt.StatusText = api.StatusUnhealthy, "All replicas failing health checks; serving anyway"
	case hasActive:
		rt.Status, rt.StatusText = api.StatusOnline, "Deployed via "+triggerName(active.Trigger)
		if offline := offlineSingletons(rt.Replicas); offline > 0 {
			rt.StatusText = "Host offline: singleton not moved. Move now"
		}
	case len(latest) > 0 && latest[0].Status == api.DepFailed:
		rt.Status, rt.StatusText = api.StatusFailed, latest[0].Diagnosis
	case len(latest) > 0 && latest[0].Status == api.DepCompleted:
		rt.Status, rt.StatusText = api.StatusCompleted, "Completed"
	case len(latest) > 0 && (latest[0].Status == api.DepRemoved || latest[0].Status == api.DepRemoving):
		rt.Status, rt.StatusText = api.StatusRemoved, "No deployment"
	default:
		rt.Status, rt.StatusText = api.StatusNoDeploy, "No deployment"
	}
	return rt
}

func offlineSingletons(rs []api.Replica) int {
	n := 0
	for _, r := range rs {
		if r.Singleton && strings.HasPrefix(r.Note, "Host offline") {
			n++
		}
	}
	return n
}

func stepDetail(d depRow, name string) string {
	for _, s := range d.Steps {
		if s.Name == name && s.State == "running" {
			return s.Detail
		}
	}
	return ""
}

func currentStep(d depRow) string {
	for i := len(d.Steps) - 1; i >= 0; i-- {
		if d.Steps[i].State == "running" {
			return stepTitle(d.Steps[i].Name)
		}
	}
	return ""
}

func stepTitle(name string) string {
	switch name {
	case stepPreDeploy:
		return "Running the pre-deploy command"
	case stepHealthcheck:
		return "Waiting for the health check"
	case stepPull:
		return "Pulling the image"
	}
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "_", " ")
}

func triggerName(t string) string {
	switch t {
	case "github":
		return "GitHub"
	case "cli":
		return "CLI"
	case "ui", "":
		return "the console"
	case "rollback":
		return "a rollback"
	case "redeploy":
		return "a redeploy"
	case "commit":
		return "a commit"
	case "template":
		return "a template"
	case "restore":
		return "a restore"
	}
	return t
}
