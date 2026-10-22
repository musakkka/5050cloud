// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/pkg/api"

	_ "modernc.org/sqlite" // read the migration tables of state.db
)

// Units the control plane upgrade touches.
const (
	ControlUnit = "5050d.service"
	AgentUnit   = "5050-agent.service"
	HelperUnit  = "5050-upgrade"
)

// stateFiles are copied before the switch and put back by a rollback that
// crossed a schema change.
var stateFiles = []string{"state.db", "state.db-wal", "state.db-shm"}

// StartControlPlane stages the release and launches the detached helper
// (systemd-run) that swaps binaries, restarts 5050d, verifies /readyz and
// rolls back on failure. Returns once the helper is running.
func StartControlPlane(ctx context.Context, o Options, r Release) error {
	o = o.Defaults()
	if st, err := Status(o); err == nil && inProgress(st) && o.Now().UnixMilli()-st.At < int64(10*time.Minute/time.Millisecond) {
		return fmt.Errorf("an upgrade to %s is already running", st.ToVersion)
	}
	base := api.UpgradeStatus{FromVersion: o.Version, ToVersion: r.Version}
	set := func(state, msg string) {
		s := base
		s.State, s.Message = state, msg
		_ = writeStatus(o, s)
	}
	set(StateDownloading, "Downloading "+r.Version+".")
	dir, err := Stage(ctx, o, r, "5050d", "5050-agent", "5050")
	if err != nil {
		set(StateFailed, sentence(err))
		return err
	}
	if o.Snapshot != nil {
		snap := filepath.Join(o.DataDir, "backups", fmt.Sprintf("state-pre-%s-%d.db.age", r.Version, o.Now().Unix()))
		if err := o.Snapshot(snap); err != nil {
			err = fmt.Errorf("the state snapshot before the upgrade failed, so nothing was changed: %w", err)
			set(StateFailed, sentence(err))
			return err
		}
	}
	agents := 0
	if o.AgentsConnected != nil {
		agents = o.AgentsConnected()
	} else if rz, err := readyz(ctx, o); err == nil && rz.Checks != nil {
		agents = rz.Checks.Agents
	}
	args := []string{
		"--unit", fmt.Sprintf("%s-%d", HelperUnit, o.Now().Unix()), "--collect", "--quiet",
		filepath.Join(dir, "5050d"), "upgrade-helper",
		"--to", r.Version, "--from", o.Version, "--release-dir", dir,
		"--install-dir", o.InstallDir, "--data-dir", o.DataDir,
		"--control-url", o.ControlURL, "--agents", strconv.Itoa(agents),
	}
	set(StateSwitching, "Switching to "+r.Version+".")
	if _, err := o.Run.Run(ctx, "systemd-run", args...); err != nil {
		err = fmt.Errorf("the upgrade helper could not start: %w", err)
		set(StateFailed, sentence(err))
		return err
	}
	return nil
}

// HelperArgs are the flags of `5050d upgrade-helper`.
type HelperArgs struct {
	To, From   string
	ReleaseDir string
	Agents     int
}

// HelperMain is `5050d upgrade-helper ARGS`. cmd/5050d calls it and exits
// with the code it returns.
func HelperMain(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("upgrade-helper", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var h HelperArgs
	var o Options
	fs.StringVar(&h.To, "to", "", "target version")
	fs.StringVar(&h.From, "from", "", "version before the upgrade")
	fs.StringVar(&h.ReleaseDir, "release-dir", "", "unpacked release directory")
	fs.IntVar(&h.Agents, "agents", 0, "agents connected before the upgrade")
	fs.StringVar(&o.InstallDir, "install-dir", "", "install directory (/opt/5050)")
	fs.StringVar(&o.DataDir, "data-dir", "", "data directory (/var/lib/5050)")
	fs.StringVar(&o.ControlURL, "control-url", "", "local control plane URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	o.ReleaseFile = "-"
	if err := RunHelper(context.Background(), o, h); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// RunHelper switches /opt/5050/current to the new release, restarts 5050d
// and waits for it to be ready. When it is not ready in time it switches
// back, restores state.db if the schema changed, restarts the old version
// and records rolled_back in upgrade.json.
func RunHelper(ctx context.Context, o Options, h HelperArgs) error {
	o = o.Defaults()
	if h.To == "" || h.ReleaseDir == "" {
		return errors.New("upgrade-helper needs --to and --release-dir")
	}
	base := api.UpgradeStatus{FromVersion: h.From, ToVersion: h.To}
	set := func(state, msg string) {
		s := base
		s.State, s.Message = state, msg
		_ = writeStatus(o, s)
	}
	set(StateSwitching, "Stopping "+h.From+" and switching to "+h.To+".")
	if _, err := o.Run.Run(ctx, "systemctl", "stop", ControlUnit); err != nil {
		set(StateFailed, sentence(err))
		return err
	}
	backupDir := filepath.Join(o.DataDir, "backups", "pre-upgrade-"+h.To)
	if err := copyState(o.DataDir, backupDir); err != nil {
		_, _ = o.Run.Run(ctx, "systemctl", "start", ControlUnit)
		err = fmt.Errorf("could not copy state.db before the upgrade, so nothing was changed: %w", err)
		set(StateFailed, sentence(err))
		return err
	}
	schemaBefore := schemaVersion(filepath.Join(o.DataDir, "state.db"))
	prev, err := swapLink(o.CurrentLink(), h.ReleaseDir)
	if err != nil {
		_, _ = o.Run.Run(ctx, "systemctl", "start", ControlUnit)
		set(StateFailed, sentence(err))
		return err
	}
	_, startErr := o.Run.Run(ctx, "systemctl", "start", ControlUnit)
	set(StateVerifying, "Waiting for "+h.To+" to report ready.")
	reason := ""
	if startErr != nil {
		reason = sentence(startErr)
	} else {
		reason = waitReady(ctx, o, h.To, h.Agents)
	}
	if reason == "" {
		set(StateDone, "Upgraded to "+h.To+".")
		_ = Prune(o, 2)
		return nil
	}

	// Roll back.
	_, _ = o.Run.Run(ctx, "systemctl", "stop", ControlUnit)
	msg := "The upgrade to " + h.To + " was rolled back: " + reason
	if prev == "" {
		set(StateFailed, msg+" There was no previous version to go back to.")
		return errors.New(msg)
	}
	if _, err := swapLink(o.CurrentLink(), prev); err != nil {
		set(StateFailed, msg+" Switching back failed too: "+sentence(err))
		return err
	}
	if schemaVersion(filepath.Join(o.DataDir, "state.db")) != schemaBefore {
		if err := restoreState(backupDir, o.DataDir); err != nil {
			msg += " Restoring state.db failed: " + sentence(err)
		} else {
			// 9.17.1: the old version starts in adopt mode so nothing
			// deployed in between is removed.
			_ = os.WriteFile(filepath.Join(o.DataDir, "adopt-after-restore"), []byte(h.To+"\n"), 0o600)
			msg += " state.db was restored from before the upgrade."
		}
	}
	if _, err := o.Run.Run(ctx, "systemctl", "start", ControlUnit); err != nil {
		msg += " Starting " + h.From + " again failed: " + sentence(err)
	}
	set(StateRolledBack, msg)
	return errors.New(msg)
}

// waitReady polls /readyz until the new version is ready with at least
// agents connected, and returns "" or why it never got there.
func waitReady(ctx context.Context, o Options, version string, agents int) string {
	deadline := o.Now().Add(o.ReadyTimeout)
	var last string
	for {
		rz, err := readyz(ctx, o)
		switch {
		case err != nil:
			last = "it did not answer on " + o.ControlURL + "/readyz (" + strings.TrimSuffix(err.Error(), ".") + ")."
		case rz.Version != version:
			last = "it reports version " + rz.Version + " instead of " + version + "."
		case !rz.Ready || rz.Checks == nil || !rz.Checks.Migrations || !rz.Checks.Hub:
			last = "it did not report ready (migrations and agent hub)."
		case rz.Checks.Agents < agents:
			last = fmt.Sprintf("only %d of %d agents reconnected.", rz.Checks.Agents, agents)
		default:
			return ""
		}
		if !o.Now().Before(deadline) {
			return fmt.Sprintf("within %s %s", o.ReadyTimeout, last)
		}
		select {
		case <-ctx.Done():
			return "the helper was stopped."
		case <-time.After(o.PollInterval):
		}
	}
}

func readyz(ctx context.Context, o Options) (api.Readyz, error) {
	var rz api.Readyz
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.ControlURL+"/readyz", nil)
	if err != nil {
		return rz, err
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return rz, err
	}
	defer resp.Body.Close()
	// A 503 still carries the body, which says what is not ready yet.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return rz, err
	}
	if err := json.Unmarshal(b, &rz); err != nil {
		return rz, fmt.Errorf("/readyz answered %s without JSON", resp.Status)
	}
	return rz, nil
}

func copyState(dataDir, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, f := range stateFiles {
		_ = os.Remove(filepath.Join(dst, f))
		if err := copyFile(filepath.Join(dataDir, f), filepath.Join(dst, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func restoreState(src, dataDir string) error {
	for _, f := range stateFiles {
		if err := os.Remove(filepath.Join(dataDir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copyFile(filepath.Join(src, f), filepath.Join(dataDir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// schemaVersion summarises every *_migrations table of state.db as
// "table:maxversion,...". "" when the file cannot be read.
func schemaVersion(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return ""
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name LIKE '%\_migrations' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return ""
	}
	var tables []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && validTable(n) {
			tables = append(tables, n)
		}
	}
	_ = rows.Close()
	var parts []string
	for _, t := range tables {
		var v sql.NullInt64
		_ = db.QueryRow(`SELECT MAX(version) FROM ` + t).Scan(&v)
		parts = append(parts, fmt.Sprintf("%s:%d", t, v.Int64))
	}
	return strings.Join(parts, ",")
}

func validTable(n string) bool {
	for _, r := range n {
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return n != ""
}

// sentence makes an error read as one sentence ending in a period.
func sentence(err error) string {
	s := strings.TrimSpace(err.Error())
	if s == "" {
		return "Unknown error."
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
