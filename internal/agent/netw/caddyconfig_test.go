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

func TestCaddyPendingRouteIsServedButNotIssued(t *testing.T) {
	in := proto.Ingress{ACMEPrimary: true, Routes: []proto.Route{
		{Domain: "waiting.example.com", UpstreamName: "svc-a", Port: 80, TLS: "pending", ServiceID: "svc_a"},
		{Domain: "ready.example.com", UpstreamName: "svc-b", Port: 80, TLS: "acme", ServiceID: "svc_b"},
	}}
	b, errs := renderCaddy(testParams, in)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var cfg struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Routes []struct {
						Match []struct {
							Host []string `json:"host"`
						} `json:"match"`
					} `json:"routes"`
				} `json:"servers"`
			} `json:"http"`
			TLS struct {
				Automation struct {
					Policies []struct {
						Subjects []string `json:"subjects"`
					} `json:"policies"`
				} `json:"automation"`
				Certificates struct {
					Automate []string `json:"automate"`
				} `json:"certificates"`
			} `json:"tls"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	hostsOf := func(server string) (hosts []string) {
		for _, r := range cfg.Apps.HTTP.Servers[server].Routes {
			for _, m := range r.Match {
				hosts = append(hosts, m.Host...)
			}
		}
		return hosts
	}
	has := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	if !has(hostsOf("http"), "waiting.example.com") || has(hostsOf("https"), "waiting.example.com") {
		t.Errorf("pending route must be on :80 only: http %v https %v", hostsOf("http"), hostsOf("https"))
	}
	if !has(hostsOf("https"), "ready.example.com") {
		t.Errorf("acme route missing from https: %v", hostsOf("https"))
	}
	var subjects []string
	for _, p := range cfg.Apps.TLS.Automation.Policies {
		subjects = append(subjects, p.Subjects...)
	}
	if has(subjects, "waiting.example.com") || has(cfg.Apps.TLS.Certificates.Automate, "waiting.example.com") {
		t.Errorf("pending domain is handed to ACME: %v", subjects)
	}
	if !has(subjects, "ready.example.com") {
		t.Errorf("acme domain is not handed to ACME: %v", subjects)
	}
}

func TestCaddyConsoleBeatsDuplicateServiceRoute(t *testing.T) {
	in := proto.Ingress{ACMEPrimary: true,
		Console: &proto.Route{Domain: "console.example.com", Port: 5050, TLS: "acme"},
		Routes:  []proto.Route{{Domain: "console.example.com", UpstreamName: "svc-evil", Port: 80, TLS: "acme", ServiceID: "svc_evil"}}}
	b, errs := renderCaddy(testParams, in)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "more than one route") {
		t.Fatalf("errors %v", errs)
	}
	if strings.Contains(string(b), "svc-evil") || !strings.Contains(string(b), "127.0.0.1:5050") {
		t.Error("the service route took the console name")
	}
}

func TestCaddyPrivate(t *testing.T) {
	in := testIngress()
	in.Private = true
	in.ACMEPrimary = false
	in.ACMEPrimaryAddr = "100.96.0.1:80"
	in.ACMEEmail = "ops@example.com"
	in.Certificates = []proto.CertBundle{{Domain: "x.example.com", CertPEM: "c", KeyPEM: "k"}}
	b, errs := renderCaddy(testParams, in)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var cfg struct {
		Apps map[string]json.RawMessage `json:"apps"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Apps["tls"]; ok {
		t.Errorf("a private host must order and load no certificates: %s", cfg.Apps["tls"])
	}
	var h struct {
		Servers map[string]struct {
			Listen    []string `json:"listen"`
			Automatic any      `json:"automatic_https"`
			Trusted   struct {
				Ranges []string `json:"ranges"`
			} `json:"trusted_proxies"`
			Headers []string `json:"client_ip_headers"`
			Routes  []struct {
				Handle []struct {
					StatusCode any `json:"status_code"`
				} `json:"handle"`
			} `json:"routes"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(cfg.Apps["http"], &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Servers) != 1 {
		t.Fatalf("servers = %v, want only plain HTTP", h.Servers)
	}
	s := h.Servers["http"]
	if len(s.Listen) != 1 || s.Listen[0] != "127.0.0.1:80" {
		t.Errorf("listen = %v, want loopback only", s.Listen)
	}
	if len(s.Trusted.Ranges) == 0 || s.Trusted.Ranges[0] != "127.0.0.1/32" {
		t.Errorf("trusted = %v, want loopback first so the visitor's scheme and address survive", s.Trusted.Ranges)
	}
	if len(s.Headers) == 0 || s.Headers[0] != "Cf-Connecting-Ip" {
		t.Errorf("client ip headers = %v", s.Headers)
	}
	if strings.Contains(string(cfg.Apps["http"]), "\"Location\"") {
		t.Error("a private host must not redirect to HTTPS: the tunnel already speaks plain HTTP here, it would loop")
	}
	if !strings.Contains(string(cfg.Apps["http"]), "\"X-Forwarded-For\"") {
		t.Error("a private host must replace X-Forwarded-For, or every visitor looks like 127.0.0.1 to the rate limits")
	}
	if strings.Contains(string(cfg.Apps["http"]), "acme-challenge") {
		t.Error("a private host must not forward ACME challenges")
	}
}
