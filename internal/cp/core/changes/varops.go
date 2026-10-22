// SPDX-License-Identifier: AGPL-3.0-only

package changes

import (
	"sort"

	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
)

// VarOps is a staged edit of one service's (or the shared) variables.
type VarOps struct {
	Set   map[string]string `json:"set,omitempty"`
	Unset []string          `json:"unset,omitempty"`
	Seal  []string          `json:"seal,omitempty"`
	// NeedsValue lists names staged by sync or import without a value.
	NeedsValue []string `json:"needsValue,omitempty"`
}

// Var is a variable as committed.
type Var struct {
	Value  string
	Sealed bool
}

// Empty reports whether ops change nothing.
func (o VarOps) Empty() bool {
	return len(o.Set) == 0 && len(o.Unset) == 0 && len(o.Seal) == 0
}

// Merge composes o then n.
func (o VarOps) Merge(n VarOps) VarOps {
	out := VarOps{Set: map[string]string{}}
	unset := toSet(o.Unset)
	seal := toSet(o.Seal)
	needs := toSet(o.NeedsValue)
	for k, v := range o.Set {
		out.Set[k] = v
	}
	for k, v := range n.Set {
		out.Set[k] = v
		delete(unset, k)
		delete(needs, k)
	}
	for _, k := range n.Unset {
		delete(out.Set, k)
		delete(seal, k)
		delete(needs, k)
		unset[k] = true
	}
	for _, k := range n.Seal {
		seal[k] = true
	}
	for _, k := range n.NeedsValue {
		needs[k] = true
	}
	out.Unset, out.Seal, out.NeedsValue = keys(unset), keys(seal), keys(needs)
	if len(out.Set) == 0 {
		out.Set = nil
	}
	return out
}

// Normalize drops operations that change nothing against current.
func (o VarOps) Normalize(current map[string]Var) VarOps {
	out := VarOps{Set: map[string]string{}}
	for k, v := range o.Set {
		if cur, ok := current[k]; ok && cur.Value == v && !contains(o.NeedsValue, k) {
			continue
		}
		out.Set[k] = v
	}
	for _, k := range o.Unset {
		if _, ok := current[k]; ok {
			out.Unset = append(out.Unset, k)
		}
	}
	for _, k := range o.Seal {
		cur, exists := current[k]
		_, staged := out.Set[k]
		if (exists && !cur.Sealed) || staged {
			out.Seal = append(out.Seal, k)
		}
	}
	for _, k := range o.NeedsValue {
		if _, ok := out.Set[k]; ok {
			out.NeedsValue = append(out.NeedsValue, k)
		}
	}
	if len(out.Set) == 0 {
		out.Set = nil
	}
	sort.Strings(out.Unset)
	sort.Strings(out.Seal)
	sort.Strings(out.NeedsValue)
	return out
}

// Apply returns current with ops applied.
func (o VarOps) Apply(current map[string]Var) map[string]Var {
	out := make(map[string]Var, len(current)+len(o.Set))
	for k, v := range current {
		out[k] = v
	}
	for k, v := range o.Set {
		cur := out[k]
		cur.Value = v
		out[k] = cur
	}
	for _, k := range o.Unset {
		delete(out, k)
	}
	for _, k := range o.Seal {
		if v, ok := out[k]; ok {
			v.Sealed = true
			out[k] = v
		}
	}
	return out
}

// Shown returns how a value appears in a diff: references are shown when
// the name does not look secret and the value is not sealed; everything
// else is masked.
func Shown(name, value string, sealed bool) any {
	if value == "" {
		return ""
	}
	if !sealed && !vars.IsSecretName(name) && vars.HasReference(value) {
		return value
	}
	return MaskedValue
}

// DiffVars lists variable changes as "variables.NAME" (or prefix.NAME) entries.
func DiffVars(prefix string, current map[string]Var, o VarOps) []api.DiffEntry {
	next := o.Apply(current)
	names := map[string]bool{}
	for k := range current {
		names[k] = true
	}
	for k := range next {
		names[k] = true
	}
	sorted := keys(names)
	var out []api.DiffEntry
	for _, k := range sorted {
		cur, had := current[k]
		nv, has := next[k]
		switch {
		case had && !has:
			out = append(out, api.DiffEntry{Path: prefix + "." + k, Old: Shown(k, cur.Value, cur.Sealed), New: nil})
		case !had && has:
			out = append(out, api.DiffEntry{Path: prefix + "." + k, Old: nil, New: Shown(k, nv.Value, nv.Sealed)})
		case had && has && (cur.Value != nv.Value || cur.Sealed != nv.Sealed):
			e := api.DiffEntry{Path: prefix + "." + k, Old: Shown(k, cur.Value, cur.Sealed), New: Shown(k, nv.Value, nv.Sealed)}
			if cur.Sealed != nv.Sealed {
				e.Path += " (sealed)"
			}
			out = append(out, e)
		}
	}
	return out
}

// StagedState returns "add", "update", "delete" or "" for one name.
func (o VarOps) StagedState(name string, current map[string]Var) string {
	_, had := current[name]
	if contains(o.Unset, name) {
		if had {
			return "delete"
		}
		return ""
	}
	if v, ok := o.Set[name]; ok {
		if !had {
			return "add"
		}
		if current[name].Value != v {
			return "update"
		}
	}
	if contains(o.Seal, name) && had && !current[name].Sealed {
		return "update"
	}
	return ""
}

func toSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
