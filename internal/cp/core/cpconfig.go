// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/mail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/internal/cp/core/webhooks"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/configfile"
	"github.com/musakkka/5050cloud/pkg/proto"
)

var _ cpapi.Config = (*Core)(nil)

// ---------------------------------------------------------------- snapshots

func (c *Core) snapshot(ctx context.Context, b *bundle, vs varSets, in instance) cpapi.ServiceSnapshot {
	s := cpapi.ServiceSnapshot{
		ProjectID: b.prj.ID, ProjectName: b.prj.Name, EnvID: b.env.ID, EnvName: b.env.Name, ServiceID: in.ServiceID,
		Rev: in.Rev, Config: in.Config, PrivateName: changes.PrivateName(in.Config),
		Domains: b.domains[in.ServiceID], TCPProxies: b.proxies[in.ServiceID],
		Egress: b.env.Egress, EgressAllow: b.env.EgressAllow, Deleted: in.Deleted,
	}
	if s.Domains == nil {
		s.Domains = []api.Domain{}
	}
	if s.TCPProxies == nil {
		s.TCPProxies = []api.TCPProxy{}
	}
	if v, ok := b.vols[in.ServiceID]; ok {
		s.Volume = &api.Volume{ID: v.ID, ServiceID: in.ServiceID, Name: v.Name, MountPath: v.MountPath, HostID: v.HostID, AlertAtPct: v.AlertAtPct}
	}
	names := map[string]bool{}
	for _, v := range vs.of(in.ServiceID) {
		for _, svc := range vars.ReferencedServices(v.Value) {
			if svc != in.Config.Name && svc != vars.Shared {
				names[svc] = true
			}
		}
	}
	s.References = sortedKeys(names)
	aliases, _ := store.Strings(ctx, c.st.Reader, `SELECT old_name FROM name_aliases WHERE environment_id = ? AND service_id = ? AND expires_at > ?`, b.env.ID, in.ServiceID, c.Now())
	s.PrivateAliases = aliases
	return s
}

func (c *Core) envSnapshots(ctx context.Context, envID string, only string) ([]cpapi.ServiceSnapshot, error) {
	b, err := c.loadBundleConfig(ctx, envID)
	if err != nil {
		return nil, err
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return nil, err
	}
	vs := groupVars(rows)
	out := []cpapi.ServiceSnapshot{}
	for _, in := range b.insts {
		if !in.committed() || (only != "" && in.ServiceID != only) {
			continue
		}
		out = append(out, c.snapshot(ctx, b, vs, in))
	}
	return out, nil
}

// ServiceInstance implements cpapi.Config (deleted instances included, flagged).
func (c *Core) ServiceInstance(ctx context.Context, envID, serviceID string) (cpapi.ServiceSnapshot, error) {
	list, err := c.envSnapshots(ctx, envID, serviceID)
	if err != nil {
		return cpapi.ServiceSnapshot{}, err
	}
	if len(list) == 1 {
		return list[0], nil
	}
	in, err := c.instance(ctx, c.st.Reader, envID, serviceID)
	if err != nil {
		return cpapi.ServiceSnapshot{}, err
	}
	if !in.committed() {
		return cpapi.ServiceSnapshot{}, fail.NotFound("The service %s is not deployed yet.", serviceID)
	}
	env, prj, _ := c.Environment(ctx, envID)
	return cpapi.ServiceSnapshot{ProjectID: prj.ID, ProjectName: prj.Name, EnvID: envID, EnvName: env.Name, ServiceID: serviceID,
		Rev: in.Rev, Config: in.Config, PrivateName: changes.PrivateName(in.Config), Deleted: true,
		Domains: []api.Domain{}, TCPProxies: []api.TCPProxy{}}, nil
}

// ServiceInstances implements cpapi.Config.
func (c *Core) ServiceInstances(ctx context.Context, envID string) ([]cpapi.ServiceSnapshot, error) {
	return c.envSnapshots(ctx, envID, "")
}

// AllInstances implements cpapi.Config.
func (c *Core) AllInstances(ctx context.Context) ([]cpapi.ServiceSnapshot, error) {
	envs, err := store.Strings(ctx, c.st.Reader, `SELECT e.id FROM environments e JOIN projects p ON p.id = e.project_id
		WHERE e.deleted_at IS NULL AND p.deleted_at IS NULL ORDER BY e.created_at`)
	if err != nil {
		return nil, err
	}
	out := []cpapi.ServiceSnapshot{}
	for _, e := range envs {
		list, err := c.envSnapshots(ctx, e, "")
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

// ---------------------------------------------------------------- variables at deploy

// ResolveVariables implements cpapi.Config.
func (c *Core) ResolveVariables(ctx context.Context, envID, serviceID string, provided map[string]string) (map[string]string, []string, error) {
	b, err := c.loadBundleConfig(ctx, envID)
	if err != nil {
		return nil, nil, err
	}
	in, ok := b.find(serviceID)
	if !ok || !in.committed() {
		return nil, nil, fail.NotFound("The service %s is not deployed in this environment.", serviceID)
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return nil, nil, err
	}
	aliases := c.railwayAliases(ctx, c.st.Reader)
	base := c.providedFor(b, in, false)
	for k, v := range provided {
		base[k] = v
	}
	if aliases {
		base = vars.WithAliases(base)
	}
	sc, _ := c.scopeOf(b, groupVars(rows), false, aliases, map[string]map[string]string{serviceID: base})
	env, unresolved := vars.ResolveService(sc, in.Config.Name)
	return env, unresolved, nil
}

// SecretValues implements cpapi.Config.
func (c *Core) SecretValues(ctx context.Context, envID, serviceID string) ([]string, error) {
	env, _, err := c.ResolveVariables(ctx, envID, serviceID, nil)
	if err != nil {
		return nil, err
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return nil, err
	}
	sealed := map[string]bool{}
	for _, r := range rows {
		if r.ServiceID == serviceID && r.Sealed {
			sealed[r.Name] = true
		}
	}
	return vars.SecretValues(env, sealed), nil
}

// ---------------------------------------------------------------- config files

// MergeConfigFile implements cpapi.Config (9.20).
func (c *Core) MergeConfigFile(ctx context.Context, envID, serviceID, commitSHA string, file *proto.ConfigOverlay) (cpapi.MergedConfig, error) {
	snap, err := c.ServiceInstance(ctx, envID, serviceID)
	if err != nil {
		return cpapi.MergedConfig{}, err
	}
	r := configfile.Merge(snap.Config, file, snap.EnvName)
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		if file == nil {
			_, err := tx.ExecContext(ctx, `DELETE FROM config_files WHERE service_id = ? AND environment_id = ?`, serviceID, envID)
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO config_files (service_id, environment_id, commit_sha, path, format, overlay, lines, seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(service_id, environment_id) DO UPDATE SET commit_sha = excluded.commit_sha, path = excluded.path, format = excluded.format,
			overlay = excluded.overlay, lines = excluded.lines, seen_at = excluded.seen_at`,
			serviceID, envID, commitSHA, file.Path, file.Format, jsonString(file.Config), jsonString(file.Lines), c.Now())
		return err
	})
	if err != nil {
		return cpapi.MergedConfig{}, err
	}
	return cpapi.MergedConfig{Config: r.Config, Source: r.Source, WatchPaths: r.WatchPaths, Notices: r.Notices}, nil
}

// ---------------------------------------------------------------- registry credentials

func regAAD(id string) string { return "registry_credentials|" + id + "|password_enc" }

// RegistryCredential implements cpapi.Config.
func (c *Core) RegistryCredential(ctx context.Context, id string) (string, string, string, error) {
	var user, server string
	var enc []byte
	err := c.st.Reader.QueryRowContext(ctx, `SELECT username, server, password_enc FROM registry_credentials WHERE id = ?`, id).Scan(&user, &server, &enc)
	if store.NoRows(err) {
		return "", "", "", fail.NotFound("The registry credential %s does not exist.", id)
	}
	if err != nil {
		return "", "", "", err
	}
	pw, err := c.box.Open(enc, regAAD(id))
	if err != nil {
		return "", "", "", fail.Internal(err)
	}
	_ = c.st.Exec(ctx, `UPDATE registry_credentials SET last_used_at = ? WHERE id = ?`, c.Now(), id)
	return user, string(pw), server, nil
}

// RegistryCredentials lists stored credentials (never the password).
func (c *Core) RegistryCredentials(ctx context.Context) ([]api.RegistryCredential, error) {
	list, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.RegistryCredential, error) {
		var x api.RegistryCredential
		return x, r.Scan(&x.ID, &x.Name, &x.Server, &x.Username, &x.CreatedAt, &x.LastUsedAt)
	}, `SELECT id, name, server, username, created_at, last_used_at FROM registry_credentials ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	insts, _ := c.AllInstances(ctx)
	for i := range list {
		list[i].UsedBy = []string{}
		for _, in := range insts {
			if in.Config.Source.RegistryCredential == list[i].ID {
				list[i].UsedBy = append(list[i].UsedBy, in.ServiceID)
			}
		}
	}
	if list == nil {
		list = []api.RegistryCredential{}
	}
	return list, nil
}

// CreateRegistryCredential stores a credential, password sealed.
func (c *Core) CreateRegistryCredential(ctx context.Context, req api.CreateRegistryCredentialRequest) (api.RegistryCredential, error) {
	req.Name, req.Server, req.Username = cleanName(req.Name), lower(trim(req.Server)), trim(req.Username)
	if req.Name == "" {
		return api.RegistryCredential{}, fail.Validation("name", "Give the credential a name.")
	}
	if req.Server == "" || strings.Contains(req.Server, "/") {
		return api.RegistryCredential{}, fail.Validation("server", "The server must be a registry host name such as ghcr.io.")
	}
	if req.Username == "" || req.Password == "" {
		return api.RegistryCredential{}, fail.Validation("password", "Enter a username and a password or token.")
	}
	id := ids.New(ids.Registry)
	enc, err := c.box.Seal([]byte(req.Password), regAAD(id))
	if err != nil {
		return api.RegistryCredential{}, err
	}
	now := c.Now()
	if err := c.st.Exec(ctx, `INSERT INTO registry_credentials (id, name, server, username, password_enc, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Server, req.Username, enc, now); err != nil {
		return api.RegistryCredential{}, err
	}
	return api.RegistryCredential{ID: id, Name: req.Name, Server: req.Server, Username: req.Username, CreatedAt: now, UsedBy: []string{}}, nil
}

// DeleteRegistryCredential refuses while a service uses it.
func (c *Core) DeleteRegistryCredential(ctx context.Context, id string) error {
	list, err := c.RegistryCredentials(ctx)
	if err != nil {
		return err
	}
	for _, x := range list {
		if x.ID == id {
			if len(x.UsedBy) > 0 {
				return fail.Conflict("This credential is used by %s; change their image source first.", plural(len(x.UsedBy), "1 service", itoa(len(x.UsedBy))+" services"))
			}
			return c.st.Exec(ctx, `DELETE FROM registry_credentials WHERE id = ?`, id)
		}
	}
	return fail.NotFound("The registry credential %s does not exist.", id)
}

// ---------------------------------------------------------------- orchestrator-driven services

// CloneServiceForRestore implements cpapi.Config: a committed copy of a
// service (config and variables, a new empty volume) under newName.
func (c *Core) CloneServiceForRestore(ctx context.Context, envID, sourceServiceID, newName, targetEnvID string) (string, error) {
	if targetEnvID == "" {
		targetEnvID = envID
	}
	src, err := c.liveInstance(ctx, c.st.Reader, envID, sourceServiceID)
	if err != nil {
		return "", err
	}
	env, _, err := c.Environment(ctx, targetEnvID)
	if err != nil {
		return "", err
	}
	rows, err := c.envVars(ctx, c.st.Reader, envID)
	if err != nil {
		return "", err
	}
	cur := groupVars(rows).of(sourceServiceID)
	values := map[string]string{}
	for k, v := range cur {
		values[k] = v.Value
	}
	vols, _ := c.volumes(ctx, c.st.Reader, envID)
	sid := ids.New(ids.Service)
	a := SystemActor("restore")
	cfg := changes.Clone(src.Config)
	cfg.Net.PrivateName = ""
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		names, err := c.takenNames(ctx, tx, targetEnvID, "")
		if err != nil {
			return err
		}
		cfg.Name = changes.UniqueName(truncName(cleanName(newName)), names)
		if _, err := c.createCommitted(ctx, tx, a, env, sid, cfg, values, "Restore copy of "+src.Config.Name); err != nil {
			return err
		}
		if v, ok := vols[sourceServiceID]; ok {
			if err := c.syncVolume(ctx, tx, targetEnvID, sid, nil, &changes.VolumeSpec{Name: changes.Slug(cfg.Name) + "-volume", MountPath: v.MountPath}); err != nil {
				return err
			}
		}
		pos, _ := c.positions(ctx, tx, envID)
		if p, ok := pos[sourceServiceID]; ok {
			return c.setPosition(ctx, tx, targetEnvID, sid, [2]float64{p[0] + 60, p[1] + 60})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if svc, err := c.Service(ctx, targetEnvID, sid); err == nil {
		c.publishService(ctx, svc)
	}
	return sid, nil
}

// SwapServiceNames implements cpapi.Config (Promote): keep takes the
// other's name, other becomes otherNewName; references follow the names.
func (c *Core) SwapServiceNames(ctx context.Context, envID, keepNameServiceID, otherServiceID, otherNewName string) error {
	a := SystemActor("promote")
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		keep, err := c.liveInstance(ctx, tx, envID, keepNameServiceID)
		if err != nil {
			return err
		}
		other, err := c.liveInstance(ctx, tx, envID, otherServiceID)
		if err != nil {
			return err
		}
		target := other.Config.Name
		keepOld := keep.Config.Name
		now := c.Now()
		write := func(in instance, name string) error {
			in.Config.Name = name
			in.Config.Net.PrivateName = ""
			rev := in.Rev + 1
			if _, err := tx.ExecContext(ctx, `UPDATE service_instances SET rev = ?, config = ? WHERE service_id = ? AND environment_id = ?`, rev, jsonString(in.Config), in.ServiceID, envID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO config_revisions (service_id, environment_id, rev, config, created_at) VALUES (?, ?, ?, ?, ?)`, in.ServiceID, envID, rev, jsonString(in.Config), now)
			return err
		}
		if err := write(other, otherNewName); err != nil {
			return err
		}
		if err := write(keep, target); err != nil {
			return err
		}
		// References to the old name now point at the promoted copy by name; references to the copy's old name follow it.
		vrows, err := c.envVars(ctx, tx, envID)
		if err != nil {
			return err
		}
		vs := groupVars(vrows)
		for _, sid := range append([]string{""}, keepNameServiceID, otherServiceID) {
			cur := vs.of(sid)
			changed := false
			for k, v := range cur {
				if nv, ok := vars.RewriteService(v.Value, keepOld, target); ok {
					v.Value, changed = nv, true
					cur[k] = v
				}
			}
			if changed {
				if err := c.writeVars(ctx, tx, envID, sid, cur); err != nil {
					return err
				}
			}
		}
		return c.insertActivity(ctx, tx, envID, api.Activity{ServiceID: keepNameServiceID, Kind: "promote", Text: keepOld + " promoted to " + target, Actor: a.Label()})
	})
	if err == nil {
		c.publishAll(ctx, envID)
		if c.orch != nil {
			c.orch.NetworkChanged(ctx, envID)
		}
	}
	return err
}

// CommitVariables implements cpapi.Config (Regenerate credentials).
func (c *Core) CommitVariables(ctx context.Context, envID, serviceID string, set map[string]string, actor, message string) (api.CommitResponse, error) {
	a := SystemActor(actor)
	var plan cpapi.CommitPlan
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		in, err := c.liveInstance(ctx, tx, envID, serviceID)
		if err != nil {
			return err
		}
		vrows, err := c.envVars(ctx, tx, envID)
		if err != nil {
			return err
		}
		vs := groupVars(vrows)
		cur := vs.of(serviceID)
		ops := changes.VarOps{Set: set}
		diff := changes.DiffVars("variables", cur, ops)
		next := ops.Apply(cur)
		if err := c.writeVars(ctx, tx, envID, serviceID, next); err != nil {
			return err
		}
		cid := ids.New(ids.Commit)
		if message == "" {
			message = "Update variables of " + in.Config.Name
		}
		summary := []api.Change{{ID: ids.New(ids.Change), ServiceID: serviceID, Service: in.Config.Name, Kind: changes.KindVariables, Diff: diff, Author: a.Label(), CreatedAt: c.Now()}}
		if _, err := tx.ExecContext(ctx, `INSERT INTO commits (id, environment_id, message, author, created_at, summary) VALUES (?, ?, ?, ?, ?, ?)`,
			cid, envID, message, a.Label(), c.Now(), jsonString(summary)); err != nil {
			return err
		}
		plan = cpapi.CommitPlan{CommitID: cid, EnvID: envID, Deploy: true, Actor: a.Label(), Message: message,
			Items: []cpapi.PlanItem{{ServiceID: serviceID, Action: changes.ActionUpdate}}}
		// Services that reference this one redeploy too.
		insts, err := c.instances(ctx, tx, envID)
		if err != nil {
			return err
		}
		for _, o := range insts {
			if !o.committed() || o.ServiceID == serviceID {
				continue
			}
			for _, v := range vs.of(o.ServiceID) {
				if contains(vars.ReferencedServices(v.Value), in.Config.Name) {
					plan.Items = append(plan.Items, cpapi.PlanItem{ServiceID: o.ServiceID, Action: changes.ActionUpdate})
					break
				}
			}
		}
		return c.insertActivity(ctx, tx, envID, api.Activity{ServiceID: serviceID, Kind: "commit", Text: message, Actor: a.Label(), Ref: cid})
	})
	if err != nil {
		return api.CommitResponse{}, err
	}
	resp := api.CommitResponse{CommitID: plan.CommitID, Deployments: []api.Deployment{}}
	if c.orch != nil {
		deps, err := c.orch.ApplyCommit(ctx, plan)
		if err != nil {
			return resp, err
		}
		if deps != nil {
			resp.Deployments = deps
		}
	}
	c.recordCommitDeployments(ctx, plan.CommitID, resp.Deployments)
	return resp, nil
}

// ---------------------------------------------------------------- notices

var defaultPrefs = map[string]api.NotificationPref{
	api.EventDeploymentFailed:  {Event: api.EventDeploymentFailed, Email: true, InApp: true},
	api.EventDeploymentCrashed: {Event: api.EventDeploymentCrashed, Email: true, InApp: true},
	api.EventBackupFailed:      {Event: api.EventBackupFailed, Email: true, InApp: true},
	api.EventDiskHigh:          {Event: api.EventDiskHigh, Email: true, InApp: true},
	api.EventVolumeHigh:        {Event: api.EventVolumeHigh, Email: true, InApp: true},
	api.EventMonitorTriggered:  {Event: api.EventMonitorTriggered, Email: true, InApp: true},
	api.EventHostOffline:       {Event: api.EventHostOffline, Email: true, InApp: true},
}

// NotificationPrefs is GET /me/notification-prefs.
func (c *Core) NotificationPrefs(ctx context.Context, userID string) ([]api.NotificationPref, error) {
	stored, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.NotificationPref, error) {
		var p api.NotificationPref
		var e, i int
		err := r.Scan(&p.Event, &e, &i)
		p.Email, p.InApp = e == 1, i == 1
		return p, err
	}, `SELECT event, email, in_app FROM notification_prefs WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	m := map[string]api.NotificationPref{}
	for k, v := range defaultPrefs {
		m[k] = v
	}
	for _, p := range stored {
		m[p.Event] = p
	}
	out := []api.NotificationPref{}
	for _, k := range sortedKeys(m) {
		out = append(out, m[k])
	}
	return out, nil
}

// PutNotificationPrefs is PUT /me/notification-prefs.
func (c *Core) PutNotificationPrefs(ctx context.Context, userID string, in []api.NotificationPref) ([]api.NotificationPref, error) {
	for _, p := range in {
		if _, ok := defaultPrefs[p.Event]; !ok {
			return nil, fail.Validation("event", "%q is not a notification event.", p.Event)
		}
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		for _, p := range in {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notification_prefs (user_id, event, email, in_app) VALUES (?, ?, ?, ?)
				ON CONFLICT(user_id, event) DO UPDATE SET email = excluded.email, in_app = excluded.in_app`, userID, p.Event, store.Bool(p.Email), store.Bool(p.InApp)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c.NotificationPrefs(ctx, userID)
}

// Notifications is GET /me/notifications.
func (c *Core) Notifications(ctx context.Context, userID, before string) (api.Page[api.Notification], error) {
	q := `SELECT id, ts, kind, text, ref, link, ifnull(read_at, 0) FROM notifications WHERE user_id = ?`
	args := []any{userID}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT 51`
	items, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.Notification, error) {
		var n api.Notification
		return n, r.Scan(&n.ID, &n.TS, &n.Kind, &n.Text, &n.Ref, &n.Link, &n.ReadAt)
	}, q, args...)
	if err != nil {
		return api.Page[api.Notification]{}, err
	}
	p := api.Page[api.Notification]{Items: items}
	if len(items) > 50 {
		p.Items, p.Next = items[:50], items[49].ID
	}
	if p.Items == nil {
		p.Items = []api.Notification{}
	}
	return p, nil
}

// MarkNotificationsRead is POST /me/notifications/read.
func (c *Core) MarkNotificationsRead(ctx context.Context, userID string, req api.MarkNotificationsReadRequest) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		if req.All {
			_, err := tx.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, c.Now(), userID)
			return err
		}
		for _, id := range req.IDs {
			if _, err := tx.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND id = ? AND read_at IS NULL`, c.Now(), userID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Notify implements cpapi.Config: the bell and email by preferences,
// project webhooks, and install alerts for Alert* events.
func (c *Core) Notify(ctx context.Context, n cpapi.Notice) error {
	in, _ := c.installRow(ctx, c.st.Reader)
	link := ""
	if n.EnvID != "" {
		if env, _, err := c.Environment(ctx, n.EnvID); err == nil {
			link = "/project/" + env.ProjectID + "?environment=" + n.EnvID
			if n.ServiceID != "" {
				link += "&service=" + n.ServiceID
			}
		}
	}
	type target struct {
		id, email string
		inApp     bool
		mail      bool
	}
	var targets []target
	if n.ProjectID != "" || n.EnvID != "" {
		users, err := c.Members(ctx)
		if err != nil {
			return err
		}
		pid := n.ProjectID
		if pid == "" {
			pid = c.EnvProject(ctx, n.EnvID)
		}
		for _, u := range users {
			if pid != "" {
				lvl, err := c.ProjectLevel(ctx, Actor{UserID: u.ID, Role: u.Role}, pid)
				if err != nil || lvl == 0 {
					continue
				}
			}
			t := target{id: u.ID, email: u.Email}
			if len(n.Users) > 0 {
				t.mail = contains(n.Users, u.ID)
				t.inApp = t.mail
			} else {
				prefs, _ := c.NotificationPrefs(ctx, u.ID)
				for _, p := range prefs {
					if p.Event == n.Event {
						t.mail, t.inApp = p.Email, p.InApp
					}
				}
			}
			targets = append(targets, t)
		}
	}
	var emails []string
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		for _, t := range targets {
			if t.inApp {
				nt := api.Notification{ID: ids.New(ids.Notification), TS: c.Now(), Kind: n.Event, Text: n.Text, Ref: n.Ref, Link: link}
				if _, err := tx.ExecContext(ctx, `INSERT INTO notifications (id, user_id, ts, kind, text, ref, link) VALUES (?, ?, ?, ?, ?, ?, ?)`,
					nt.ID, t.id, nt.TS, nt.Kind, nt.Text, nt.Ref, nt.Link); err != nil {
					return err
				}
				c.broker.PublishUser(t.id, api.Event{Type: "notification", Notification: &nt})
			}
			if t.mail {
				emails = append(emails, t.email)
			}
		}
		pid := n.ProjectID
		if pid == "" && n.EnvID != "" {
			pid = c.EnvProject(ctx, n.EnvID)
		}
		body := webhooks.Body{Project: pid, Environment: n.EnvID, Service: n.ServiceID, Deployment: n.Ref, Details: n.Details}
		if pid != "" {
			if err := c.enqueueHooks(ctx, tx, pid, n.Event, n.Webhooks, body); err != nil {
				return err
			}
		}
		if strings.Contains(n.Event, ".") && n.ProjectID == "" && n.EnvID == "" {
			al, _ := c.Alerts(ctx)
			if contains(al.Events, n.Event) {
				emails = append(emails, al.Emails...)
				if al.WebhookURL != "" {
					if err := c.hooks.Enqueue(ctx, tx, "", al.WebhookURL, n.Event, body); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(emails) > 0 {
		sm, err := c.smtpSettings(ctx, c.st.Reader)
		if err == nil && sm.Host != "" {
			subj, text := mail.Notice(in.Name, n.Text, c.ConsoleURL(ctx)+link)
			go func(to []string) {
				if err := c.mailer(sm).Send(context.WithoutCancel(ctx), mail.Message{To: uniq(to), Subject: subj, Text: text}); err != nil {
					c.log.Warn("cannot send a notification email", "err", err)
				}
			}(emails)
		}
	}
	return nil
}

// FireWebhook implements cpapi.Config.
func (c *Core) FireWebhook(ctx context.Context, projectID, event string, payload any) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		return c.enqueueHooks(ctx, tx, projectID, event, nil, webhooks.Body{Project: projectID, Details: payload})
	})
}

func (c *Core) enqueueHooks(ctx context.Context, tx *sql.Tx, projectID, event string, only []string, body webhooks.Body) error {
	hooks, err := c.projectWebhooks(ctx, tx, projectID)
	if err != nil {
		return err
	}
	for _, h := range hooks {
		if !h.Enabled || (len(only) > 0 && !contains(only, h.ID)) {
			continue
		}
		if len(only) == 0 && !webhooks.Subscribed(h.Events, h.Preset, event) {
			continue
		}
		if err := c.hooks.Enqueue(ctx, tx, h.ID, "", event, body); err != nil {
			return err
		}
	}
	return nil
}
