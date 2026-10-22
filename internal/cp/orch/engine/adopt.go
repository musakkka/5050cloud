// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Adopt switches adopt mode on after a state restore (9.17.1): a new epoch,
// and every desired state carries Adopt so agents remove nothing.
func (o *Orchestrator) Adopt(ctx context.Context, snapshotAt int64) error {
	if err := o.hub.StartAdopt(ctx, snapshotAt); err != nil {
		return err
	}
	o.activity(ctx, "", "", "host", "Restored from a backup; containers the backup does not know keep running until reviewed", "", "", "")
	o.publishInstall(ctx)
	return nil
}

// AdoptStatus returns the restore banner's numbers, or nil outside adopt mode.
func (o *Orchestrator) AdoptStatus(ctx context.Context) (*api.RestoreStatus, error) {
	if !o.hub.Adopting() {
		return nil, nil
	}
	st, err := o.hub.AdoptInfo(ctx)
	if err != nil {
		return nil, err
	}
	var n, hosts int
	if err := o.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM o_unknown_replicas`).Scan(&n); err != nil {
		return nil, err
	}
	if err := o.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM o_hosts WHERE state = 'unknown'`).Scan(&hosts); err != nil {
		return nil, err
	}
	return &api.RestoreStatus{SnapshotAt: st.SnapshotAt, UnknownReplicas: n, UnknownHosts: hosts}, nil
}

type unknownRow struct {
	ID, HostID, ContainerID, ReplicaID, Image string
	Labels                                    map[string]string
	ReportedAt                                int64
}

func (o *Orchestrator) unknownRows(ctx context.Context, where string, args ...any) ([]unknownRow, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT id, host_id, container_id, replica_id, labels, image, reported_at FROM o_unknown_replicas WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []unknownRow
	for rows.Next() {
		var u unknownRow
		var labels string
		if err := rows.Scan(&u.ID, &u.HostID, &u.ContainerID, &u.ReplicaID, &labels, &u.Image, &u.ReportedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &u.Labels)
		if u.Labels == nil {
			u.Labels = map[string]string{}
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UnknownReplicas lists the containers the restored state does not know.
func (o *Orchestrator) UnknownReplicas(ctx context.Context) ([]api.UnknownReplica, error) {
	us, err := o.unknownRows(ctx, `1 = 1 ORDER BY reported_at, id`)
	if err != nil {
		return nil, err
	}
	names := o.hostNames(ctx)
	out := []api.UnknownReplica{}
	for _, u := range us {
		ur := api.UnknownReplica{ID: u.ID, HostID: u.HostID, HostName: names[u.HostID], ContainerID: u.ContainerID, Image: u.Image,
			Labels: u.Labels, ReportedAt: u.ReportedAt}
		if env, svc := u.Labels[labelEnvironment], u.Labels[labelService]; env != "" && svc != "" {
			if s, err := o.cfg.ServiceInstance(ctx, env, svc); err == nil && !s.Deleted {
				ur.ServiceID, ur.ServiceName = svc, s.Config.Name
			}
		}
		out = append(out, ur)
	}
	return out, nil
}

// AdoptReplica creates the deployment and replica records of a running
// container from its labels, when its service exists in the restored state.
func (o *Orchestrator) AdoptReplica(ctx context.Context, unknownID, actor string) error {
	us, err := o.unknownRows(ctx, `id = ?`, unknownID)
	if err != nil {
		return err
	}
	if len(us) == 0 {
		return errorf("This container is no longer in the review list.")
	}
	u := us[0]
	envID, svcID, depID := u.Labels[labelEnvironment], u.Labels[labelService], u.Labels[labelDeployment]
	repID := firstNonEmpty(u.Labels[labelReplica], u.ReplicaID)
	if envID == "" || svcID == "" || repID == "" {
		return errorf("This container has no 5050cloud service labels, so it cannot be adopted. Remove it instead.")
	}
	snap, err := o.cfg.ServiceInstance(ctx, envID, svcID)
	if err != nil || snap.Deleted {
		return errorf("The service this container belonged to is not in the restored state, so it cannot be adopted. Recreate the service and adopt again, or remove the container.")
	}
	host, err := hub.LoadHost(ctx, o.db, u.HostID)
	if err != nil {
		return err
	}
	if depID == "" {
		depID = hub.NewID("dep")
	}
	d, err := o.loadDep(ctx, o.db, depID)
	newDep := errors.Is(err, errNoDeployment)
	if err != nil && !newDep {
		return err
	}
	inst, err := o.cfg.Install(ctx)
	if err != nil {
		return err
	}
	now := o.now().UnixMilli()
	if newDep {
		env, _, err := o.cfg.ResolveVariables(ctx, envID, svcID, providedVars(snap, depID, nil, inst))
		if err != nil {
			return errorf("The variables of %s could not be resolved, so the container cannot be adopted.", snap.Config.Name)
		}
		enc, err := o.sealEnv(depID, env)
		if err != nil {
			return err
		}
		d = depRow{ID: depID, EnvID: envID, ServiceID: svcID, ProjectID: snap.ProjectID, Status: api.DepSuccess, Trigger: "restore",
			Actor: actor, Message: "Adopted after a state restore", ConfigRev: snap.Rev, Config: snap.Config, Image: u.Image,
			EnvEnc: enc, CreatedAt: now, StartedAt: now, FinishedAt: now,
			Steps: []api.DeployStep{{Name: stepCutover, State: "ok", StartedAt: now, FinishedAt: now, Detail: "Adopted after a state restore"}}}
	}
	ip := u.Labels[labelIP]
	err = o.st.Write(ctx, func(tx *sql.Tx) error {
		if newDep {
			if err := insertDep(ctx, tx, d); err != nil {
				return err
			}
		}
		ok, err := ipam.Reserve(ctx, tx, o.plan, host.ID, host.MeshIndex, envID, repID, ip)
		if err != nil {
			return err
		}
		if !ok {
			// The address is taken or unknown: give it a new one; the agent recreates the container on it.
			ip, err = ipam.Allocate(ctx, tx, o.plan, host.ID, host.Name, host.MeshIndex, envID, repID)
			if err != nil {
				return err
			}
		}
		r := repRow{ID: repID, DeploymentID: d.ID, ServiceID: svcID, EnvID: envID, HostID: host.ID, IP: ip, State: "running",
			ContainerID: u.ContainerID, CreatedAt: now, Desired: desiredRunning, Serving: true, Adopted: true}
		if err := insertRep(ctx, tx, r); err != nil {
			return err
		}
		if snap.Volume != nil {
			if _, err := tx.ExecContext(ctx, `INSERT INTO o_volume_hosts (volume_id, service_id, environment_id, host_id, created_at)
				VALUES (?, ?, ?, ?, ?) ON CONFLICT(volume_id) DO NOTHING`, snap.Volume.ID, svcID, envID, host.ID, now); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM o_unknown_replicas WHERE id = ?`, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	if newDep {
		// The adopted deployment is what runs now: it becomes the active one
		// and the one the backup knew drains (removed once adopt mode ends).
		prev, hasPrev, _ := o.activeDep(ctx, envID, svcID)
		o.activate(ctx, &d)
		if hasPrev && prev.ID != d.ID {
			o.startDrain(prev, 0, 1)
		}
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.audit(ctx, actor, "restore.adopt", repID, u.ContainerID)
	return o.maybeEndAdopt(ctx, actor)
}

// RemoveUnknownReplica drops a container from the review list. Agents
// remove nothing in adopt mode, so the container goes when adopt mode ends.
func (o *Orchestrator) RemoveUnknownReplica(ctx context.Context, unknownID, actor string) error {
	us, err := o.unknownRows(ctx, `id = ?`, unknownID)
	if err != nil {
		return err
	}
	if len(us) == 0 {
		return errorf("This container is no longer in the review list.")
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM o_unknown_replicas WHERE id = ?`, unknownID)
		return err
	}); err != nil {
		return err
	}
	o.audit(ctx, actor, "restore.remove", us[0].ContainerID, us[0].HostID)
	return o.maybeEndAdopt(ctx, actor)
}

// maybeEndAdopt ends adopt mode once nothing is left to review.
func (o *Orchestrator) maybeEndAdopt(ctx context.Context, actor string) error {
	st, err := o.AdoptStatus(ctx)
	if err != nil || st == nil {
		return err
	}
	if st.UnknownReplicas == 0 && st.UnknownHosts == 0 {
		return o.FinishRestore(ctx, actor)
	}
	o.publishInstall(ctx)
	return nil
}

// ReadoptHost gives a host the restored state does not know a place again:
// a mesh index, its roles and a new token over its open socket.
func (o *Orchestrator) ReadoptHost(ctx context.Context, hostID, actor string) (api.Host, error) {
	h, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return api.Host{}, err
	}
	if h.State != sched.StateUnknown {
		return api.Host{}, errorf("Host %s is already part of this install.", h.Name)
	}
	roles := h.Roles
	if len(roles) == 0 {
		roles = []string{"app"}
	}
	if err := o.hub.Readopt(ctx, hostID, roles); err != nil {
		return api.Host{}, err
	}
	o.audit(ctx, actor, "host.readopt", hostID, "")
	o.activity(ctx, "", "", "host", "Host "+h.Name+" was re-adopted", actor, hostID, "")
	_ = o.maybeEndAdopt(ctx, actor)
	return o.apiHost(ctx, hostID)
}

// FinishRestore ends adopt mode: containers still in the review list are
// removed by their agents with the next desired state.
func (o *Orchestrator) FinishRestore(ctx context.Context, actor string) error {
	if !o.hub.Adopting() {
		return nil
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM o_unknown_replicas`)
		return err
	}); err != nil {
		return err
	}
	if err := o.hub.FinishAdopt(ctx); err != nil {
		return err
	}
	o.audit(ctx, actor, "restore.finish", "", "")
	o.activity(ctx, "", "", "host", "Restore finished; removals are allowed again", actor, "", "")
	o.invalidateNet()
	o.hub.MarkDirty()
	o.publishInstall(ctx)
	return nil
}

func (o *Orchestrator) publishInstall(ctx context.Context) {
	inst, err := o.cfg.Install(ctx)
	if err != nil {
		return
	}
	inst.Restore, _ = o.AdoptStatus(ctx)
	o.cfg.Publish(api.Event{Type: "install", Install: &inst})
}
