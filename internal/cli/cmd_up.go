// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

func terminal(s api.DeploymentStatus) bool {
	switch s {
	case api.DepSuccess, api.DepFailed, api.DepCrashed, api.DepCompleted, api.DepSkipped, api.DepRemoved, api.DepCancelled:
		return true
	}
	return false
}

func failed(s api.DeploymentStatus) bool {
	return s == api.DepFailed || s == api.DepCrashed || s == api.DepCancelled || s == api.DepRemoved
}

// archiveRoot works out the archive root and the subdirectory to include.
func archiveRoot(linkDir, cwd string, args []string, pathAsRoot bool) (root, only string, err error) {
	root = linkDir
	if root == "" {
		root = cwd
	}
	if len(args) == 0 {
		return root, "", nil
	}
	p := args[0]
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	st, err := os.Stat(p)
	if err != nil {
		return "", "", err
	}
	if !st.IsDir() {
		return "", "", fmt.Errorf("%s is not a directory", args[0])
	}
	if pathAsRoot {
		return p, "", nil
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p, "", nil //nolint:nilerr // outside the linked directory: it is its own root
	}
	if rel == "." {
		rel = ""
	}
	return root, rel, nil
}

func (a *App) cmdUp() *cobra.Command {
	var detach, ci, isNew, noGitignore, pathAsRoot bool
	var message, name string
	c := &cobra.Command{
		Use:   "up [PATH]",
		Short: "Pack the directory (respects .gitignore and .5050ignore), upload, deploy into the linked service (creates one with --new), stream build and deploy logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, !isNew)
			if err != nil {
				return err
			}
			root, only, err := archiveRoot(t.LinkDir, a.cwd(), args, pathAsRoot)
			if err != nil {
				return err
			}
			if isNew {
				if name == "" {
					name = filepath.Base(filepath.Join(root, only))
				}
				var cr api.CreateServiceResponse
				req := api.CreateServiceRequest{Name: name, Source: api.SourceConfig{Type: "upload"}, DeployNow: true}
				if err := t.C.Do(ctx, "POST", "/environments/"+t.Env.ID+"/services", req, &cr); err != nil {
					return err
				}
				if len(cr.Services) == 0 {
					return fmt.Errorf("the server did not create the service")
				}
				t.Service = &cr.Services[0]
				fmt.Fprintf(a.Err, "Created service %s\n", t.Service.Config.Name)
				// Link the new service when this directory is linked without one,
				// so later commands (up, logs, domain) target it.
				if _, link, dir, err := a.client(); err == nil && dir != "" && link.Service == "" && a.Project == "" {
					link.Service = t.Service.ID
					if link.Environment == "" {
						link.Environment = t.Env.ID
					}
					_ = a.saveLink(dir, link)
				}
			}
			tmp, err := os.CreateTemp("", "5050-up-*.tar.gz")
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			defer tmp.Close()
			if _, _, err := Pack(PackOptions{Root: root, Only: only, NoGitignore: noGitignore}, tmp); err != nil {
				return err
			}
			size, _ := tmp.Seek(0, io.SeekCurrent)
			if _, err := tmp.Seek(0, io.SeekStart); err != nil {
				return err
			}
			var up api.UploadResponse
			if err := t.C.DoRaw(ctx, "POST", svcPath(t)+"/uploads"+q("message", message), tmp, "application/gzip", &up); err != nil {
				return err
			}
			fmt.Fprintf(a.Err, "Uploaded %s\n", humanBytes(size))
			if detach {
				if a.JSON {
					return a.printJSON(up)
				}
				a.printf("Deployment %s started. Follow it with `5050 logs %s`\n", up.DeploymentID, up.DeploymentID)
				return nil
			}
			return a.followDeployment(ctx, t, up.DeploymentID, !ci)
		},
	}
	f := c.Flags()
	f.BoolVarP(&detach, "detach", "d", false, "do not wait for the build and deploy")
	f.BoolVarP(&ci, "ci", "c", false, "stream build logs only, then exit with the result")
	f.StringVarP(&message, "message", "m", "", "deployment message")
	f.BoolVar(&isNew, "new", false, "create a new service for this upload")
	f.StringVar(&name, "name", "", "name of the new service (with --new)")
	f.BoolVar(&noGitignore, "no-gitignore", false, "include files listed in .gitignore")
	f.BoolVar(&pathAsRoot, "path-as-root", false, "use PATH as the archive root instead of the linked directory")
	return c
}

// followDeployment streams build logs, waits for the result, prints the first
// deploy lines and the URL, or the diagnosis with exit code 1.
func (a *App) followDeployment(ctx context.Context, t *Target, did string, deployLogs bool) error {
	_ = t.C.SSE(ctx, "/deployments/"+did+"/build-logs?follow=1", func(_ string, data []byte) bool {
		a.printLines(data)
		return true
	})
	var d api.DeploymentDetail
	for {
		if err := t.C.Do(ctx, "GET", "/deployments/"+did, nil, &d); err != nil {
			return err
		}
		if terminal(d.Status) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if failed(d.Status) || d.Status == api.DepSkipped {
		msg := d.Diagnosis
		if msg == "" {
			msg = d.Reason
		}
		if msg == "" {
			msg = "The deployment ended as " + api.StatusLabel(d.Status, d.Active) + "."
		}
		if a.JSON {
			_ = a.printJSON(d)
		}
		return &exitError{code: 1, msg: msg}
	}
	if deployLogs && !a.JSON {
		var lr api.LogsResponse
		if err := t.C.Do(ctx, "GET", "/logs"+q("environment", t.Env.ID, "deployment", did, "type", "deploy", "limit", "20"), nil, &lr); err == nil {
			for _, l := range lr.Lines {
				a.printLine(l)
			}
		}
	}
	var s api.Service
	_ = t.C.Do(ctx, "GET", svcPath(t), nil, &s)
	if a.JSON {
		return a.printJSON(d)
	}
	if u := serviceURL(&s); u != "" {
		a.printf("Deployed: %s\n", u)
	} else {
		a.printf("Deployed: %s (no public domain yet; add one with `5050 domain`)\n", t.Service.Config.Name)
	}
	return nil
}

// printLines prints an SSE data payload that is a []LogLine or one LogLine.
func (a *App) printLines(data []byte) {
	var lines []api.LogLine
	if json.Unmarshal(data, &lines) != nil {
		var one api.LogLine
		if json.Unmarshal(data, &one) != nil {
			return
		}
		lines = []api.LogLine{one}
	}
	for _, l := range lines {
		a.printLine(l)
	}
}

func (a *App) printLine(l api.LogLine) {
	if a.JSON {
		b, _ := json.Marshal(l)
		fmt.Fprintln(a.Out, string(b))
		return
	}
	fmt.Fprintln(a.Out, strings.TrimRight(l.Msg, "\n"))
}
