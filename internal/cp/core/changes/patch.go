// SPDX-License-Identifier: AGPL-3.0-only

// Package changes is the pure logic of staged changes (docs/SPEC.md 9.5):
// JSON merge patches of api.ServiceConfig (RFC 7396) and how later edits
// merge into one change, variable operations, the leaf diff shown in the
// Details modal with secrets masked, validation on commit, the commit plan
// classification (rebuild, redeploy, scale), and environment sync tags.
// Core persists changes; this package has no database.
package changes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Change kinds.
const (
	KindCreate        = "service.create"
	KindUpdate        = "service.update"
	KindDelete        = "service.delete"
	KindRename        = "service.rename"
	KindVariables     = "variables"
	KindShared        = "shared_variables"
	KindVolumeCreate  = "volume.create"
	KindVolumeUpdate  = "volume.update"
	KindVolumeDelete  = "volume.delete"
	MaskedValue       = "•••"
	MaxServiceNameLen = 32
)

// Kinds lists every change kind.
var Kinds = []string{KindCreate, KindUpdate, KindDelete, KindRename, KindVariables, KindShared, KindVolumeCreate, KindVolumeUpdate, KindVolumeDelete}

// ToMap converts a config to its JSON object form.
func ToMap(c api.ServiceConfig) map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// FromMap converts a JSON object to a config, strictly.
func FromMap(m map[string]any) (api.ServiceConfig, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return api.ServiceConfig{}, err
	}
	var c api.ServiceConfig
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			return c, fail.Validation(te.Field, "%s must be %s.", te.Field, kindName(te.Type))
		}
		return c, fail.Validation("patch", "The change cannot be applied: %s.", strings.TrimPrefix(err.Error(), "json: "))
	}
	return c, nil
}

func kindName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Int, reflect.Int64:
		return "a whole number"
	case reflect.Float64:
		return "a number"
	case reflect.Bool:
		return "true or false"
	case reflect.Slice, reflect.Array:
		return "a list"
	}
	return "an object"
}

// CheckPatch rejects keys that are not ServiceConfig fields.
func CheckPatch(patch map[string]any) error {
	return checkKeys(patch, reflect.TypeOf(api.ServiceConfig{}), "")
}

func checkKeys(m map[string]any, t reflect.Type, prefix string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		fields[name] = f.Type
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		ft, ok := fields[k]
		if !ok {
			return fail.Validation(p, "%s is not a service setting.", p)
		}
		if sub, ok := m[k].(map[string]any); ok {
			if err := checkKeys(sub, ft, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// MergePatch composes two merge patches: applying the result equals
// applying a then b.
func MergePatch(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		bm, bIsMap := v.(map[string]any)
		am, aIsMap := out[k].(map[string]any)
		if bIsMap && aIsMap {
			out[k] = MergePatch(am, bm)
		} else {
			out[k] = v
		}
	}
	return out
}

// applyMerge applies an RFC 7396 merge patch to target.
func applyMerge(target, patch map[string]any) map[string]any {
	out := make(map[string]any, len(target))
	for k, v := range target {
		out[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(out, k)
			continue
		}
		pm, ok := v.(map[string]any)
		if !ok {
			out[k] = v
			continue
		}
		tm, _ := out[k].(map[string]any)
		if tm == nil {
			tm = map[string]any{}
		}
		out[k] = applyMerge(tm, pm)
	}
	return out
}

// Apply applies a merge patch to a config.
func Apply(c api.ServiceConfig, patch map[string]any) (api.ServiceConfig, error) {
	if len(patch) == 0 {
		return c, nil
	}
	if err := CheckPatch(patch); err != nil {
		return c, err
	}
	return FromMap(applyMerge(ToMap(c), patch))
}

// PatchBetween returns a merge patch that turns a into b (used by sync and
// to drop no-op edits).
func PatchBetween(a, b api.ServiceConfig) map[string]any {
	return diffPatch(ToMap(a), ToMap(b))
}

func diffPatch(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			out[k] = nil
			continue
		}
		am, aMap := av.(map[string]any)
		bm, bMap := bv.(map[string]any)
		if aMap && bMap {
			if sub := diffPatch(am, bm); len(sub) > 0 {
				out[k] = sub
			}
			continue
		}
		if !reflect.DeepEqual(av, bv) {
			out[k] = bv
		}
	}
	for k, bv := range b {
		if _, ok := a[k]; !ok {
			out[k] = bv
		}
	}
	return out
}

// ---------------------------------------------------------------- diff

// Diff lists the leaf paths that differ between old and new configs.
func Diff(old, new api.ServiceConfig) []api.DiffEntry {
	return DiffMaps("", flatten(ToMap(old)), flatten(ToMap(new)))
}

// DiffMaps compares two flat maps and prefixes each path.
func DiffMaps(prefix string, a, b map[string]any) []api.DiffEntry {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out []api.DiffEntry
	for _, k := range sorted {
		av, bv := a[k], b[k]
		if reflect.DeepEqual(av, bv) {
			continue
		}
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		out = append(out, api.DiffEntry{Path: p, Old: av, New: bv})
	}
	return out
}

func flatten(m map[string]any) map[string]any {
	out := map[string]any{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			for k, sv := range sub {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, sv)
			}
			return
		}
		if sub, ok := v.(map[string]any); ok && len(sub) == 0 {
			return // empty objects are "not set"
		}
		out[prefix] = v
	}
	walk("", m)
	return out
}

// Paths returns the leaf paths a patch sets ("deploy.startCommand").
func Paths(patch map[string]any) []string {
	var out []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if sub, ok := v.(map[string]any); ok {
			for k, sv := range sub {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, sv)
			}
			return
		}
		out = append(out, prefix)
	}
	walk("", patch)
	sort.Strings(out)
	return out
}

// OnlyReplicas reports whether the only difference between a and b is deploy.replicas.
func OnlyReplicas(a, b api.ServiceConfig) bool {
	d := Diff(a, b)
	return len(d) == 1 && d[0].Path == "deploy.replicas"
}

// Clone deep-copies a config.
func Clone(c api.ServiceConfig) api.ServiceConfig {
	out, err := FromMap(ToMap(c))
	if err != nil {
		panic(fmt.Sprintf("changes: config does not round-trip: %v", err))
	}
	return out
}
