// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/orch/hub"
	"github.com/musakkka/5050cloud/internal/cp/orch/sched"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Upgrade starts a control plane upgrade (9.16): internal/upgrade resolves
// and verifies the release, takes over with a detached helper and rolls back
// when the new version is not ready. Agents follow, one host at a time.
func (o *Orchestrator) Upgrade(ctx context.Context, req api.UpgradeRequest, actor string) (api.UpgradeStatus, error) {
	o.agentUp.Lock()
	defer o.agentUp.Unlock()
	cur, _ := upgrade.Status(o.o.Upgrade)
	switch cur.State {
	case "downloading", "switching", "verifying":
		return cur, errorf("An upgrade to %s is already running.", firstNonEmpty(cur.ToVersion, "a new version"))
	}
	rel, err := upgrade.Resolve(ctx, o.o.Upgrade, strings.TrimSpace(req.Version))
	if err != nil {
		return api.UpgradeStatus{}, errorf("The release could not be found or verified: %s", sentenceOf(err))
	}
	if rel.Version == o.o.ServerVersion {
		return api.UpgradeStatus{State: "done", FromVersion: o.o.ServerVersion, ToVersion: rel.Version, At: o.now().UnixMilli(),
			Message: "This install already runs " + rel.Version + "."}, nil
	}
	o.audit(ctx, actor, "install.upgrade", rel.Version, "from "+o.o.ServerVersion)
	if err := upgrade.StartControlPlane(ctx, o.o.Upgrade, rel); err != nil {
		return api.UpgradeStatus{}, errorf("The upgrade to %s could not start: %s", rel.Version, sentenceOf(err))
	}
	return o.UpgradeStatus(ctx)
}

// UpgradeStatus reads upgrade.json and adds the agents' progress.
func (o *Orchestrator) UpgradeStatus(ctx context.Context) (api.UpgradeStatus, error) {
	st, err := upgrade.Status(o.o.Upgrade)
	if err != nil {
		return api.UpgradeStatus{}, err
	}
	var agents []api.AgentUpgrade
	if _, err := hub.GetState(ctx, o.db, "agent_upgrades", &agents); err != nil {
		return st, err
	}
	st.Agents = agents
	return st, nil
}

// agentUpgradeLoop upgrades agents that run another version than the
// control plane, one host at a time, after the control plane switched.
func (o *Orchestrator) agentUpgradeLoop(ctx context.Context) {
	if !releaseVersion(o.o.ServerVersion) {
		return // development builds never push agents around
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			o.upgradeOneAgent(ctx)
		}
	}
}

func releaseVersion(v string) bool {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	return len(parts) == 3 && !strings.Contains(v, "dev") && !strings.Contains(v, "dirty")
}

func (o *Orchestrator) setAgentUpgrade(ctx context.Context, a api.AgentUpgrade) {
	_ = o.st.Write(ctx, func(tx *sql.Tx) error {
		var list []api.AgentUpgrade
		if _, err := hub.GetState(ctx, tx, "agent_upgrades", &list); err != nil {
			return err
		}
		found := false
		for i := range list {
			if list[i].HostID == a.HostID {
				list[i], found = a, true
			}
		}
		if !found {
			list = append(list, a)
		}
		return hub.PutState(ctx, tx, "agent_upgrades", list)
	})
}

func (o *Orchestrator) upgradeOneAgent(ctx context.Context) {
	o.agentUp.Lock()
	defer o.agentUp.Unlock()
	hosts, err := hub.LoadHosts(ctx, o.db)
	if err != nil {
		return
	}
	var failed []api.AgentUpgrade
	_, _ = hub.GetState(ctx, o.db, "agent_upgrades", &failed)
	reverted := map[string]bool{}
	for _, a := range failed {
		if a.State == "reverted" && a.Version == o.o.ServerVersion {
			reverted[a.HostID] = true
		}
	}
	for _, h := range hosts {
		if !o.hub.Connected(h.ID) || h.State == sched.StateMaintenance || reverted[h.ID] {
			continue
		}
		v := o.hub.AgentVersion(h.ID)
		if v == "" || v == o.o.ServerVersion || !releaseVersion(v) {
			continue
		}
		opts := o.o.Upgrade
		opts.Arch = firstNonEmpty(h.Facts.Arch, "amd64")
		rel, err := upgrade.Resolve(ctx, opts, o.o.ServerVersion)
		if err != nil {
			o.log.Info("agent upgrade release not available", "version", o.o.ServerVersion, "err", err)
			return
		}
		o.setAgentUpgrade(ctx, api.AgentUpgrade{HostID: h.ID, State: "upgrading", Version: rel.Version})
		if err := o.hub.Send(ctx, h.ID, proto.TUpgrade, hub.NewID("req"), proto.Upgrade{Version: rel.Version, URL: rel.URL, SHA256: rel.SHA256}); err != nil {
			o.setAgentUpgrade(ctx, api.AgentUpgrade{HostID: h.ID, State: "pending", Version: rel.Version, Message: sentenceOf(err)})
			return
		}
		deadline := time.Now().Add(4 * time.Minute)
		for time.Now().Before(deadline) {
			if o.hub.AgentVersion(h.ID) == rel.Version {
				o.setAgentUpgrade(ctx, api.AgentUpgrade{HostID: h.ID, State: "done", Version: rel.Version})
				o.activity(ctx, "", "", "host", fmt.Sprintf("Agent on %s upgraded to %s", h.Name, rel.Version), "", h.ID, "")
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		o.agentUpgradeFailed(ctx, h.ID, fmt.Sprintf("The agent on %s did not come back on %s within 4 minutes.", h.Name, rel.Version))
		return // one host at a time; stop on a failure
	}
}

// agentUpgradeFailed records a reverted agent upgrade.
func (o *Orchestrator) agentUpgradeFailed(ctx context.Context, hostID, text string) {
	h, _ := hub.LoadHost(ctx, o.db, hostID)
	msg := firstNonEmpty(text, fmt.Sprintf("The agent on %s could not upgrade and went back to its previous version.", h.Name))
	o.setAgentUpgrade(ctx, api.AgentUpgrade{HostID: hostID, State: "reverted", Version: o.o.ServerVersion, Message: msg})
	o.notice(ctx, noticeFor(api.AlertVersionSkew, hostID, msg))
}
