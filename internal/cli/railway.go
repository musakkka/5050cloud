// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// railwayUnavailable is the answer for Railway verbs 5050cloud does not have.
func railwayUnavailable(verb string) string {
	return fmt.Sprintf("railway %s is not available in 5050cloud. See 5050cloud.com/docs/coming-from-railway", verb)
}

// Railway verbs that map to ours unchanged (flags may still be renamed).
var railwaySame = map[string]bool{
	"login": true, "logout": true, "whoami": true, "init": true, "link": true, "unlink": true,
	"status": true, "list": true, "open": true, "up": true, "redeploy": true, "restart": true,
	"down": true, "deploy": true, "add": true, "logs": true, "variables": true, "variable": true,
	"run": true, "shell": true, "ssh": true, "connect": true, "domain": true, "environment": true,
	"env": true, "service": true, "volume": true, "completion": true, "help": true, "scale": true,
}

// Railway flags with a value that we drop (workspaces and teams do not exist here).
var railwayDropWithValue = map[string]bool{"--team": true, "-w": true, "--workspace": true}

// Railway flags without a value that we drop.
var railwayDrop = map[string]bool{"--verbose": true, "--deployment": true}

// TranslateRailway maps a Railway CLI invocation to the 5050 equivalent. A
// non-empty message means the verb is not available.
func TranslateRailway(args []string) ([]string, string) {
	if len(args) == 0 {
		return args, ""
	}
	vi := -1
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			vi = i
			break
		}
	}
	if vi < 0 {
		for _, a := range args {
			if a == "-V" || a == "--version" {
				return []string{"version"}, ""
			}
		}
		return args, ""
	}
	verb := args[vi]
	if !railwaySame[verb] {
		return nil, railwayUnavailable(verb)
	}
	pre := append([]string{}, args[:vi]...)
	rest := args[vi+1:]
	out := []string{verb}
	switch verb {
	case "variables", "variable":
		out = translateVariables(verb, rest)
	case "add":
		out = append(out, translateAdd(rest)...)
	case "domain":
		out = append(out, renameFlags(rest, map[string]string{"-p": "--port"})...)
	case "volume":
		if len(rest) > 0 && (rest[0] == "update" || rest[0] == "detach" || rest[0] == "attach") {
			return nil, railwayUnavailable("volume " + rest[0])
		}
		out = append(out, renameFlags(rest, map[string]string{"-m": "--mount", "--mount-path": "--mount"})...)
	case "environment", "env":
		out = append(out, renameFlags(rest, map[string]string{"-d": "--duplicate", "--copy": "--duplicate"})...)
	case "ssh":
		out = append(out, dashBeforeCommand(rest)...)
	case "logs":
		out = append(out, renameFlags(rest, map[string]string{"-d": ""})...)
	case "scale":
		for _, a := range rest {
			if strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
				return nil, railwayUnavailable("scale with regions")
			}
		}
		out = append(out, rest...)
	case "link", "init":
		// -t is --team there; teams do not exist in 5050cloud.
		out = append(out, renameFlags(dropValued(rest, "-t"), nil)...)
	default:
		out = append(out, renameFlags(rest, nil)...)
	}
	return append(pre, out...), ""
}

// renameFlags renames flags (an empty new name drops the flag) and drops
// Railway-only flags. It stops at "--".
func renameFlags(args []string, m map[string]string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(out, args[i:]...)
		}
		name, val, hasVal := strings.Cut(a, "=")
		if railwayDrop[name] {
			continue
		}
		if railwayDropWithValue[name] {
			if !hasVal {
				i++
			}
			continue
		}
		if n, ok := m[name]; ok {
			if n == "" {
				continue
			}
			if hasVal {
				out = append(out, n+"="+val)
			} else {
				out = append(out, n)
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// dropValued removes flags that take a value, with their value.
func dropValued(args []string, names ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		n, _, hasVal := strings.Cut(args[i], "=")
		drop := false
		for _, x := range names {
			drop = drop || n == x
		}
		if !drop {
			out = append(out, args[i])
		} else if !hasVal {
			i++
		}
	}
	return out
}

func translateVariables(verb string, rest []string) []string {
	if len(rest) > 0 && (rest[0] == "set" || rest[0] == "list" || rest[0] == "delete") {
		return append([]string{"variables"}, renameFlags(rest, map[string]string{"-k": "--kv"})...)
	}
	var sets, other []string
	stdinKey := ""
	kv := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		name, val, hasVal := strings.Cut(a, "=")
		switch {
		case name == "--set":
			if hasVal {
				sets = append(sets, val)
			} else if i+1 < len(rest) {
				sets = append(sets, rest[i+1])
				i++
			}
		case name == "--set-from-stdin":
			if hasVal {
				stdinKey = val
			} else if i+1 < len(rest) {
				stdinKey = rest[i+1]
				i++
			}
		case a == "--kv" || a == "-k":
			kv = true
		default:
			other = append(other, a)
		}
	}
	other = renameFlags(other, nil)
	switch {
	case stdinKey != "":
		return append(append([]string{"variables", "set", stdinKey, "--stdin"}, sets...), other...)
	case len(sets) > 0:
		return append(append([]string{"variables", "set"}, sets...), other...)
	case kv:
		return append([]string{"variables", "list", "--kv"}, other...)
	}
	return append([]string{"variables", "list"}, other...)
}

func translateAdd(rest []string) []string {
	out := renameFlags(rest, map[string]string{"-r": "--repo", "-i": "--image", "--variables": "--variable"})
	has := func(names ...string) bool {
		for _, a := range out {
			n, _, _ := strings.Cut(a, "=")
			for _, x := range names {
				if n == x {
					return true
				}
			}
		}
		return false
	}
	// `railway add --service NAME` alone creates an empty service.
	if !has("-d", "--database", "--repo", "--image") {
		for i, a := range out {
			n, val, hasVal := strings.Cut(a, "=")
			if n == "-s" || n == "--service" {
				if hasVal {
					out[i] = "--empty=" + val
				} else {
					out[i] = "--empty"
				}
			}
		}
	}
	return out
}

// dashBeforeCommand inserts "--" before the first positional argument of
// `railway ssh cmd...`, skipping the values of -p, -e, -s and friends.
func dashBeforeCommand(args []string) []string {
	valued := map[string]bool{"-p": true, "--project": true, "-e": true, "--environment": true, "-s": true, "--service": true, "--replica": true, "--session": true, "-d": true, "--deployment-instance": true}
	out := renameFlags(args, map[string]string{"-d": "--replica", "--deployment-instance": "--replica"})
	for i := 0; i < len(out); i++ {
		a := out[i]
		if a == "--" {
			return out
		}
		if strings.HasPrefix(a, "-") {
			if valued[a] {
				i++
			}
			continue
		}
		return append(append(append([]string{}, out[:i]...), "--"), out[i:]...)
	}
	return out
}

// ---------------------------------------------------------------- alias install/remove

func selfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func aliasName() string {
	if runtime.GOOS == "windows" {
		return "railway.exe"
	}
	return "railway"
}

func (a *App) cmdAlias() *cobra.Command {
	c := &cobra.Command{Use: "alias install|remove", Short: "Manage the railway symlink"}
	install := &cobra.Command{
		Use: "install", Short: "Install the railway alias next to this binary", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			self, err := selfPath()
			if err != nil {
				return err
			}
			if p, err := exec.LookPath("railway"); err == nil {
				if r, _ := filepath.EvalSymlinks(p); r != self {
					return fmt.Errorf("a railway binary is already on your PATH at %s. Remove it first, or keep using 5050", p)
				}
				a.printf("The railway alias is already installed at %s\n", p)
				return nil
			}
			dst := filepath.Join(filepath.Dir(self), aliasName())
			if runtime.GOOS == "windows" {
				b, err := os.ReadFile(self)
				if err != nil {
					return err
				}
				if err := os.WriteFile(dst, b, 0o755); err != nil {
					return err
				}
			} else if err := os.Symlink(self, dst); err != nil {
				return fmt.Errorf("could not create %s: %w", dst, err)
			}
			a.printf("Installed %s. `railway` now runs 5050cloud\n", dst)
			return nil
		},
	}
	remove := &cobra.Command{
		Use: "remove", Short: "Remove the railway alias", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			self, err := selfPath()
			if err != nil {
				return err
			}
			cands := []string{filepath.Join(filepath.Dir(self), aliasName())}
			if p, err := exec.LookPath("railway"); err == nil && runtime.GOOS != "windows" {
				cands = append(cands, p)
			}
			for _, p := range cands {
				fi, err := os.Lstat(p)
				if err != nil {
					continue
				}
				r, _ := filepath.EvalSymlinks(p)
				// Only remove our own symlink (or, on Windows, our copy next to
				// this binary); a real railway binary is never touched.
				if runtime.GOOS != "windows" && (fi.Mode()&os.ModeSymlink == 0 || r != self) {
					continue
				}
				if err := os.Remove(p); err != nil {
					return err
				}
				a.printf("Removed %s\n", p)
				return nil
			}
			return errors.New("the railway alias is not installed")
		},
	}
	c.AddCommand(install, remove)
	return c
}
