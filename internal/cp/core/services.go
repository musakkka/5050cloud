// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/github"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ---------------------------------------------------------------- environment bundle

// bundle is everything the service views of one environment need, read once.
type bundle struct {
	env     api.Environment
	prj     api.Project
	insts   []instance
	cs      changeSet
	rt      map[string]cpapi.Runtime
	domains map[string][]api.Domain
	proxies map[string][]api.TCPProxy
	vols    map[string]volumeRow
	files   map[string]configFileRow
	base    string // base domain or public IP for TCP proxy addresses
}

type configFileRow struct {
	CommitSHA string
	Path      string
	Format    string
	Lines     map[string]int
}

func (c *Core) loadBundle(ctx context.Context, envID string) (*bundle, error) {
	return c.loadBundleWith(ctx, envID, true)
}

// loadBundleConfig loads a bundle without runtime state. The cpapi.Config
// methods use it: the orchestrator's Runtime calls ServiceInstances, so
// asking it for runtime here would recurse without end.
func (c *Core) loadBundleConfig(ctx context.Context, envID string) (*bundle, error) {
	return c.loadBundleWith(ctx, envID, false)
}

func (c *Core) loadBundleWith(ctx context.Context, envID string, withRuntime bool) (*bundle, error) {
	env, prj, err := c.Environment(ctx, envID)
	if err != nil {
		return nil, err
	}
	b := &bundle{env: env, prj: prj}
	q := c.st.Reader
	if b.insts, err = c.instances(ctx, q, envID); err != nil {
		return nil, err
	}
	rows, err := c.envChanges(ctx, q, envID)
	if err != nil {
		return nil, err
	}
	b.cs = indexChanges(rows)
	if withRuntime {
		b.rt = c.runtime(ctx, envID)
	}
	if b.domains, err = c.envDomains(ctx, envID); err != nil {
		return nil, err
	}
	in, _ := c.installRow(ctx, q)
	b.base = in.BaseDomain
	if b.base == "" {
		b.base = in.PublicIP
	}
	if b.proxies, err = c.envProxies(ctx, q, envID, b.base); err != nil {
		return nil, err
	}
	if b.vols, err = c.volumes(ctx, q, envID); err != nil {
		return nil, err
	}
	b.files = map[string]configFileRow{}
	frows, err := store.All(ctx, q, func(r *sql.Rows) (struct {
		sid string
		f   configFileRow
	}, error) {
		var x struct {
			sid string
			f   configFileRow
		}
		var lines string
		err := r.Scan(&x.sid, &x.f.CommitSHA, &x.f.Path, &x.f.Format, &lines)
		_ = json.Unmarshal([]byte(lines), &x.f.Lines)
		return x, err
	}, `SELECT service_id, commit_sha, path, format, lines FROM config_files WHERE environment_id = ?`, envID)
	if err != nil {
		return nil, err
	}
	for _, x := range frows {
		b.files[x.sid] = x.f
	}
	return b, nil
}

func (b *bundle) find(sid string) (instance, bool) {
	for _, in := range b.insts {
		if in.ServiceID == sid {
			return in, true
		}
	}
	return instance{}, false
}

// stagedConfig is the committed config with staged edits and a rename applied.
func stagedConfig(in instance, cs changeSet) (api.ServiceConfig, string) {
	if !in.committed() {
		return in.Config, "create"
	}
	cfg := in.Config
	action := ""
	if r := cs.get(in.ServiceID, changes.KindUpdate); r != nil {
		if next, err := changes.Apply(cfg, r.Patch); err == nil {
			cfg = next
			action = "update"
		}
	}
	if r := cs.get(in.ServiceID, changes.KindRename); r != nil {
		if n, ok := r.Patch["name"].(string); ok {
			cfg.Name = n
			action = "rename"
		}
	}
	if cs.get(in.ServiceID, changes.KindDelete) != nil {
		action = "delete"
	}
	if action == "" {
		for _, k := range []string{changes.KindVariables, changes.KindVolumeCreate, changes.KindVolumeUpdate, changes.KindVolumeDelete} {
			if cs.get(in.ServiceID, k) != nil {
				action = "update"
			}
		}
	}
	return cfg, action
}

func (c *Core) serviceView(ctx context.Context, b *bundle, in instance) api.Service {
	s := api.Service{
		ID: in.ServiceID, ProjectID: in.ProjectID, EnvironmentID: in.EnvID, Config: in.Config,
		Kind: kindOf(in.Config), Status: api.StatusNoDeploy, CreatedAt: in.CreatedAt,
		Domains: b.domains[in.ServiceID], TCPProxies: b.proxies[in.ServiceID],
		Replicas: []api.Replica{}, ListeningPorts: []int{}, OutboundIPs: []string{},
		PrivateDomain: changes.PrivateName(in.Config) + ".internal",
	}
	if s.Domains == nil {
		s.Domains = []api.Domain{}
	}
	if s.TCPProxies == nil {
		s.TCPProxies = []api.TCPProxy{}
	}
	staged, action := stagedConfig(in, b.cs)
	if action != "" {
		cp := staged
		s.Staged, s.StagedAction = &cp, action
	}
	if !in.committed() {
		s.StatusText = "Staged: press Deploy to create it"
	}
	if r, ok := b.rt[in.ServiceID]; ok {
		if r.Status != "" {
			s.Status = r.Status
		}
		s.StatusText = r.StatusText
		s.ActiveDeployment = r.ActiveDeployment
		if r.Replicas != nil {
			s.Replicas = r.Replicas
		}
		if r.ListeningPorts != nil {
			s.ListeningPorts = r.ListeningPorts
		}
		if r.OutboundIPs != nil {
			s.OutboundIPs = r.OutboundIPs
		}
		s.Processes, s.ProcessesText, s.MonitorFiring = r.Processes, r.ProcessesText, r.MonitorFiring
	}
	if s.StatusText == "" && s.Status == api.StatusNoDeploy {
		s.StatusText = "No deployment yet"
	}
	if v, ok := b.vols[in.ServiceID]; ok {
		vol := api.Volume{ID: v.ID, ServiceID: in.ServiceID, Name: v.Name, MountPath: v.MountPath, HostID: v.HostID, AlertAtPct: v.AlertAtPct}
		if r, ok := b.rt[in.ServiceID]; ok {
			vol.UsedBytes, vol.HostLost = r.VolumeUsedBytes, r.VolumeHostLost
		}
		if b.cs.get(in.ServiceID, changes.KindVolumeDelete) != nil {
			vol.Staged = "delete"
		} else if b.cs.get(in.ServiceID, changes.KindVolumeUpdate) != nil {
			vol.Staged = "update"
		}
		s.Volume = &vol
	} else if r := b.cs.get(in.ServiceID, changes.KindVolumeCreate); r != nil {
		n, _ := r.Patch["name"].(string)
		mp, _ := r.Patch["mountPath"].(string)
		s.Volume = &api.Volume{ServiceID: in.ServiceID, Name: n, MountPath: mp, AlertAtPct: 80, Staged: "create"}
	}
	if f, ok := b.files[in.ServiceID]; ok {
		s.Source = map[string]string{}
		for field, line := range f.Lines {
			s.Source[field] = f.Path + ":" + itoa(line)
		}
		info := api.ConfigFileInfo{Path: f.Path, Format: f.Format, CommitSHA: f.CommitSHA}
		if in.Config.Source.Type == "github" && f.CommitSHA != "" {
			info.URL = githubFileURL(in.Config.Source.Repo, f.CommitSHA, path.Join(strings.TrimPrefix(in.Config.Source.RootDir, "/"), f.Path))
		}
		s.ConfigFile = &info
	}
	return s
}

func githubFileURL(repo, sha, file string) string {
	r := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(repo, "https://github.com/"), "http://github.com/"), ".git")
	if r == "" {
		return ""
	}
	return "https://github.com/" + r + "/blob/" + sha + "/" + strings.TrimPrefix(file, "/")
}

func itoa(n int) string { return strconv.Itoa(n) }

// Service is GET /environments/{eid}/services/{sid}.
func (c *Core) Service(ctx context.Context, envID, serviceID string) (api.Service, error) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return api.Service{}, err
	}
	in, ok := b.find(serviceID)
	if !ok {
		return api.Service{}, fail.NotFound("The service %s does not exist in environment %s.", serviceID, b.env.Name)
	}
	return c.serviceView(ctx, b, in), nil
}

// ---------------------------------------------------------------- create

// defaultName picks a service name from its source.
func defaultName(src api.SourceConfig) string {
	switch src.Type {
	case "github":
		r := strings.TrimSuffix(strings.TrimRight(src.Repo, "/"), ".git")
		if i := strings.LastIndex(r, "/"); i >= 0 {
			r = r[i+1:]
		}
		if r != "" {
			return r
		}
	case "image":
		im := src.Image
		if i := strings.LastIndex(im, "/"); i >= 0 {
			im = im[i+1:]
		}
		if i := strings.IndexAny(im, ":@"); i >= 0 {
			im = im[:i]
		}
		if im != "" {
			return im
		}
	}
	return "service"
}

func defaultIcon(src api.SourceConfig) string {
	switch src.Type {
	case "github":
		return "github"
	case "image":
		return "docker"
	}
	return ""
}

func (c *Core) checkSource(src *api.SourceConfig) error {
	switch src.Type {
	case "github":
		if trim(src.Repo) == "" {
			return fail.Validation("source.repo", "Choose a GitHub repository or paste a public repository URL.")
		}
		ref, ok := github.ParseRepo(src.Repo)
		if !ok {
			return fail.Validation("source.repo", "%q is not a GitHub repository. Use owner/name or https://github.com/owner/name.", src.Repo)
		}
		if ref.URL != "" {
			src.Repo = "https://github.com/" + ref.FullName
		} else {
			src.Repo = ref.FullName
		}
		if src.WaitForCI {
			return fail.Validation("source.waitForCi", "Wait for CI is not in 5050cloud v0.1. Deploys start when the push arrives.")
		}
	case "image":
		if trim(src.Image) == "" || strings.ContainsAny(src.Image, " \t\n") {
			return fail.Validation("source.image", "Enter an image reference such as nginx:1.27 or ghcr.io/owner/app:tag.")
		}
		src.Image = trim(src.Image)
	case "upload", "empty":
	case "":
		src.Type = "empty"
	default:
		return fail.Validation("source.type", "The source type must be github, image, upload or empty.")
	}
	return nil
}

// CreateService adds a service: committed and deployed at once with
// DeployNow, otherwise staged ("Add Variables", then Deploy).
func (c *Core) CreateService(ctx context.Context, a Actor, envID string, req api.CreateServiceRequest) (api.CreateServiceResponse, error) {
	if req.Template != "" {
		return c.createFromTemplate(ctx, a, envID, req)
	}
	env, _, err := c.Environment(ctx, envID)
	if err != nil {
		return api.CreateServiceResponse{}, err
	}
	src := req.Source
	if err := c.checkSource(&src); err != nil {
		return api.CreateServiceResponse{}, err
	}
	if req.RootDir != "" {
		src.RootDir = req.RootDir
	}
	cfg := api.ServiceConfig{Name: cleanName(req.Name), Icon: defaultIcon(src), Source: src}
	if len(req.WatchPaths) > 0 {
		cfg.Build.WatchPaths = req.WatchPaths
	}
	if cfg.Name == "" {
		cfg.Name = defaultName(src)
	}
	if err := changes.CheckServiceName(cfg.Name); err != nil && req.Name != "" {
		return api.CreateServiceResponse{}, err
	}
	values := map[string]string{}
	for k, v := range req.Variables {
		ev, err := vars.EvalFunctions(v, nil)
		if err != nil {
			return api.CreateServiceResponse{}, fail.Validation("variables."+k, "The value of %s is not valid: %s.", k, err.Error())
		}
		values[k] = ev
	}
	if err := changes.CheckVariables(values); err != nil {
		return api.CreateServiceResponse{}, err
	}
	sid := ids.New(ids.Service)
	var plan *cpapi.CommitPlan
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		names, err := c.takenNames(ctx, tx, envID, "")
		if err != nil {
			return err
		}
		if req.Name != "" && names[cfg.Name] {
			return fail.Conflict("Environment %s already has a service named %q.", env.Name, cfg.Name)
		}
		cfg.Name = changes.UniqueName(truncName(cfg.Name), names)
		if req.DeployNow {
			if err := changes.Validate(cfg, false); err != nil {
				return err
			}
			p, err := c.createCommitted(ctx, tx, a, env, sid, cfg, values, "Create "+cfg.Name)
			if err != nil {
				return err
			}
			plan = &p
		} else {
			if err := c.insertStagedInstance(ctx, tx, a, envID, sid, cfg); err != nil {
				return err
			}
			if len(values) > 0 {
				if err := c.stage(ctx, tx, envID, sid, changes.KindVariables, a.Label(), func(r *changeRow) {
					r.Vars = changes.VarOps{Set: values}
				}); err != nil {
					return err
				}
			}
		}
		if req.Position != nil {
			if err := c.setPosition(ctx, tx, envID, sid, *req.Position); err != nil {
				return err
			}
		}
		c.touchProject(ctx, tx, envID)
		return nil
	})
	if err != nil {
		return api.CreateServiceResponse{}, err
	}
	var resp api.CreateServiceResponse
	if plan != nil {
		resp.Deployments = c.applyPlan(ctx, *plan)
		c.recordCommitDeployments(ctx, plan.CommitID, resp.Deployments)
	}
	svc, err := c.Service(ctx, envID, sid)
	if err != nil {
		return resp, err
	}
	resp.Services = []api.Service{svc}
	resp.ChangesCount = c.changesCount(ctx, envID)
	c.suggestFromScan(ctx, svc.Config)
	c.publishService(ctx, svc)
	c.publishChanges(ctx, envID)
	return resp, nil
}

func truncName(n string) string {
	if len(n) > changes.MaxServiceNameLen {
		return strings.TrimSpace(n[:changes.MaxServiceNameLen])
	}
	return n
}

// takenNames lists names in use in an environment, committed and staged,
// except for service skip.
func (c *Core) takenNames(ctx context.Context, q store.Querier, envID, skip string) (map[string]bool, error) {
	insts, err := c.instances(ctx, q, envID)
	if err != nil {
		return nil, err
	}
	rows, err := c.envChanges(ctx, q, envID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for n, sid := range envNames(insts, indexChanges(rows)) {
		if sid != skip {
			out[n] = true
		}
	}
	return out, nil
}

// insertStagedInstance adds a rev-0 instance and its service.create change.
func (c *Core) insertStagedInstance(ctx context.Context, tx *sql.Tx, a Actor, envID, sid string, cfg api.ServiceConfig) error {
	var pid string
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM environments WHERE id = ?`, envID).Scan(&pid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO services (id, project_id, created_at) VALUES (?, ?, ?)`, sid, pid, c.Now()); err != nil {
		return err
	}
	// A deleted instance of the same service (a re-created environment) is replaced.
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_instances WHERE service_id = ? AND environment_id = ? AND deleted_at IS NOT NULL`, sid, envID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO service_instances (service_id, environment_id, rev, config, created_at) VALUES (?, ?, 0, ?, ?)`,
		sid, envID, jsonString(cfg), c.Now()); err != nil {
		if isUnique(err) {
			return fail.Conflict("This environment already has a service named %q.", cfg.Name)
		}
		return err
	}
	return c.stage(ctx, tx, envID, sid, changes.KindCreate, a.Label(), func(r *changeRow) {})
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// createCommitted inserts a committed instance with its variables and
// returns the plan that deploys it ("Deploy Now", templates, restores).
func (c *Core) createCommitted(ctx context.Context, tx *sql.Tx, a Actor, env api.Environment, sid string, cfg api.ServiceConfig, values map[string]string, message string) (cpapi.CommitPlan, error) {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO services (id, project_id, created_at) VALUES (?, ?, ?)`, sid, env.ProjectID, c.Now()); err != nil {
		return cpapi.CommitPlan{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_instances WHERE service_id = ? AND environment_id = ? AND deleted_at IS NOT NULL`, sid, env.ID); err != nil {
		return cpapi.CommitPlan{}, err
	}
	cid := ids.New(ids.Commit)
	if _, err := tx.ExecContext(ctx, `INSERT INTO service_instances (service_id, environment_id, rev, config, created_at) VALUES (?, ?, 1, ?, ?)`,
		sid, env.ID, jsonString(cfg), c.Now()); err != nil {
		if isUnique(err) {
			return cpapi.CommitPlan{}, fail.Conflict("Environment %s already has a service named %q.", env.Name, cfg.Name)
		}
		return cpapi.CommitPlan{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO config_revisions (service_id, environment_id, rev, config, commit_id, created_at) VALUES (?, ?, 1, ?, ?, ?)`,
		sid, env.ID, jsonString(cfg), cid, c.Now()); err != nil {
		return cpapi.CommitPlan{}, err
	}
	want := map[string]changes.Var{}
	for k, v := range values {
		want[k] = changes.Var{Value: v}
	}
	if err := c.writeVars(ctx, tx, env.ID, sid, want); err != nil {
		return cpapi.CommitPlan{}, err
	}
	diff := changes.Diff(api.ServiceConfig{}, cfg)
	for _, k := range sortedKeys(values) {
		diff = append(diff, api.DiffEntry{Path: "variables." + k, New: changes.Shown(k, values[k], false)})
	}
	summary := []api.Change{{ID: ids.New(ids.Change), ServiceID: sid, Service: cfg.Name, Kind: changes.KindCreate, Diff: diff, Author: a.Label(), CreatedAt: c.Now()}}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commits (id, environment_id, message, author, created_at, summary) VALUES (?, ?, ?, ?, ?, ?)`,
		cid, env.ID, message, a.Label(), c.Now(), jsonString(summary)); err != nil {
		return cpapi.CommitPlan{}, err
	}
	if err := c.insertActivity(ctx, tx, env.ID, api.Activity{ServiceID: sid, Kind: "commit", Text: message, Actor: a.Label(), Ref: cid}); err != nil {
		return cpapi.CommitPlan{}, err
	}
	// An upload service has no source until `5050 up` sends one, and that
	// upload starts its own deployment; deploying now would only fail.
	return cpapi.CommitPlan{CommitID: cid, EnvID: env.ID, Deploy: cfg.Source.Type != "upload", Actor: a.Label(), Message: message,
		Items: []cpapi.PlanItem{{ServiceID: sid, Action: changes.ActionCreate, Rebuild: true}}}, nil
}

// ---------------------------------------------------------------- edit

// UpdateServiceConfig stages a merge patch of the service config.
func (c *Core) UpdateServiceConfig(ctx context.Context, a Actor, envID, sid string, patch map[string]any) (api.Service, error) {
	if len(patch) == 0 {
		return c.Service(ctx, envID, sid)
	}
	if err := changes.CheckPatch(patch); err != nil {
		return api.Service{}, err
	}
	var rename *string
	if n, ok := patch["name"]; ok {
		s, _ := n.(string)
		rename = &s
		delete(patch, "name")
	}
	if src, ok := patch["source"].(map[string]any); ok {
		if w, ok := src["waitForCi"].(bool); ok && w {
			return api.Service{}, fail.Validation("source.waitForCi", "Wait for CI is not in 5050cloud v0.1. Deploys start when the push arrives.")
		}
	}
	if n, ok := patch["networking"].(map[string]any); ok {
		if _, ok := n["allowMetadata"]; ok && InstallLevel(a) < Admin {
			return api.Service{}, fail.Forbidden("Only admins can let a service reach the metadata address and the mesh.")
		}
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		in, err := c.liveInstance(ctx, tx, envID, sid)
		if err != nil {
			return err
		}
		if err := c.checkFileFields(ctx, tx, envID, sid, patch); err != nil {
			return err
		}
		if !in.committed() {
			next, err := changes.Apply(in.Config, patch)
			if err != nil {
				return err
			}
			if next.Source.Type != in.Config.Source.Type || next.Source.Repo != in.Config.Source.Repo || next.Source.Image != in.Config.Source.Image {
				if err := c.checkSource(&next.Source); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE service_instances SET config = ? WHERE service_id = ? AND environment_id = ?`, jsonString(next), sid, envID); err != nil {
				return err
			}
			if err := c.stage(ctx, tx, envID, sid, changes.KindCreate, a.Label(), func(r *changeRow) {}); err != nil {
				return err
			}
		} else {
			var stageErr error
			if err := c.stage(ctx, tx, envID, sid, changes.KindUpdate, a.Label(), func(r *changeRow) {
				merged := changes.MergePatch(r.Patch, patch)
				next, err := changes.Apply(in.Config, merged)
				if err != nil {
					stageErr = err
					return
				}
				if next.Source.Repo != in.Config.Source.Repo || next.Source.Image != in.Config.Source.Image || next.Source.Type != in.Config.Source.Type {
					if err := c.checkSource(&next.Source); err != nil {
						stageErr = err
						return
					}
				}
				next.Name = in.Config.Name
				r.Patch = changes.PatchBetween(in.Config, next)
			}); err != nil {
				return err
			}
			if stageErr != nil {
				return stageErr
			}
		}
		if rename != nil {
			return c.stageRename(ctx, tx, a, in, *rename)
		}
		return nil
	})
	if err != nil {
		return api.Service{}, err
	}
	return c.afterStage(ctx, envID, sid)
}

func (c *Core) afterStage(ctx context.Context, envID, sid string) (api.Service, error) {
	svc, err := c.Service(ctx, envID, sid)
	if err == nil {
		c.publishService(ctx, svc)
	}
	c.publishChanges(ctx, envID)
	return svc, err
}

// checkFileFields refuses edits of fields a config-as-code file sets (9.20.1).
func (c *Core) checkFileFields(ctx context.Context, tx *sql.Tx, envID, sid string, patch map[string]any) error {
	var p, lines string
	err := tx.QueryRowContext(ctx, `SELECT path, lines FROM config_files WHERE environment_id = ? AND service_id = ?`, envID, sid).Scan(&p, &lines)
	if store.NoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	m := map[string]int{}
	_ = json.Unmarshal([]byte(lines), &m)
	for _, f := range changes.Paths(patch) {
		if line, ok := m[f]; ok {
			return fail.Validation(f, "%s is set by %s line %d. Change it in the file, or remove it there to edit it here.", f, p, line)
		}
	}
	return nil
}

// UpdateServiceInfo renames (staged) or changes the icon (immediate).
func (c *Core) UpdateServiceInfo(ctx context.Context, a Actor, envID, sid string, req api.UpdateServiceInfoRequest) (api.Service, error) {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		in, err := c.liveInstance(ctx, tx, envID, sid)
		if err != nil {
			return err
		}
		if req.Icon != nil {
			icon := trim(*req.Icon)
			if len(icon) > 200 {
				return fail.Validation("icon", "The icon must be an icon name or a path of at most 200 characters.")
			}
			in.Config.Icon = icon
			if _, err := tx.ExecContext(ctx, `UPDATE service_instances SET config = json_set(config, '$.icon', ?) WHERE service_id = ? AND environment_id = ?`, icon, sid, envID); err != nil {
				return err
			}
		}
		if req.Name != nil {
			return c.stageRename(ctx, tx, a, in, *req.Name)
		}
		return nil
	})
	if err != nil {
		return api.Service{}, err
	}
	return c.afterStage(ctx, envID, sid)
}

func (c *Core) stageRename(ctx context.Context, tx *sql.Tx, a Actor, in instance, name string) error {
	name = cleanName(name)
	if err := changes.CheckServiceName(name); err != nil {
		return err
	}
	taken, err := c.takenNames(ctx, tx, in.EnvID, in.ServiceID)
	if err != nil {
		return err
	}
	if taken[name] {
		return fail.Conflict("This environment already has a service named %q.", name)
	}
	if !in.committed() {
		in.Config.Name = name
		_, err := tx.ExecContext(ctx, `UPDATE service_instances SET config = ? WHERE service_id = ? AND environment_id = ?`, jsonString(in.Config), in.ServiceID, in.EnvID)
		return err
	}
	if name == in.Config.Name {
		_, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE environment_id = ? AND service_id = ? AND kind = ?`, in.EnvID, in.ServiceID, changes.KindRename)
		return err
	}
	return c.stage(ctx, tx, in.EnvID, in.ServiceID, changes.KindRename, a.Label(), func(r *changeRow) {
		r.Patch = map[string]any{"name": name}
	})
}

// DuplicateService stages "<name>-copy" with config and non-sealed variables,
// no domains and a new empty volume when the source has one.
func (c *Core) DuplicateService(ctx context.Context, a Actor, envID, sid string) (api.Service, error) {
	nid := ids.New(ids.Service)
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		in, err := c.liveInstance(ctx, tx, envID, sid)
		if err != nil {
			return err
		}
		rows, err := c.envChanges(ctx, tx, envID)
		if err != nil {
			return err
		}
		cfg, _ := stagedConfig(in, indexChanges(rows))
		cfg = changes.Clone(cfg)
		taken, err := c.takenNames(ctx, tx, envID, "")
		if err != nil {
			return err
		}
		base := truncName(cfg.Name)
		if len(base)+5 > changes.MaxServiceNameLen {
			base = strings.TrimSpace(base[:changes.MaxServiceNameLen-5])
		}
		cfg.Name = changes.UniqueName(base+"-copy", taken)
		cfg.Net.PrivateName = ""
		cfg.Net.PublicAccess = false
		if err := c.insertStagedInstance(ctx, tx, a, envID, nid, cfg); err != nil {
			return err
		}
		vrows, err := c.envVars(ctx, tx, envID)
		if err != nil {
			return err
		}
		if ops := changes.CopyVarsDropSealed(groupVars(vrows).of(sid)); !ops.Empty() {
			if err := c.stage(ctx, tx, envID, nid, changes.KindVariables, a.Label(), func(r *changeRow) { r.Vars = ops }); err != nil {
				return err
			}
		}
		vols, err := c.volumes(ctx, tx, envID)
		if err != nil {
			return err
		}
		if v, ok := vols[sid]; ok {
			if err := c.stage(ctx, tx, envID, nid, changes.KindVolumeCreate, a.Label(), func(r *changeRow) {
				r.Patch = map[string]any{"name": cfg.Name + "-volume", "mountPath": v.MountPath}
			}); err != nil {
				return err
			}
		}
		pos, _ := c.positions(ctx, tx, envID)
		if p, ok := pos[sid]; ok {
			return c.setPosition(ctx, tx, envID, nid, [2]float64{p[0] + 40, p[1] + 40})
		}
		return nil
	})
	if err != nil {
		return api.Service{}, err
	}
	return c.afterStage(ctx, envID, nid)
}

// DeleteService stages a delete (or discards a staged create).
func (c *Core) DeleteService(ctx context.Context, a Actor, envID, sid, confirm string) error {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		in, err := c.liveInstance(ctx, tx, envID, sid)
		if err != nil {
			return err
		}
		if confirm != in.Config.Name {
			return fail.Validation("confirm", "Type the service name %q to confirm.", in.Config.Name)
		}
		if !in.committed() {
			return c.discardCreate(ctx, tx, envID, sid)
		}
		return c.stage(ctx, tx, envID, sid, changes.KindDelete, a.Label(), func(r *changeRow) {})
	})
	if err != nil {
		return err
	}
	if svc, err := c.Service(ctx, envID, sid); err == nil {
		c.publishService(ctx, svc)
	} else {
		c.broker.Publish(api.Event{Type: "service", EnvironmentID: envID, ServiceID: sid})
	}
	c.publishChanges(ctx, envID)
	return nil
}

// discardCreate removes a staged (never committed) service instance.
func (c *Core) discardCreate(ctx context.Context, tx *sql.Tx, envID, sid string) error {
	if err := dropChanges(ctx, tx, envID, sid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_instances WHERE service_id = ? AND environment_id = ? AND rev = 0`, sid, envID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE environment_id = ? AND service_id = ?`, envID, sid); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM service_instances WHERE service_id = ?`, sid).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE id = ?`, sid); err != nil {
			return err
		}
	}
	return nil
}
