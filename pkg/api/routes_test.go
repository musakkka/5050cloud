// SPDX-License-Identifier: MIT

package api

import (
	"bufio"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

var wildcard = regexp.MustCompile(`\{[a-z]+\}`)

// norm makes "/environments/{eid}/x" and "/environments/{id}/x" comparable.
func norm(p string) string { return wildcard.ReplaceAllString(p, "{}") }

// TestRoutesRegister mounts every route on a ServeMux, which panics on
// conflicting or malformed patterns.
func TestRoutesRegister(t *testing.T) {
	mux := http.NewServeMux()
	seen := map[string]bool{}
	for _, r := range Routes {
		key := r.Method + " " + r.Path
		if seen[key] {
			t.Errorf("duplicate route %s", key)
		}
		seen[key] = true
		prefix := "/api/v1"
		if r.NoPrefix {
			prefix = ""
		}
		func() {
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("route %s does not register: %v", key, v)
				}
			}()
			mux.HandleFunc(r.Method+" "+prefix+r.Path, func(http.ResponseWriter, *http.Request) {})
		}()
	}
}

func TestRoutesWellFormed(t *testing.T) {
	roles := map[string]bool{Public: true, Viewer: true, Member: true, Admin: true, Owner: true}
	audits := map[string]string{}
	for _, r := range append(append([]Route{}, Routes...), AgentRoutes...) {
		key := r.Method + " " + r.Path
		if !roles[r.Role] {
			t.Errorf("%s: unknown role %q", key, r.Role)
		}
		if !strings.HasPrefix(r.Path, "/") {
			t.Errorf("%s: path must start with /", key)
		}
		switch r.Stream {
		case "", "sse", "ws", "redirect":
		default:
			t.Errorf("%s: unknown stream kind %q", key, r.Stream)
		}
		if r.Method != http.MethodGet && r.Audit == "" && !r.NoAudit {
			t.Errorf("%s: a mutating route needs an audit action name", key)
		}
		if r.Audit != "" {
			if !regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`).MatchString(r.Audit) {
				t.Errorf("%s: audit name %q is not <resource>.<verb>", key, r.Audit)
			}
			if other, ok := audits[r.Audit]; ok {
				t.Errorf("%s: audit name %q is also used by %s", key, r.Audit, other)
			}
			audits[r.Audit] = key
		}
		if r.Method == http.MethodGet && r.Req != nil {
			t.Errorf("%s: GET routes take no body", key)
		}
	}
}

// TestRoutesMatchSpec reads the tables of docs/SPEC.md section 11 and checks
// that every path they mention is in Routes, and that every route's path is
// mentioned there. It keeps the Go table and the spec from drifting apart.
func TestRoutesMatchSpec(t *testing.T) {
	f, err := os.Open("../../docs/SPEC.md")
	if err != nil {
		t.Skipf("docs/SPEC.md not found: %v", err)
	}
	defer f.Close()

	const svc = "/environments/{eid}/services/{sid}"
	tick := regexp.MustCompile("`([^`]+)`")
	var specPaths []string
	in11, agent := false, false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			in11 = strings.HasPrefix(line, "## 11.")
			continue
		}
		if strings.HasPrefix(line, "### ") {
			// 11.6 lists the agent endpoints, which live in AgentRoutes.
			agent = strings.HasPrefix(line, "### 11.6")
			continue
		}
		if agent {
			continue
		}
		if !in11 || !strings.HasPrefix(line, "| ") {
			continue
		}
		cols := strings.Split(strings.ReplaceAll(line, `\|`, "/"), "|")
		if len(cols) < 4 {
			continue
		}
		pathCol := cols[2]
		if strings.TrimSpace(cols[1]) == "Method" {
			continue
		}
		for _, m := range tick.FindAllStringSubmatch(pathCol, -1) {
			p := m[1]
			p, _, _ = strings.Cut(p, "?")
			p = strings.Replace(p, ".../services/{sid}", svc, 1)
			p = strings.Replace(p, "...", svc, 1)
			p = strings.TrimSuffix(p, " (no prefix)")
			if !strings.HasPrefix(p, "/") {
				continue
			}
			// "/tcp-proxies[/{id}]" names both "/tcp-proxies" and "/tcp-proxies/{id}".
			if i := strings.Index(p, "["); i >= 0 {
				specPaths = append(specPaths, p[:i], p[:i]+strings.Trim(p[i:], "[]"))
				continue
			}
			specPaths = append(specPaths, p)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(specPaths) < 100 {
		t.Fatalf("found only %d paths in section 11; the parser no longer matches the spec layout", len(specPaths))
	}

	have := map[string]bool{}
	for _, r := range Routes {
		have[norm(r.Path)] = true
	}
	mentioned := map[string]bool{}
	for _, p := range specPaths {
		n := norm(p)
		mentioned[n] = true
		if have[n] {
			continue
		}
		// Relative suffixes such as `/rollback` after `/deployments/{did}/redeploy`.
		ok := false
		for h := range have {
			if strings.HasSuffix(h, n) {
				ok = true
				mentioned[h] = true
			}
		}
		if !ok {
			t.Errorf("SPEC section 11 mentions %s but Routes has no such path", p)
		}
	}
	for _, r := range Routes {
		n := norm(r.Path)
		if !mentioned[n] {
			t.Errorf("route %s %s is not in SPEC section 11", r.Method, r.Path)
		}
	}
}
