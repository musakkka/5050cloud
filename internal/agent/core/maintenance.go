// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// maintenanceStartWait bounds how long ending maintenance waits for one
// replica to become healthy before it starts the next one.
const maintenanceStartWait = 2 * time.Minute

// maintain carries out a maintenance order (docs/SPEC.md 9.16). The
// control plane has already moved replicas without volumes. With On, the
// listed replicas stop in the given order (apps before the databases they
// use), each with its stop grace, then any replica still running; nothing
// is started again until maintenance ends. With On false, the listed
// replicas start in order, each waiting until it is healthy (at most two
// minutes), and a reconcile pass starts the rest.
func (a *Agent) maintain(ctx context.Context, reqID string, m proto.Maintenance) {
	a.maintMu.Lock()
	defer a.maintMu.Unlock()
	var errs []string
	if m.On {
		a.mu.Lock()
		a.maintOn = true
		for _, id := range m.Order {
			a.maintenance[id] = true
		}
		for id := range a.replicas {
			a.maintenance[id] = true
		}
		a.mu.Unlock()
		order := append([]string(nil), m.Order...)
		order = append(order, a.replicaIDs()...)
		seen := map[string]bool{}
		for _, id := range order {
			if seen[id] {
				continue
			}
			seen[id] = true
			if err := a.stopForMaintenance(ctx, id); err != nil {
				errs = append(errs, err.Error())
			}
		}
	} else {
		a.mu.Lock()
		a.maintOn = false
		a.maintenance = map[string]bool{}
		a.mu.Unlock()
		for _, id := range m.Order {
			if err := a.startAfterMaintenance(ctx, id); err != nil {
				errs = append(errs, err.Error())
			}
		}
		a.trigger()
	}
	ack := proto.Ack{OK: len(errs) == 0}
	if len(errs) > 0 {
		ack.Error = joinSentences(errs)
	}
	if reqID != "" {
		a.send(proto.TAck, reqID, ack)
	}
}

func (a *Agent) replicaIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.replicas))
	for id := range a.replicas {
		out = append(out, id)
	}
	return out
}

// waitIdle waits until no create, stop or remove runs for the replica and
// marks it busy. It returns false when ctx ends first.
func (a *Agent) waitIdle(ctx context.Context, r *replica) bool {
	for {
		if a.setBusy(r, true) {
			return true
		}
		if !sleepCtx(ctx, 100*time.Millisecond) {
			return false
		}
	}
}

func (a *Agent) stopForMaintenance(ctx context.Context, id string) error {
	a.mu.Lock()
	r := a.replicas[id]
	a.mu.Unlock()
	if r == nil {
		return nil
	}
	if !a.waitIdle(ctx, r) {
		return ctx.Err()
	}
	defer a.setBusy(r, false)
	c, err := a.eng.Inspect(ctx, ContainerName(id))
	if errors.Is(err, engine.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("replica %s could not be inspected: %s", id, sentence(err))
	}
	if !c.Running() {
		return nil
	}
	a.stopReplica(ctx, r, c)
	return nil
}

func (a *Agent) startAfterMaintenance(ctx context.Context, id string) error {
	a.mu.Lock()
	r := a.replicas[id]
	a.mu.Unlock()
	if r == nil {
		return nil
	}
	c, err := a.eng.Inspect(ctx, ContainerName(id))
	if errors.Is(err, engine.ErrNotFound) {
		return nil // the next reconcile creates it
	}
	if err != nil {
		return fmt.Errorf("replica %s could not be inspected: %s", id, sentence(err))
	}
	if !c.Running() {
		if !a.waitIdle(ctx, r) {
			return ctx.Err()
		}
		err := a.eng.Start(ctx, c.ID)
		if err == nil {
			a.mu.Lock()
			r.stoppedByAgent = false
			a.mu.Unlock()
			if nc, ierr := a.eng.Inspect(ctx, c.ID); ierr == nil {
				c = nc
			}
			a.startFollowers(r, c)
		}
		a.setBusy(r, false)
		if err != nil {
			return fmt.Errorf("replica %s could not start: %s", id, sentence(err))
		}
	}
	deadline := time.Now().Add(maintenanceStartWait)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		state := r.status.State
		a.mu.Unlock()
		switch state {
		case "healthy":
			return nil
		case "crashed", "unhealthy":
			return fmt.Errorf("replica %s did not become healthy after maintenance (%s)", id, state)
		}
		if !sleepCtx(ctx, 250*time.Millisecond) {
			return ctx.Err()
		}
	}
	return nil // keep going; the control plane sees its status
}

func joinSentences(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += sentence(errors.New(p))
	}
	return out
}

// selfUpgrade stages a new agent binary and restarts through systemd
// (9.16, internal/upgrade). The new agent reverts itself when it cannot
// connect within 90 s.
func (a *Agent) selfUpgrade(ctx context.Context, reqID string, u proto.Upgrade) {
	if !a.upgradeMu.TryLock() {
		a.send(proto.TAck, reqID, proto.Ack{Error: "An agent upgrade is already running on this host."})
		return
	}
	defer a.upgradeMu.Unlock()
	a.log.Info("upgrading the agent", "version", u.Version)
	if err := upgradeAgent(ctx, a.o.Upgrade, u); err != nil {
		msg := fmt.Sprintf("The agent could not upgrade to %s: %s", u.Version, sentence(err))
		a.log.Error(msg)
		a.send(proto.TEvent, "", proto.Event{Kind: "upgrade.failed", Text: msg, At: time.Now().UnixMilli()})
		a.send(proto.TAck, reqID, proto.Ack{Error: msg})
		return
	}
	a.send(proto.TAck, reqID, proto.Ack{OK: true})
}

// upgradeAgent is internal/upgrade's UpgradeAgent; tests replace it.
var upgradeAgent = upgrade.UpgradeAgent
