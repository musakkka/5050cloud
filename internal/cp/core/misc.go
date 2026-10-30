// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/base32"
	"os"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/compose"
	"github.com/musakkka/5050cloud/pkg/manifest"
)

// ---------------------------------------------------------------- recovery key

// RecoveryKeyFor formats a master key as "5050R-" plus base32 in groups of 4.
func RecoveryKeyFor(key []byte) string {
	s := strings.TrimRight(base32.StdEncoding.EncodeToString(key), "=")
	var parts []string
	for len(s) > 4 {
		parts = append(parts, s[:4])
		s = s[4:]
	}
	parts = append(parts, s)
	return "5050R-" + strings.Join(parts, "-")
}

// ParseRecoveryKey reverses RecoveryKeyFor.
func ParseRecoveryKey(k string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(k), "5050R-"), "-", ""))
	for len(s)%8 != 0 {
		s += "="
	}
	b, err := base32.StdEncoding.DecodeString(s)
	if err != nil || len(b) != 32 {
		return nil, fail.Validation("recoveryKey", "This is not a 5050cloud recovery key.")
	}
	return b, nil
}

// RecoveryKey is POST /settings/recovery-key (Owner, password confirmed).
func (c *Core) RecoveryKey(ctx context.Context, a Actor, password string) (api.RecoveryKeyResponse, error) {
	u, err := c.userByID(ctx, c.st.Reader, a.UserID)
	if err != nil {
		return api.RecoveryKeyResponse{}, err
	}
	if !c.passwordOK(u.PasswordHash, password) {
		return api.RecoveryKeyResponse{}, fail.Validation("password", "The password is not correct.")
	}
	key, err := os.ReadFile(c.keyPath)
	if err != nil {
		return api.RecoveryKeyResponse{}, fail.Unavailable("The master key file cannot be read on this host.")
	}
	return api.RecoveryKeyResponse{RecoveryKey: RecoveryKeyFor(key)}, nil
}

// ---------------------------------------------------------------- search

// Search is GET /search: projects, environments and services by name, and settings pages.
func (c *Core) Search(ctx context.Context, a Actor, q string) (api.SearchResult, error) {
	res := api.SearchResult{Projects: []api.SearchItem{}, Services: []api.SearchItem{}, Environments: []api.SearchItem{}, Settings: []api.SearchItem{}, Deployments: []api.SearchItem{}}
	q = lower(trim(q))
	if q == "" {
		return res, nil
	}
	projects, err := c.Projects(ctx, a, "name")
	if err != nil {
		return res, err
	}
	for _, p := range projects {
		if strings.Contains(lower(p.Name), q) {
			res.Projects = append(res.Projects, api.SearchItem{ID: p.ID, Title: p.Name, Subtitle: p.Description, Path: "/project/" + p.ID})
		}
		for _, e := range p.Environments {
			if strings.Contains(lower(e.Name), q) {
				res.Environments = append(res.Environments, api.SearchItem{ID: e.ID, Title: e.Name, Subtitle: p.Name, Path: "/project/" + p.ID + "?environment=" + e.ID})
			}
			insts, err := c.instances(ctx, c.st.Reader, e.ID)
			if err != nil {
				continue
			}
			for _, in := range insts {
				if strings.Contains(lower(in.Config.Name), q) {
					res.Services = append(res.Services, api.SearchItem{ID: in.ServiceID, Title: in.Config.Name, Subtitle: p.Name + " / " + e.Name,
						Path: "/project/" + p.ID + "/service/" + in.ServiceID + "?environment=" + e.ID})
				}
			}
		}
	}
	for _, s := range []api.SearchItem{
		{ID: "general", Title: "General", Path: "/settings"}, {ID: "members", Title: "Members", Path: "/settings/members"},
		{ID: "hosts", Title: "Hosts", Path: "/settings/hosts"}, {ID: "backups", Title: "Backups", Path: "/settings/backups"},
		{ID: "email", Title: "Email", Path: "/settings/email"}, {ID: "github", Title: "GitHub", Path: "/settings/github"},
		{ID: "tokens", Title: "Tokens", Path: "/account/tokens"}, {ID: "audit", Title: "Audit log", Path: "/settings/audit"},
		{ID: "alerts", Title: "Alerts", Path: "/settings/alerts"}, {ID: "registry", Title: "Registry credentials", Path: "/settings/registry"},
	} {
		if strings.Contains(lower(s.Title), q) {
			res.Settings = append(res.Settings, s)
		}
	}
	return res, nil
}

// ---------------------------------------------------------------- export

// Export is GET /environments/{eid}/export: the environment as a 5050.yaml.
// Secret-looking and sealed values are written as empty strings.
func (c *Core) Export(ctx context.Context, envID string) ([]byte, error) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return nil, err
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return nil, err
	}
	vs := groupVars(rows)
	pf := manifest.ProjectFile{Project: b.prj.Name, Services: map[string]manifest.ServiceFile{}, Shared: map[string]string{}}
	clean := func(m map[string]changes.Var) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			if changes.IsSecret(k, v) {
				out[k] = ""
			} else {
				out[k] = v.Value
			}
		}
		return out
	}
	pf.Shared = clean(vs.Shared)
	for _, in := range b.insts {
		if !in.committed() {
			continue
		}
		sf := manifest.ServiceFile{ServiceConfig: in.Config, Variables: clean(vs.of(in.ServiceID))}
		for _, d := range b.domains[in.ServiceID] {
			if d.Kind == "custom" {
				sf.Domains = append(sf.Domains, d.Domain)
			}
		}
		if v, ok := b.vols[in.ServiceID]; ok {
			sf.Volume = &manifest.TemplateVolume{MountPath: v.MountPath}
		}
		pf.Services[in.Config.Name] = sf
	}
	return yaml.Marshal(pf)
}

// ---------------------------------------------------------------- environment sync

// SyncEnvironment stages the difference between from and envID (9.5).
func (c *Core) SyncEnvironment(ctx context.Context, a Actor, envID, from string) (api.ChangesResponse, error) {
	dst, _, err := c.Environment(ctx, envID)
	if err != nil {
		return api.ChangesResponse{}, err
	}
	srcEnv, _, err := c.Environment(ctx, from)
	if err != nil {
		return api.ChangesResponse{}, err
	}
	if srcEnv.ProjectID != dst.ProjectID || from == envID {
		return api.ChangesResponse{}, fail.Validation("from", "Pick another environment of the same project.")
	}
	var removed []string
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		load := func(env string) (map[string]changes.EnvService, map[string]changes.Var, error) {
			insts, err := c.instances(ctx, tx, env)
			if err != nil {
				return nil, nil, err
			}
			vrows, err := c.envVars(ctx, tx, env)
			if err != nil {
				return nil, nil, err
			}
			vs := groupVars(vrows)
			vols, err := c.volumes(ctx, tx, env)
			if err != nil {
				return nil, nil, err
			}
			out := map[string]changes.EnvService{}
			for _, in := range insts {
				if !in.committed() {
					continue
				}
				s := changes.EnvService{ID: in.ServiceID, Config: in.Config, Vars: vs.of(in.ServiceID)}
				if v, ok := vols[in.ServiceID]; ok {
					s.Volume = &changes.VolumeSpec{Name: v.Name, MountPath: v.MountPath}
				}
				out[in.ServiceID] = s
			}
			return out, vs.Shared, nil
		}
		src, srcShared, err := load(from)
		if err != nil {
			return err
		}
		cur, curShared, err := load(envID)
		if err != nil {
			return err
		}
		r := changes.Sync(src, cur, srcShared, curShared)
		for _, cr := range r.Creates {
			if err := c.insertStagedInstance(ctx, tx, a, envID, cr.Service.ID, changes.Clone(cr.Service.Config)); err != nil {
				return err
			}
			if !cr.Vars.Empty() || len(cr.Vars.NeedsValue) > 0 {
				ops := cr.Vars
				if err := c.stage(ctx, tx, envID, cr.Service.ID, changes.KindVariables, a.Label(), func(row *changeRow) { row.Vars = row.Vars.Merge(ops); row.Tag = "new" }); err != nil {
					return err
				}
			}
			if cr.Service.Volume != nil {
				vol := *cr.Service.Volume
				if err := c.stage(ctx, tx, envID, cr.Service.ID, changes.KindVolumeCreate, a.Label(), func(row *changeRow) {
					row.Patch = map[string]any{"name": vol.Name, "mountPath": vol.MountPath}
					row.Tag = "new"
				}); err != nil {
					return err
				}
			}
		}
		for _, ed := range r.Edits {
			if len(ed.Patch) > 0 {
				p := ed.Patch
				if err := c.stage(ctx, tx, envID, ed.ServiceID, changes.KindUpdate, a.Label(), func(row *changeRow) { row.Patch = changes.MergePatch(row.Patch, p); row.Tag = "edited" }); err != nil {
					return err
				}
			}
			if !ed.Vars.Empty() || len(ed.Vars.NeedsValue) > 0 {
				ops := ed.Vars
				if err := c.stage(ctx, tx, envID, ed.ServiceID, changes.KindVariables, a.Label(), func(row *changeRow) { row.Vars = row.Vars.Merge(ops); row.Tag = "edited" }); err != nil {
					return err
				}
			}
		}
		if !r.Shared.Empty() {
			ops := r.Shared
			if err := c.stage(ctx, tx, envID, "", changes.KindShared, a.Label(), func(row *changeRow) { row.Vars = row.Vars.Merge(ops); row.Tag = "edited" }); err != nil {
				return err
			}
		}
		removed = r.Removed
		return nil
	})
	if err != nil {
		return api.ChangesResponse{}, err
	}
	resp, err := c.Changes(ctx, envID)
	if err != nil {
		return resp, err
	}
	for _, sid := range removed {
		in, err := c.instance(ctx, c.st.Reader, envID, sid)
		if err == nil {
			resp.Candidates = append(resp.Candidates, api.Change{ID: "remove:" + sid, ServiceID: sid, Service: in.Config.Name, Kind: changes.KindDelete, Tag: "removed", Diff: []api.DiffEntry{}, Author: a.Label(), CreatedAt: c.Now()})
		}
	}
	c.publishChanges(ctx, envID)
	c.publishAll(ctx, envID)
	return resp, nil
}

// ---------------------------------------------------------------- compose import

// ImportCompose stages the services of a docker-compose file (9.22).
func (c *Core) ImportCompose(ctx context.Context, a Actor, envID string, data []byte) (api.Import, error) {
	env, _, err := c.Environment(ctx, envID)
	if err != nil {
		return api.Import{}, err
	}
	if len(data) > compose.MaxFileBytes {
		return api.Import{}, fail.Validation("body", "The compose file is larger than 1 MiB.")
	}
	p, err := compose.Parse(data, compose.Options{})
	if err != nil {
		return api.Import{}, fail.Validation("body", "%s", sentence(err.Error()))
	}
	imp := api.Import{ID: ids.New(ids.Import), Kind: "compose", ProjectID: env.ProjectID, EnvironmentID: envID, Status: "done", CreatedAt: c.Now(),
		Report: api.ImportReport{Landed: []api.ImportItem{}, Skipped: append([]api.ImportItem{}, p.Skipped...)}}
	type pending struct {
		sid string
		s   compose.Service
	}
	var templated []compose.Service
	var created []pending
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		taken, err := c.takenNames(ctx, tx, envID, "")
		if err != nil {
			return err
		}
		pos, _ := c.positions(ctx, tx, envID)
		x := float64(len(pos)) * 40
		for i, s := range p.Services {
			if s.Template != "" {
				templated = append(templated, s)
				continue
			}
			cfg := changes.Clone(s.Config)
			cfg.Name = changes.UniqueName(truncName(cleanName(s.Name)), taken)
			taken[cfg.Name] = true
			if cfg.Icon == "" {
				cfg.Icon = defaultIcon(cfg.Source)
			}
			sid := ids.New(ids.Service)
			if err := c.insertStagedInstance(ctx, tx, a, envID, sid, cfg); err != nil {
				return err
			}
			if len(s.Variables) > 0 {
				set := map[string]string{}
				for k, v := range s.Variables {
					if vars.CheckName(k) == nil {
						set[k] = v
					}
				}
				if err := c.stage(ctx, tx, envID, sid, changes.KindVariables, a.Label(), func(r *changeRow) { r.Vars = changes.VarOps{Set: set} }); err != nil {
					return err
				}
			}
			if s.Volume != nil {
				vol := *s.Volume
				if err := c.stage(ctx, tx, envID, sid, changes.KindVolumeCreate, a.Label(), func(r *changeRow) {
					r.Patch = map[string]any{"name": vol.Name, "mountPath": vol.MountPath}
				}); err != nil {
					return err
				}
			}
			if err := c.setPosition(ctx, tx, envID, sid, [2]float64{x + float64(i)*280, 320}); err != nil {
				return err
			}
			detail := "Staged"
			if len(s.Notes) > 0 {
				detail = strings.Join(s.Notes, " ")
			}
			imp.Report.Landed = append(imp.Report.Landed, api.ImportItem{Kind: "service", Name: cfg.Name, Detail: detail})
			created = append(created, pending{sid: sid, s: s})
		}
		return nil
	})
	if err != nil {
		return api.Import{}, err
	}
	for _, s := range templated {
		if _, err := c.TemplateDeploy(ctx, a, envID, s.Template, api.TemplateDeployRequest{MajorVersion: s.TemplateVersion}); err != nil {
			imp.Report.Skipped = append(imp.Report.Skipped, api.ImportItem{Kind: "database", Name: s.Name, Detail: sentence(err.Error())})
			continue
		}
		imp.Report.Landed = append(imp.Report.Landed, api.ImportItem{Kind: "database", Name: s.Name, Detail: "Created from the " + s.Template + " template"})
	}
	for _, p := range created {
		if p.s.TCPProxyPort > 0 {
			imp.Report.Landed = append(imp.Report.Landed, api.ImportItem{Kind: "tcp_proxy", Name: p.s.Name, Detail: "Add a TCP proxy to port " + itoa(p.s.TCPProxyPort) + " after the first deploy"})
		}
		if p.s.Domain {
			imp.Report.Landed = append(imp.Report.Landed, api.ImportItem{Kind: "domain", Name: p.s.Name, Detail: "Generate a domain after the first deploy"})
		}
	}
	if err := c.saveImport(ctx, imp); err != nil {
		return api.Import{}, err
	}
	c.publishChanges(ctx, envID)
	c.publishAll(ctx, envID)
	return imp, nil
}
