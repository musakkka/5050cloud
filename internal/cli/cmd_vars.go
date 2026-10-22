// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

func (a *App) revealAll(ctx context.Context, t *Target) (map[string]string, error) {
	var r api.RevealResponse
	if err := t.C.Do(ctx, "POST", svcPath(t)+"/variables/reveal", api.RevealRequest{Resolved: true}, &r); err != nil {
		return nil, err
	}
	return r.Values, nil
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// shellQuote quotes a value for KEY=VALUE output when needed.
func shellQuote(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n'\"\\$`#&|;<>()*?[]{}!~") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func (a *App) cmdVariables() *cobra.Command {
	var kv bool
	list := func(cmd *cobra.Command) error {
		ctx := cmd.Context()
		t, err := a.resolve(ctx, true)
		if err != nil {
			return err
		}
		if kv {
			vals, err := a.revealAll(ctx, t)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(vals)
			}
			for _, k := range sortedKeys(vals) {
				a.printf("%s=%s\n", k, shellQuote(vals[k]))
			}
			return nil
		}
		var vr api.VariablesResponse
		if err := t.C.Do(ctx, "GET", svcPath(t)+"/variables", nil, &vr); err != nil {
			return err
		}
		if a.JSON {
			return a.printJSON(vr)
		}
		for _, v := range vr.Variables {
			line := v.Name
			if v.Reference != "" {
				line += " = " + v.Reference
			}
			if v.Sealed {
				line += " (sealed)"
			}
			if v.Staged != "" {
				line += " (staged " + v.Staged + ")"
			}
			a.printf("%s\n", line)
		}
		return nil
	}
	c := &cobra.Command{
		Use:     "variables",
		Aliases: []string{"variable", "vars"},
		Short:   "List, set and delete variables",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return list(cmd) },
	}
	c.Flags().BoolVar(&kv, "kv", false, "print KEY=VALUE with values")
	lc := &cobra.Command{
		Use: "list", Short: "List variable names (--kv reveals values)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return list(cmd) },
	}
	lc.Flags().BoolVar(&kv, "kv", false, "print KEY=VALUE with values")

	var stdin, stage, skipDeploys bool
	stageAndCommit := func(cmd *cobra.Command, req api.SetVariablesRequest, what string) error {
		ctx := cmd.Context()
		t, err := a.resolve(ctx, true)
		if err != nil {
			return err
		}
		var vr api.VariablesResponse
		if err := t.C.Do(ctx, "PUT", svcPath(t)+"/variables", req, &vr); err != nil {
			return err
		}
		if stage {
			a.printf("%s on %s (staged; deploy the changes to apply them)\n", what, t.Service.Config.Name)
			return nil
		}
		cr, err := a.commit(ctx, t, what, !skipDeploys)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.printJSON(cr)
		}
		a.printf("%s on %s", what, t.Service.Config.Name)
		if len(cr.Deployments) > 0 {
			a.printf(", deploying")
		}
		a.printf("\n")
		return nil
	}
	set := &cobra.Command{
		Use: "set KEY=VALUE [KEY=VALUE...]", Short: "Set variables (staged and committed unless --stage)",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var m map[string]string
			if stdin {
				if len(args) != 1 || strings.Contains(args[0], "=") {
					return errors.New("with --stdin pass one KEY and the value on standard input")
				}
				b, err := io.ReadAll(a.In)
				if err != nil {
					return err
				}
				m = map[string]string{args[0]: strings.TrimSuffix(string(b), "\n")}
			} else {
				var err error
				if m, err = parseKV(args); err != nil {
					return err
				}
			}
			return stageAndCommit(cmd, api.SetVariablesRequest{Set: m}, "Set "+strings.Join(sortedKeys(m), ", "))
		},
	}
	set.Flags().BoolVar(&stdin, "stdin", false, "read the value from standard input")
	for _, cc := range []*cobra.Command{set} {
		cc.Flags().BoolVar(&stage, "stage", false, "stage without committing")
		cc.Flags().BoolVar(&skipDeploys, "skip-deploys", false, "commit without deploying")
	}
	del := &cobra.Command{
		Use: "delete KEY [KEY...]", Aliases: []string{"unset", "rm"}, Short: "Delete variables",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stageAndCommit(cmd, api.SetVariablesRequest{Unset: args}, "Deleted "+strings.Join(args, ", "))
		},
	}
	del.Flags().BoolVar(&stage, "stage", false, "stage without committing")
	del.Flags().BoolVar(&skipDeploys, "skip-deploys", false, "commit without deploying")
	c.AddCommand(lc, set, del)
	return c
}

func envWith(vals map[string]string) []string {
	env := os.Environ()
	for _, k := range sortedKeys(vals) {
		env = append(env, k+"="+vals[k])
	}
	return env
}

func (a *App) cmdRun() *cobra.Command {
	return &cobra.Command{
		Use:   "run -- CMD",
		Short: "Run a local command with the service's resolved variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			vals, err := a.revealAll(ctx, t)
			if err != nil {
				return err
			}
			return a.runLocal(args[0], args[1:], envWith(vals))
		},
	}
}

func (a *App) runLocal(name string, args, env []string) error {
	c := exec.Command(name, args...)
	c.Env = env
	c.Stdin, c.Stdout, c.Stderr = a.In, a.Out, a.Err
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &exitError{code: ee.ExitCode()}
		}
		return fmt.Errorf("could not run %s: %w", name, err)
	}
	return nil
}

func (a *App) cmdShell() *cobra.Command {
	var silent bool
	c := &cobra.Command{
		Use:   "shell",
		Short: "Local subshell with the variables loaded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			vals, err := a.revealAll(ctx, t)
			if err != nil {
				return err
			}
			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/sh"
				if runtime.GOOS == "windows" {
					sh = "cmd.exe"
				}
			}
			if !silent {
				fmt.Fprintf(a.Err, "Entering a shell with the variables of %s. Type exit to leave.\n", t.Service.Config.Name)
			}
			return a.runLocal(sh, nil, envWith(vals))
		},
	}
	c.Flags().BoolVar(&silent, "silent", false, "do not print the banner")
	return c
}
