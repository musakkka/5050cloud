// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"io"
	"net/http"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
)

var connectKinds = map[string][3]string{
	"postgres": {"psql", "DATABASE_URL", "DATABASE_PUBLIC_URL"},
	"pgvector": {"psql", "DATABASE_URL", "DATABASE_PUBLIC_URL"},
	"postgis":  {"psql", "DATABASE_URL", "DATABASE_PUBLIC_URL"},
	"mysql":    {"mysql", "MYSQL_URL", "MYSQL_PUBLIC_URL"},
	"redis":    {"redis-cli", "REDIS_URL", "REDIS_PUBLIC_URL"},
	"mongodb":  {"mongosh", "MONGO_URL", "MONGO_PUBLIC_URL"},
}

func connectInfoFor(svc api.Service, env map[string]string) (api.ConnectInfo, error) {
	k, ok := connectKinds[svc.Config.Template]
	if !ok {
		return api.ConnectInfo{}, fail.Validation("service", "%s is not a database service.", svc.Config.Name)
	}
	ci := api.ConnectInfo{Client: k[0], PrivateVariable: k[1], PrivateURL: vars.MaskURL(env[k[1]]), CLICommand: "5050 connect " + quoteName(svc.Config.Name)}
	if svc.Config.Net.PublicAccess || len(svc.TCPProxies) > 0 {
		ci.PublicVariable, ci.PublicURL = k[2], vars.MaskURL(env[k[2]])
	}
	return ci, nil
}

func quoteName(n string) string {
	for _, r := range n {
		if r == ' ' {
			return "\"" + n + "\""
		}
	}
	return n
}

func (s *Server) templateRoutes(add func(string, handler)) {
	c := s.C
	ctxOf := func(x *Ctx) context.Context { return x.R.Context() }
	add("GET /templates", func(x *Ctx) (any, error) { return c.Templates(x.q("q")), nil })
	add("GET /templates/{id}", func(x *Ctx) (any, error) { return c.Template(x.p("id")) })
	add("POST "+pEnv+"/templates/{id}/deploy", func(x *Ctx) (any, error) {
		req, err := body[api.TemplateDeployRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = x.p("id")
		return c.TemplateDeploy(ctxOf(x), x.A, x.p("eid"), x.p("id"), req)
	})
	add("GET /icons/{name}", func(x *Ctx) (any, error) {
		b, ok := c.Icon(x.p("name"))
		if !ok {
			return nil, fail.NotFound("There is no icon %q.", x.p("name"))
		}
		x.W.Header().Set("Cache-Control", "public, max-age=86400")
		return rawBody{"image/svg+xml", b}, nil
	})
	add("GET /github", func(x *Ctx) (any, error) { return c.GitHubStatus(ctxOf(x)) })
	add("POST /github/app", func(x *Ctx) (any, error) {
		req, err := body[api.GitHubAppRequest](x)
		if err != nil {
			return nil, err
		}
		return c.CreateGitHubApp(ctxOf(x), req)
	})
	add("GET /github/app/callback", func(x *Ctx) (any, error) {
		to, err := c.GitHubAppCallback(ctxOf(x), x.q("code"), x.q("state"))
		if err != nil {
			return nil, err
		}
		http.Redirect(x.W, x.R, to, http.StatusFound)
		return handled{}, nil
	})
	add("GET /github/repos", func(x *Ctx) (any, error) { return c.GitHubRepos(ctxOf(x), x.q("q")) })
	add("GET /github/branches", func(x *Ctx) (any, error) { return c.GitHubBranches(ctxOf(x), x.q("repo")) })
	add("POST /github/scan", func(x *Ctx) (any, error) {
		req, err := body[api.ScanRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = req.Repo
		return c.GitHubScan(ctxOf(x), req)
	})
	add("POST /github/webhook", func(x *Ctx) (any, error) {
		b, err := io.ReadAll(http.MaxBytesReader(x.W, x.R.Body, 25<<20))
		if err != nil {
			return nil, fail.Validation("body", "The payload is too large.")
		}
		x.Detail = x.R.Header.Get("X-GitHub-Event")
		return nil, c.GitHubWebhook(ctxOf(x), x.R.Header.Get("X-GitHub-Event"), x.R.Header.Get("X-Hub-Signature-256"), b)
	})
	add("GET /projects/{pid}/webhooks", func(x *Ctx) (any, error) { return c.Webhooks(ctxOf(x), x.p("pid")) })
	add("POST /projects/{pid}/webhooks", func(x *Ctx) (any, error) {
		req, err := body[api.Webhook](x)
		if err != nil {
			return nil, err
		}
		return c.CreateWebhook(ctxOf(x), x.p("pid"), req)
	})
	add("PATCH /webhooks/{id}", func(x *Ctx) (any, error) {
		req, err := body[api.Webhook](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateWebhook(ctxOf(x), x.p("id"), req)
	})
	add("DELETE /webhooks/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteWebhook(ctxOf(x), x.p("id")) })
	add("POST /webhooks/{id}/test", func(x *Ctx) (any, error) { return c.TestWebhook(ctxOf(x), x.p("id")) })
}
