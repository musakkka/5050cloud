// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/changes"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
)

// ---------------------------------------------------------------- instances

// instance is a service_instances row. Rev 0 means the creation is still
// staged: the row holds the staged config so names stay unique.
type instance struct {
	ServiceID string
	EnvID     string
	ProjectID string
	Rev       int64
	Config    api.ServiceConfig
	CreatedAt int64
	Deleted   bool
}

func (i instance) committed() bool { return i.Rev > 0 }

const instCols = `si.service_id, si.environment_id, s.project_id, si.rev, si.config, si.created_at, si.deleted_at`

func scanInstance(r interface{ Scan(...any) error }) (instance, error) {
	var in instance
	var cfg string
	var del sql.NullInt64
	if err := r.Scan(&in.ServiceID, &in.EnvID, &in.ProjectID, &in.Rev, &cfg, &in.CreatedAt, &del); err != nil {
		return in, err
	}
	in.Deleted = del.Valid
	if err := json.Unmarshal([]byte(cfg), &in.Config); err != nil {
		return in, fail.Wrapf(err, "config of %s", in.ServiceID)
	}
	return in, nil
}

// instances lists the live instances of an environment (staged creates included).
func (c *Core) instances(ctx context.Context, q store.Querier, envID string) ([]instance, error) {
	return store.All(ctx, q, func(r *sql.Rows) (instance, error) { return scanInstance(r) },
		`SELECT `+instCols+` FROM service_instances si JOIN services s ON s.id = si.service_id
		 WHERE si.environment_id = ? AND si.deleted_at IS NULL ORDER BY si.created_at, si.service_id`, envID)
}

// instance loads one instance, deleted ones included.
func (c *Core) instance(ctx context.Context, q store.Querier, envID, serviceID string) (instance, error) {
	in, err := scanInstance(q.QueryRowContext(ctx, `SELECT `+instCols+` FROM service_instances si JOIN services s ON s.id = si.service_id
		WHERE si.environment_id = ? AND si.service_id = ?`, envID, serviceID))
	if store.NoRows(err) {
		return in, fail.NotFound("The service %s does not exist in this environment.", serviceID)
	}
	return in, err
}

// liveInstance is instance without deleted ones.
func (c *Core) liveInstance(ctx context.Context, q store.Querier, envID, serviceID string) (instance, error) {
	in, err := c.instance(ctx, q, envID, serviceID)
	if err == nil && in.Deleted {
		return in, fail.NotFound("The service %s was deleted.", serviceID)
	}
	return in, err
}

// ---------------------------------------------------------------- variables

func varAAD(id string) string { return "variables|" + id + "|value_enc" }

type varRow struct {
	ID        string
	ServiceID string // "" = shared
	Name      string
	Value     string
	Sealed    bool
	UpdatedAt int64
}

// envVars loads every committed variable of an environment, decrypted.
func (c *Core) envVars(ctx context.Context, q store.Querier, envID string) ([]varRow, error) {
	type raw struct {
		varRow
		enc []byte
	}
	rows, err := store.All(ctx, q, func(r *sql.Rows) (raw, error) {
		var x raw
		var sid sql.NullString
		var sealed int
		err := r.Scan(&x.ID, &sid, &x.Name, &x.enc, &sealed, &x.UpdatedAt)
		x.ServiceID, x.Sealed = sid.String, sealed == 1
		return x, err
	}, `SELECT id, service_id, name, value_enc, sealed, updated_at FROM variables WHERE environment_id = ? ORDER BY name`, envID)
	if err != nil {
		return nil, err
	}
	out := make([]varRow, 0, len(rows))
	for _, x := range rows {
		pt, err := c.box.Open(x.enc, varAAD(x.ID))
		if err != nil {
			return nil, fail.Internal(fail.Wrapf(err, "open variable %s", x.ID))
		}
		x.Value = string(pt)
		out = append(out, x.varRow)
	}
	return out, nil
}

// varSets groups variables: shared and by service id.
type varSets struct {
	Shared  map[string]changes.Var
	Service map[string]map[string]changes.Var
	Updated map[string]int64 // "<sid>|<name>" -> updated_at
}

func groupVars(rows []varRow) varSets {
	s := varSets{Shared: map[string]changes.Var{}, Service: map[string]map[string]changes.Var{}, Updated: map[string]int64{}}
	for _, r := range rows {
		v := changes.Var{Value: r.Value, Sealed: r.Sealed}
		s.Updated[r.ServiceID+"|"+r.Name] = r.UpdatedAt
		if r.ServiceID == "" {
			s.Shared[r.Name] = v
			continue
		}
		if s.Service[r.ServiceID] == nil {
			s.Service[r.ServiceID] = map[string]changes.Var{}
		}
		s.Service[r.ServiceID][r.Name] = v
	}
	return s
}

func (s varSets) of(sid string) map[string]changes.Var {
	if sid == "" {
		return s.Shared
	}
	if m := s.Service[sid]; m != nil {
		return m
	}
	return map[string]changes.Var{}
}

// writeVars replaces the committed variables of one service ("" = shared)
// with want, keeping row ids of unchanged names.
func (c *Core) writeVars(ctx context.Context, tx *sql.Tx, envID, serviceID string, want map[string]changes.Var) error {
	type cur struct {
		id     string
		enc    []byte
		sealed bool
	}
	existing := map[string]cur{}
	rows, err := tx.QueryContext(ctx, `SELECT id, name, value_enc, sealed FROM variables WHERE environment_id = ? AND ifnull(service_id, '') = ?`, envID, serviceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var x cur
		var name string
		var sealed int
		if err := rows.Scan(&x.id, &name, &x.enc, &sealed); err != nil {
			_ = rows.Close()
			return err
		}
		x.sealed = sealed == 1
		existing[name] = x
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := c.Now()
	for name, x := range existing {
		if _, ok := want[name]; !ok {
			if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE id = ?`, x.id); err != nil {
				return err
			}
		}
	}
	for _, name := range sortedKeys(want) {
		v := want[name]
		isRef := vars.HasReference(v.Value)
		refRaw := ""
		if isRef && !v.Sealed && !vars.IsSecretName(name) {
			refRaw = v.Value
		}
		if x, ok := existing[name]; ok {
			old, err := c.box.Open(x.enc, varAAD(x.id))
			if err == nil && string(old) == v.Value && x.sealed == v.Sealed {
				continue
			}
			enc, err := c.box.Seal([]byte(v.Value), varAAD(x.id))
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE variables SET value_enc = ?, is_reference = ?, reference_raw = ?, sealed = ?, updated_at = ? WHERE id = ?`,
				enc, store.Bool(isRef), refRaw, store.Bool(v.Sealed || x.sealed), now, x.id); err != nil {
				return err
			}
			continue
		}
		id := newID("var")
		enc, err := c.box.Seal([]byte(v.Value), varAAD(id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO variables (id, environment_id, service_id, name, value_enc, is_reference, reference_raw, sealed, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, envID, nullStr(serviceID), name, enc, store.Bool(isRef), refRaw, store.Bool(v.Sealed), now); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- staged changes

// changeRow is a changes row. Variable ops are stored with every value
// sealed inside the JSON as {"$enc": "<base64>"}.
type changeRow struct {
	ID        string
	EnvID     string
	ServiceID string
	Kind      string
	Patch     map[string]any // service.update, service.rename, volume.*
	Vars      changes.VarOps // variables, shared_variables
	Tag       string
	Author    string
	CreatedAt int64
	UpdatedAt int64
}

type sealedOps struct {
	Set        map[string]map[string]string `json:"set,omitempty"`
	Unset      []string                     `json:"unset,omitempty"`
	Seal       []string                     `json:"seal,omitempty"`
	NeedsValue []string                     `json:"needsValue,omitempty"`
}

func changeAAD(id, name string) string { return "changes|" + id + "|" + name }

func isVarKind(k string) bool { return k == changes.KindVariables || k == changes.KindShared }

func (c *Core) encodeChange(ch changeRow) (string, error) {
	if !isVarKind(ch.Kind) {
		if ch.Patch == nil {
			ch.Patch = map[string]any{}
		}
		return jsonString(ch.Patch), nil
	}
	so := sealedOps{Unset: ch.Vars.Unset, Seal: ch.Vars.Seal, NeedsValue: ch.Vars.NeedsValue}
	if len(ch.Vars.Set) > 0 {
		so.Set = map[string]map[string]string{}
		for k, v := range ch.Vars.Set {
			enc, err := c.box.Seal([]byte(v), changeAAD(ch.ID, k))
			if err != nil {
				return "", err
			}
			so.Set[k] = map[string]string{"$enc": base64.StdEncoding.EncodeToString(enc)}
		}
	}
	return jsonString(so), nil
}

func (c *Core) decodeChange(ch *changeRow, raw string) error {
	if !isVarKind(ch.Kind) {
		ch.Patch = map[string]any{}
		return json.Unmarshal([]byte(raw), &ch.Patch)
	}
	var so sealedOps
	if err := json.Unmarshal([]byte(raw), &so); err != nil {
		return err
	}
	ch.Vars = changes.VarOps{Unset: so.Unset, Seal: so.Seal, NeedsValue: so.NeedsValue}
	if len(so.Set) > 0 {
		ch.Vars.Set = map[string]string{}
		for k, v := range so.Set {
			b, err := base64.StdEncoding.DecodeString(v["$enc"])
			if err != nil {
				return err
			}
			pt, err := c.box.Open(b, changeAAD(ch.ID, k))
			if err != nil {
				return fail.Internal(fail.Wrapf(err, "open staged variable %s", k))
			}
			ch.Vars.Set[k] = string(pt)
		}
	}
	return nil
}

// envChanges loads the staged changes of an environment, oldest first.
func (c *Core) envChanges(ctx context.Context, q store.Querier, envID string) ([]changeRow, error) {
	type raw struct {
		changeRow
		patch string
	}
	rows, err := store.All(ctx, q, func(r *sql.Rows) (raw, error) {
		var x raw
		return x, r.Scan(&x.ID, &x.EnvID, &x.ServiceID, &x.Kind, &x.patch, &x.Tag, &x.Author, &x.CreatedAt, &x.UpdatedAt)
	}, `SELECT id, environment_id, service_id, kind, patch, tag, author, created_at, updated_at FROM changes WHERE environment_id = ? ORDER BY created_at, id`, envID)
	if err != nil {
		return nil, err
	}
	out := make([]changeRow, 0, len(rows))
	for _, x := range rows {
		ch := x.changeRow
		if err := c.decodeChange(&ch, x.patch); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, nil
}

// changeSet indexes staged changes by service and kind.
type changeSet map[string]map[string]*changeRow

func indexChanges(rows []changeRow) changeSet {
	cs := changeSet{}
	for i := range rows {
		r := &rows[i]
		if cs[r.ServiceID] == nil {
			cs[r.ServiceID] = map[string]*changeRow{}
		}
		cs[r.ServiceID][r.Kind] = r
	}
	return cs
}

func (cs changeSet) get(sid, kind string) *changeRow {
	if m := cs[sid]; m != nil {
		return m[kind]
	}
	return nil
}

// stage merges an edit into the (environment, service, kind) row.
func (c *Core) stage(ctx context.Context, tx *sql.Tx, envID, serviceID, kind, author string, merge func(cur *changeRow)) error {
	rows, err := c.envChanges(ctx, tx, envID)
	if err != nil {
		return err
	}
	cur := indexChanges(rows).get(serviceID, kind)
	now := c.Now()
	fresh := cur == nil
	if fresh {
		cur = &changeRow{ID: newID("chg"), EnvID: envID, ServiceID: serviceID, Kind: kind, Patch: map[string]any{}, Author: author, CreatedAt: now}
	}
	merge(cur)
	cur.UpdatedAt = now
	if !fresh && cur.Author != author {
		cur.Author = author
	}
	empty := false
	switch {
	case isVarKind(kind):
		empty = cur.Vars.Empty()
	case kind == changes.KindUpdate, kind == changes.KindVolumeUpdate:
		empty = len(cur.Patch) == 0
	}
	if empty {
		if !fresh {
			_, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE id = ?`, cur.ID)
			return err
		}
		return nil
	}
	enc, err := c.encodeChange(*cur)
	if err != nil {
		return err
	}
	if fresh {
		_, err = tx.ExecContext(ctx, `INSERT INTO changes (id, environment_id, service_id, kind, patch, tag, author, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cur.ID, envID, serviceID, kind, enc, cur.Tag, author, cur.CreatedAt, now)
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE changes SET patch = ?, tag = ?, author = ?, updated_at = ? WHERE id = ?`, enc, cur.Tag, cur.Author, now, cur.ID)
	return err
}

// dropChanges removes every staged change of a service in an environment.
func dropChanges(ctx context.Context, tx *sql.Tx, envID, serviceID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE environment_id = ? AND service_id = ?`, envID, serviceID)
	return err
}

// ---------------------------------------------------------------- volumes

type volumeRow struct {
	ID         string
	ServiceID  string
	Name       string
	MountPath  string
	HostID     string
	AlertAtPct int
	CreatedAt  int64
}

func (c *Core) volumes(ctx context.Context, q store.Querier, envID string) (map[string]volumeRow, error) {
	rows, err := store.All(ctx, q, func(r *sql.Rows) (volumeRow, error) {
		var v volumeRow
		return v, r.Scan(&v.ID, &v.ServiceID, &v.Name, &v.MountPath, &v.HostID, &v.AlertAtPct, &v.CreatedAt)
	}, `SELECT id, service_id, name, mount_path, host_id, alert_at_pct, created_at FROM volumes WHERE environment_id = ? AND deleted_at IS NULL`, envID)
	out := map[string]volumeRow{}
	for _, v := range rows {
		out[v.ServiceID] = v
	}
	return out, err
}

// ---------------------------------------------------------------- names

// envNames maps live service names (committed or staged) to service ids.
func envNames(insts []instance, cs changeSet) map[string]string {
	out := map[string]string{}
	for _, in := range insts {
		out[in.Config.Name] = in.ServiceID
		if r := cs.get(in.ServiceID, changes.KindRename); r != nil {
			if n, ok := r.Patch["name"].(string); ok {
				out[n] = in.ServiceID
			}
		}
	}
	return out
}

func newID(prefix string) string { return ids.New(prefix) }

// cleanName trims a service name.
func cleanName(s string) string { return strings.Join(strings.Fields(s), " ") }
