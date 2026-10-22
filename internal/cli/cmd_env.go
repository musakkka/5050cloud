// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/pkg/api"
)

// dotPatch turns "deploy.startCommand" and a value into a nested merge patch.
// The value is parsed as JSON when it is valid JSON (numbers, booleans, null,
// arrays, objects) and is a string otherwise.
func dotPatch(path, value string) (map[string]any, error) {
	parts := strings.Split(path, ".")
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("%q is not a config path like deploy.startCommand", path)
		}
	}
	var v any
	if json.Unmarshal([]byte(value), &v) != nil {
		v = value
	}
	root := map[string]any{}
	cur := root
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = v
			break
		}
		next := map[string]any{}
		cur[p] = next
		cur = next
	}
	return root, nil
}

func (a *App) relink(l func(*Link)) error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	link, dir, ok := cfg.FindLink(a.cwd())
	if !ok {
		return errors.New("this directory is not linked. Run `5050 link` first")
	}
	l(&link)
	cfg.Links[dir] = link
	return cfg.Save()
}

func (a *App) cmdEnvironment() *cobra.Command {
	c := &cobra.Command{
		Use:     "environment [NAME]",
		Aliases: []string{"env"},
		Short:   "Switch the linked environment; new, delete, rename, sync, config, edit",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			envFlag := a.Environment
			a.Environment = ""
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			ref := envFlag
			if len(args) == 1 {
				ref = args[0]
			}
			var e api.Environment
			if ref != "" {
				var ok bool
				if e, ok = findEnv(t.Project, ref); !ok {
					return fmt.Errorf("no environment named %q in %s", ref, t.Project.Name)
				}
			} else {
				names := make([]string, len(t.Project.Environments))
				for i, x := range t.Project.Environments {
					names[i] = x.Name
				}
				i, err := a.pick("Select an environment", names)
				if err != nil {
					return err
				}
				e = t.Project.Environments[i]
			}
			if err := a.relink(func(l *Link) { l.Environment = e.ID }); err != nil {
				return err
			}
			a.printf("Switched to environment %s\n", e.Name)
			return nil
		},
	}
	var dup string
	newEnv := &cobra.Command{
		Use: "new NAME", Short: "Create an environment (--duplicate ENV copies one)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			req := api.CreateEnvironmentRequest{Name: args[0], Mode: "empty"}
			if dup != "" {
				src, ok := findEnv(t.Project, dup)
				if !ok {
					return fmt.Errorf("no environment named %q", dup)
				}
				req.Mode, req.From = "duplicate", src.ID
			}
			var e api.Environment
			if err := t.C.Do(ctx, "POST", "/projects/"+t.Project.ID+"/environments", req, &e); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(e)
			}
			a.printf("Created environment %s\n", e.Name)
			return nil
		},
	}
	newEnv.Flags().StringVar(&dup, "duplicate", "", "environment to copy")
	del := &cobra.Command{
		Use: "delete [NAME]", Short: "Delete an environment", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) == 1 {
				a.Environment = args[0]
			}
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			if err := a.confirm(fmt.Sprintf("Delete environment %s and everything in it?", t.Env.Name)); err != nil {
				return err
			}
			if err := t.C.Do(ctx, "DELETE", "/environments/"+t.Env.ID, api.ConfirmRequest{Confirm: t.Env.Name}, nil); err != nil {
				return err
			}
			a.printf("Deleted environment %s\n", t.Env.Name)
			return nil
		},
	}
	rename := &cobra.Command{
		Use: "rename NEW_NAME", Short: "Rename the environment", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			var e api.Environment
			if err := t.C.Do(ctx, "PATCH", "/environments/"+t.Env.ID, api.UpdateEnvironmentRequest{Name: &args[0]}, &e); err != nil {
				return err
			}
			a.printf("Renamed %s to %s\n", t.Env.Name, e.Name)
			return nil
		},
	}
	var from string
	sync := &cobra.Command{
		Use: "sync --from ENV", Short: "Stage the differences from another environment", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if from == "" {
				return errors.New("pass --from ENV")
			}
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			src, ok := findEnv(t.Project, from)
			if !ok {
				return fmt.Errorf("no environment named %q", from)
			}
			var cr api.ChangesResponse
			if err := t.C.Do(ctx, "POST", "/environments/"+t.Env.ID+"/sync", api.SyncRequest{From: src.ID}, &cr); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(cr)
			}
			for _, ch := range cr.Changes {
				a.printf("  %-8s %s %s\n", ch.Tag, ch.Kind, ch.Service)
			}
			a.printf("Staged %d changes from %s. Review and deploy them in the console.\n", len(cr.Changes), src.Name)
			return nil
		},
	}
	sync.Flags().StringVar(&from, "from", "", "source environment")
	config := &cobra.Command{
		Use: "config", Short: "Print the environment's effective config as JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			var raw []byte
			if err := t.C.Do(ctx, "GET", "/environments/"+t.Env.ID+"/export", nil, &raw); err != nil {
				return err
			}
			js, err := yaml.YAMLToJSON(raw)
			if err != nil {
				return err
			}
			var v any
			if err := json.Unmarshal(js, &v); err != nil {
				return err
			}
			return a.printJSON(v)
		},
	}
	var svcCfg string
	var stage bool
	edit := &cobra.Command{
		Use: "edit --service-config SVC DOT.PATH VALUE", Short: "Change one config field of a service", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if svcCfg == "" {
				return errors.New("pass --service-config SERVICE")
			}
			a.Service = svcCfg
			return a.patchConfig(cmd, args[0], args[1], stage)
		},
	}
	edit.Flags().StringVar(&svcCfg, "service-config", "", "service to change")
	edit.Flags().BoolVar(&stage, "stage", false, "stage without committing")
	c.AddCommand(newEnv, del, rename, sync, config, edit)
	return c
}

func (a *App) patchConfig(cmd *cobra.Command, path, value string, stage bool) error {
	ctx := cmd.Context()
	t, err := a.resolve(ctx, true)
	if err != nil {
		return err
	}
	patch, err := dotPatch(path, value)
	if err != nil {
		return err
	}
	return a.stagePatch(cmd, t, patch, fmt.Sprintf("Set %s on %s", path, t.Service.Config.Name), stage)
}

func (a *App) stagePatch(cmd *cobra.Command, t *Target, patch map[string]any, what string, stage bool) error {
	ctx := cmd.Context()
	var s api.Service
	if err := t.C.Do(ctx, "PATCH", svcPath(t)+"/config", api.UpdateServiceConfigRequest{Patch: patch}, &s); err != nil {
		return err
	}
	if stage {
		a.printf("%s (staged)\n", what)
		return nil
	}
	cr, err := a.commit(ctx, t, what, true)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.printJSON(cr)
	}
	a.printf("%s\n", what)
	return nil
}

func (a *App) cmdService() *cobra.Command {
	c := &cobra.Command{
		Use:   "service [NAME]",
		Short: "Switch the linked service; list, status, scale, source, rename",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ref := a.Service
			a.Service = ""
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			if err := a.loadServices(ctx, t); err != nil {
				return err
			}
			if len(args) == 1 {
				ref = args[0]
			}
			var s api.Service
			if ref != "" {
				var ok bool
				if s, ok = findService(t.Services, ref); !ok {
					return fmt.Errorf("no service named %q", ref)
				}
			} else {
				names := make([]string, len(t.Services))
				for i, x := range t.Services {
					names[i] = x.Config.Name
				}
				i, err := a.pick("Select a service", names)
				if err != nil {
					return err
				}
				s = t.Services[i]
			}
			if err := a.relink(func(l *Link) { l.Service = s.ID; l.Environment = t.Env.ID; l.Project = t.Project.ID }); err != nil {
				if err := a.saveLink("", Link{Instance: t.URL, Project: t.Project.ID, Environment: t.Env.ID, Service: s.ID}); err != nil {
					return err
				}
			}
			a.printf("Linked service %s\n", s.Config.Name)
			return nil
		},
	}
	list := &cobra.Command{
		Use: "list", Short: "List services", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			a.Service = ""
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			if err := a.loadServices(ctx, t); err != nil {
				return err
			}
			return a.printServices(t.Services)
		},
	}
	var all bool
	status := &cobra.Command{
		Use: "status", Short: "Status of the service (--all for every service)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if all {
				a.Service = ""
				t, err := a.resolve(ctx, false)
				if err != nil {
					return err
				}
				if err := a.loadServices(ctx, t); err != nil {
					return err
				}
				return a.printServices(t.Services)
			}
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			return a.printServices([]api.Service{*t.Service})
		},
	}
	status.Flags().BoolVar(&all, "all", false, "every service in the environment")
	source := &cobra.Command{Use: "source", Short: "Connect or disconnect the service's source"}
	var repo, image, branch string
	connect := &cobra.Command{
		Use: "connect --repo owner/name | --image IMAGE", Short: "Connect a repository or image", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var src map[string]any
			switch {
			case repo != "":
				src = map[string]any{"type": "github", "repo": repo, "image": nil}
				if branch != "" {
					src["branch"] = branch
				}
			case image != "":
				src = map[string]any{"type": "image", "image": image, "repo": nil, "branch": nil}
			default:
				return errors.New("pass --repo or --image")
			}
			t, err := a.resolve(cmd.Context(), true)
			if err != nil {
				return err
			}
			return a.stagePatch(cmd, t, map[string]any{"source": src}, "Connected the source of "+t.Service.Config.Name, false)
		},
	}
	connect.Flags().StringVar(&repo, "repo", "", "GitHub repository owner/name")
	connect.Flags().StringVar(&branch, "branch", "", "branch (with --repo)")
	connect.Flags().StringVar(&image, "image", "", "Docker image")
	disconnect := &cobra.Command{
		Use: "disconnect", Short: "Disconnect the source", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := a.resolve(cmd.Context(), true)
			if err != nil {
				return err
			}
			patch := map[string]any{"source": map[string]any{"type": "empty", "repo": nil, "branch": nil, "image": nil}}
			return a.stagePatch(cmd, t, patch, "Disconnected the source of "+t.Service.Config.Name, false)
		},
	}
	source.AddCommand(connect, disconnect)
	rename := &cobra.Command{
		Use: "rename NAME", Short: "Rename the service", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			var s api.Service
			if err := t.C.Do(ctx, "PATCH", svcPath(t)+"/info", api.UpdateServiceInfoRequest{Name: &args[0]}, &s); err != nil {
				return err
			}
			if _, err := a.commit(ctx, t, "Rename "+t.Service.Config.Name+" to "+args[0], true); err != nil {
				return err
			}
			a.printf("Renamed %s to %s\n", t.Service.Config.Name, args[0])
			return nil
		},
	}
	c.AddCommand(list, status, a.cmdScale(), source, rename)
	return c
}

func (a *App) printServices(ss []api.Service) error {
	if a.JSON {
		return a.printJSON(ss)
	}
	rows := [][]string{{"NAME", "KIND", "STATUS", "URL"}}
	for i := range ss {
		s := &ss[i]
		rows = append(rows, []string{s.Config.Name, string(s.Kind), string(s.Status), serviceURL(s)})
	}
	a.table(a.Out, rows)
	return nil
}

func (a *App) cmdScale() *cobra.Command {
	return &cobra.Command{
		Use:   "scale N",
		Short: "Set replicas (commits)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 0 {
				return fmt.Errorf("%q is not a number of replicas", args[0])
			}
			t, err := a.resolve(cmd.Context(), true)
			if err != nil {
				return err
			}
			patch := map[string]any{"deploy": map[string]any{"replicas": n}}
			return a.stagePatch(cmd, t, patch, fmt.Sprintf("Scaled %s to %d replicas", t.Service.Config.Name, n), false)
		},
	}
}
