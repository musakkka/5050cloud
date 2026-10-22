// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/github"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
)

// GitHubStatus is GET /github.
func (c *Core) GitHubStatus(ctx context.Context) (api.GitHubStatus, error) {
	st := api.GitHubStatus{Installations: []string{}}
	app, err := c.githubApp(ctx)
	if err != nil || app == nil {
		return st, err
	}
	st.AppConfigured, st.AppSlug, st.InstallURL = true, app.Slug, github.InstallURL(app.Slug)
	insts, err := c.gh.Installations(ctx, *app)
	if err != nil {
		c.log.Warn("cannot list GitHub App installations", "err", err)
		return st, nil
	}
	for _, in := range insts {
		st.Installations = append(st.Installations, in.Account)
	}
	if len(insts) > 0 {
		st.ConfigureURL = "https://github.com/settings/installations/" + itoa(int(insts[0].ID))
	}
	return st, nil
}

var appStates = struct {
	sync.Mutex
	m map[string]appState
}{m: map[string]appState{}}

type appState struct {
	returnTo string
	exp      time.Time
}

// CreateGitHubApp starts the manifest flow.
func (c *Core) CreateGitHubApp(ctx context.Context, req api.GitHubAppRequest) (api.GitHubAppResponse, error) {
	in, _ := c.installRow(ctx, c.st.Reader)
	state := auth.NewToken(16)
	ret := req.ReturnTo
	if !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") {
		ret = "/settings/github"
	}
	appStates.Lock()
	appStates.m[state] = appState{returnTo: ret, exp: time.Now().Add(time.Hour)}
	appStates.Unlock()
	return api.GitHubAppResponse{FormURL: github.FormURL(req.Owner, state), Manifest: github.Manifest(in.Name, c.ConsoleURL(ctx), ret)}, nil
}

// GitHubAppCallback converts the manifest code and stores the app; it returns the console path to redirect to.
func (c *Core) GitHubAppCallback(ctx context.Context, code, state string) (string, error) {
	appStates.Lock()
	st, ok := appStates.m[state]
	delete(appStates.m, state)
	appStates.Unlock()
	if !ok || time.Now().After(st.exp) {
		return "", fail.Validation("state", "This GitHub App link expired. Start again from Settings > GitHub.")
	}
	app, err := c.gh.ConvertManifest(ctx, code)
	if err != nil {
		return "", fail.Unavailable("GitHub did not accept the app manifest: %s", err.Error())
	}
	b, _ := json.Marshal(app)
	if err := c.st.Write(ctx, func(tx *sql.Tx) error {
		return c.putSetting(ctx, tx, "github_app", map[string]any{"slug": app.Slug, "id": app.ID}, b)
	}); err != nil {
		return "", err
	}
	sep := "?"
	if strings.Contains(st.returnTo, "?") {
		sep = "&"
	}
	return st.returnTo + sep + "githubApp=" + app.Slug + "&install=" + github.InstallURL(app.Slug), nil
}

// GitHubRepos lists repositories of every installation.
func (c *Core) GitHubRepos(ctx context.Context, q string) ([]api.GitHubRepo, error) {
	out := []api.GitHubRepo{}
	app, err := c.githubApp(ctx)
	if err != nil || app == nil {
		return out, err
	}
	insts, err := c.gh.Installations(ctx, *app)
	if err != nil {
		return nil, fail.Unavailable("GitHub could not be reached: %s", err.Error())
	}
	q = lower(trim(q))
	for _, in := range insts {
		tok, err := c.gh.InstallationToken(ctx, *app, in.ID)
		if err != nil {
			return nil, fail.Unavailable("GitHub refused an installation token: %s", err.Error())
		}
		repos, err := c.gh.Repos(ctx, tok, in.ID)
		if err != nil {
			return nil, fail.Unavailable("GitHub could not list repositories: %s", err.Error())
		}
		for _, r := range repos {
			if q == "" || strings.Contains(lower(r.FullName), q) {
				out = append(out, api.GitHubRepo{FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch, UpdatedAt: r.UpdatedAt})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// GitHubBranches lists branches of a repository (App or public).
func (c *Core) GitHubBranches(ctx context.Context, repo string) ([]string, error) {
	ref, ok := github.ParseRepo(repo)
	if !ok {
		return nil, fail.Validation("repo", "%q is not a GitHub repository.", repo)
	}
	tok, _ := c.GitHubCloneToken(ctx, repo)
	if tok != "" {
		b, err := c.gh.Branches(ctx, tok, ref.FullName)
		if err == nil {
			return b, nil
		}
	}
	heads, err := c.git.Heads(ctx, ref.CloneURL())
	if err != nil {
		return nil, fail.Unavailable("Cannot read the branches of %s: %s", ref.FullName, err.Error())
	}
	return heads, nil
}

// GitHubScan is POST /github/scan.
func (c *Core) GitHubScan(ctx context.Context, req api.ScanRequest) (api.ScanResult, error) {
	ref, ok := github.ParseRepo(req.Repo)
	if !ok {
		return api.ScanResult{}, fail.Validation("repo", "%q is not a GitHub repository.", req.Repo)
	}
	tok, _ := c.GitHubCloneToken(ctx, req.Repo)
	r, err := github.ScanRemote(ctx, c.gh, c.git, ref, tok, req.Branch, req.RootDir)
	if err != nil {
		return api.ScanResult{}, fail.Unavailable("Cannot scan %s: %s", ref.FullName, err.Error())
	}
	RememberScan(req.Repo, req.Branch, req.RootDir, r)
	return r, nil
}

// GitHubWebhook handles a push event (signature verified here).
func (c *Core) GitHubWebhook(ctx context.Context, event, signature string, body []byte) error {
	app, err := c.githubApp(ctx)
	if err != nil {
		return err
	}
	if app == nil || !github.VerifySignature(app.WebhookSecret, body, signature) {
		return fail.Unauthorized("The webhook signature is not valid.")
	}
	if event != "push" {
		return nil
	}
	p, err := github.ParsePush(body)
	if err != nil {
		return fail.Validation("body", "The push payload could not be read.")
	}
	if p.Branch == "" || p.Deleted || c.orch == nil {
		return nil
	}
	insts, err := c.AllInstances(ctx)
	if err != nil {
		return err
	}
	for _, in := range insts {
		src := in.Config.Source
		if src.Type != "github" || !github.SameRepo(src.Repo, p.Repo) || (src.AutoDeploy != nil && !*src.AutoDeploy) {
			continue
		}
		branch := src.Branch
		if branch == "" {
			branch = p.DefaultBranch
		}
		if branch != p.Branch {
			continue
		}
		files := p.Files
		if p.Truncated && !p.FirstPush() {
			if tok, _ := c.GitHubCloneToken(ctx, p.Repo); tok != "" {
				if f, err := c.gh.CompareFiles(ctx, tok, p.Repo, p.Before, p.After); err == nil {
					files = f
				}
			}
		}
		watch := in.Config.Build.WatchPaths
		var overlay string
		if err := c.st.Reader.QueryRowContext(ctx, `SELECT overlay FROM config_files WHERE service_id = ? AND environment_id = ?`, in.ServiceID, in.EnvID).Scan(&overlay); err == nil {
			var rc manifest.RailwayConfig
			if json.Unmarshal([]byte(overlay), &rc) == nil && rc.Build != nil && len(rc.Build.WatchPatterns) > 0 {
				watch = rc.Build.WatchPatterns
			}
		}
		skip := (!p.FirstPush() && !p.Forced) && len(watch) > 0 && !github.MatchWatch(watch, files)
		commit := &api.Commit{SHA: p.Head.SHA, Author: p.Head.Author, Message: p.Head.Message, Branch: p.Branch, URL: p.Head.URL, Time: p.Head.Time}
		if commit.SHA == "" {
			commit.SHA = p.After
		}
		if _, err := c.orch.Deploy(ctx, cpapi.DeployInput{EnvID: in.EnvID, ServiceID: in.ServiceID, Trigger: "github", Actor: "github:" + p.Head.Author,
			Message: p.Head.Message, Commit: commit, Rebuild: true, Skipped: skip}); err != nil {
			c.log.Warn("cannot deploy a pushed commit", "service", in.ServiceID, "err", err)
		}
	}
	return nil
}
