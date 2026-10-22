// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

// MaxProjectName is the longest project or environment name.
const MaxProjectName = 64

func scanProject(r interface{ Scan(...any) error }) (api.Project, error) {
	var p api.Project
	var pub int
	err := r.Scan(&p.ID, &p.Name, &p.Description, &pub, &p.CreatedAt, &p.LastActivityAt)
	p.PublicRead = pub == 1
	p.Environments = []api.Environment{}
	return p, err
}

const projectCols = `id, name, description, public_read, created_at, last_activity_at`

func scanEnv(r interface{ Scan(...any) error }) (api.Environment, error) {
	var e api.Environment
	var allow string
	err := r.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Egress, &allow, &e.CreatedAt)
	_ = json.Unmarshal([]byte(allow), &e.EgressAllow)
	return e, err
}

const envCols = `id, project_id, name, egress, egress_allow, created_at`

func (c *Core) projectEnvs(ctx context.Context, q store.Querier, projectID string) ([]api.Environment, error) {
	envs, err := store.All(ctx, q, func(r *sql.Rows) (api.Environment, error) { return scanEnv(r) },
		`SELECT `+envCols+` FROM environments WHERE project_id = ? AND deleted_at IS NULL ORDER BY created_at, id`, projectID)
	if envs == nil {
		envs = []api.Environment{}
	}
	return envs, err
}

// Project returns a project with its environments.
func (c *Core) Project(ctx context.Context, id string) (api.Project, error) {
	p, err := scanProject(c.st.Reader.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ? AND deleted_at IS NULL`, id))
	if store.NoRows(err) {
		return p, fail.NotFound("The project %s does not exist.", id)
	}
	if err != nil {
		return p, err
	}
	p.Environments, err = c.projectEnvs(ctx, c.st.Reader, id)
	return p, err
}

// ProjectPublic reports whether a project is public read-only.
func (c *Core) ProjectPublic(ctx context.Context, id string) bool {
	var pub int
	err := c.st.Reader.QueryRowContext(ctx, `SELECT public_read FROM projects WHERE id = ? AND deleted_at IS NULL`, id).Scan(&pub)
	return err == nil && pub == 1
}

// Environment implements cpapi.Config.
func (c *Core) Environment(ctx context.Context, envID string) (api.Environment, api.Project, error) {
	e, err := scanEnv(c.st.Reader.QueryRowContext(ctx, `SELECT `+envCols+` FROM environments WHERE id = ? AND deleted_at IS NULL`, envID))
	if store.NoRows(err) {
		return e, api.Project{}, fail.NotFound("The environment %s does not exist.", envID)
	}
	if err != nil {
		return e, api.Project{}, err
	}
	p, err := c.Project(ctx, e.ProjectID)
	return e, p, err
}

// EnvProject returns the project id of an environment ("" when unknown).
func (c *Core) EnvProject(ctx context.Context, envID string) string {
	var pid string
	_ = c.st.Reader.QueryRowContext(ctx, `SELECT project_id FROM environments WHERE id = ?`, envID).Scan(&pid)
	return pid
}

// Projects is GET /projects, filtered by grants, with summaries.
func (c *Core) Projects(ctx context.Context, a Actor, sortBy string) ([]api.Project, error) {
	order := "last_activity_at DESC, created_at DESC"
	switch sortBy {
	case "", "activity":
	case "created":
		order = "created_at DESC"
	case "name":
		order = "name COLLATE NOCASE"
	default:
		return nil, fail.Validation("sort", "Sort by activity, created or name.")
	}
	ps, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.Project, error) { return scanProject(r) },
		`SELECT `+projectCols+` FROM projects WHERE deleted_at IS NULL ORDER BY `+order)
	if err != nil {
		return nil, err
	}
	hidden, err := c.hiddenProjects(ctx, a)
	if err != nil {
		return nil, err
	}
	out := []api.Project{}
	for _, p := range ps {
		if hidden[p.ID] {
			continue
		}
		p.Environments, err = c.projectEnvs(ctx, c.st.Reader, p.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range p.Environments {
			s, err := c.envSummary(ctx, e.ID)
			if err != nil {
				return nil, err
			}
			p.Summary = append(p.Summary, s)
		}
		out = append(out, p)
	}
	return out, nil
}

func (c *Core) envSummary(ctx context.Context, envID string) (api.EnvSummary, error) {
	s := api.EnvSummary{EnvironmentID: envID, Icons: []api.IconChip{}}
	insts, err := c.instances(ctx, c.st.Reader, envID)
	if err != nil {
		return s, err
	}
	rt := c.runtime(ctx, envID)
	pos, _ := c.positions(ctx, c.st.Reader, envID)
	for _, in := range insts {
		if !in.committed() {
			continue
		}
		s.Total++
		st := statusOf(in, rt)
		if st == api.StatusOnline || st == api.StatusCompleted {
			s.Online++
		}
		s.Icons = append(s.Icons, api.IconChip{Icon: in.Config.Icon, Kind: string(kindOf(in.Config)), Source: in.Config.Source.Type})
		s.Mini = append(s.Mini, api.MiniNode{ServiceID: in.ServiceID, Icon: in.Config.Icon, Status: st, Position: pos[in.ServiceID]})
	}
	return s, nil
}

func (c *Core) runtime(ctx context.Context, envID string) map[string]cpapi.Runtime {
	if c.orch == nil {
		return nil
	}
	rt, err := c.orch.Runtime(ctx, envID)
	if err != nil {
		c.log.Debug("runtime unavailable", "env", envID, "err", err)
		return nil
	}
	return rt
}

func statusOf(in instance, rt map[string]cpapi.Runtime) api.Status {
	if r, ok := rt[in.ServiceID]; ok && r.Status != "" {
		return r.Status
	}
	return api.StatusNoDeploy
}

func kindOf(cfg api.ServiceConfig) api.ServiceKind {
	switch {
	case cfg.Kind != "":
		return cfg.Kind
	case cfg.Deploy.CronSchedule != "":
		return api.KindCron
	}
	return api.KindWeb
}

// CreateProject makes a project with a "production" environment.
func (c *Core) CreateProject(ctx context.Context, a Actor, req api.CreateProjectRequest) (api.Project, error) {
	name := cleanName(req.Name)
	if name == "" {
		name = ids.TwoWordName()
	}
	if len(name) > MaxProjectName {
		return api.Project{}, fail.Validation("name", "The project name must be at most %d characters.", MaxProjectName)
	}
	if len(req.Description) > 500 {
		return api.Project{}, fail.Validation("description", "The description must be at most 500 characters.")
	}
	p := api.Project{ID: ids.New(ids.Project), Name: name, Description: trim(req.Description), CreatedAt: c.Now()}
	p.LastActivityAt = p.CreatedAt
	env := api.Environment{ID: ids.New(ids.Environment), ProjectID: p.ID, Name: "production", Egress: "allowed", CreatedAt: p.CreatedAt}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, name, description, created_at, last_activity_at) VALUES (?, ?, ?, ?, ?)`,
			p.ID, p.Name, p.Description, p.CreatedAt, p.CreatedAt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO environments (id, project_id, name, egress, created_at) VALUES (?, ?, ?, 'allowed', ?)`, env.ID, p.ID, env.Name, env.CreatedAt)
		return err
	})
	if err != nil {
		return api.Project{}, err
	}
	p.Environments = []api.Environment{env}
	return p, nil
}

// UpdateProject renames a project or changes its description or visibility.
func (c *Core) UpdateProject(ctx context.Context, a Actor, id string, req api.UpdateProjectRequest) (api.Project, error) {
	if req.PublicRead != nil && InstallLevel(a) < Admin {
		return api.Project{}, fail.Forbidden("Only admins can change a project's visibility.")
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fail.NotFound("The project %s does not exist.", id)
		}
		if req.Name != nil {
			name := cleanName(*req.Name)
			if name == "" || len(name) > MaxProjectName {
				return fail.Validation("name", "The project name must be 1 to %d characters.", MaxProjectName)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET name = ? WHERE id = ?`, name, id); err != nil {
				return err
			}
		}
		if req.Description != nil {
			if len(*req.Description) > 500 {
				return fail.Validation("description", "The description must be at most 500 characters.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET description = ? WHERE id = ?`, trim(*req.Description), id); err != nil {
				return err
			}
		}
		if req.PublicRead != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE projects SET public_read = ? WHERE id = ?`, store.Bool(*req.PublicRead), id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return api.Project{}, err
	}
	return c.Project(ctx, id)
}

// DeleteProject stops everything in a project. Volumes are deleted by the
// orchestrator 48 hours later.
func (c *Core) DeleteProject(ctx context.Context, a Actor, id, confirm string) error {
	p, err := c.Project(ctx, id)
	if err != nil {
		return err
	}
	if confirm != p.Name {
		return fail.Validation("confirm", "Type the project name %q to confirm.", p.Name)
	}
	var plans []cpapi.CommitPlan
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		now := c.Now()
		for _, e := range p.Environments {
			plan, err := c.markEnvDeleted(ctx, tx, e.ID, a, now)
			if err != nil {
				return err
			}
			plans = append(plans, plan)
		}
		_, err := tx.ExecContext(ctx, `UPDATE projects SET deleted_at = ? WHERE id = ?`, now, id)
		return err
	})
	if err != nil {
		return err
	}
	for _, plan := range plans {
		c.applyPlan(ctx, plan)
	}
	return nil
}

// markEnvDeleted deletes every instance of an environment and returns the
// plan that removes them.
func (c *Core) markEnvDeleted(ctx context.Context, tx *sql.Tx, envID string, a Actor, now int64) (cpapi.CommitPlan, error) {
	plan := cpapi.CommitPlan{CommitID: ids.New(ids.Commit), EnvID: envID, Deploy: true, Actor: a.Label(), Message: "Environment deleted"}
	insts, err := c.instances(ctx, tx, envID)
	if err != nil {
		return plan, err
	}
	for _, in := range insts {
		if in.committed() {
			plan.Items = append(plan.Items, cpapi.PlanItem{ServiceID: in.ServiceID, Action: changes.ActionDelete})
		}
	}
	for _, q := range []string{
		`UPDATE service_instances SET deleted_at = ? WHERE environment_id = ? AND deleted_at IS NULL`,
		`UPDATE volumes SET deleted_at = ? WHERE environment_id = ? AND deleted_at IS NULL`,
		`UPDATE environments SET deleted_at = ? WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, now, envID); err != nil {
			return plan, err
		}
	}
	for _, q := range []string{
		`DELETE FROM changes WHERE environment_id = ?`, `DELETE FROM domains WHERE environment_id = ?`,
		`DELETE FROM tcp_proxies WHERE environment_id = ?`, `DELETE FROM monitors WHERE environment_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, envID); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// applyPlan hands a plan to the orchestrator, logging failures (the plan
// is already committed; the orchestrator reconciles on its next pass).
func (c *Core) applyPlan(ctx context.Context, plan cpapi.CommitPlan) []api.Deployment {
	if c.orch == nil || len(plan.Items) == 0 {
		return nil
	}
	deps, err := c.orch.ApplyCommit(ctx, plan)
	if err != nil {
		c.log.Warn("the orchestrator could not apply a commit", "commit", plan.CommitID, "err", err)
	}
	c.orch.NetworkChanged(ctx, plan.EnvID)
	return deps
}

// CreateEnvironment makes an empty environment or a staged duplicate.
func (c *Core) CreateEnvironment(ctx context.Context, a Actor, projectID string, req api.CreateEnvironmentRequest) (api.Environment, error) {
	name := cleanName(req.Name)
	if name == "" || len(name) > MaxProjectName {
		return api.Environment{}, fail.Validation("name", "The environment name must be 1 to %d characters.", MaxProjectName)
	}
	mode := req.Mode
	if mode == "" {
		mode = "empty"
		if req.From != "" {
			mode = "duplicate"
		}
	}
	if mode != "empty" && mode != "duplicate" {
		return api.Environment{}, fail.Validation("mode", "The mode must be \"empty\" or \"duplicate\".")
	}
	if _, err := c.Project(ctx, projectID); err != nil {
		return api.Environment{}, err
	}
	env := api.Environment{ID: ids.New(ids.Environment), ProjectID: projectID, Name: name, Egress: "allowed", CreatedAt: c.Now()}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM environments WHERE project_id = ? AND name = ? AND deleted_at IS NULL`, projectID, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fail.Conflict("This project already has an environment named %q.", name)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO environments (id, project_id, name, egress, created_at) VALUES (?, ?, ?, 'allowed', ?)`, env.ID, projectID, name, env.CreatedAt); err != nil {
			return err
		}
		if mode == "empty" {
			return nil
		}
		src, err := scanEnv(tx.QueryRowContext(ctx, `SELECT `+envCols+` FROM environments WHERE id = ? AND deleted_at IS NULL`, req.From))
		if err != nil || src.ProjectID != projectID {
			return fail.Validation("from", "Choose an environment of this project to duplicate.")
		}
		return c.duplicateEnv(ctx, tx, a, src.ID, env.ID)
	})
	if err != nil {
		return api.Environment{}, err
	}
	c.publishChanges(ctx, env.ID)
	return env, nil
}

// duplicateEnv stages a create for every service of src with its config and
// non-sealed variables, plus the shared variables (9.5). Volumes are new and empty.
func (c *Core) duplicateEnv(ctx context.Context, tx *sql.Tx, a Actor, srcEnv, dstEnv string) error {
	insts, err := c.instances(ctx, tx, srcEnv)
	if err != nil {
		return err
	}
	vrows, err := c.envVars(ctx, tx, srcEnv)
	if err != nil {
		return err
	}
	vs := groupVars(vrows)
	vols, err := c.volumes(ctx, tx, srcEnv)
	if err != nil {
		return err
	}
	pos, _ := c.positions(ctx, tx, srcEnv)
	for _, in := range insts {
		if !in.committed() {
			continue
		}
		cfg := changes.Clone(in.Config)
		if err := c.insertStagedInstance(ctx, tx, a, dstEnv, in.ServiceID, cfg); err != nil {
			return err
		}
		ops := changes.CopyVarsDropSealed(vs.of(in.ServiceID))
		if !ops.Empty() {
			if err := c.stage(ctx, tx, dstEnv, in.ServiceID, changes.KindVariables, a.Label(), func(r *changeRow) { r.Vars = ops }); err != nil {
				return err
			}
		}
		if v, ok := vols[in.ServiceID]; ok {
			if err := c.stage(ctx, tx, dstEnv, in.ServiceID, changes.KindVolumeCreate, a.Label(), func(r *changeRow) {
				r.Patch = map[string]any{"name": v.Name, "mountPath": v.MountPath}
			}); err != nil {
				return err
			}
		}
	}
	if ops := changes.CopyVarsDropSealed(vs.Shared); !ops.Empty() {
		if err := c.stage(ctx, tx, dstEnv, "", changes.KindShared, a.Label(), func(r *changeRow) { r.Vars = ops }); err != nil {
			return err
		}
	}
	if len(pos) > 0 {
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO canvas_positions (environment_id, positions) VALUES (?, ?)`, dstEnv, jsonString(pos))
		return err
	}
	return nil
}

// UpdateEnvironment renames an environment or changes its egress policy.
func (c *Core) UpdateEnvironment(ctx context.Context, a Actor, envID string, req api.UpdateEnvironmentRequest) (api.Environment, error) {
	if (req.Egress != nil || req.EgressAllow != nil) && InstallLevel(a) < Admin {
		return api.Environment{}, fail.Forbidden("Only admins can change an environment's egress policy.")
	}
	e, _, err := c.Environment(ctx, envID)
	if err != nil {
		return e, err
	}
	netChanged := false
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		if req.Name != nil {
			name := cleanName(*req.Name)
			if name == "" || len(name) > MaxProjectName {
				return fail.Validation("name", "The environment name must be 1 to %d characters.", MaxProjectName)
			}
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM environments WHERE project_id = ? AND name = ? AND id != ? AND deleted_at IS NULL`, e.ProjectID, name, envID).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return fail.Conflict("This project already has an environment named %q.", name)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE environments SET name = ? WHERE id = ?`, name, envID); err != nil {
				return err
			}
		}
		if req.Egress != nil {
			if *req.Egress != "allowed" && *req.Egress != "blocked" {
				return fail.Validation("egress", "Egress must be \"allowed\" or \"blocked\".")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE environments SET egress = ? WHERE id = ?`, *req.Egress, envID); err != nil {
				return err
			}
			netChanged = true
		}
		if req.EgressAllow != nil {
			list := []string{}
			for _, h := range *req.EgressAllow {
				h = lower(trim(h))
				if h == "" {
					continue
				}
				if len(h) > 253 || strings.ContainsAny(h, " /") && !strings.Contains(h, "/") {
					return fail.Validation("egressAllow", "%q is not a host name, IP address or CIDR range.", h)
				}
				list = append(list, h)
			}
			sort.Strings(list)
			if _, err := tx.ExecContext(ctx, `UPDATE environments SET egress_allow = ? WHERE id = ?`, jsonString(uniq(list)), envID); err != nil {
				return err
			}
			netChanged = true
		}
		return nil
	})
	if err != nil {
		return api.Environment{}, err
	}
	if netChanged && c.orch != nil {
		c.orch.NetworkChanged(ctx, envID)
	}
	e, _, err = c.Environment(ctx, envID)
	return e, err
}

// DeleteEnvironment removes an environment that is not the project's last.
func (c *Core) DeleteEnvironment(ctx context.Context, a Actor, envID, confirm string) error {
	e, p, err := c.Environment(ctx, envID)
	if err != nil {
		return err
	}
	if confirm != e.Name {
		return fail.Validation("confirm", "Type the environment name %q to confirm.", e.Name)
	}
	if len(p.Environments) <= 1 {
		return fail.Conflict("A project needs at least one environment. Delete the project instead.")
	}
	var plan cpapi.CommitPlan
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		plan, err = c.markEnvDeleted(ctx, tx, envID, a, c.Now())
		return err
	})
	if err != nil {
		return err
	}
	c.applyPlan(ctx, plan)
	return nil
}

func (c *Core) touchProject(ctx context.Context, tx *sql.Tx, envID string) {
	_, _ = tx.ExecContext(ctx, `UPDATE projects SET last_activity_at = ? WHERE id = (SELECT project_id FROM environments WHERE id = ?)`, c.Now(), envID)
}
