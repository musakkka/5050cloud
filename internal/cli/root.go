// SPDX-License-Identifier: MIT

// Package cli is the `5050` command line client (SPEC section 12). It only
// imports pkg/ and MIT or permissive third-party modules, so the binary stays
// MIT licensed.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Main runs the CLI with os.Args and returns the process exit code.
func Main() int {
	a := &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return a.Run(name, os.Args[1:])
}

// Run runs one invocation. name is argv[0]'s base name ("5050" or "railway").
func (a *App) Run(name string, args []string) int {
	if name == "railway" {
		a.Railway = true
		var msg string
		args, msg = TranslateRailway(args)
		if msg != "" {
			fmt.Fprintln(a.Err, msg)
			return 1
		}
		a.aliasNotice()
	}
	root := a.Root()
	root.SetArgs(args)
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(a.Err, ee.msg)
			}
			return ee.code
		}
		fmt.Fprintln(a.Err, "Error: "+sentence(err.Error()))
		return 1
	}
	return 0
}

// exitError ends a command with a code (and an optional message already formatted).
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "?") && !strings.HasSuffix(s, "!") {
		s += "."
	}
	return s
}

func (a *App) aliasNotice() {
	cfg, err := a.config()
	if err != nil || cfg.AliasNoticeShown {
		return
	}
	u := cfg.Default
	if v := normURL(a.env("FIFTY_URL")); v != "" {
		u = v
	}
	if u == "" {
		u = "your 5050cloud install"
	}
	fmt.Fprintf(a.Err, "Using 5050cloud at %s; run `5050 alias remove` to undo\n", u)
	cfg.AliasNoticeShown = true
	_ = cfg.Save()
}

// Root builds the command tree.
func (a *App) Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "5050",
		Short:         "Deploy and manage apps on your 5050cloud install",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&a.Project, "project", "p", "", "project name or id")
	pf.StringVarP(&a.Environment, "environment", "e", "", "environment name or id")
	pf.StringVarP(&a.Service, "service", "s", "", "service name or id")
	pf.BoolVar(&a.JSON, "json", false, "print JSON")
	pf.BoolVarP(&a.Yes, "yes", "y", false, "answer yes to confirmations")

	root.AddCommand(
		a.cmdLogin(), a.cmdLogout(), a.cmdWhoami(),
		a.cmdInit(), a.cmdLink(), a.cmdUnlink(), a.cmdStatus(), a.cmdList(), a.cmdOpen(),
		a.cmdUp(), a.cmdRedeploy(), a.cmdRestart(), a.cmdDown(),
		a.cmdDeploy(), a.cmdTemplates(), a.cmdAdd(), a.cmdImport(),
		a.cmdLogs(), a.cmdVariables(), a.cmdRun(), a.cmdShell(), a.cmdSSH(), a.cmdConnect(),
		a.cmdDomain(), a.cmdTCPProxy(), a.cmdEnvironment(), a.cmdService(), a.cmdScale(),
		a.cmdVolume(), a.cmdCron(), a.cmdMetrics(), a.cmdUsage(), a.cmdConfig(),
		a.cmdHost(), a.cmdUpgrade(), a.cmdAlias(), a.cmdCompletion(root), a.cmdVersion(),
	)
	return root
}

// out is a small helper for commands that print a table row per item.
func (a *App) table(w io.Writer, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			if i == len(r)-1 {
				b.WriteString(c)
			} else {
				b.WriteString(c + strings.Repeat(" ", widths[i]-len(c)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}
