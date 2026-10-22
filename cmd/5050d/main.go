// SPDX-License-Identifier: AGPL-3.0-only

// Command 5050d is the 5050cloud control plane (docs/SPEC.md 3 and 8.3).
//
//	5050d init --public-ip IP [flags]      create config, keys, state and the setup token
//	5050d serve [--dev]                    run the control plane
//	5050d setup-token                      print a new setup link
//	5050d reset-password --email E         print a password reset link
//	5050d reset-2fa --email E              turn off two-factor login for a user
//	5050d backup-state --out FILE          write an encrypted state snapshot
//	5050d restore-state --from FILE --recovery-key K
//	5050d upgrade [--version vX.Y.Z]       upgrade the control plane
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/musakkka/5050cloud/internal/cp/core/config"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/version"
)

const usage = `5050d is the 5050cloud control plane.

Usage:
  5050d init [flags]                        create config, keys, state.db and a setup token
  5050d serve [--dev] [flags]               run the control plane (systemd starts it)
  5050d setup-token                         print a new one-time setup link
  5050d reset-password --email EMAIL        print a one-time password reset link
  5050d reset-2fa --email EMAIL             turn off two-factor login for a user
  5050d backup-state --out FILE             write an encrypted snapshot of state.db
  5050d restore-state --from FILE --recovery-key KEY [flags]
  5050d rotate-master-key                   not available in this release
  5050d upgrade [--version vX.Y.Z]          upgrade this install
  5050d version                             print the version

Every command takes --config (default /etc/5050/config.json, or FIFTY_CONFIG).
Run "5050d <command> -h" for the flags of a command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "init":
		err = cmdInit(ctx, rest, stdout)
	case "serve":
		err = cmdServe(ctx, rest, stdout)
	case "setup-token":
		err = cmdSetupToken(ctx, rest, stdout)
	case "reset-password":
		err = cmdResetPassword(ctx, rest, stdout)
	case "reset-2fa":
		err = cmdReset2FA(ctx, rest, stdout)
	case "backup-state":
		err = cmdBackupState(ctx, rest, stdout)
	case "restore-state":
		err = cmdRestoreState(ctx, rest, stdout)
	case "rotate-master-key":
		err = errors.New("rotate-master-key is not available in this release; keep the current recovery key safe")
	case "upgrade":
		err = cmdUpgrade(ctx, rest, stdout)
	case "upgrade-helper":
		return upgrade.HelperMain(rest, stderr)
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "5050d %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		return 0
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "5050d: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		_, _ = fmt.Fprintln(stderr, "5050d: "+sentence(err.Error()))
		return 1
	}
	return 0
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// env returns FIFTY_<name> or def.
func env(name, def string) string {
	if v := os.Getenv("FIFTY_" + name); v != "" {
		return v
	}
	return def
}

func configFlag(fs *flag.FlagSet) *string {
	return fs.String("config", env("CONFIG", config.DefaultPath), "path of config.json")
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs
}

func logger() *slog.Logger {
	lvl := slog.LevelInfo
	if os.Getenv("FIFTY_DEBUG") != "" {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
