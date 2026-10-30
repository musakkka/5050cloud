// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/logs"
	"github.com/musakkka/5050cloud/internal/cp/orch/metrics"
	"github.com/musakkka/5050cloud/internal/cp/orch/routes"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Container labels the agent sets on every replica (7.2).
const (
	labelReplica     = "5050.replica"
	labelDeployment  = "5050.deployment"
	labelService     = "5050.service"
	labelEnvironment = "5050.environment"
	labelIP          = "5050.ip"
)

// HostConnected imports what the agent reports in its hello: the state of
// every replica it runs, confirmations of removed replicas, and in adopt
// mode the containers this state does not know (9.17.1).
func (o *Orchestrator) HostConnected(ctx context.Context, hostID string, hello proto.Hello) {
	running := map[string]proto.ReplicaStatus{}
	for _, st := range hello.Replicas {
		if st.ReplicaID != "" {
			running[st.ReplicaID] = st
		}
	}
	rows, err := o.queryReps(ctx, `host_id = ? AND removed_at = 0`, hostID)
	if err != nil {
		o.log.Error("hello import", "host", hostID, "err", err)
		return
	}
	known := map[string]bool{}
	for _, r := range rows {
		known[r.ID] = true
		st, ok := running[r.ID]
		switch {
		case ok:
			o.applyStatus(ctx, hostID, st)
		case r.Desired == desiredRemoved:
			// The container is gone: the removal is confirmed and its address can be reused.
			o.confirmRemoved(ctx, r)
		case r.State != "pending":
			o.setRepState(ctx, r.ID, "pending")
		}
	}
	if o.hub.Adopting() {
		var unknown []proto.ReplicaStatus
		for id, st := range running {
			if !known[id] {
				unknown = append(unknown, st)
			}
		}
		o.recordUnknown(ctx, hostID, unknown)
	}
	o.invalidateNet()
	o.notify()
	o.publishHost(ctx, hostID)
}

// HostDisconnected is called when a host's socket closes. Routes stay for
// 60 s; the liveness loop withdraws and reschedules after that (7.1).
func (o *Orchestrator) HostDisconnected(hostID string) {
	o.notify()
	o.publishHost(o.bg(), hostID)
}

// HostEvent turns hub events into activity and alerts.
func (o *Orchestrator) HostEvent(ctx context.Context, kind, hostID, text string) {
	switch kind {
	case hub.EventDuplicate:
		o.notice(ctx, cpapi.Notice{Event: api.AlertHostDuplicate, Ref: hostID, Text: text})
	case hub.EventVersionSkew:
		o.notice(ctx, cpapi.Notice{Event: api.AlertVersionSkew, Ref: hostID, Text: text})
	case hub.EventJoined, hub.EventUnknownHost:
		o.publishHost(ctx, hostID)
	}
	o.log.Info("host event", "kind", kind, "host", hostID, "text", text)
}

// HostMessage handles every agent message that is not a request reply.
func (o *Orchestrator) HostMessage(ctx context.Context, hostID string, env proto.Envelope) {
	switch env.T {
	case proto.TReplicaStatus:
		var st proto.ReplicaStatus
		if json.Unmarshal(env.D, &st) == nil {
			o.applyStatus(ctx, hostID, st)
		}
	case proto.TStateApplied:
		var sa proto.StateApplied
		if json.Unmarshal(env.D, &sa) != nil {
			return
		}
		for _, e := range sa.Errors {
			o.log.Warn("host could not apply part of its state", "host", hostID, "generation", sa.Generation, "error", e)
		}
		if o.hub.Adopting() && len(sa.Unknown) > 0 {
			o.recordUnknown(ctx, hostID, sa.Unknown)
		}
		o.notify()
	case proto.TMetrics:
		var m proto.Metrics
		if json.Unmarshal(env.D, &m) == nil {
			o.ingestMetrics(ctx, hostID, m)
		}
	case proto.TEvent:
		var e proto.Event
		if json.Unmarshal(env.D, &e) == nil {
			o.hostEvent(ctx, hostID, e)
		}
	case proto.TBuildLog:
		var bl proto.BuildLog
		if json.Unmarshal(env.D, &bl) == nil && len(bl.Lines) > 0 {
			_ = o.builds.Append(bl.BuildID, o.now().UnixMilli(), o.maskBuild(ctx, bl.BuildID, bl.Lines))
		}
	case proto.TBuildResult:
		var res proto.BuildResult
		if json.Unmarshal(env.D, &res) == nil && !o.deliverBuild(res) {
			o.log.Info("build result nobody waits for", "build", res.BuildID, "host", hostID)
		}
	case proto.TJobLog:
		var jl proto.JobLog
		if json.Unmarshal(env.D, &jl) == nil {
			o.jobs.log(jl)
		}
	case proto.TJobResult:
		var jr proto.JobResult
		if json.Unmarshal(env.D, &jr) == nil {
			o.jobs.result(jr)
		}
	case proto.TCertIssued:
		var ci proto.CertIssued
		if json.Unmarshal(env.D, &ci) == nil {
			if err := o.storeCert(ctx, ci.CertBundle); err != nil {
				o.log.Error("store certificate", "domain", ci.Domain, "err", err)
			}
		}
	case proto.TCertStatus:
		var cs proto.CertStatus
		if json.Unmarshal(env.D, &cs) == nil {
			o.storeCertStatus(ctx, cs)
		}
	case proto.THostLogsTail:
		var t proto.HostLogsTail
		if json.Unmarshal(env.D, &t) == nil && len(t.Lines) > 0 {
			if err := o.st.Write(ctx, func(tx *sql.Tx) error { return logs.StoreTail(ctx, tx, hostID, t.Lines, o.now()) }); err != nil {
				o.log.Warn("store host log tail", "host", hostID, "err", err)
			}
		}
	}
}

// maskBuild hides secret values in build log lines (the builder masks too).
func (o *Orchestrator) maskBuild(ctx context.Context, depID string, lines []string) []string {
	d, err := o.loadDep(ctx, o.db, depID)
	if err != nil {
		return lines
	}
	secrets, err := o.cfg.SecretValues(ctx, d.EnvID, d.ServiceID)
	if err != nil || len(secrets) == 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		for _, s := range secrets {
			if len(s) >= 4 {
				l = strings.ReplaceAll(l, s, "*****")
			}
		}
		out[i] = l
	}
	return out
}

// applyStatus records a replica report and reacts to it: routes follow
// health, crashes end deployments, removals release addresses.
func (o *Orchestrator) applyStatus(ctx context.Context, hostID string, st proto.ReplicaStatus) {
	rs, err := o.queryReps(ctx, `id = ?`, st.ReplicaID)
	if err != nil || len(rs) == 0 {
		return
	}
	r := rs[0]
	if r.HostID != hostID {
		return // a stale report from a host the replica moved away from
	}
	prevState := r.State
	if st.State == "removed" {
		o.confirmRemoved(ctx, r)
		return
	}
	lp, _ := json.Marshal(nonNilInts(st.ListeningPorts))
	lo, _ := json.Marshal(nonNilInts(st.LoopbackOnly))
	var exit any
	if st.ExitCode != nil {
		exit = *st.ExitCode
	}
	now := o.now().UnixMilli()
	err = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET state = ?, container_id = CASE WHEN ? = '' THEN container_id ELSE ? END,
			restarts = ?, exit_code = ?, oom = ?, diagnosis = ?, listening_ports = ?, loopback_only = ?,
			started_at = CASE WHEN ? > 0 THEN ? ELSE started_at END, updated_at = ? WHERE id = ?`,
			st.State, st.ContainerID, st.ContainerID, st.Restarts, exit, b2i(st.OOMKilled), st.Diagnosis, string(lp), string(lo),
			st.StartedAt, st.StartedAt, now, r.ID)
		return err
	})
	if err != nil {
		o.log.Warn("record replica status", "replica", r.ID, "err", err)
		return
	}
	if prevState != st.State {
		o.invalidateNet()
		o.hub.MarkDirty() // upstream answers and DNS follow health; no Caddy reload
		o.afterHealthChange(ctx, r, st)
	}
	o.notify()
}

// afterHealthChange handles crashes of active deployments and the panic alert.
func (o *Orchestrator) afterHealthChange(ctx context.Context, r repRow, st proto.ReplicaStatus) {
	d, err := o.loadDep(ctx, o.db, r.DeploymentID)
	if err != nil || !d.Active {
		return
	}
	switch st.State {
	case "crashed":
		if d.Status == api.DepSuccess && r.Desired == desiredRunning {
			r.State, r.OOM, r.Diagnosis, r.ExitCode = st.State, st.OOMKilled, st.Diagnosis, st.ExitCode
			diag := o.replicaDiagnosis(ctx, d, r)
			d.Status = api.DepCrashed
			d.Diagnosis = diag
			if err := o.saveDep(ctx, &d); err != nil {
				o.log.Warn("save crashed deployment", "err", err)
			}
			o.activity(ctx, d.EnvID, d.ServiceID, "deployment", "Deployment "+shortID(d.ID)+" crashed: "+diag, "", d.ID, api.LabelCrashed)
			o.notice(ctx, cpapi.Notice{Event: api.EventDeploymentCrashed, ProjectID: d.ProjectID, EnvID: d.EnvID, ServiceID: d.ServiceID,
				Ref: d.ID, Text: diag, Details: map[string]any{"deploymentId": d.ID}})
			if err := o.cfg.FireWebhook(ctx, d.ProjectID, api.HookDeploymentCrashed, o.apiDeployment(d)); err != nil {
				o.log.Warn("webhook", "err", err)
			}
		}
	case "unhealthy", "healthy":
		out, _, _, err := o.netState(ctx)
		if err != nil {
			return
		}
		k := routes.Key(d.EnvID, d.ServiceID)
		o.mu.Lock()
		was := o.alerted[k]
		now := out.Panic[k]
		o.alerted[k] = now
		o.mu.Unlock()
		if now && !was {
			text := "All replicas failing health checks; serving anyway"
			o.activity(ctx, d.EnvID, d.ServiceID, "alert", text, "", d.ID, "")
			if err := o.cfg.FireWebhook(ctx, d.ProjectID, api.HookServiceUnhealthy, map[string]any{
				"serviceId": d.ServiceID, "environmentId": d.EnvID, "deploymentId": d.ID, "text": text + "."}); err != nil {
				o.log.Warn("webhook", "err", err)
			}
		}
		if st.State == "unhealthy" && !now {
			o.activity(ctx, d.EnvID, d.ServiceID, "alert", "Replica "+shortID(r.ID)+" failed its health checks and left the routes", "", d.ID, "")
		}
	}
}

func nonNilInts(x []int) []int {
	if x == nil {
		return []int{}
	}
	return x
}

func (o *Orchestrator) setRepState(ctx context.Context, id, state string) {
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET state = ?, updated_at = ? WHERE id = ?`, state, o.now().UnixMilli(), id)
		return err
	})
}

// confirmRemoved records that a replica's container is gone and frees its
// address (9.2: never earlier).
func (o *Orchestrator) confirmRemoved(ctx context.Context, r repRow) {
	if r.Desired != desiredRemoved {
		// The agent removed something we still want (a crash loop cleanup):
		// report it as pending so the next state recreates it.
		o.setRepState(ctx, r.ID, "pending")
		return
	}
	now := o.now().UnixMilli()
	err := o.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE o_replicas SET state = 'removed', removed_at = ?, updated_at = ? WHERE id = ?`, now, now, r.ID); err != nil {
			return err
		}
		return ipam.Release(ctx, tx, r.ID)
	})
	if err != nil {
		o.log.Warn("confirm removal", "replica", r.ID, "err", err)
		return
	}
	o.met.Forget(r.ID)
	o.invalidateNet()
	o.hub.MarkDirty(r.HostID)
	o.notify()
}

// recordUnknown stores containers the restored state does not know (adopt mode).
func (o *Orchestrator) recordUnknown(ctx context.Context, hostID string, list []proto.ReplicaStatus) {
	if len(list) == 0 {
		return
	}
	now := o.now().UnixMilli()
	err := o.st.Write(ctx, func(tx *sql.Tx) error {
		for _, st := range list {
			var n int
			if st.ReplicaID != "" {
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM o_replicas WHERE id = ?`, st.ReplicaID).Scan(&n); err != nil {
					return err
				}
			}
			if n > 0 || st.ContainerID == "" {
				continue
			}
			labels := st.Labels
			if labels == nil {
				labels = map[string]string{}
			}
			if st.ReplicaID != "" && labels[labelReplica] == "" {
				labels[labelReplica] = st.ReplicaID
			}
			if st.DeploymentID != "" && labels[labelDeployment] == "" {
				labels[labelDeployment] = st.DeploymentID
			}
			lb, _ := json.Marshal(labels)
			if _, err := tx.ExecContext(ctx, `INSERT INTO o_unknown_replicas (id, host_id, container_id, replica_id, labels, image, reported_at)
				VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(host_id, container_id) DO UPDATE SET labels = excluded.labels,
				image = excluded.image, reported_at = excluded.reported_at`,
				hub.NewID("unk"), hostID, st.ContainerID, st.ReplicaID, string(lb), st.Image, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		o.log.Warn("record unknown replicas", "host", hostID, "err", err)
	}
}

// ingestMetrics stores a metrics message and passes database samples on.
func (o *Orchestrator) ingestMetrics(ctx context.Context, hostID string, m proto.Metrics) {
	info := map[string]metrics.ReplicaInfo{}
	ids := make([]string, 0, len(m.Containers)+len(m.DB))
	for _, c := range m.Containers {
		ids = append(ids, c.ReplicaID)
	}
	for _, d := range m.DB {
		ids = append(ids, d.ReplicaID)
	}
	if len(ids) > 0 {
		reps, err := o.queryReps(ctx, `host_id = ?`, hostID)
		if err == nil {
			for _, r := range reps {
				info[r.ID] = metrics.ReplicaInfo{EnvID: r.EnvID, ServiceID: r.ServiceID}
			}
		}
	}
	httpEnv := o.httpEnvResolver(ctx)
	if err := o.met.Ingest(ctx, hostID, m, func(id string) (metrics.ReplicaInfo, bool) {
		i, ok := info[id]
		return i, ok
	}, httpEnv); err != nil && ctx.Err() == nil {
		o.log.Warn("store metrics", "host", hostID, "err", err)
	}
	for _, d := range m.DB {
		if i, ok := info[d.ReplicaID]; ok {
			o.data.DBSample(ctx, i.EnvID, i.ServiceID, d)
		}
	}
}

// httpEnvResolver maps an HTTP sample's service id to its environment: the
// environment whose instance of the service has domains, production first.
// proto.HTTPSample carries no environment id (e2e/requests/WP2a-httpsample-environment.md).
func (o *Orchestrator) httpEnvResolver(ctx context.Context) func(string) string {
	_, snaps, _, err := o.netState(ctx)
	if err != nil {
		return func(string) string { return "" }
	}
	best := map[string]cpapi.ServiceSnapshot{}
	for _, s := range snaps {
		if len(s.Domains) == 0 {
			continue
		}
		cur, ok := best[s.ServiceID]
		if !ok || (s.EnvName == "production" && cur.EnvName != "production") {
			best[s.ServiceID] = s
		}
	}
	return func(svc string) string { return best[svc].EnvID }
}

// hostEvent handles folded container and host events.
func (o *Orchestrator) hostEvent(ctx context.Context, hostID string, e proto.Event) {
	host, _ := hub.LoadHost(ctx, o.db, hostID)
	switch e.Kind {
	case "oom":
		rs, err := o.queryReps(ctx, `id = ?`, e.ReplicaID)
		if err != nil || len(rs) == 0 {
			return
		}
		r := rs[0]
		d, err := o.loadDep(ctx, o.db, r.DeploymentID)
		if err != nil {
			return
		}
		text := firstNonEmpty(e.Text, "A replica ran out of memory and was restarted.")
		o.activity(ctx, d.EnvID, d.ServiceID, "alert", strings.TrimSuffix(text, "."), "", d.ID, "")
		if err := o.cfg.FireWebhook(ctx, d.ProjectID, api.HookReplicaOOM, map[string]any{"replicaId": r.ID, "deploymentId": d.ID,
			"serviceId": d.ServiceID, "environmentId": d.EnvID, "text": text}); err != nil {
			o.log.Warn("webhook", "err", err)
		}
	case "disk.high":
		o.notice(ctx, cpapi.Notice{Event: api.AlertDiskHigh, Ref: hostID, Text: firstNonEmpty(e.Text, fmt.Sprintf("The disk of host %s is filling up.", host.Name))})
	case "disk.critical":
		o.notice(ctx, cpapi.Notice{Event: api.AlertDiskCritical, Ref: hostID, Text: firstNonEmpty(e.Text, fmt.Sprintf("The disk of host %s is almost full.", host.Name))})
	case "mesh.unreachable":
		o.log.Warn("mesh peer unreachable", "host", hostID, "text", e.Text)
		o.publishHost(ctx, hostID)
	case "host.warning":
		o.log.Warn("host warning", "host", hostID, "text", e.Text)
		o.publishHost(ctx, hostID)
	case "upgrade.failed":
		o.agentUpgradeFailed(ctx, hostID, e.Text)
	}
}

// publishHost sends a host event so the Hosts page updates live.
func (o *Orchestrator) publishHost(ctx context.Context, hostID string) {
	h, err := o.apiHost(ctx, hostID)
	if err != nil {
		return
	}
	o.cfg.Publish(api.Event{Type: "host", Host: &h})
}
