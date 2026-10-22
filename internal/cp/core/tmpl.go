// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"strings"
	"sync"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/templates"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

var catalogOnce struct {
	sync.Mutex
	c map[*Core]*templates.Catalog
}

// Catalog returns the parsed templates.
func (c *Core) Catalog() *templates.Catalog {
	catalogOnce.Lock()
	defer catalogOnce.Unlock()
	if catalogOnce.c == nil {
		catalogOnce.c = map[*Core]*templates.Catalog{}
	}
	if cat, ok := catalogOnce.c[c]; ok {
		return cat
	}
	cat, err := templates.Load(c.templates)
	if err != nil {
		c.log.Error("cannot load templates", "err", err)
		cat, _ = templates.Load(nil)
	}
	catalogOnce.c[c] = cat
	return cat
}

// Templates is GET /templates.
func (c *Core) Templates(q string) []api.TemplateSummary { return c.Catalog().List(q) }

// Template is GET /templates/{id}.
func (c *Core) Template(id string) (api.TemplateDetail, error) {
	t, ok := c.Catalog().Get(id)
	if !ok {
		return api.TemplateDetail{}, fail.NotFound("There is no template %q.", id)
	}
	return templates.Detail(t), nil
}

// Icon is GET /icons/{name}.
func (c *Core) Icon(name string) ([]byte, bool) { return c.Catalog().Icon(name) }

func (c *Core) createFromTemplate(ctx context.Context, a Actor, envID string, req api.CreateServiceRequest) (api.CreateServiceResponse, error) {
	r, err := c.TemplateDeploy(ctx, a, envID, req.Template, api.TemplateDeployRequest{Inputs: req.TemplateVariables, MajorVersion: req.TemplateVersion})
	if err != nil {
		return api.CreateServiceResponse{}, err
	}
	return api.CreateServiceResponse{Services: r.Services, Deployments: r.Deployments, ChangesCount: c.changesCount(ctx, envID)}, nil
}

// TemplateDeploy creates every service of a template, committed, and deploys them in dependency order.
func (c *Core) TemplateDeploy(ctx context.Context, a Actor, envID, id string, req api.TemplateDeployRequest) (api.TemplateDeployResponse, error) {
	t, ok := c.Catalog().Get(id)
	if !ok {
		return api.TemplateDeployResponse{}, fail.NotFound("There is no template %q.", id)
	}
	env, _, err := c.Environment(ctx, envID)
	if err != nil {
		return api.TemplateDeployResponse{}, err
	}
	major := req.MajorVersion
	if major != "" && !contains(t.MajorVersions, major) {
		return api.TemplateDeployResponse{}, fail.Validation("majorVersion", "%s offers versions %s.", t.Name, strings.Join(t.MajorVersions, ", "))
	}
	inputs := map[string]string{}
	for _, in := range t.Inputs {
		v := trim(req.Inputs[in.Name])
		if v == "" {
			v = in.Default
		}
		if v == "" && in.Generate != "" {
			g, err := vars.Generate(in.Generate)
			if err != nil {
				return api.TemplateDeployResponse{}, err
			}
			v = g
		}
		if v == "" && in.Required {
			return api.TemplateDeployResponse{}, fail.Validation("inputs."+in.Name, "%s is required.", in.Label)
		}
		inputs[in.Name] = v
	}
	order := templates.Ordered(t)
	sids := map[string]string{}
	finalName := map[string]string{}
	var plan cpapi.CommitPlan
	var domainFor []string
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		taken, err := c.takenNames(ctx, tx, envID, "")
		if err != nil {
			return err
		}
		for _, sv := range order {
			n := changes.UniqueName(truncName(sv.Config.Name), taken)
			if sv.Config.Name == "" {
				n = changes.UniqueName(truncName(sv.Name), taken)
			}
			taken[n] = true
			finalName[sv.Name] = n
		}
		pos, _ := c.positions(ctx, tx, envID)
		x := float64(len(pos)) * 40
		for i, sv := range order {
			cfg := changes.Clone(sv.Config)
			cfg.Name = finalName[sv.Name]
			cfg.Template = t.ID
			if major != "" {
				if img, ok := sv.Images[major]; ok {
					cfg.Source.Image = img
				}
				cfg.TemplateVersion = major
			}
			if cfg.Icon == "" {
				cfg.Icon = t.ID
			}
			if cfg.Source.Type == "" {
				cfg.Source.Type = "image"
			}
			values := map[string]string{}
			for k, v := range sv.Variables {
				ev, err := vars.EvalFunctions(v, inputs)
				if err != nil {
					return fail.Validation("variables."+k, "The template value of %s is not valid: %s.", k, err.Error())
				}
				for orig, final := range finalName {
					if orig != final {
						if nv, ok := vars.RewriteService(ev, orig, final); ok {
							ev = nv
						}
					}
				}
				values[k] = ev
			}
			if err := changes.Validate(cfg, sv.Volume != nil); err != nil {
				return err
			}
			sid := ids.New(ids.Service)
			sids[sv.Name] = sid
			p, err := c.createCommitted(ctx, tx, a, env, sid, cfg, values, "Deploy template "+t.Name)
			if err != nil {
				return err
			}
			if plan.CommitID == "" {
				plan = p
			} else {
				plan.Items = append(plan.Items, p.Items...)
			}
			if sv.Volume != nil {
				if err := c.syncVolume(ctx, tx, envID, sid, nil, &changes.VolumeSpec{Name: changes.Slug(cfg.Name) + "-volume", MountPath: sv.Volume.MountPath}); err != nil {
					return err
				}
			}
			for _, m := range sv.Monitors {
				if _, err := tx.ExecContext(ctx, `INSERT INTO monitors (id, environment_id, service_id, metric, threshold, unit, for_minutes, channels, state, created_at) VALUES (?, ?, ?, ?, ?, 'pct', ?, '{}', 'ok', ?)`,
					ids.New(ids.Monitor), envID, sid, m.Metric, m.Threshold, m.ForMinutes, c.Now()); err != nil {
					return err
				}
			}
			if err := c.setPosition(ctx, tx, envID, sid, [2]float64{x + float64(i)*280, 120}); err != nil {
				return err
			}
			if sv.Domain {
				domainFor = append(domainFor, sid)
			}
		}
		c.touchProject(ctx, tx, envID)
		return nil
	})
	if err != nil {
		return api.TemplateDeployResponse{}, err
	}
	for _, sid := range domainFor {
		if _, err := c.CreateDomain(ctx, a, envID, sid, api.CreateDomainRequest{Kind: "generated"}); err != nil {
			c.log.Warn("cannot generate a domain for a template service", "service", sid, "err", err)
		}
	}
	resp := api.TemplateDeployResponse{Services: []api.Service{}, Deployments: []api.Deployment{}}
	if deps := c.applyPlan(ctx, plan); deps != nil {
		resp.Deployments = deps
	}
	c.recordCommitDeployments(ctx, plan.CommitID, resp.Deployments)
	for _, sv := range order {
		if svc, err := c.Service(ctx, envID, sids[sv.Name]); err == nil {
			resp.Services = append(resp.Services, svc)
			c.publishService(ctx, svc)
		}
	}
	return resp, nil
}
