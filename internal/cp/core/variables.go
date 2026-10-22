// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/github"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ---------------------------------------------------------------- scope

// providedFor computes the provided variables of a service that do not
// depend on a deployment or replica (others are added by the orchestrator
// at deploy time). They let ${{api.RAILWAY_PUBLIC_DOMAIN}} resolve.
func (c *Core) providedFor(b *bundle, in instance, aliases bool) map[string]string {
	cfg := in.Config
	p := map[string]string{
		"FIFTY_PRIVATE_DOMAIN":    changes.PrivateName(cfg) + ".internal",
		"FIFTY_PROJECT_ID":        b.prj.ID,
		"FIFTY_PROJECT_NAME":      b.prj.Name,
		"FIFTY_ENVIRONMENT_ID":    b.env.ID,
		"FIFTY_ENVIRONMENT_NAME":  b.env.Name,
		"FIFTY_SERVICE_ID":        in.ServiceID,
		"FIFTY_SERVICE_NAME":      cfg.Name,
		"FIFTY_PUBLIC_DOMAIN":     "",
		"FIFTY_TCP_PROXY_DOMAIN":  "",
		"FIFTY_TCP_PROXY_PORT":    "",
		"FIFTY_VOLUME_NAME":       "",
		"FIFTY_VOLUME_MOUNT_PATH": "",
	}
	port := cfg.Net.Port
	if port == 0 {
		port = 8080
	}
	p["PORT"] = strconv.Itoa(port)
	for _, d := range b.domains[in.ServiceID] {
		if p["FIFTY_PUBLIC_DOMAIN"] == "" || d.Kind == "custom" && strings.HasSuffix(p["FIFTY_PUBLIC_DOMAIN"], b.base) {
			p["FIFTY_PUBLIC_DOMAIN"] = d.Domain
		}
	}
	if px := b.proxies[in.ServiceID]; len(px) > 0 {
		p["FIFTY_TCP_PROXY_DOMAIN"] = b.base
		p["FIFTY_TCP_PROXY_PORT"] = strconv.Itoa(px[0].HostPort)
		p["FIFTY_TCP_APPLICATION_PORT"] = strconv.Itoa(px[0].TargetPort)
	}
	if v, ok := b.vols[in.ServiceID]; ok {
		p["FIFTY_VOLUME_NAME"], p["FIFTY_VOLUME_MOUNT_PATH"] = v.Name, v.MountPath
	}
	if aliases {
		p = vars.WithAliases(p)
	}
	return p
}

// scopeOf builds the reference scope of an environment from committed
// variables (or the staged state when staged is set), keyed by service name.
func (c *Core) scopeOf(b *bundle, vs varSets, staged bool, aliases bool, override map[string]map[string]string) (vars.Scope, map[string]string) {
	sc := vars.Scope{Shared: map[string]string{}, Services: map[string]map[string]string{}}
	nameOf := map[string]string{}
	shared := vs.Shared
	if staged {
		if r := b.cs.get("", changes.KindShared); r != nil {
			shared = r.Vars.Apply(shared)
		}
	}
	for k, v := range shared {
		sc.Shared[k] = v.Value
	}
	for _, in := range b.insts {
		if !staged && !in.committed() {
			continue
		}
		cfg := in.Config
		cur := vs.of(in.ServiceID)
		if staged {
			cfg, _ = stagedConfig(in, b.cs)
			if r := b.cs.get(in.ServiceID, changes.KindVariables); r != nil {
				cur = r.Vars.Apply(cur)
			}
		}
		inst := in
		inst.Config = cfg
		user := map[string]string{}
		for k, v := range cur {
			user[k] = v.Value
		}
		prov := c.providedFor(b, inst, aliases)
		if o, ok := override[in.ServiceID]; ok {
			prov = o
		}
		sc.Services[cfg.Name] = vars.Merge(prov, user)
		nameOf[in.ServiceID] = cfg.Name
	}
	return sc, nameOf
}

// ---------------------------------------------------------------- read

var suggestCache = struct {
	sync.Mutex
	m map[string]suggestEntry
}{m: map[string]suggestEntry{}}

type suggestEntry struct {
	names []string
	at    time.Time
}

// RememberScan caches the variable names a scan found, for the Suggested list.
func RememberScan(repo, branch, root string, r api.ScanResult) {
	suggestCache.Lock()
	defer suggestCache.Unlock()
	suggestCache.m[repo+"|"+branch+"|"+root] = suggestEntry{names: r.EnvNames, at: time.Now()}
}

func suggested(cfg api.ServiceConfig) []string {
	if cfg.Source.Type != "github" {
		return nil
	}
	suggestCache.Lock()
	defer suggestCache.Unlock()
	e, ok := suggestCache.m[cfg.Source.Repo+"|"+cfg.Source.Branch+"|"+cfg.Source.RootDir]
	if !ok || time.Since(e.at) > time.Hour {
		return nil
	}
	return e.names
}

// Variables is GET .../variables (serviceID "" = shared variables). Values
// are never returned here; references are.
func (c *Core) Variables(ctx context.Context, envID, serviceID string) (api.VariablesResponse, error) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return api.VariablesResponse{}, err
	}
	var in instance
	if serviceID != "" {
		var ok bool
		if in, ok = b.find(serviceID); !ok {
			return api.VariablesResponse{}, fail.NotFound("The service %s does not exist in environment %s.", serviceID, b.env.Name)
		}
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return api.VariablesResponse{}, err
	}
	vs := groupVars(rows)
	aliases := c.railwayAliases(ctx, c.st.Reader)
	sc, _ := c.scopeOf(b, vs, true, aliases, nil)
	cur := vs.of(serviceID)
	kind := changes.KindVariables
	if serviceID == "" {
		kind = changes.KindShared
	}
	ops := changes.VarOps{}
	if r := b.cs.get(serviceID, kind); r != nil {
		ops = r.Vars
	}
	after := ops.Apply(cur)
	selfName := vars.Shared
	if serviceID != "" {
		cfg, _ := stagedConfig(in, b.cs)
		selfName = cfg.Name
	}
	resp := api.VariablesResponse{Variables: []api.Variable{}, Provided: vars.ProvidedNames(aliases)}
	names := map[string]bool{}
	for k := range cur {
		names[k] = true
	}
	for k := range after {
		names[k] = true
	}
	for _, name := range sortedKeys(names) {
		v, live := after[name]
		if !live {
			v = cur[name]
		}
		out := api.Variable{Name: name, Sealed: v.Sealed, Staged: ops.StagedState(name, cur), UpdatedAt: vs.Updated[serviceID+"|"+name]}
		if showReference(name, v) {
			out.Reference = v.Value
		}
		if live && vars.HasReference(v.Value) {
			out.Unresolved = vars.Unresolved(sc, selfName, v.Value)
		}
		out.NeedsValue = contains(ops.NeedsValue, name)
		resp.Variables = append(resp.Variables, out)
	}
	if serviceID != "" {
		for _, n := range suggested(in.Config) {
			if !names[n] {
				resp.Suggested = append(resp.Suggested, n)
			}
		}
	}
	return resp, nil
}

// showReference reports whether a value's raw template may be shown: it
// contains a reference, is not sealed, and either the name does not look
// secret or the value is nothing but references.
func showReference(name string, v changes.Var) bool {
	if v.Sealed || !vars.HasReference(v.Value) {
		return false
	}
	if !vars.IsSecretName(name) {
		return true
	}
	parts, err := vars.Parse(v.Value)
	if err != nil {
		return false
	}
	for _, p := range parts {
		if p.Ref == nil && strings.TrimSpace(p.Literal) != "" {
			return false
		}
	}
	return true
}

// Reveal returns values (staged state) for the editor; sealed values never.
func (c *Core) Reveal(ctx context.Context, envID, serviceID string, req api.RevealRequest) (api.RevealResponse, error) {
	b, err := c.loadBundle(ctx, envID)
	if err != nil {
		return api.RevealResponse{}, err
	}
	selfName := vars.Shared
	if serviceID != "" {
		in, ok := b.find(serviceID)
		if !ok {
			return api.RevealResponse{}, fail.NotFound("The service %s does not exist in environment %s.", serviceID, b.env.Name)
		}
		cfg, _ := stagedConfig(in, b.cs)
		selfName = cfg.Name
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return api.RevealResponse{}, err
	}
	vs := groupVars(rows)
	kind := changes.KindVariables
	if serviceID == "" {
		kind = changes.KindShared
	}
	cur := vs.of(serviceID)
	if r := b.cs.get(serviceID, kind); r != nil {
		cur = r.Vars.Apply(cur)
	}
	want := req.Names
	if len(want) == 0 {
		want = sortedKeys(cur)
	}
	var sc vars.Scope
	if req.Resolved {
		sc, _ = c.scopeOf(b, vs, true, c.railwayAliases(ctx, c.st.Reader), nil)
	}
	out := api.RevealResponse{Values: map[string]string{}}
	for _, n := range want {
		v, ok := cur[n]
		if !ok {
			continue
		}
		if v.Sealed {
			if len(req.Names) > 0 {
				return api.RevealResponse{}, fail.Forbidden("%s is sealed. Sealed values can never be revealed.", n)
			}
			continue
		}
		val := v.Value
		if req.Resolved && vars.HasReference(val) {
			val, _ = vars.ResolveValue(sc, selfName, val)
		}
		out.Values[n] = val
	}
	return out, nil
}

// ---------------------------------------------------------------- stage

// SetVariables stages variable edits (serviceID "" = shared variables).
// Template functions are evaluated now, once, when the value is saved.
func (c *Core) SetVariables(ctx context.Context, a Actor, envID, serviceID string, req api.SetVariablesRequest) (api.VariablesResponse, error) {
	if err := c.stageVariables(ctx, a, envID, serviceID, req, ""); err != nil {
		return api.VariablesResponse{}, err
	}
	if serviceID != "" {
		if svc, err := c.Service(ctx, envID, serviceID); err == nil {
			c.publishService(ctx, svc)
		}
	}
	c.publishChanges(ctx, envID)
	return c.Variables(ctx, envID, serviceID)
}

func (c *Core) stageVariables(ctx context.Context, a Actor, envID, serviceID string, req api.SetVariablesRequest, tag string) error {
	if _, _, err := c.Environment(ctx, envID); err != nil {
		return err
	}
	kind := changes.KindVariables
	if serviceID == "" {
		kind = changes.KindShared
	}
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		if serviceID != "" {
			if _, err := c.liveInstance(ctx, tx, envID, serviceID); err != nil {
				return err
			}
		}
		rows, err := c.envVars(ctx, tx, envID)
		if err != nil {
			return err
		}
		cur := groupVars(rows).of(serviceID)
		chs, err := c.envChanges(ctx, tx, envID)
		if err != nil {
			return err
		}
		staged := changes.VarOps{}
		if r := indexChanges(chs).get(serviceID, kind); r != nil {
			staged = r.Vars
		}
		before := staged.Apply(cur)
		ops, err := c.opsFromRequest(req, before)
		if err != nil {
			return err
		}
		return c.stage(ctx, tx, envID, serviceID, kind, a.Label(), func(r *changeRow) {
			r.Vars = r.Vars.Merge(ops).Normalize(cur)
			if tag != "" {
				r.Tag = tag
			}
		})
	})
}

// opsFromRequest turns a SetVariablesRequest into ops against the staged
// state before it (for the Raw Editor and seal checks).
func (c *Core) opsFromRequest(req api.SetVariablesRequest, before map[string]changes.Var) (changes.VarOps, error) {
	ops := changes.VarOps{Set: map[string]string{}}
	if req.Raw != nil {
		parsed, err := vars.ParseRaw(*req.Raw)
		if err != nil {
			return ops, fail.Validation("raw", "The text is not valid .env or JSON: %s.", strings.TrimSuffix(err.Error(), "."))
		}
		for k, v := range parsed {
			if b, ok := before[k]; ok && b.Sealed {
				continue // sealed values are not in the Raw Editor and stay as they are
			}
			ops.Set[k] = v
		}
		for k, v := range before {
			if _, ok := parsed[k]; !ok && !v.Sealed {
				ops.Unset = append(ops.Unset, k)
			}
		}
	}
	for k, v := range req.Set {
		if b, ok := before[k]; ok && b.Sealed && b.Value != v {
			// A sealed variable may get a new value, but stays sealed.
			ops.Seal = append(ops.Seal, k)
		}
		ops.Set[k] = v
	}
	for k, v := range ops.Set {
		if b, ok := before[k]; ok && b.Value == v {
			continue
		}
		ev, err := vars.EvalFunctions(v, nil)
		if err != nil {
			return ops, fail.Validation("variables."+k, "The value of %s is not valid: %s.", k, strings.TrimSuffix(err.Error(), "."))
		}
		ops.Set[k] = ev
	}
	if err := changes.CheckVariables(ops.Set); err != nil {
		return ops, err
	}
	for _, k := range req.Unset {
		if !vars.ValidName(k) {
			return ops, fail.Validation("unset", "%q is not a variable name.", k)
		}
		delete(ops.Set, k)
		ops.Unset = append(ops.Unset, k)
	}
	for _, k := range req.Seal {
		if _, ok := before[k]; !ok {
			if _, staged := ops.Set[k]; !staged {
				return ops, fail.Validation("seal", "There is no variable named %s to seal.", k)
			}
		}
		ops.Seal = append(ops.Seal, k)
	}
	sort.Strings(ops.Unset)
	ops.Unset = uniq(ops.Unset)
	ops.Seal = uniq(ops.Seal)
	if len(ops.Set) == 0 {
		ops.Set = nil
	}
	return ops, nil
}

// ShareVariable stages NAME=${{shared.NAME}} in the chosen services.
func (c *Core) ShareVariable(ctx context.Context, a Actor, envID string, req api.ShareVariableRequest) error {
	if !vars.ValidName(req.Name) {
		return fail.Validation("name", "%q is not a variable name.", req.Name)
	}
	if len(req.Services) == 0 {
		return fail.Validation("services", "Choose at least one service to share %s with.", req.Name)
	}
	resp, err := c.Variables(ctx, envID, "")
	if err != nil {
		return err
	}
	found := false
	for _, v := range resp.Variables {
		if v.Name == req.Name && v.Staged != "delete" {
			found = true
		}
	}
	if !found {
		return fail.Validation("name", "There is no shared variable named %s.", req.Name)
	}
	ref := "${{shared." + req.Name + "}}"
	for _, sid := range uniq(req.Services) {
		if err := c.stageVariables(ctx, a, envID, sid, api.SetVariablesRequest{Set: map[string]string{req.Name: ref}}, ""); err != nil {
			return err
		}
	}
	c.publishChanges(ctx, envID)
	return nil
}

// ImportHeroku stages variables pasted from `heroku config --json` or `-s`.
func (c *Core) ImportHeroku(ctx context.Context, a Actor, envID, serviceID, text string) (api.Import, error) {
	_, prj, err := c.Environment(ctx, envID)
	if err != nil {
		return api.Import{}, err
	}
	parsed, err := vars.ParseHeroku(text)
	if err != nil {
		return api.Import{}, fail.Validation("body", "The text is not `heroku config --json` or `heroku config -s` output: %s.", strings.TrimSuffix(err.Error(), "."))
	}
	if len(parsed) == 0 {
		return api.Import{}, fail.Validation("body", "No variables were found in the pasted text.")
	}
	rep := api.ImportReport{Landed: []api.ImportItem{}, Skipped: []api.ImportItem{}}
	set := map[string]string{}
	for _, k := range sortedKeys(parsed) {
		if err := vars.CheckName(k); err != nil {
			rep.Skipped = append(rep.Skipped, api.ImportItem{Kind: "variable", Name: k, Detail: "The name is not a valid variable name."})
			continue
		}
		if err := vars.CheckValue(k, parsed[k]); err != nil {
			rep.Skipped = append(rep.Skipped, api.ImportItem{Kind: "variable", Name: k, Detail: "The value is larger than 64 KiB."})
			continue
		}
		set[k] = parsed[k]
		rep.Landed = append(rep.Landed, api.ImportItem{Kind: "variable", Name: k, Detail: "Staged"})
	}
	if len(set) > 0 {
		if err := c.stageVariables(ctx, a, envID, serviceID, api.SetVariablesRequest{Set: set}, ""); err != nil {
			return api.Import{}, err
		}
	}
	imp := api.Import{ID: ids.New(ids.Import), Kind: "heroku_env", ProjectID: prj.ID, EnvironmentID: envID, Status: "done", Report: rep, CreatedAt: c.Now()}
	if err := c.saveImport(ctx, imp); err != nil {
		return api.Import{}, err
	}
	c.publishChanges(ctx, envID)
	return imp, nil
}

// suggestFromScan runs a scan in the background to fill the Suggested list.
func (c *Core) suggestFromScan(ctx context.Context, cfg api.ServiceConfig) {
	if cfg.Source.Type != "github" || suggested(cfg) != nil {
		return
	}
	ref, ok := github.ParseRepo(cfg.Source.Repo)
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		tok, _ := c.GitHubCloneToken(ctx, cfg.Source.Repo)
		r, err := github.ScanRemote(ctx, c.gh, c.git, ref, tok, cfg.Source.Branch, cfg.Source.RootDir)
		if err == nil {
			RememberScan(cfg.Source.Repo, cfg.Source.Branch, cfg.Source.RootDir, r)
		}
	}()
}
