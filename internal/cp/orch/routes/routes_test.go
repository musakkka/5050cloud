// SPDX-License-Identifier: AGPL-3.0-only

package routes

import (
	"testing"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

func TestTLSModeAndSlug(t *testing.T) {
	if TLSMode("app.example.com") != "acme" || TLSMode("db.internal") != "internal" || TLSMode("localhost") != "internal" {
		t.Fatal("TLS mode mismatch")
	}
	if got := Slug("My API_Server"); got != "my-api-server" {
		t.Fatalf("slug = %q", got)
	}
	if out := Compute(nil); len(out.Routes) != 0 {
		t.Fatalf("no services must give no routes, got %d", len(out.Routes))
	}
}

// A custom domain whose DNS does not point here yet is held back from ACME.
func TestCustomDomainWaitsForDNS(t *testing.T) {
	svc := Service{Port: 8080, Snap: cpapi.ServiceSnapshot{EnvID: "env_1", ServiceID: "svc_1", Domains: []api.Domain{
		{Domain: "waiting.example.com", Kind: "custom", Port: 8080},
		{Domain: "ready.example.com", Kind: "custom", Port: 8080, DNSOK: true},
		{Domain: "web-ab12.203-0-113-7.sslip.io", Kind: "generated", Port: 8080, DNSOK: true},
		{Domain: "only.internal", Kind: "custom", Port: 8080},
	}}}
	got := map[string]string{}
	for _, r := range Compute([]Service{svc}).Routes {
		got[r.Domain] = r.TLS
	}
	want := map[string]string{
		"waiting.example.com": "pending", "ready.example.com": "acme",
		"web-ab12.203-0-113-7.sslip.io": "acme", "only.internal": "internal",
	}
	for d, m := range want {
		if got[d] != m {
			t.Errorf("%s: TLS %q, want %q", d, got[d], m)
		}
	}
}
