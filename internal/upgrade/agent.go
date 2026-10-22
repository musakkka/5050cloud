// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxAgentBoots is how often a new agent may start without connecting
// before it reverts at once (it keeps crashing on start).
const maxAgentBoots = 5

// pendingAgent is <DataDir>/agent/upgrade-pending.json.
type pendingAgent struct {
	Version   string `json:"version"`
	Previous  string `json:"previous"` // release directory to go back to
	StagedAt  int64  `json:"stagedAt"`
	FirstBoot int64  `json:"firstBoot,omitempty"` // unix ms of the new agent's first start
	Boots     int    `json:"boots,omitempty"`
}

// RevertedAgent is <DataDir>/agent/upgrade-reverted.json, left by a revert
// so the old agent can log what happened.
type RevertedAgent struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
	At      int64  `json:"at"`
}

func (o Options) pendingPath() string {
	return filepath.Join(o.agentStateDir(), "upgrade-pending.json")
}
func (o Options) revertedPath() string {
	return filepath.Join(o.agentStateDir(), "upgrade-reverted.json")
}

// UpgradeAgent is called by the agent on proto.TUpgrade: it stages the new
// binary, writes the pending-upgrade marker and re-executes through systemd.
// The new agent reverts itself if it cannot reach the control plane within 90 s.
func UpgradeAgent(ctx context.Context, o Options, u proto.Upgrade) error {
	o = o.Defaults()
	if !ValidVersion(u.Version) {
		return fmt.Errorf("%q is not a release version", u.Version)
	}
	if u.URL == "" || len(u.SHA256) != 64 {
		return errors.New("the upgrade request has no tarball URL or SHA-256")
	}
	dir, err := Stage(ctx, o, Release{Version: u.Version, URL: u.URL, SHA256: u.SHA256}, "5050-agent")
	if err != nil {
		return err
	}
	prev, err := linkTarget(o.AgentLink())
	if err != nil {
		// Older installs only have /opt/5050/current.
		if prev, err = linkTarget(o.CurrentLink()); err != nil {
			return fmt.Errorf("cannot tell which agent runs now: %w", err)
		}
	}
	if prev == filepath.Clean(dir) {
		return nil // already there
	}
	p := pendingAgent{Version: u.Version, Previous: prev, StagedAt: o.Now().UnixMilli()}
	if err := writeJSON(o.pendingPath(), p); err != nil {
		return err
	}
	if _, err := swapLink(o.AgentLink(), dir); err != nil {
		_ = os.Remove(o.pendingPath())
		return err
	}
	if _, err := o.Run.Run(ctx, "systemctl", "--no-block", "restart", AgentUnit); err != nil {
		_, _ = swapLink(o.AgentLink(), prev)
		_ = os.Remove(o.pendingPath())
		return err
	}
	return nil
}

// CheckPendingAgentUpgrade is called first thing at agent start. When this
// agent was just upgraded it watches connected in the background and
// reverts to the previous binary if the control plane was not reached
// within AgentConnectTimeout of the first start.
func CheckPendingAgentUpgrade(o Options, connected func() bool) error {
	return checkPending(context.Background(), o.Defaults(), connected)
}

func checkPending(ctx context.Context, o Options, connected func() bool) error {
	var p pendingAgent
	if err := readJSON(o.pendingPath(), &p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		_ = os.Remove(o.pendingPath())
		return fmt.Errorf("the pending upgrade marker was unreadable and was removed: %w", err)
	}
	if p.Version != o.Version {
		// The old agent runs (a revert happened or the restart never did).
		_ = os.Remove(o.pendingPath())
		return nil
	}
	now := o.Now().UnixMilli()
	if p.FirstBoot == 0 {
		p.FirstBoot = now
	}
	p.Boots++
	if err := writeJSON(o.pendingPath(), p); err != nil {
		return err
	}
	if p.Boots > maxAgentBoots {
		return revertAgent(ctx, o, p, fmt.Sprintf("the new agent started %d times without connecting", p.Boots-1))
	}
	deadline := time.UnixMilli(p.FirstBoot).Add(o.AgentConnectTimeout)
	go func() { _ = watchPending(ctx, o, p, deadline, connected) }()
	return nil
}

// watchPending is the background half of checkPending; it returns the
// error of a revert (tests call it directly).
func watchPending(ctx context.Context, o Options, p pendingAgent, deadline time.Time, connected func() bool) error {
	for {
		if connected() {
			return os.Remove(o.pendingPath())
		}
		if !o.Now().Before(deadline) {
			return revertAgent(ctx, o, p, fmt.Sprintf("the new agent could not reach the control plane within %s", o.AgentConnectTimeout))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(o.PollInterval):
		}
	}
}

func revertAgent(ctx context.Context, o Options, p pendingAgent, reason string) error {
	if _, err := swapLink(o.AgentLink(), p.Previous); err != nil {
		return err
	}
	_ = os.Remove(o.pendingPath())
	_ = writeJSON(o.revertedPath(), RevertedAgent{Version: p.Version, Reason: reason, At: o.Now().UnixMilli()})
	_, err := o.Run.Run(ctx, "systemctl", "--no-block", "restart", AgentUnit)
	return err
}

// LastAgentRevert returns the last agent revert, if any, and clears it.
func LastAgentRevert(o Options) (RevertedAgent, bool) {
	o = o.Defaults()
	var r RevertedAgent
	if err := readJSON(o.revertedPath(), &r); err != nil {
		return r, false
	}
	_ = os.Remove(o.revertedPath())
	return r, true
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o600)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
