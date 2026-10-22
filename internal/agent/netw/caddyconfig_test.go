// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

var testParams = caddyParams{
	Dir:          "/var/lib/5050/caddy",
	AdminSocket:  "/var/lib/5050/caddy/admin.sock",
	ResolverAddr: "127.0.0.1:5354",
	HTTPPort:     80,
	HTTPSPort:    443,
}

func testIngress() proto.Ingress {
	return proto.Ingress{
		ACMEEmail:   "ops@example.com",
		ACMEPrimary: true,
		Routes: []proto.Route{
			{Domain: "web-ab12.203-0-113-5.sslip.io", UpstreamName: "svc-web", Port: 8080, TLS: "acme", ServiceID: "svc_web"},
			{Domain: "API.Example.com", UpstreamName: "svc-api", Port: 3000, ServiceID: "svc_api", Panic: true},
			{Domain: "plain.example.com", UpstreamName: "svc-plain", Port: 80, TLS: "off", ServiceID: "svc_plain"},
			{Domain: "dev.internal.test", UpstreamName: "svc-dev", Port: 5173, TLS: "internal", ServiceID: "svc_dev"},
		},
		Upstreams:      []proto.UpstreamSet{{Name: "svc-web", IPs: []string{"10.129.4.7"}}},
		Console:        &proto.Route{Domain: "console.example.com", Port: 5050, TLS: "acme"},
		TrustedProxies: []string{"173.245.48.0/20"},
	}
}

func TestCaddyGoldenPrimary(t *testing.T) {
	b, errs := renderCaddy(testParams, testIngress())
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	golden(t, "caddy-primary.golden.json", b)
}

func TestCaddyGoldenSecondary(t *testing.T) {
	in := testIngress()
	in.ACMEPrimary = false
	in.ACMEPrimaryAddr = "100.96.0.1:80"
	in.ACMECA = "staging"
	in.StreamCloseDelaySec = 120
	in.Certificates = []proto.CertBundle{
		{Domain: "web-ab12.203-0-113-5.sslip.io", CertPEM: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n", KeyPEM: "-----BEGIN EC PRIVATE KEY-----\nMHc\n-----END EC PRIVATE KEY-----\n", NotAfter: 1767225600000},
	}
	in.TrustedProxies = nil
	b, errs := renderCaddy(testParams, in)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	golden(t, "caddy-secondary.golden.json", b)
}

func TestCaddyConfigContract(t *testing.T) {
	b, _ := renderCaddy(testParams, testIngress())
	s := string(b)
	for _, want := range []string{
		`"listen": "unix//var/lib/5050/caddy/admin.sock|0600"`,
		`"source": "a"`,
		`"name": "svc-web.upstream.5050"`,
		`"udp/127.0.0.1:5354"`,
		`"refresh": "1s"`,
		`"stream_close_delay": "300s"`,
		`"policy": "random"`,
		`"try_duration": "5s"`,
		`"X-Real-Ip"`,
		`No service is attached to this domain`,
		`"dial": "127.0.0.1:5050"`, // console without an upstream name
		`"status_code": 308`,
		`"ca": "https://acme.zerossl.com/v2/DV90"`,
		`"module": "internal"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %s", want)
		}
	}
	if strings.Contains(s, "10.129.4.7") {
		t.Error("upstream addresses leaked into the Caddy config; they must come from the resolver")
	}
	if strings.Contains(s, "acme-challenge") {
		t.Error("the primary must not forward ACME challenges")
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	// The panic route keeps every replica: no passive health checks on it.
	routes := v["apps"].(map[string]any)["http"].(map[string]any)["servers"].(map[string]any)["https"].(map[string]any)["routes"].([]any)
	for _, r := range routes {
		rm := r.(map[string]any)
		m, ok := rm["match"].([]any)
		if !ok {
			continue
		}
		host := m[0].(map[string]any)["host"].([]any)[0].(string)
		h := rm["handle"].([]any)[0].(map[string]any)
		_, hc := h["health_checks"]
		if host == "api.example.com" && hc {
			t.Error("panic route has health checks")
		}
		if host == "web-ab12.203-0-113-5.sslip.io" && !hc {
			t.Error("normal route lacks passive health checks")
		}
	}
}

func TestCaddyRouteChangesOnlyWhenRoutesChange(t *testing.T) {
	a, _ := renderCaddy(testParams, testIngress())
	in := testIngress()
	in.Upstreams = []proto.UpstreamSet{{Name: "svc-web", IPs: []string{"10.129.4.8", "10.130.4.2"}}}
	b, _ := renderCaddy(testParams, in)
	if string(a) != string(b) {
		t.Error("an upstream change changed the Caddy config, which would reload Caddy")
	}
	in.Routes = in.Routes[1:]
	c, _ := renderCaddy(testParams, in)
	if string(a) == string(c) {
		t.Error("a route change did not change the config")
	}
}

func TestCaddyValidation(t *testing.T) {
	in := proto.Ingress{
		ACMEPrimary:     false,
		ACMEPrimaryAddr: "not-an-address",
		ACMECA:          "ftp://nope",
		Routes: []proto.Route{
			{Domain: "*.example.com", UpstreamName: "a", Port: 80},
			{Domain: "ok.example.com", UpstreamName: "a", Port: 80, TLS: "weird"},
			{Domain: "ok.example.com", UpstreamName: "b", Port: 80},
			{Domain: "noport.example.com", UpstreamName: "a"},
			{Domain: "-bad.example.com", UpstreamName: "a", Port: 80},
		},
		Certificates:   []proto.CertBundle{{Domain: "x.example.com"}},
		TrustedProxies: []string{"nonsense"},
	}
	_, errs := renderCaddy(testParams, in)
	if len(errs) != 8 {
		for _, e := range errs {
			t.Log(e)
		}
		t.Fatalf("errors = %d, want 8", len(errs))
	}
	in.ACMEPrimary = true
	if _, errs := renderCaddy(testParams, in); len(errs) != 7 {
		t.Errorf("primary with a bad CA: errors = %d, want 7 (the CA falls back to production)", len(errs))
	}
	for _, d := range []string{"a.b", "x-1.example.com", "localhost"} {
		if !validDomain(d) {
			t.Errorf("%q rejected", d)
		}
	}
	for _, d := range []string{"", "a..b", "a.", "UPPER.com", strings.Repeat("a", 64) + ".com", "a_b.com"} {
		if validDomain(d) {
			t.Errorf("%q accepted", d)
		}
	}
}

func TestACMEIssuers(t *testing.T) {
	iss, err := acmeIssuers(proto.Ingress{})
	if err != nil || len(iss) != 1 || iss[0]["email"] != nil {
		t.Errorf("production without email = %v %v (ZeroSSL needs an email)", iss, err)
	}
	iss, _ = acmeIssuers(proto.Ingress{ACMECA: "https://acme.example.test/dir", ACMEEmail: "a@b.c"})
	if len(iss) != 1 || iss[0]["ca"] != "https://acme.example.test/dir" || iss[0]["email"] != "a@b.c" {
		t.Errorf("custom = %v", iss)
	}
}

func TestBootstrapConfig(t *testing.T) {
	b := bootstrapCaddy(testParams)
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v["admin"].(map[string]any)["listen"] != "unix//var/lib/5050/caddy/admin.sock|0600" {
		t.Errorf("bootstrap admin = %v", v["admin"])
	}
	if _, ok := v["apps"]; ok {
		t.Error("bootstrap config must not serve anything")
	}
}
