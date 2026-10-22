// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

func (a *App) saveLink(dir string, l Link) error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	if dir == "" {
		dir = a.cwd()
	}
	cfg.Links[dir] = l
	return cfg.Save()
}

func (a *App) cmdInit() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "init",
		Short: "Create a project and link this directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			if name == "" && a.interactive() {
				if name, err = a.prompt("Project name (empty for a generated one)", ""); err != nil {
					return err
				}
			}
			var p api.Project
			if err := cl.Do(cmd.Context(), "POST", "/projects", api.CreateProjectRequest{Name: name}, &p); err != nil {
				return err
			}
			env, _ := findEnv(p, "")
			if err := a.saveLink("", Link{Instance: cl.Base, Project: p.ID, Environment: env.ID}); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(p)
			}
			a.printf("Created project %s and linked this directory (environment %s)\n", p.Name, env.Name)
			return nil
		},
	}
	c.Flags().StringVarP(&name, "name", "n", "", "project name")
	return c
}

func (a *App) cmdLink() *cobra.Command {
	return &cobra.Command{
		Use:   "link",
		Short: "Link this directory to project, environment, service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			var projects []api.Project
			if err := cl.Do(ctx, "GET", "/projects", nil, &projects); err != nil {
				return err
			}
			if len(projects) == 0 {
				return errors.New("there are no projects yet. Run `5050 init` to create one")
			}
			var p api.Project
			if a.Project != "" {
				var ok bool
				if p, ok = findProject(projects, a.Project); !ok {
					return fmt.Errorf("no project named %q", a.Project)
				}
			} else {
				names := make([]string, len(projects))
				for i, x := range projects {
					names[i] = x.Name
				}
				i, err := a.pick("Select a project", names)
				if err != nil {
					return err
				}
				p = projects[i]
			}
			var env api.Environment
			if a.Environment != "" || len(p.Environments) <= 1 || !a.interactive() {
				var ok bool
				if env, ok = findEnv(p, a.Environment); !ok {
					return fmt.Errorf("no environment named %q in %s", a.Environment, p.Name)
				}
			} else {
				names := make([]string, len(p.Environments))
				for i, x := range p.Environments {
					names[i] = x.Name
				}
				i, err := a.pick("Select an environment", names)
				if err != nil {
					return err
				}
				env = p.Environments[i]
			}
			l := Link{Instance: cl.Base, Project: p.ID, Environment: env.ID}
			t := &Target{C: cl, Project: p, Env: env}
			if err := a.loadServices(ctx, t); err != nil {
				return err
			}
			if a.Service != "" {
				s, ok := findService(t.Services, a.Service)
				if !ok {
					return fmt.Errorf("no service named %q in %s/%s", a.Service, p.Name, env.Name)
				}
				l.Service = s.ID
			} else if len(t.Services) > 0 && a.interactive() {
				names := []string{"(none)"}
				for _, s := range t.Services {
					names = append(names, s.Config.Name)
				}
				i, err := a.pick("Select a service", names)
				if err != nil {
					return err
				}
				if i > 0 {
					l.Service = t.Services[i-1].ID
				}
			}
			if err := a.saveLink("", l); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(l)
			}
			a.printf("Linked to %s/%s", p.Name, env.Name)
			if s, ok := findService(t.Services, l.Service); ok && l.Service != "" {
				a.printf("/%s", s.Config.Name)
			}
			a.printf("\n")
			return nil
		},
	}
}

func (a *App) cmdUnlink() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Remove the link of this directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			_, dir, ok := cfg.FindLink(a.cwd())
			if !ok {
				return errors.New("this directory is not linked")
			}
			delete(cfg.Links, dir)
			if err := cfg.Save(); err != nil {
				return err
			}
			a.printf("Unlinked %s\n", dir)
			return nil
		},
	}
}

func (a *App) cmdStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Linked project, environment, service, its status and URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := a.resolve(cmd.Context(), false)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{"instance": t.URL, "project": t.Project, "environment": t.Env, "service": t.Service})
			}
			a.printf("Project:     %s\nEnvironment: %s\n", t.Project.Name, t.Env.Name)
			if t.Service != nil {
				s := t.Service
				a.printf("Service:     %s\nStatus:      %s", s.Config.Name, s.Status)
				if s.StatusText != "" {
					a.printf(" (%s)", s.StatusText)
				}
				a.printf("\n")
				if u := serviceURL(s); u != "" {
					a.printf("URL:         %s\n", u)
				}
			} else {
				a.printf("Service:     none linked\n")
			}
			return nil
		},
	}
}

func (a *App) cmdList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			var ps []api.Project
			if err := cl.Do(cmd.Context(), "GET", "/projects", nil, &ps); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(ps)
			}
			rows := [][]string{{"NAME", "ID", "ENVIRONMENTS"}}
			for _, p := range ps {
				rows = append(rows, []string{p.Name, p.ID, fmt.Sprint(len(p.Environments))})
			}
			a.table(a.Out, rows)
			return nil
		},
	}
}

func (a *App) cmdOpen() *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the canvas (or --service panel) in the browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := a.resolve(cmd.Context(), a.Service != "")
			if err != nil {
				return err
			}
			u := t.URL + "/project/" + t.Project.ID + "?environment=" + t.Env.ID
			if a.Service != "" && t.Service != nil {
				u = t.URL + "/project/" + t.Project.ID + "/service/" + t.Service.ID + "?environment=" + t.Env.ID
			}
			if err := openBrowser(u); err != nil {
				a.printf("%s\n", u)
				return nil
			}
			a.printf("Opened %s\n", u)
			return nil
		},
	}
}
