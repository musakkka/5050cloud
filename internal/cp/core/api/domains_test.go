// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/musakkka/5050cloud/internal/cp/core/fakeorch"
	"github.com/musakkka/5050cloud/pkg/api"
)

// fakeDNS is a resolver whose answers a test sets per name.
type fakeDNS struct {
	mu     sync.Mutex
	a4, a6 map[string][]string
}

func (f *fakeDNS) set(name string, a4, a6 []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.a4 == nil {
		f.a4, f.a6 = map[string][]string{}, map[string][]string{}
	}
	f.a4[name], f.a6[name] = a4, a6
}

func (f *fakeDNS) lookup(_ context.Context, name string) ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.a4[name], f.a6[name]
}

type domainEnv struct {
	*env
	owner *client
	eid   string
	sid   string
	dns   *fakeDNS
}

func newDomainEnv(t *testing.T) *domainEnv {
	t.Helper()
	e := newEnv(t)
	owner := e.client()
	tok, err := e.c.CreateSetupToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var me api.Me
	owner.do("POST", "/api/v1/setup", api.SetupRequest{Token: tok, Email: "owner@example.com", Name: "Owner", Password: "correct horse battery"}, &me, 200)
	var prj api.Project
	owner.do("POST", "/api/v1/projects", api.CreateProjectRequest{Name: "shop"}, &prj, 200)
	eid := prj.Environments[0].ID
	var cs api.CreateServiceResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/services", api.CreateServiceRequest{Name: "web", DeployNow: true,
		Source: api.SourceConfig{Type: "image", Image: "traefik/whoami"}}, &cs, 200)
	d := &fakeDNS{}
	e.c.SetDomainResolver(d.lookup)
	return &domainEnv{env: e, owner: owner, eid: eid, sid: cs.Services[0].ID, dns: d}
}

func (d *domainEnv) add(name string, want int) api.Domain {
	d.t.Helper()
	var dom api.Domain
	d.owner.do("POST", "/api/v1/environments/"+d.eid+"/services/"+d.sid+"/domains", api.CreateDomainRequest{Kind: "custom", Domain: name}, &dom, want)
	return dom
}

func (d *domainEnv) check(id string) api.Domain {
	d.t.Helper()
	var dom api.Domain
	d.owner.do("POST", "/api/v1/domains/"+id+"/check", nil, &dom, 200)
	return dom
}

func TestCustomDomainLifecycle(t *testing.T) {
	d := newDomainEnv(t)
	d.orch.IngressHosts = []api.Host{
		{ID: "h1", Roles: []string{"control", "ingress"}, PublicIP: "203.0.113.7"},
		{ID: "h2", Roles: []string{"ingress"}, PublicIP: "203.0.113.8"},
		{ID: "h3", Roles: []string{"app"}, PublicIP: "203.0.113.9"},
	}
	// The name resolves nowhere yet.
	dom := d.add("shop.example.com", 200)
	if dom.Kind != "custom" || dom.Status != "pending_dns" || dom.DNSOK {
		t.Fatalf("new custom domain: %+v", dom)
	}
	// One A record per ingress host (not the app host) and a CNAME to the generated name.
	var gotA []string
	var cname *api.DNSRecord
	for i, r := range dom.DNS {
		switch r.Type {
		case "A":
			gotA = append(gotA, r.Value)
			if r.Host != "shop" {
				t.Errorf("A record host %q, want shop", r.Host)
			}
		case "CNAME":
			cname = &dom.DNS[i]
		}
	}
	if strings.Join(gotA, ",") != "203.0.113.7,203.0.113.8" {
		t.Fatalf("A records %v", gotA)
	}
	_ = cname // no generated domain on this service yet, so no CNAME offered

	// Points at the second ingress host: accepted. The certificate is not yet issued.
	d.dns.set("shop.example.com", []string{"203.0.113.8"}, nil)
	dom = d.check(dom.ID)
	if !dom.DNSOK || dom.Status != "issuing" || dom.Message != "" {
		t.Fatalf("after DNS: %+v", dom)
	}
	// The certificate fails: the view carries the sentence.
	d.orch.Certs = map[string]fakeorch.CertResult{"shop.example.com": {Status: "error", Message: "The certificate for shop.example.com could not be issued yet: boom"}}
	var list []api.Domain
	d.owner.do("GET", "/api/v1/environments/"+d.eid+"/services/"+d.sid+"/domains", nil, &list, 200)
	if list[0].Status != "error" || !strings.Contains(list[0].Message, "boom") {
		t.Fatalf("errored domain: %+v", list[0])
	}
	// Retry reaches the orchestrator.
	d.owner.do("POST", "/api/v1/domains/"+dom.ID+"/retry-certificate", nil, &dom, 200)
	if len(d.orch.Retried) != 1 || d.orch.Retried[0] != "shop.example.com" {
		t.Fatalf("retried %v", d.orch.Retried)
	}
	// Active, with an expiry.
	d.orch.Certs = map[string]fakeorch.CertResult{"shop.example.com": {Status: "active", NotAfter: 4102444800000}}
	dom = d.check(dom.ID)
	if dom.Status != "active" || !dom.CertOK || dom.CertExpiresAt != 4102444800000 {
		t.Fatalf("active domain: %+v", dom)
	}
	// Removing it drops the stored certificate.
	d.owner.do("DELETE", "/api/v1/domains/"+dom.ID, nil, nil, 204)
	if len(d.orch.Forgotten) != 1 || d.orch.Forgotten[0] != "shop.example.com" {
		t.Fatalf("forgotten %v", d.orch.Forgotten)
	}
}

func TestDomainDNSChecks(t *testing.T) {
	d := newDomainEnv(t)
	d.orch.IngressHosts = []api.Host{{ID: "h1", Roles: []string{"ingress"}, PublicIP: "203.0.113.7"}}

	other := d.add("other.example.com", 200)
	d.dns.set("other.example.com", []string{"192.0.2.1"}, nil)
	other = d.check(other.ID)
	if other.DNSOK || other.Status != "pending_dns" || !strings.Contains(other.Message, "192.0.2.1") || !strings.Contains(other.Message, "203.0.113.7") {
		t.Fatalf("wrong address: %+v", other)
	}

	cf := d.add("proxied.example.com", 200)
	d.dns.set("proxied.example.com", []string{"104.16.0.1"}, nil)
	cf = d.check(cf.ID)
	if !cf.Cloudflare || !cf.DNSOK || !strings.Contains(cf.Message, "Full (strict)") || !strings.Contains(cf.Message, "Always Use HTTPS") {
		t.Fatalf("cloudflare: %+v", cf)
	}

	v6 := d.add("six.example.com", 200)
	d.dns.set("six.example.com", []string{"203.0.113.7"}, []string{"2001:db8::1"})
	v6 = d.check(v6.ID)
	if !v6.DNSOK || !strings.Contains(v6.Message, "Remove the AAAA record for six.example.com") {
		t.Fatalf("foreign AAAA: %+v", v6)
	}
	// A retry before DNS is right is refused: it would only burn a failed validation.
	d.owner.do("POST", "/api/v1/domains/"+other.ID+"/retry-certificate", nil, nil, 409)
}

func TestApexRecordsCarryANote(t *testing.T) {
	d := newDomainEnv(t)
	var gen api.Domain
	d.owner.do("POST", "/api/v1/environments/"+d.eid+"/services/"+d.sid+"/domains", api.CreateDomainRequest{Kind: "generated"}, &gen, 200)
	apex := d.add("example.org", 200)
	var sawA, sawCNAME bool
	for _, r := range apex.DNS {
		if r.Host != "@" {
			t.Errorf("apex host %q", r.Host)
		}
		switch r.Type {
		case "A":
			sawA = r.Note == ""
		case "CNAME":
			sawCNAME = strings.Contains(r.Note, "flattening")
		}
	}
	if !sawA || !sawCNAME {
		t.Fatalf("apex records %+v", apex.DNS)
	}
	// A subdomain under a multi-part suffix gets the relative label.
	sub := d.add("www.shop.co.uk", 200)
	if sub.DNS[0].Host != "www" {
		t.Fatalf("host %q", sub.DNS[0].Host)
	}
	for _, r := range sub.DNS {
		if r.Type == "CNAME" && r.Note != "" {
			t.Errorf("subdomain CNAME has a note: %q", r.Note)
		}
	}
}

func TestDomainNamesAreRefused(t *testing.T) {
	d := newDomainEnv(t)
	d.owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ConsoleDomain: ptr("console.example.org")}, nil, 200)
	for _, name := range []string{"console.example.org", "*.example.com", "1.2.3.4"} {
		d.add(name, 422)
	}
	// After a change the old console name stays reserved while it still answers.
	d.owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ConsoleDomain: ptr("admin.example.org")}, nil, 200)
	d.add("console.example.org", 422)
	d.add("admin.example.org", 422)
	var in api.Install
	d.owner.do("GET", "/api/v1/install", nil, &in, 200)
	if in.OldConsoleDomain != "console.example.org" || in.OldConsoleUntil == 0 {
		t.Fatalf("install %+v", in)
	}
	// The reverse: a name a service uses cannot become the console's.
	d.add("taken.example.org", 200)
	d.owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ConsoleDomain: ptr("taken.example.org")}, nil, 422)
}
