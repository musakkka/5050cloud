// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/ipam"
	"github.com/musakkka/5050cloud/internal/cp/orch/routes"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Hosts lists every host for the Hosts page.
func (o *Orchestrator) Hosts(ctx context.Context) ([]api.Host, error) {
	rows, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return nil, err
	}
	out := make([]api.Host, 0, len(rows))
	for _, h := range rows {
		ah, err := o.hostView(ctx, h, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ah)
	}
	return out, nil
}

func (o *Orchestrator) apiHost(ctx context.Context, hostID string) (api.Host, error) {
	rows, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return api.Host{}, err
	}
	for _, h := range rows {
		if h.ID == hostID {
			return o.hostView(ctx, h, rows)
		}
	}
	return api.Host{}, hub.ErrNoHost
}

// hostState is the state the Hosts page shows.
func (o *Orchestrator) hostState(h hub.HostRow) string {
	switch h.State {
	case sched.StateMaintenance, sched.StateLost, sched.StateUnknown:
		return h.State
	}
	if !o.hub.Connected(h.ID) {
		if h.State == sched.StateJoining {
			return sched.StateJoining
		}
		if o.routed(h) {
			return sched.StateOnline // within the 60 s grace
		}
		return sched.StateOffline
	}
	if h.Drain {
		return sched.StateDraining
	}
	return sched.StateOnline
}

func (o *Orchestrator) hostView(ctx context.Context, h hub.HostRow, all []hub.HostRow) (api.Host, error) {
	out := api.Host{ID: h.ID, Name: h.Name, Roles: h.Roles, State: o.hostState(h), Version: h.Version, Arch: h.Facts.Arch,
		OS: h.Facts.OS, CPUs: h.Facts.CPUs, MemBytes: h.Facts.MemBytes, ReserveMB: h.ReserveMB, PublicIP: h.PublicIP,
		EgressIP: h.Facts.EgressIP, LastSeen: h.LastSeen, RebootRequired: h.Facts.RebootRequired, Disks: []api.Disk{}}
	if v := o.hub.AgentVersion(h.ID); v != "" {
		out.Version = v
	}
	if ls := o.hub.LastSeen(h.ID); ls > 0 {
		out.LastSeen = ls
	}
	if out.ReserveMB == 0 {
		out.ReserveMB = sched.DefaultReserveMB(int(h.Facts.MemBytes >> 20))
	}
	if h.MeshIndex > 0 {
		out.MeshIP = o.plan.MeshAddr(h.MeshIndex).String()
	}
	for _, d := range h.Facts.Disks {
		out.Disks = append(out.Disks, api.Disk{Mount: d.Mount, Used: d.Used, Total: d.Total})
	}
	if hs, ok := o.met.HostSample(h.ID); ok {
		out.CPUUsed, out.MemUsed = hs.CPUUsed, hs.MemUsed
		if len(hs.Disks) > 0 {
			out.Disks = out.Disks[:0]
			for _, d := range hs.Disks {
				out.Disks = append(out.Disks, api.Disk{Mount: d.Mount, Used: d.Used, Total: d.Total})
			}
		}
	}
	reps, err := o.queryReps(ctx, `host_id = ? AND desired IN ('running', 'stopped')`, h.ID)
	if err != nil {
		return out, err
	}
	out.Replicas = len(reps)
	res := map[string]int{}
	for _, r := range reps {
		v, ok := res[r.DeploymentID]
		if !ok {
			v = 256
			if d, err := o.loadDep(ctx, o.db, r.DeploymentID); err == nil {
				v = sched.ReservationMB(d.Config, isDatabase(d.Config))
			}
			res[r.DeploymentID] = v
		}
		out.ReservedMB += v
	}
	cs, err := hub.Conflicts(ctx, o.db, h.ID)
	if err != nil {
		return out, err
	}
	for _, c := range cs {
		out.Conflicts = append(out.Conflicts, api.HostConflict{MachineID: c.MachineID, WGPublicKey: c.WGPublicKey, IP: c.IP, SeenAt: c.SeenAt})
	}
	if len(cs) > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("Duplicate host detected: a second machine is using the identity of host %s. Remove it or run `5050-agent rejoin` on it.", h.Name))
	}
	for peer := range o.unreachable(h.ID) {
		out.Unreachable = append(out.Unreachable, peer)
	}
	sort.Strings(out.Unreachable)
	names := map[string]string{}
	var ingress []routes.IngressHost
	for _, x := range all {
		names[x.ID] = x.Name
		if x.HasRole("ingress") && x.MeshIndex > 0 && x.State != sched.StateUnknown {
			ingress = append(ingress, routes.IngressHost{ID: x.ID, Online: o.hub.Connected(x.ID), Control: x.MeshIndex == 1, CreatedAt: x.CreatedAt})
		}
	}
	if p, _ := routes.ACMEPrimary(ingress); p == h.ID {
		out.ACMEPrimary = true
	}
	for _, peer := range out.Unreachable {
		out.Warnings = append(out.Warnings, fmt.Sprintf("Hosts %s and %s cannot reach each other over WireGuard (both are behind NAT, or UDP 51820 is blocked).", h.Name, firstNonEmpty(names[peer], peer)))
	}
	if h.MeshIndex == 1 && len(all) >= 2 && o.allDatabasesOnControl(ctx) {
		out.Warnings = append(out.Warnings, "All databases are on the control host. If it fails, they stop until it is rebuilt. Give another host the data role.")
	}
	if h.State == sched.StateUnknown {
		out.Warnings = append(out.Warnings, "This host is not in the restored state. Re-adopt it or remove it.")
	}
	return out, nil
}

// allDatabasesOnControl reports whether every database replica runs on the control host.
func (o *Orchestrator) allDatabasesOnControl(ctx context.Context) bool {
	ctrl, err := o.controlHost(ctx)
	if err != nil {
		return false
	}
	reps, err := o.queryReps(ctx, `desired = 'running'`)
	if err != nil {
		return false
	}
	found := false
	seen := map[string]bool{}
	for _, r := range reps {
		if seen[r.DeploymentID] {
			continue
		}
		d, err := o.loadDep(ctx, o.db, r.DeploymentID)
		if err != nil || !isDatabase(d.Config) {
			seen[r.DeploymentID] = true
			continue
		}
		found = true
		if r.HostID != ctrl {
			return false
		}
	}
	return found
}

// CreateJoinToken makes a single-use join token and the command to run.
func (o *Orchestrator) CreateJoinToken(ctx context.Context, req api.CreateJoinTokenRequest) (api.JoinToken, error) {
	roles, err := hub.NormalizeRoles(req.Roles)
	if err != nil {
		return api.JoinToken{}, err
	}
	tok, exp, err := o.hub.CreateJoinToken(ctx, roles, req.Name, "")
	if err != nil {
		return api.JoinToken{}, err
	}
	cmd := hub.JoinCommand(o.o.PublicURL, tok, roles)
	if inst, err := o.cfg.Install(ctx); err == nil && inst.Private {
		// Without the flag the new host would keep its ports open.
		cmd += " --private"
	}
	return api.JoinToken{Command: cmd, Token: tok, ExpiresAt: exp}, nil
}

// UpdateHost changes a host's name, roles, drain flag or reserve.
func (o *Orchestrator) UpdateHost(ctx context.Context, hostID string, req api.UpdateHostRequest) (api.Host, error) {
	h, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return api.Host{}, err
	}
	u := hub.HostUpdate{Name: req.Name, Drain: req.Drain, ReserveMB: req.ReserveMB}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 64 {
			return api.Host{}, errorf("A host name must have 1 to 64 characters.")
		}
		u.Name = &n
	}
	if req.Roles != nil {
		roles, err := hub.NormalizeRoles(*req.Roles)
		if err != nil {
			return api.Host{}, err
		}
		hasCtl := false
		for _, r := range roles {
			if r == "control" {
				hasCtl = true
			}
		}
		if hasCtl != (h.MeshIndex == 1) {
			return api.Host{}, errorf("The control role stays with the control host and cannot be moved in v0.1.")
		}
		u.Roles = &roles
	}
	if req.ReserveMB != nil && *req.ReserveMB < 0 {
		return api.Host{}, errorf("The host reserve cannot be negative.")
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error { return hub.UpdateHost(ctx, tx, hostID, u) }); err != nil {
		return api.Host{}, err
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	if req.Drain != nil && *req.Drain && !h.Drain {
		o.activity(ctx, "", "", "host", "Draining host "+h.Name, "", hostID, "")
		laneWG.Add(1)
		go func() {
			defer laneWG.Done()
			o.evacuate(o.bg(), hostID, false)
		}()
	}
	o.notify()
	o.publishHost(ctx, hostID)
	return o.apiHost(ctx, hostID)
}

// evacuate moves every replica without a volume off a host, one at a time
// (draining, maintenance, host loss). With singletons set, singleton
// replicas move too.
func (o *Orchestrator) evacuate(ctx context.Context, hostID string, singletons bool) []string {
	var stuck []string
	reps, err := o.queryReps(ctx, `host_id = ? AND desired = 'running' ORDER BY created_at, id`, hostID)
	if err != nil {
		return nil
	}
	for _, r := range reps {
		if err := o.moveAway(ctx, r, singletons, true); err != nil {
			stuck = append(stuck, r.ID)
		}
	}
	return stuck
}

// errPinned says a replica stays on its host (volume or singleton).
var errPinned = errors.New("pinned")

// moveAway places a replacement for r on another host and takes r out of
// the desired state. Its address stays reserved until the host confirms.
func (o *Orchestrator) moveAway(ctx context.Context, r repRow, singletons, wait bool) error {
	d, err := o.loadDep(ctx, o.db, r.DeploymentID)
	if err != nil {
		return err
	}
	snap, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID)
	if err != nil || snap.Deleted {
		return errPinned
	}
	if snap.Volume != nil {
		return errPinned
	}
	if !singletons && sched.Singleton(d.Config, sched.Kind(d.Config, len(snap.Domains) > 0)) {
		return errPinned
	}
	nr, err := o.placeOne(ctx, &d, snap, "", placeOpts{serving: r.Serving, exclude: map[string]bool{r.HostID: true}})
	if err != nil {
		return err
	}
	o.hub.MarkDirty(nr.HostID)
	if wait && o.hub.Connected(r.HostID) {
		o.waitReplicaHealthy(ctx, nr.ID, time.Duration(healthDeadline(d.Config))*time.Second+o.t.HealthSlack)
	}
	o.markRemoved(ctx, r.ID)
	return nil
}

// ---------------------------------------------------------------- liveness

// livenessLoop moves hosts through online, offline (routes withdrawn at
// 60 s) and reschedule (another 120 s) and rotates host tokens (7.1).
func (o *Orchestrator) livenessLoop(ctx context.Context) {
	t := time.NewTicker(o.t.Liveness)
	defer t.Stop()
	rot := time.NewTicker(time.Hour)
	defer rot.Stop()
	phase := map[string]sched.Phase{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-rot.C:
			o.hub.CheckRotations(ctx)
		case <-t.C:
			o.checkLiveness(ctx, phase)
		}
	}
}

func (o *Orchestrator) checkLiveness(ctx context.Context, phase map[string]sched.Phase) {
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return
	}
	now := o.now()
	for _, h := range hosts {
		if h.State == sched.StateUnknown || h.State == sched.StateLost || h.MeshIndex == 0 {
			continue
		}
		since := time.Time{}
		if h.DisconnectedAt > 0 {
			since = time.UnixMilli(h.DisconnectedAt)
		} else if h.LastSeen > 0 {
			since = time.UnixMilli(h.LastSeen)
		} else {
			since = time.UnixMilli(h.CreatedAt)
		}
		// After a control plane restart every agent is briefly disconnected:
		// count from the start, so nothing is withdrawn or moved before agents
		// had their chance to reconnect.
		if since.Before(o.startedAt) {
			since = o.startedAt
		}
		p := sched.HostPhaseWith(o.hub.Connected(h.ID), since, now, o.t.OfflineAfter, o.t.RescheduleAfter)
		prev, seen := phase[h.ID]
		phase[h.ID] = p
		if seen && prev == p && p != sched.PhaseReschedule {
			continue
		}
		switch p {
		case sched.PhaseOnline:
			if seen && prev >= sched.PhaseOffline {
				o.activity(ctx, "", "", "host", "Host "+h.Name+" is back online", "", h.ID, "")
				o.invalidateNet()
				o.hub.MarkDirty()
				o.notify()
			}
		case sched.PhaseOffline:
			if !seen || prev < sched.PhaseOffline {
				o.hostWentOffline(ctx, h)
			}
		case sched.PhaseReschedule:
			if !seen || prev < sched.PhaseOffline {
				o.hostWentOffline(ctx, h)
			}
			o.rescheduleFrom(ctx, h)
		}
	}
}

// hostWentOffline withdraws a host's replicas from every route and DNS answer.
func (o *Orchestrator) hostWentOffline(ctx context.Context, h hub.HostRow) {
	if h.State == sched.StateOnline || h.State == sched.StateJoining {
		_ = o.st.Write(ctx, func(tx *sql.Tx) error { return hub.SetHostState(ctx, tx, h.ID, sched.StateOffline) })
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.notify()
	if h.State == sched.StateMaintenance {
		return
	}
	text := fmt.Sprintf("Host %s is offline. Its replicas were taken out of the routes.", h.Name)
	o.activity(ctx, "", "", "host", strings.TrimSuffix(text, "."), "", h.ID, "")
	o.notice(ctx, cpapi.Notice{Event: api.AlertHostOffline, Ref: h.ID, Text: text})
	o.publishHost(ctx, h.ID)
}

// rescheduleFrom moves replicas without volumes and not singleton off an
// offline host, one at a time; what does not fit stays and an alert fires (9.2).
func (o *Orchestrator) rescheduleFrom(ctx context.Context, h hub.HostRow) {
	if h.State == sched.StateMaintenance {
		return
	}
	reps, err := o.queryReps(ctx, `host_id = ? AND desired = 'running' ORDER BY created_at, id`, h.ID)
	if err != nil || len(reps) == 0 {
		return
	}
	for _, r := range reps {
		err := o.moveAway(ctx, r, false, false)
		if err == nil {
			o.log.Info("rescheduled replica", "replica", r.ID, "from", h.ID)
			continue
		}
		if errors.Is(err, errPinned) {
			continue
		}
		key := "pending:" + r.ID
		o.mu.Lock()
		was := o.alerted[key]
		o.alerted[key] = true
		o.mu.Unlock()
		if !was {
			msg := err.Error()
			var ce *sched.NoCapacityError
			if errors.As(err, &ce) {
				msg = ce.Pending()
			}
			d, _ := o.loadDep(ctx, o.db, r.DeploymentID)
			o.activity(ctx, d.EnvID, d.ServiceID, "alert", "A replica from offline host "+h.Name+" could not move: "+strings.TrimSuffix(msg, "."), "", d.ID, "")
			o.notice(ctx, cpapi.Notice{Event: api.AlertHostOffline, ProjectID: d.ProjectID, EnvID: d.EnvID, ServiceID: d.ServiceID, Ref: r.ID,
				Text: "A replica from offline host " + h.Name + " could not move: " + sentenceOf(err)})
		}
	}
}

// ---------------------------------------------------------------- maintenance, removal, loss

// SetMaintenance moves volume-less replicas elsewhere, then stops the rest
// in reverse dependency order (apps before their databases) and marks the
// host maintenance; ending maintenance starts them in dependency order (9.16).
func (o *Orchestrator) SetMaintenance(ctx context.Context, hostID string, on bool, actor string) (api.Host, error) {
	h, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return api.Host{}, err
	}
	if on && h.State == sched.StateMaintenance {
		return o.apiHost(ctx, hostID)
	}
	if !on && h.State != sched.StateMaintenance {
		return api.Host{}, errorf("Host %s is not in maintenance.", h.Name)
	}
	o.audit(ctx, actor, "host.maintenance", hostID, fmt.Sprintf("on=%v", on))
	if on {
		// No new placements while it empties.
		drain := true
		if err := o.st.Write(ctx, func(tx *sql.Tx) error { return hub.UpdateHost(ctx, tx, hostID, hub.HostUpdate{Drain: &drain}) }); err != nil {
			return api.Host{}, err
		}
		laneWG.Add(1)
		go func() {
			defer laneWG.Done()
			o.enterMaintenance(o.bg(), h, actor)
		}()
	} else {
		laneWG.Add(1)
		go func() {
			defer laneWG.Done()
			o.leaveMaintenance(o.bg(), h, actor)
		}()
	}
	return o.apiHost(ctx, hostID)
}

// stopOrder groups a host's remaining replicas by service in maintenance order.
func (o *Orchestrator) stopOrder(ctx context.Context, hostID, desired string) ([][]repRow, error) {
	reps, err := o.queryReps(ctx, `host_id = ? AND desired = ? ORDER BY created_at, id`, hostID, desired)
	if err != nil {
		return nil, err
	}
	bySvc := map[string][]repRow{}
	var svcs []string
	names := map[string]string{} // env/name -> key
	for _, r := range reps {
		k := r.EnvID + "/" + r.ServiceID
		if _, ok := bySvc[k]; !ok {
			svcs = append(svcs, k)
			if s, err := o.cfg.ServiceInstance(ctx, r.EnvID, r.ServiceID); err == nil {
				names[r.EnvID+"/"+s.Config.Name] = k
				names[r.EnvID+"/"+s.PrivateName] = k
			}
		}
		bySvc[k] = append(bySvc[k], r)
	}
	refs := map[string][]string{}
	for _, k := range svcs {
		env, svc, _ := strings.Cut(k, "/")
		if s, err := o.cfg.ServiceInstance(ctx, env, svc); err == nil {
			for _, n := range s.References {
				if t := names[env+"/"+n]; t != "" {
					refs[k] = append(refs[k], t)
				}
			}
		}
	}
	var out [][]repRow
	for _, k := range sched.MaintenanceOrder(svcs, refs) {
		out = append(out, bySvc[k])
	}
	return out, nil
}

func (o *Orchestrator) enterMaintenance(ctx context.Context, h hub.HostRow, actor string) {
	o.evacuate(ctx, h.ID, true)
	groups, err := o.stopOrder(ctx, h.ID, desiredRunning)
	if err != nil {
		o.log.Error("maintenance order", "host", h.ID, "err", err)
	}
	var order []string
	for _, g := range groups {
		for _, r := range g {
			order = append(order, r.ID)
		}
	}
	for _, g := range groups {
		for _, r := range g {
			o.stopOne(ctx, r)
		}
	}
	if err := o.st.Write(ctx, func(tx *sql.Tx) error { return hub.SetHostState(ctx, tx, h.ID, sched.StateMaintenance) }); err != nil {
		o.log.Error("maintenance", "host", h.ID, "err", err)
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.sendMaintenance(ctx, h.ID, proto.Maintenance{On: true, Order: order})
	o.activity(ctx, "", "", "host", "Host "+h.Name+" is in maintenance", actor, h.ID, "")
	o.publishHost(ctx, h.ID)
}

func (o *Orchestrator) leaveMaintenance(ctx context.Context, h hub.HostRow, actor string) {
	drain := false
	if err := o.st.Write(ctx, func(tx *sql.Tx) error {
		if err := hub.UpdateHost(ctx, tx, h.ID, hub.HostUpdate{Drain: &drain}); err != nil {
			return err
		}
		state := sched.StateOffline
		if o.hub.Connected(h.ID) {
			state = sched.StateOnline
		}
		return hub.SetHostState(ctx, tx, h.ID, state)
	}); err != nil {
		o.log.Error("end maintenance", "host", h.ID, "err", err)
		return
	}
	groups, _ := o.stopOrder(ctx, h.ID, desiredStopped)
	var order []string
	for i := len(groups) - 1; i >= 0; i-- {
		for _, r := range groups[i] {
			order = append(order, r.ID)
		}
	}
	o.sendMaintenance(ctx, h.ID, proto.Maintenance{On: false, Order: order})
	// Start in dependency order: databases before the apps that reference them.
	for i := len(groups) - 1; i >= 0; i-- {
		for _, r := range groups[i] {
			_ = o.st.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'running', serving = (SELECT active FROM o_deployments WHERE id = ?) WHERE id = ?`, r.DeploymentID, r.ID)
				return err
			})
			o.invalidateNet()
			o.hub.MarkDirty(h.ID)
		}
		for _, r := range groups[i] {
			o.waitReplicaHealthy(ctx, r.ID, 5*time.Minute)
		}
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.activity(ctx, "", "", "host", "Host "+h.Name+" left maintenance", actor, h.ID, "")
	o.publishHost(ctx, h.ID)
}

func (o *Orchestrator) sendMaintenance(ctx context.Context, hostID string, m proto.Maintenance) {
	if !o.hub.Connected(hostID) {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := o.hub.Call(cctx, hostID, proto.TMaintenance, "", m, nil); err != nil {
		o.log.Info("maintenance order not acknowledged", "host", hostID, "err", err)
	}
}

// RemoveHost deletes a host; refused while it runs replicas unless force.
func (o *Orchestrator) RemoveHost(ctx context.Context, hostID string, force bool) error {
	h, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return err
	}
	if h.MeshIndex == 1 {
		return errorf("The control host cannot be removed. Rebuild it on a new VM instead (see the Disaster recovery guide).")
	}
	reps, err := o.queryReps(ctx, `host_id = ? AND desired IN ('running', 'stopped')`, hostID)
	if err != nil {
		return err
	}
	if len(reps) > 0 && !force {
		return errorf("Host %s still runs %d %s. Drain it first, or remove it with force.", h.Name, len(reps), plural(len(reps), "replica", "replicas"))
	}
	for _, r := range reps {
		if err := o.moveAway(ctx, r, true, false); err != nil {
			o.markRemoved(ctx, r.ID)
		}
	}
	if err := o.forgetHost(ctx, h, true); err != nil {
		return err
	}
	o.hub.Disconnect(hostID, proto.CloseRevoked)
	o.activity(ctx, "", "", "host", "Host "+h.Name+" was removed", "", hostID, "")
	return nil
}

// forgetHost deletes a host record, its addresses and its replicas' rows.
// Volumes on it switch to Host lost.
func (o *Orchestrator) forgetHost(ctx context.Context, h hub.HostRow, deleteRow bool) error {
	now := o.now().UnixMilli()
	err := o.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE o_replicas SET desired = 'removed', serving = 0, draining = 0, state = 'removed', removed_at = ?
			WHERE host_id = ? AND removed_at = 0`, now, h.ID); err != nil {
			return err
		}
		if err := ipam.ReleaseHost(ctx, tx, h.ID); err != nil {
			return err
		}
		if deleteRow {
			return hub.DeleteHost(ctx, tx, h.ID)
		}
		return hub.SetHostState(ctx, tx, h.ID, sched.StateLost)
	})
	if err != nil {
		return err
	}
	o.invalidateNet()
	o.hub.MarkDirty()
	o.notify()
	return nil
}

// ClearHostConflicts dismisses the duplicate-host warning.
func (o *Orchestrator) ClearHostConflicts(ctx context.Context, hostID string) (api.Host, error) {
	if err := o.st.Write(ctx, func(tx *sql.Tx) error { return hub.ClearConflicts(ctx, tx, hostID) }); err != nil {
		return api.Host{}, err
	}
	return o.apiHost(ctx, hostID)
}

// HostLost marks a host permanently gone (9.17.2): volumes on it show Host
// lost and its replicas are rescheduled where possible, singletons included.
func (o *Orchestrator) HostLost(ctx context.Context, hostID, actor string) error {
	h, err := hub.LoadHost(ctx, o.db, hostID)
	if err != nil {
		return err
	}
	if o.hub.Connected(hostID) {
		return errorf("Host %s is connected. Only a host that is gone can be marked lost.", h.Name)
	}
	reps, err := o.queryReps(ctx, `host_id = ? AND desired = 'running' ORDER BY created_at, id`, hostID)
	if err != nil {
		return err
	}
	for _, r := range reps {
		if err := o.moveAway(ctx, r, true, false); err != nil && !errors.Is(err, errPinned) {
			o.log.Warn("host lost: replica not moved", "replica", r.ID, "err", err)
		}
	}
	if err := o.forgetHost(ctx, h, false); err != nil {
		return err
	}
	o.audit(ctx, actor, "host.lost", hostID, "")
	o.activity(ctx, "", "", "host", "Host "+h.Name+" was marked lost", actor, hostID, "")
	o.publishHost(ctx, hostID)
	return nil
}

// HostMetrics answers the host page's charts.
func (o *Orchestrator) HostMetrics(ctx context.Context, hostID string, fromMs, untilMs int64) (api.MetricsResponse, error) {
	if _, err := hub.LoadHost(ctx, o.db, hostID); err != nil {
		return api.MetricsResponse{}, err
	}
	fromMs, untilMs = o.window(fromMs, untilMs)
	return o.met.HostMetrics(ctx, hostID, fromMs, untilMs)
}

// Sync re-pushes the full desired state to one host ("" = every host).
func (o *Orchestrator) Sync(ctx context.Context, hostID string) error {
	o.invalidateNet()
	if hostID == "" {
		o.hub.MarkDirty()
		return nil
	}
	if _, err := hub.LoadHost(ctx, o.db, hostID); err != nil {
		return err
	}
	if !o.hub.Connected(hostID) {
		return &hub.ErrOffline{Host: hostID}
	}
	o.hub.MarkDirty(hostID)
	return nil
}

// window clamps a metrics window to 30 days back and defaults to the last hour.
func (o *Orchestrator) window(from, until int64) (int64, int64) {
	now := o.now().UnixMilli()
	if until <= 0 || until > now {
		until = now
	}
	if from <= 0 || from >= until {
		from = until - time.Hour.Milliseconds()
	}
	if min := now - 30*24*time.Hour.Milliseconds(); from < min {
		from = min
	}
	return from, until
}
