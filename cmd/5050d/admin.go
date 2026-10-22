// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/musakkka/5050cloud/internal/cp/core"
	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/api"
)

func cmdSetupToken(ctx context.Context, args []string, stdout io.Writer) error {
	fs := newFlags("setup-token")
	cfgPath := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := openPlane(ctx, *cfgPath, nil, nil)
	if err != nil {
		return err
	}
	defer p.Close()
	tok, err := p.c.CreateSetupToken(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Create the owner account (valid 24 hours, works once):\n%s/setup?token=%s\n", p.c.ConsoleURL(ctx), tok)
	return nil
}

func emailFlag(name string, args []string) (string, string, error) {
	fs := newFlags(name)
	cfgPath := configFlag(fs)
	email := fs.String("email", "", "email of the user")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if *email == "" && fs.NArg() > 0 {
		*email = fs.Arg(0)
	}
	if *email == "" {
		return "", "", errors.New("pass --email EMAIL")
	}
	return *cfgPath, *email, nil
}

func cmdResetPassword(ctx context.Context, args []string, stdout io.Writer) error {
	cfgPath, email, err := emailFlag("reset-password", args)
	if err != nil {
		return err
	}
	p, err := openPlane(ctx, cfgPath, nil, nil)
	if err != nil {
		return err
	}
	defer p.Close()
	link, err := p.c.ResetLink(ctx, email)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Open this link to set a new password (works once):\n%s\n", link)
	return nil
}

func cmdReset2FA(ctx context.Context, args []string, stdout io.Writer) error {
	cfgPath, email, err := emailFlag("reset-2fa", args)
	if err != nil {
		return err
	}
	p, err := openPlane(ctx, cfgPath, nil, nil)
	if err != nil {
		return err
	}
	defer p.Close()
	if err := p.c.Reset2FA(ctx, email); err != nil {
		return err
	}
	_ = p.c.Audit(ctx, apiAudit("system", "member.reset_2fa", email))
	_, _ = fmt.Fprintf(stdout, "Two-factor login is off for %s. They can turn it on again in Account settings.\n", email)
	return nil
}

// ---------------------------------------------------------------- state snapshots

// snapshot writes an age-encrypted VACUUM INTO copy of state.db to out.
func snapshot(ctx context.Context, p *plane, out string) error {
	key, err := os.ReadFile(p.file.MasterKeyPath)
	if err != nil {
		return err
	}
	rec, err := age.NewScryptRecipient(core.RecoveryKeyFor(key))
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(p.file.StatePath()), fmt.Sprintf(".snapshot-%d.db", time.Now().UnixNano()))
	defer func() { _ = os.Remove(tmp) }()
	if err := p.st.VacuumInto(ctx, tmp); err != nil {
		return err
	}
	plain, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rec)
	if err != nil {
		return err
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return config.WriteSecret(out, buf.Bytes())
}

func cmdBackupState(ctx context.Context, args []string, stdout io.Writer) error {
	fs := newFlags("backup-state")
	cfgPath := configFlag(fs)
	out := fs.String("out", "", "file to write (encrypted with the recovery key)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("pass --out FILE")
	}
	p, err := openPlane(ctx, *cfgPath, nil, nil)
	if err != nil {
		return err
	}
	defer p.Close()
	if err := snapshot(ctx, p, *out); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Wrote %s (decrypt it with the recovery key).\n", *out)
	return nil
}

// hourlySnapshots keeps 24 hourly and 7 daily snapshots in <data>/backups (9.17).
func hourlySnapshots(ctx context.Context, p *plane) {
	dir := filepath.Join(p.file.DataDir, "backups")
	take := func() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		out := filepath.Join(dir, fmt.Sprintf("state-%s.db.age", time.Now().UTC().Format("20060102T150405Z")))
		if err := snapshot(ctx, p, out); err != nil {
			p.log.Warn("the hourly state snapshot failed", "err", err)
			return
		}
		pruneSnapshots(dir)
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			take()
		}
	}
}

func pruneSnapshots(dir string) {
	names, _ := filepath.Glob(filepath.Join(dir, "state-2*.db.age"))
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	days := map[string]bool{}
	for i, n := range names {
		day := strings.SplitN(filepath.Base(n), "T", 2)[0]
		switch {
		case i < 24:
		case !days[day] && len(days) < 7:
		default:
			_ = os.Remove(n)
		}
		days[day] = true
	}
}

// restorePending marks that serve must start in adopt mode (9.17.1).
type restorePending struct {
	SnapshotAt int64 `json:"snapshotAt"`
}

func pendingPath(dataDir string) string { return filepath.Join(dataDir, "restore-pending.json") }

func finishPendingRestore(ctx context.Context, p *plane, o cpapi.Orchestrator) error {
	b, err := os.ReadFile(pendingPath(p.file.DataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var rp restorePending
	_ = json.Unmarshal(b, &rp)
	if err := o.Adopt(ctx, rp.SnapshotAt); err != nil {
		return fmt.Errorf("cannot switch to adopt mode after the restore: %w", err)
	}
	return os.Remove(pendingPath(p.file.DataDir))
}

func cmdRestoreState(ctx context.Context, args []string, stdout io.Writer) error {
	fs := newFlags("restore-state")
	cfgPath := configFlag(fs)
	from := fs.String("from", "", "snapshot file written by backup-state")
	rk := fs.String("recovery-key", env("RECOVERY_KEY", ""), "the recovery key (5050R-...)")
	f := &config.File{}
	fs.StringVar(&f.DataDir, "data-dir", env("DATA_DIR", config.DefaultDataDir), "data directory")
	fs.StringVar(&f.PublicIP, "public-ip", env("PUBLIC_IP", ""), "public IPv4 address (when config.json does not exist yet)")
	fs.StringVar(&f.BaseDomain, "base-domain", env("DOMAIN", ""), "base domain (when config.json does not exist yet)")
	fs.StringVar(&f.ConsoleDomain, "console-domain", env("CONSOLE_DOMAIN", ""), "console domain (when config.json does not exist yet)")
	fs.StringVar(&f.ACMEEmail, "acme-email", env("ACME_EMAIL", ""), "")
	fs.StringVar(&f.ACMECA, "acme-ca", env("ACME_CA", "production"), "")
	fs.StringVar(&f.ReleaseBase, "release-base", env("RELEASE_BASE", ""), "")
	fs.String("recovery-key-file", "", "ignored on restore (the key is the one you passed)")
	setupFile := fs.String("setup-url-file", "", "ignored on restore (the owner already exists)")
	fs.String("join-token-file", "", "ignored on restore")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = setupFile
	if *from == "" || *rk == "" {
		return errors.New("pass --from FILE and --recovery-key KEY")
	}
	if strings.HasPrefix(*from, "s3://") || *from == "litestream" {
		return errors.New("restoring straight from a bucket is not available in this release; download the newest state-*.db.age from <bucket>/<prefix>/_state/ and pass the file")
	}
	key, err := core.ParseRecoveryKey(*rk)
	if err != nil {
		return err
	}
	id, err := age.NewScryptIdentity(core.RecoveryKeyFor(key))
	if err != nil {
		return err
	}
	in, err := os.Open(*from)
	if err != nil {
		return err
	}
	defer in.Close()
	r, err := age.Decrypt(in, id)
	if err != nil {
		return fmt.Errorf("cannot decrypt %s with this recovery key: %w", *from, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if existing, err := config.Load(*cfgPath); err == nil {
		f = existing
	} else {
		dir := filepath.Dir(*cfgPath)
		f.MasterKeyPath, f.HostKeyPath = filepath.Join(dir, "master.key"), filepath.Join(dir, "host.key")
		if f.PublicIP == "" {
			return errors.New("config.json does not exist yet, so pass --public-ip")
		}
		f.Defaults()
		if err := config.Save(*cfgPath, f); err != nil {
			return err
		}
	}
	f.Defaults()
	if err := config.WriteSecret(f.MasterKeyPath, key); err != nil {
		return err
	}
	if _, _, err := config.EnsureKey(f.HostKeyPath); err != nil {
		return err
	}
	if err := os.MkdirAll(f.DataDir, 0o700); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(f.StatePath() + suffix)
	}
	if err := config.WriteSecret(f.StatePath(), plain); err != nil {
		return err
	}
	st, _ := os.Stat(*from)
	at := time.Now().UnixMilli()
	if st != nil {
		at = st.ModTime().UnixMilli()
	}
	b, _ := json.Marshal(restorePending{SnapshotAt: at})
	if err := config.WriteSecret(pendingPath(f.DataDir), b); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Restored %s. Start 5050d; it begins in adopt mode and keeps every running container.\n", f.StatePath())
	return nil
}

// ---------------------------------------------------------------- upgrade

func cmdUpgrade(ctx context.Context, args []string, stdout io.Writer) error {
	fs := newFlags("upgrade")
	cfgPath := configFlag(fs)
	ver := fs.String("version", "", "release to install (default latest stable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o := upgrade.Options{}
	if f, err := config.Load(*cfgPath); err == nil {
		o.ReleaseBase, o.LatestURL, o.DataDir = f.ReleaseBase, f.LatestURL, f.DataDir
		o.Snapshot = func(path string) error {
			p, err := openPlane(ctx, *cfgPath, nil, nil)
			if err != nil {
				return err
			}
			defer p.Close()
			return snapshot(ctx, p, path)
		}
	}
	rel, err := upgrade.Resolve(ctx, o, strings.TrimSpace(*ver))
	if err != nil {
		return err
	}
	if err := upgrade.StartControlPlane(ctx, o, rel); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Upgrading to %s. Follow it in Settings > Upgrades or with `journalctl -u 5050-upgrade -f`.\n", rel.Version)
	return nil
}

func apiAudit(actor, action, target string) api.AuditEntry {
	return api.AuditEntry{Actor: actor, Action: action, Target: target}
}
