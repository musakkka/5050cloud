// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/musakkka/5050cloud/pkg/api"
)

// Upgrade states written to upgrade.json (api.UpgradeStatus.State).
const (
	StateIdle        = "idle"
	StateDownloading = "downloading"
	StateSwitching   = "switching"
	StateVerifying   = "verifying"
	StateDone        = "done"
	StateRolledBack  = "rolled_back"
	StateFailed      = "failed"
)

// Status reads <DataDir>/upgrade.json. A missing file is the idle state.
func Status(o Options) (api.UpgradeStatus, error) {
	o = o.Defaults()
	b, err := os.ReadFile(o.statusPath())
	if errors.Is(err, os.ErrNotExist) {
		return api.UpgradeStatus{State: StateIdle}, nil
	}
	if err != nil {
		return api.UpgradeStatus{}, fmt.Errorf("cannot read the upgrade status: %w", err)
	}
	var s api.UpgradeStatus
	if err := json.Unmarshal(b, &s); err != nil {
		return api.UpgradeStatus{}, fmt.Errorf("%s is not valid JSON: %w", o.statusPath(), err)
	}
	if s.State == "" {
		s.State = StateIdle
	}
	return s, nil
}

// inProgress reports whether s describes an upgrade that is still running.
func inProgress(s api.UpgradeStatus) bool {
	switch s.State {
	case StateDownloading, StateSwitching, StateVerifying:
		return true
	}
	return false
}

func writeStatus(o Options, s api.UpgradeStatus) error {
	if s.At == 0 {
		s.At = o.Now().UnixMilli()
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(o.statusPath(), append(b, '\n'), 0o644)
}

// writeFileAtomic writes data through a temp file and a rename.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
