// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

// activeDeployment returns the service's active deployment id, or its latest.
func activeDeployment(ctx context.Context, t *Target) (string, error) {
	if t.Service.ActiveDeployment != "" {
		return t.Service.ActiveDeployment, nil
	}
	var ds []api.Deployment
	if err := t.C.Do(ctx, "GET", svcPath(t)+"/deployments?limit=1", nil, &ds); err != nil {
		return "", err
	}
	if len(ds) == 0 {
		return "", fmt.Errorf("%s has no deployment yet. Run `5050 up` first", t.Service.Config.Name)
	}
	return ds[0].ID, nil
}

func (a *App) deploymentAction(use, short, action string, confirm bool) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			did, err := activeDeployment(ctx, t)
			if err != nil {
				return err
			}
			if confirm {
				if err := a.confirm(fmt.Sprintf("%s the deployment of %s?", strings.ToUpper(action[:1])+action[1:], t.Service.Config.Name)); err != nil {
					return err
				}
			}
			var d api.Deployment
			if err := t.C.Do(ctx, "POST", "/deployments/"+did+"/"+action, nil, &d); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(d)
			}
			a.printf("%s: deployment %s is %s\n", t.Service.Config.Name, d.ID, d.Label)
			return nil
		},
	}
}

func (a *App) cmdRedeploy() *cobra.Command {
	return a.deploymentAction("redeploy", "Redeploy the active deployment", "redeploy", true)
}

func (a *App) cmdRestart() *cobra.Command {
	return a.deploymentAction("restart", "Restart the active deployment", "restart", true)
}

func (a *App) cmdDown() *cobra.Command {
	return a.deploymentAction("down", "Remove the active deployment", "remove", true)
}

func (a *App) cmdDeploy() *cobra.Command {
	var tmpl string
	var vars []string
	c := &cobra.Command{
		Use:   "deploy --template ID",
		Short: "Add a template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tmpl == "" {
				return errors.New("pass --template ID (see `5050 templates list`)")
			}
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			in, err := parseKV(vars)
			if err != nil {
				return err
			}
			var r api.TemplateDeployResponse
			if err := t.C.Do(ctx, "POST", "/environments/"+t.Env.ID+"/templates/"+tmpl+"/deploy", api.TemplateDeployRequest{Inputs: in}, &r); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(r)
			}
			for _, s := range r.Services {
				a.printf("Added %s\n", s.Config.Name)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&tmpl, "template", "t", "", "template id")
	c.Flags().StringArrayVarP(&vars, "variable", "v", nil, "template input KEY=VALUE")
	return c
}

func (a *App) cmdTemplates() *cobra.Command {
	list := func(cmd *cobra.Command, query string) error {
		cl, _, _, err := a.client()
		if err != nil {
			return err
		}
		var ts []api.TemplateSummary
		if err := cl.Do(cmd.Context(), "GET", "/templates"+q("q", query), nil, &ts); err != nil {
			return err
		}
		if a.JSON {
			return a.printJSON(ts)
		}
		rows := [][]string{{"ID", "NAME", "CATEGORY", "TAGLINE"}}
		for _, t := range ts {
			rows = append(rows, []string{t.ID, t.Name, t.Category, t.Tagline})
		}
		a.table(a.Out, rows)
		return nil
	}
	c := &cobra.Command{
		Use:   "templates",
		Short: "List and search templates",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return list(cmd, "") },
	}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List templates", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return list(cmd, "") },
	}, &cobra.Command{
		Use: "search QUERY", Short: "Search templates", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return list(cmd, args[0]) },
	})
	return c
}

// databaseTemplates maps the -d names to template ids.
var databaseTemplates = map[string]string{
	"postgres": "postgres", "postgresql": "postgres", "pg": "postgres",
	"mysql": "mysql", "redis": "redis", "mongo": "mongodb", "mongodb": "mongodb",
}

func (a *App) cmdAdd() *cobra.Command {
	var db, repo, image, empty string
	var vars []string
	c := &cobra.Command{
		Use:   "add",
		Short: "Add a service: -d/--database postgres|mysql|redis|mongo, --repo owner/name, --image nginx:1.27, --empty NAME",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// -s names the new service here, not an existing one.
			newName := a.Service
			a.Service = ""
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			vm, err := parseKV(vars)
			if err != nil {
				return err
			}
			req := api.CreateServiceRequest{Variables: vm, DeployNow: true}
			switch {
			case db != "":
				id, ok := databaseTemplates[strings.ToLower(db)]
				if !ok {
					return fmt.Errorf("unknown database %q: use postgres, mysql, redis or mongo", db)
				}
				req.Template = id
			case repo != "":
				req.Source = api.SourceConfig{Type: "github", Repo: repo}
			case image != "":
				req.Source = api.SourceConfig{Type: "image", Image: image}
			case empty != "":
				req.Name = empty
				req.Source = api.SourceConfig{Type: "empty"}
			default:
				return errors.New("pass one of --database, --repo, --image or --empty NAME")
			}
			if newName != "" && req.Name == "" {
				req.Name = newName
			}
			var r api.CreateServiceResponse
			if err := t.C.Do(ctx, "POST", "/environments/"+t.Env.ID+"/services", req, &r); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(r)
			}
			for _, s := range r.Services {
				a.printf("Added %s\n", s.Config.Name)
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVarP(&db, "database", "d", "", "postgres, mysql, redis or mongo")
	f.StringVar(&repo, "repo", "", "GitHub repository owner/name")
	f.StringVar(&image, "image", "", "Docker image, for example nginx:1.27")
	f.StringVar(&empty, "empty", "", "create an empty service with this name")
	f.StringArrayVarP(&vars, "variable", "v", nil, "variable KEY=VALUE")
	return c
}

func (a *App) printImport(im api.Import) error {
	if a.JSON {
		return a.printJSON(im)
	}
	a.printf("Import %s: %s\n", im.ID, im.Status)
	if im.Error != "" {
		a.printf("%s\n", im.Error)
	}
	for _, it := range im.Report.Landed {
		a.printf("  imported %s %s %s\n", it.Kind, it.Name, it.Detail)
	}
	for _, it := range im.Report.Skipped {
		a.printf("  skipped  %s %s: %s\n", it.Kind, it.Name, it.Detail)
	}
	if im.Status != "failed" {
		a.printf("The changes are staged. Review and deploy them in the console or with `5050 environment`.\n")
	}
	return nil
}

func (a *App) cmdImport() *cobra.Command {
	c := &cobra.Command{
		Use:   "import",
		Short: "Import: railway --token T (staged project), compose FILE, heroku-env FILE",
	}
	var token, pname string
	rw := &cobra.Command{
		Use: "railway", Short: "Import a Railway project as a staged project", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = a.env("RAILWAY_TOKEN")
			}
			if token == "" {
				return errors.New("pass --token with a Railway project token")
			}
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			var im api.Import
			if err := cl.Do(cmd.Context(), "POST", "/imports/railway", api.RailwayImportRequest{Token: token, ProjectName: pname}, &im); err != nil {
				return err
			}
			return a.printImport(im)
		},
	}
	rw.Flags().StringVar(&token, "token", "", "Railway project token (used once, never stored)")
	rw.Flags().StringVar(&pname, "name", "", "name of the new project")
	compose := &cobra.Command{
		Use: "compose FILE", Short: "Import a Docker Compose file into the environment", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			var im api.Import
			if err := t.C.DoRaw(ctx, "POST", "/environments/"+t.Env.ID+"/imports/compose", f, "application/yaml", &im); err != nil {
				return err
			}
			return a.printImport(im)
		},
	}
	heroku := &cobra.Command{
		Use: "heroku-env FILE", Short: "Import Heroku config vars into the service", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			var im api.Import
			if err := t.C.DoRaw(ctx, "POST", svcPath(t)+"/imports/heroku-env", f, "text/plain", &im); err != nil {
				return err
			}
			return a.printImport(im)
		},
	}
	c.AddCommand(rw, compose, heroku)
	return c
}
