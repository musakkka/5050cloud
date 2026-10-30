// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"encoding/json"
	"net/mail"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Actor is who performs an operation.
type Actor struct {
	UserID string
	Email  string
	Name   string
	Role   api.Role
	// TokenName is set when an API token authenticated the request.
	TokenName string
	// TokenScope is "account" or "environment"; TokenEnv the environment.
	TokenScope string
	TokenEnv   string
	IP         string
	// SessionHash identifies the browser session (presence, event echo).
	SessionHash string
	// System marks internal callers (GitHub pushes, the orchestrator, imports).
	System string
}

// Label is the audit and activity form: an email, "token:<name>" or the system name.
func (a Actor) Label() string {
	switch {
	case a.System != "":
		return a.System
	case a.TokenName != "":
		return "token:" + a.TokenName
	case a.Email != "":
		return a.Email
	}
	return "anonymous"
}

// IsAdmin reports Owner or Admin.
func (a Actor) IsAdmin() bool {
	return a.Role == api.RoleOwner || a.Role == api.RoleAdmin || a.System != ""
}

// SystemActor returns an internal actor.
func SystemActor(name string) Actor { return Actor{System: name, Role: api.RoleOwner} }

func trim(s string) string  { return strings.TrimSpace(s) }
func lower(s string) string { return strings.ToLower(s) }

func validEmail(s string) bool {
	if len(s) > 254 || !strings.Contains(s, "@") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// NormEmail is the form an account is looked up by; anything counted per account
// has to use it too.
func NormEmail(s string) string { return normEmail(s) }

func cleanCIDRs(field string, in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		s = trim(s)
		if s == "" {
			continue
		}
		var p netip.Prefix
		var err error
		if strings.Contains(s, "/") {
			p, err = netip.ParsePrefix(s)
		} else {
			var a netip.Addr
			a, err = netip.ParseAddr(s)
			if err == nil {
				p = netip.PrefixFrom(a, a.BitLen())
			}
		}
		if err != nil {
			return nil, fail.Validation(field, "%q is not an IP address or a CIDR range.", s)
		}
		k := p.Masked().String()
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out, nil
}

// newerVersion reports whether a (vX.Y.Z) is newer than b. Development
// builds never see an update.
func newerVersion(a, b string) bool {
	pa, oka := semver(a)
	pb, okb := semver(b)
	if !oka || !okb {
		return false
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func semver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
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

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
