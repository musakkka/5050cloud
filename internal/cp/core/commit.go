// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ---------------------------------------------------------------- listing

// renameRewrites lists the variable values a staged rename rewrites.
func renameRewrites(b *bundle, vs varSets, oldName, newName string) []api.DiffEntry {
	var out []api.DiffEntry
	add := func(prefix string, cur map[string]changes.Var) {
		for _, k := range sortedKeys(cur) {
			v := cur[k]
			if nv, ok := vars.RewriteService(v.Value, oldName, newName); ok {
				out = append(out, api.DiffEntry{Path: prefix + k, Old: changes.Shown(k, v.Value, v.Sealed), New: changes.Shown(k, nv, v.Sealed)})
			}
		}
	}
	shared := vs.Shared
	if r := b.cs.get("", changes.KindShared); r != nil {
		shared = r.Vars.Apply(shared)
	}
	add("shared.", shared)
	for _, in := range b.insts {
		cur := vs.of(in.ServiceID)
		if r := b.cs.get(in.ServiceID, changes.KindVariables); r != nil {
			cur = r.Vars.Apply(cur)
		}
		cfg, _ := stagedConfig(in, b.cs)
		add(cfg.Name+".", cur)
	}
	return out
}

// changeViews renders the staged changes of an environment.
func (c *Core) changeViews(b *bundle, vs varSets) []api.Change {
	out := []api.Change{}
	name := func(sid string) string {
		if in, ok := b.find(sid); ok {
			return in.Config.Name
		}
		return ""
	}
	rows := []*changeRow{}
	for _, m := range b.cs {
		for _, r := range m {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt != rows[j].CreatedAt {
			return rows[i].CreatedAt < rows[j].CreatedAt
		}
		return rows[i].ID < rows[j].ID
	})
	for _, r := range rows {
		ch := api.Change{ID: r.ID, ServiceID: r.ServiceID, Service: name(r.ServiceID), Kind: r.Kind, Tag: r.Tag, Author: r.Author, CreatedAt: r.CreatedAt, Diff: []api.DiffEntry{}}
		in, _ := b.find(r.ServiceID)
		switch r.Kind {
		case changes.KindCreate:
			ch.Diff = changes.Diff(api.ServiceConfig{}, in.Config)
		case changes.KindUpdate:
			if next, err := changes.Apply(in.Config, r.Patch); err == nil {
				ch.Diff = changes.Diff(in.Config, next)
			}
		case changes.KindRename:
			n, _ := r.Patch["name"].(string)
			ch.Diff = []api.DiffEntry{{Path: "name", Old: in.Config.Name, New: n}}
			ch.Rewrites = renameRewrites(b, vs, in.Config.Name, n)
		case changes.KindDelete:
		case changes.KindVariables:
			ch.Diff = changes.DiffVars("variables", vs.of(r.ServiceID), r.Vars)
			for _, k := range r.Vars.NeedsValue {
				ch.Notes = append(ch.Notes, k+": set a value")
			}
		case changes.KindShared:
			ch.Diff = changes.DiffVars("shared", vs.Shared, r.Vars)
		case changes.KindVolumeCreate:
			ch.Diff = changes.DiffMaps("volume", map[string]any{}, r.Patch)
		case changes.KindVolumeUpdate:
			old := map[string]any{}
			if v, ok := b.vols[r.ServiceID]; ok {
				old["mountPath"] = v.MountPath
			}
			ch.Diff = changes.DiffMaps("volume", old, r.Patch)
		case changes.KindVolumeDelete:
			if v, ok := b.vols[r.ServiceID]; ok {
				ch.Diff = []api.DiffEntry{{Path: "volume", Old: v.Name, New: nil}}
			}
		}
		if ch.Diff == nil {
			ch.Diff = []api.DiffEntry{}
		}
		out = append(out, ch)
	}
	return out
}

// Changes is GET /environments/{eid}/changes.
func (c *Core) Changes(ctx context.Context, envID string) (api.ChangesResponse, error) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return api.ChangesResponse{}, err
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return api.ChangesResponse{}, err
	}
	return api.ChangesResponse{Changes: c.changeViews(b, groupVars(rows))}, nil
}

// DiscardChange removes one staged change ("create:<sid>" discards a staged create).
func (c *Core) DiscardChange(ctx context.Context, a Actor, envID, cid string) error {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var sid, kind string
		err := tx.QueryRowContext(ctx, `SELECT service_id, kind FROM changes WHERE id = ? AND environment_id = ?`, cid, envID).Scan(&sid, &kind)
		if store.NoRows(err) {
			return fail.NotFound("The change %s does not exist.", cid)
		}
		if err != nil {
			return err
		}
		if kind == changes.KindCreate {
			return c.discardCreate(ctx, tx, envID, sid)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE id = ? AND environment_id = ?`, cid, envID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fail.NotFound("The change %s does not exist.", cid)
		}
		return nil
	})
	if err == nil {
		c.publishChanges(ctx, envID)
		c.publishAll(ctx, envID)
	}
	return err
}

// DiscardAll removes every staged change of an environment.
func (c *Core) DiscardAll(ctx context.Context, a Actor, envID string) error {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		insts, err := c.instances(ctx, tx, envID)
		if err != nil {
			return err
		}
		for _, in := range insts {
			if !in.committed() {
				if err := c.discardCreate(ctx, tx, envID, in.ServiceID); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM changes WHERE environment_id = ?`, envID)
		return err
	})
	if err == nil {
		c.publishChanges(ctx, envID)
		c.publishAll(ctx, envID)
	}
	return err
}

func (c *Core) publishAll(ctx context.Context, envID string) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return
	}
	for _, in := range b.insts {
		c.publishService(ctx, c.serviceView(ctx, b, in))
	}
}

// ---------------------------------------------------------------- commit

// Commit applies every staged change of an environment in one transaction
// and hands the plan to the orchestrator (9.5).
func (c *Core) Commit(ctx context.Context, a Actor, envID string, req api.CommitRequest) (api.CommitResponse, error) {
	var plan cpapi.CommitPlan
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var err error
		plan, err = c.commitTx(ctx, tx, a, envID, req)
		return err
	})
	if err != nil {
		return api.CommitResponse{}, err
	}
	resp := api.CommitResponse{CommitID: plan.CommitID, Deployments: []api.Deployment{}}
	if deps := c.applyPlan(ctx, plan); deps != nil {
		resp.Deployments = deps
	}
	c.recordCommitDeployments(ctx, plan.CommitID, resp.Deployments)
	c.publishChanges(ctx, envID)
	c.publishAll(ctx, envID)
	return resp, nil
}

func (c *Core) commitTx(ctx context.Context, tx *sql.Tx, a Actor, envID string, req api.CommitRequest) (cpapi.CommitPlan, error) {
	env, _, err := c.Environment(ctx, envID)
	if err != nil {
		return cpapi.CommitPlan{}, err
	}
	b := &bundle{env: env}
	if b.insts, err = c.instances(ctx, tx, envID); err != nil {
		return cpapi.CommitPlan{}, err
	}
	crow, err := c.envChanges(ctx, tx, envID)
	if err != nil {
		return cpapi.CommitPlan{}, err
	}
	b.cs = indexChanges(crow)
	if b.vols, err = c.volumes(ctx, tx, envID); err != nil {
		return cpapi.CommitPlan{}, err
	}
	vrows, err := c.envVars(ctx, tx, envID)
	if err != nil {
		return cpapi.CommitPlan{}, err
	}
	vs := groupVars(vrows)
	summary := c.changeViews(b, vs)
	if len(summary) == 0 {
		return cpapi.CommitPlan{}, fail.Validation("changes", "There are no staged changes to deploy.")
	}
	now := c.Now()
	cid := ids.New(ids.Commit)
	msg := trim(req.Message)
	if msg == "" {
		msg = plural(len(summary), "1 change", itoa(len(summary))+" changes")
	}

	// Next state of every service.
	type next struct {
		in       instance
		cfg      api.ServiceConfig
		vars     map[string]changes.Var
		varsDiff bool
		del      bool
		vol      *changes.VolumeSpec // desired volume (nil = none)
		volDiff  bool
	}
	nexts := map[string]*next{}
	order := []string{}
	for _, in := range b.insts {
		cfg, action := stagedConfig(in, b.cs)
		n := &next{in: in, cfg: cfg, vars: vs.of(in.ServiceID), del: action == "delete"}
		if r := b.cs.get(in.ServiceID, changes.KindVariables); r != nil {
			n.vars = r.Vars.Apply(n.vars)
			n.varsDiff = true
			for _, k := range r.Vars.NeedsValue {
				if n.vars[k].Value == "" {
					return cpapi.CommitPlan{}, fail.Validation("variables."+k, "%s in %s needs a value before deploying.", k, cfg.Name)
				}
			}
		}
		if v, ok := b.vols[in.ServiceID]; ok {
			n.vol = &changes.VolumeSpec{Name: v.Name, MountPath: v.MountPath}
		}
		if r := b.cs.get(in.ServiceID, changes.KindVolumeCreate); r != nil {
			nm, _ := r.Patch["name"].(string)
			mp, _ := r.Patch["mountPath"].(string)
			n.vol, n.volDiff = &changes.VolumeSpec{Name: nm, MountPath: mp}, true
		}
		if r := b.cs.get(in.ServiceID, changes.KindVolumeUpdate); r != nil && n.vol != nil {
			if mp, ok := r.Patch["mountPath"].(string); ok {
				v := *n.vol
				v.MountPath = mp
				n.vol, n.volDiff = &v, true
			}
		}
		if b.cs.get(in.ServiceID, changes.KindVolumeDelete) != nil {
			n.vol, n.volDiff = nil, true
		}
		nexts[in.ServiceID] = n
		order = append(order, in.ServiceID)
	}
	shared := vs.Shared
	sharedDiff := false
	if r := b.cs.get("", changes.KindShared); r != nil {
		shared = r.Vars.Apply(shared)
		sharedDiff = true
	}

	// Renames rewrite references everywhere in the environment.
	for _, sid := range order {
		n := nexts[sid]
		if !n.in.committed() || n.del || n.cfg.Name == n.in.Config.Name {
			continue
		}
		oldName, newName := n.in.Config.Name, n.cfg.Name
		for _, osid := range order {
			o := nexts[osid]
			for k, v := range o.vars {
				if nv, ok := vars.RewriteService(v.Value, oldName, newName); ok {
					v.Value = nv
					o.vars[k] = v
					o.varsDiff = true
				}
			}
		}
		for k, v := range shared {
			if nv, ok := vars.RewriteService(v.Value, oldName, newName); ok {
				v.Value = nv
				shared[k] = v
				sharedDiff = true
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO name_aliases (environment_id, old_name, service_id, expires_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(environment_id, old_name) DO UPDATE SET service_id = excluded.service_id, expires_at = excluded.expires_at`,
			envID, changes.PrivateName(n.in.Config), sid, now+day); err != nil {
			return cpapi.CommitPlan{}, err
		}
	}

	// Validate everything first (all or nothing).
	names := map[string]string{}
	for _, sid := range order {
		n := nexts[sid]
		if n.del {
			continue
		}
		if other, ok := names[n.cfg.Name]; ok && other != sid {
			return cpapi.CommitPlan{}, fail.Validation("name", "Two services would be named %q.", n.cfg.Name)
		}
		names[n.cfg.Name] = sid
		if err := changes.Validate(n.cfg, n.vol != nil); err != nil {
			return cpapi.CommitPlan{}, err
		}
		if n.vol != nil {
			if err := changes.CheckMountPath(n.vol.MountPath); err != nil {
				return cpapi.CommitPlan{}, err
			}
		}
		vals := map[string]string{}
		for k, v := range n.vars {
			vals[k] = v.Value
		}
		if err := changes.CheckVariables(vals); err != nil {
			return cpapi.CommitPlan{}, err
		}
	}

	// Services referencing a changed shared variable redeploy.
	sharedChanged := map[string]bool{}
	if sharedDiff {
		for k := range shared {
			if vs.Shared[k] != shared[k] {
				sharedChanged[k] = true
			}
		}
		for k := range vs.Shared {
			if _, ok := shared[k]; !ok {
				sharedChanged[k] = true
			}
		}
	}

	plan := cpapi.CommitPlan{CommitID: cid, EnvID: envID, Deploy: req.Deploy, Actor: a.Label(), Message: msg}
	for _, sid := range order {
		n := nexts[sid]
		in := n.in
		switch {
		case !in.committed():
			if _, err := tx.ExecContext(ctx, `UPDATE service_instances SET rev = 1, config = ? WHERE service_id = ? AND environment_id = ?`, jsonString(n.cfg), sid, envID); err != nil {
				return plan, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO config_revisions (service_id, environment_id, rev, config, commit_id, created_at) VALUES (?, ?, 1, ?, ?, ?)`,
				sid, envID, jsonString(n.cfg), cid, now); err != nil {
				return plan, err
			}
			if err := c.writeVars(ctx, tx, envID, sid, n.vars); err != nil {
				return plan, err
			}
			if err := c.syncVolume(ctx, tx, envID, sid, nil, n.vol); err != nil {
				return plan, err
			}
			plan.Items = append(plan.Items, cpapi.PlanItem{ServiceID: sid, Action: changes.ActionCreate, Rebuild: true})
		case n.del:
			for _, q := range []string{
				`UPDATE service_instances SET deleted_at = ? WHERE service_id = ? AND environment_id = ?`,
				`UPDATE volumes SET deleted_at = ? WHERE service_id = ? AND environment_id = ? AND deleted_at IS NULL`,
			} {
				if _, err := tx.ExecContext(ctx, q, now, sid, envID); err != nil {
					return plan, err
				}
			}
			for _, q := range []string{
				`DELETE FROM domains WHERE service_id = ? AND environment_id = ?`, `DELETE FROM tcp_proxies WHERE service_id = ? AND environment_id = ?`,
				`DELETE FROM monitors WHERE service_id = ? AND environment_id = ?`, `DELETE FROM variables WHERE service_id = ? AND environment_id = ?`,
			} {
				if _, err := tx.ExecContext(ctx, q, sid, envID); err != nil {
					return plan, err
				}
			}
			plan.Items = append(plan.Items, cpapi.PlanItem{ServiceID: sid, Action: changes.ActionDelete})
		default:
			refsShared := false
			for _, v := range n.vars {
				for _, r := range vars.References(v.Value) {
					if r.Service == vars.Shared && sharedChanged[r.Name] {
						refsShared = true
					}
				}
			}
			old := in.Config
			action, rebuild, ok := changes.Classify(&old, &n.cfg, n.varsDiff || n.volDiff || refsShared)
			if !ok {
				continue
			}
			rev := in.Rev + 1
			if _, err := tx.ExecContext(ctx, `UPDATE service_instances SET rev = ?, config = ? WHERE service_id = ? AND environment_id = ?`, rev, jsonString(n.cfg), sid, envID); err != nil {
				return plan, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO config_revisions (service_id, environment_id, rev, config, commit_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				sid, envID, rev, jsonString(n.cfg), cid, now); err != nil {
				return plan, err
			}
			if n.varsDiff {
				if err := c.writeVars(ctx, tx, envID, sid, n.vars); err != nil {
					return plan, err
				}
			}
			if n.volDiff {
				var cur *changes.VolumeSpec
				if v, ok := b.vols[sid]; ok {
					cur = &changes.VolumeSpec{Name: v.Name, MountPath: v.MountPath}
				}
				if err := c.syncVolume(ctx, tx, envID, sid, cur, n.vol); err != nil {
					return plan, err
				}
			}
			plan.Items = append(plan.Items, cpapi.PlanItem{ServiceID: sid, Action: action, Rebuild: rebuild})
		}
	}
	if sharedDiff {
		if err := c.writeVars(ctx, tx, envID, "", shared); err != nil {
			return plan, err
		}
	}
	if !req.Deploy {
		kept := plan.Items[:0]
		for _, it := range plan.Items {
			if it.Action == changes.ActionDelete || it.Action == changes.ActionScale {
				kept = append(kept, it)
			}
		}
		plan.Items = kept
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE environment_id = ?`, envID); err != nil {
		return plan, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commits (id, environment_id, message, author, created_at, summary) VALUES (?, ?, ?, ?, ?, ?)`,
		cid, envID, msg, a.Label(), now, jsonString(summary)); err != nil {
		return plan, err
	}
	if err := c.insertActivity(ctx, tx, envID, api.Activity{Kind: "commit", Text: msg, Actor: a.Label(), Ref: cid}); err != nil {
		return plan, err
	}
	c.touchProject(ctx, tx, envID)
	return plan, nil
}

// syncVolume makes the volumes table match want.
func (c *Core) syncVolume(ctx context.Context, tx *sql.Tx, envID, sid string, cur, want *changes.VolumeSpec) error {
	now := c.Now()
	switch {
	case want == nil && cur != nil:
		_, err := tx.ExecContext(ctx, `UPDATE volumes SET deleted_at = ? WHERE environment_id = ? AND service_id = ? AND deleted_at IS NULL`, now, envID, sid)
		return err
	case want != nil && cur == nil:
		_, err := tx.ExecContext(ctx, `INSERT INTO volumes (id, environment_id, service_id, name, mount_path, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			ids.New(ids.Volume), envID, sid, want.Name, want.MountPath, now)
		return err
	case want != nil:
		_, err := tx.ExecContext(ctx, `UPDATE volumes SET name = ?, mount_path = ? WHERE environment_id = ? AND service_id = ? AND deleted_at IS NULL`, want.Name, want.MountPath, envID, sid)
		return err
	}
	return nil
}

// CommitDetail is GET /commits/{id}.
func (c *Core) CommitDetail(ctx context.Context, id string) (api.CommitDetail, error) {
	var d api.CommitDetail
	var sum, deps string
	err := c.st.Reader.QueryRowContext(ctx, `SELECT id, environment_id, message, author, created_at, summary, deployments FROM commits WHERE id = ?`, id).
		Scan(&d.ID, &d.EnvironmentID, &d.Message, &d.Author, &d.CreatedAt, &sum, &deps)
	if store.NoRows(err) {
		return d, fail.NotFound("The commit %s does not exist.", id)
	}
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(sum), &d.Changes)
	_ = json.Unmarshal([]byte(deps), &d.Deployments)
	if d.Changes == nil {
		d.Changes = []api.Change{}
	}
	if d.Deployments == nil {
		d.Deployments = []string{}
	}
	return d, nil
}

// CommitEnv returns the environment of a commit (access checks).
func (c *Core) CommitEnv(ctx context.Context, id string) (string, error) {
	var env string
	err := c.st.Reader.QueryRowContext(ctx, `SELECT environment_id FROM commits WHERE id = ?`, id).Scan(&env)
	if store.NoRows(err) {
		return "", fail.NotFound("The commit %s does not exist.", id)
	}
	return env, err
}

// ---------------------------------------------------------------- canvas

// Canvas is GET /environments/{eid}/canvas.
func (c *Core) Canvas(ctx context.Context, envID string) (api.Canvas, error) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return api.Canvas{}, err
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return api.Canvas{}, err
	}
	vs := groupVars(rows)
	cv := api.Canvas{Project: b.prj, Environment: b.env, Services: []api.Service{}, Edges: []api.Edge{}, Presence: c.presence.list(envID)}
	byName := map[string]string{}
	for _, in := range b.insts {
		cfg, _ := stagedConfig(in, b.cs)
		byName[cfg.Name] = in.ServiceID
		byName[in.Config.Name] = in.ServiceID
	}
	seen := map[string]bool{}
	for _, in := range b.insts {
		cv.Services = append(cv.Services, c.serviceView(ctx, b, in))
		cur := vs.of(in.ServiceID)
		if r := b.cs.get(in.ServiceID, changes.KindVariables); r != nil {
			cur = r.Vars.Apply(cur)
		}
		for _, v := range cur {
			for _, svc := range vars.ReferencedServices(v.Value) {
				if t, ok := byName[svc]; ok && t != in.ServiceID && !seen[in.ServiceID+">"+t] {
					seen[in.ServiceID+">"+t] = true
					cv.Edges = append(cv.Edges, api.Edge{Source: in.ServiceID, Target: t})
				}
			}
		}
	}
	if cv.Groups, err = c.Groups(ctx, c.st.Reader, envID); err != nil {
		return cv, err
	}
	if cv.Groups == nil {
		cv.Groups = []api.Group{}
	}
	if cv.Positions, err = c.positions(ctx, c.st.Reader, envID); err != nil {
		return cv, err
	}
	cv.ChangesCount = c.changesCount(ctx, envID)
	return cv, nil
}
