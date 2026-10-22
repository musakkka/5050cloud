// SPDX-License-Identifier: AGPL-3.0-only

package changes

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Plan actions (cpapi.PlanItem.Action).
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionScale  = "scale"
)

// Classify decides what a commit does to one service (9.1): old is nil for
// a create, new is nil for a delete. A change of source or build rebuilds;
// a replicas-only change scales in place; anything else, including
// variables, redeploys the last image. ok is false when nothing changed.
func Classify(old, new *api.ServiceConfig, varsChanged bool) (action string, rebuild, ok bool) {
	switch {
	case old == nil && new == nil:
		return "", false, false
	case old == nil:
		return ActionCreate, true, true
	case new == nil:
		return ActionDelete, false, true
	}
	if !reflect.DeepEqual(jsonOf(old.Source), jsonOf(new.Source)) || !reflect.DeepEqual(jsonOf(old.Build), jsonOf(new.Build)) {
		return ActionUpdate, true, true
	}
	same := reflect.DeepEqual(ToMap(*old), ToMap(*new))
	if same && !varsChanged {
		return "", false, false
	}
	if !varsChanged && OnlyReplicas(*old, *new) {
		return ActionScale, false, true
	}
	return ActionUpdate, false, true
}

// ---------------------------------------------------------------- environment sync

// EnvService is one service instance as sync sees it.
type EnvService struct {
	ID     string
	Config api.ServiceConfig
	Vars   map[string]Var
	Volume *VolumeSpec
}

// VolumeSpec is a volume's settings.
type VolumeSpec struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

// SyncCreate is a service only in the source (tag "new").
type SyncCreate struct {
	Service EnvService
	Vars    VarOps
}

// SyncEdit is a service in both whose config or variables differ (tag "edited").
type SyncEdit struct {
	ServiceID string
	Patch     map[string]any
	Vars      VarOps
}

// SyncResult is what Sync stages.
type SyncResult struct {
	Creates []SyncCreate
	Edits   []SyncEdit
	// Removed are service ids only in the target: candidates, unticked by default.
	Removed []string
	Shared  VarOps
}

// IsSecret reports whether a variable is never copied by sync or duplication.
func IsSecret(name string, v Var) bool { return v.Sealed || vars.IsSecretName(name) }

// CopyVars returns ops that recreate vars without secrets: secret names
// are staged empty and flagged "Set a value".
func CopyVars(in map[string]Var) VarOps {
	ops := VarOps{Set: map[string]string{}}
	for _, k := range sortedVarKeys(in) {
		v := in[k]
		if IsSecret(k, v) {
			ops.Set[k] = ""
			ops.NeedsValue = append(ops.NeedsValue, k)
			continue
		}
		ops.Set[k] = v.Value
	}
	if len(ops.Set) == 0 {
		ops.Set = nil
	}
	return ops
}

// CopyVarsDropSealed returns ops that copy every non-sealed variable with
// its value (service and environment duplication).
func CopyVarsDropSealed(in map[string]Var) VarOps {
	ops := VarOps{Set: map[string]string{}}
	for _, k := range sortedVarKeys(in) {
		if !in[k].Sealed {
			ops.Set[k] = in[k].Value
		}
	}
	if len(ops.Set) == 0 {
		ops.Set = nil
	}
	return ops
}

// syncVars returns ops that make dst's variables match src without copying secrets.
func syncVars(src, dst map[string]Var) VarOps {
	ops := VarOps{Set: map[string]string{}}
	for _, k := range sortedVarKeys(src) {
		sv := src[k]
		dv, ok := dst[k]
		secret := IsSecret(k, sv)
		switch {
		case !ok && secret:
			ops.Set[k] = ""
			ops.NeedsValue = append(ops.NeedsValue, k)
		case !ok:
			ops.Set[k] = sv.Value
		case !secret && !dv.Sealed && dv.Value != sv.Value:
			ops.Set[k] = sv.Value
		}
	}
	for _, k := range sortedVarKeys(dst) {
		if _, ok := src[k]; !ok {
			ops.Unset = append(ops.Unset, k)
		}
	}
	if len(ops.Set) == 0 {
		ops.Set = nil
	}
	return ops
}

// Sync compares a source environment with the target and returns what to
// stage (9.5). Services are matched by id.
func Sync(src, dst map[string]EnvService, srcShared, dstShared map[string]Var) SyncResult {
	var r SyncResult
	for _, id := range sortedSvcKeys(src) {
		s := src[id]
		d, ok := dst[id]
		if !ok {
			r.Creates = append(r.Creates, SyncCreate{Service: s, Vars: CopyVars(s.Vars)})
			continue
		}
		want := Clone(s.Config)
		want.Name = d.Config.Name
		patch := PatchBetween(d.Config, want)
		vo := syncVars(s.Vars, d.Vars)
		if len(patch) == 0 && vo.Empty() {
			continue
		}
		r.Edits = append(r.Edits, SyncEdit{ServiceID: id, Patch: patch, Vars: vo})
	}
	for _, id := range sortedSvcKeys(dst) {
		if _, ok := src[id]; !ok {
			r.Removed = append(r.Removed, id)
		}
	}
	r.Shared = syncVars(srcShared, dstShared)
	return r
}

func sortedVarKeys(m map[string]Var) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSvcKeys(m map[string]EnvService) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// UniqueName returns name, or name-2, name-3... so it is not in taken.
func UniqueName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		suffix := "-" + itoa(i)
		base := name
		if len(base)+len(suffix) > MaxServiceNameLen {
			base = base[:MaxServiceNameLen-len(suffix)]
		}
		if n := base + suffix; !taken[n] {
			return n
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
