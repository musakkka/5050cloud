// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Caddy settings (docs/SPEC.md 8.4).
const (
	accessLoggerName    = "fifty"
	defaultStreamDelay  = 300 // seconds
	acmeStagingURL      = "https://acme-staging-v02.api.letsencrypt.org/directory"
	acmeZeroSSLURL      = "https://acme.zerossl.com/v2/DV90"
	acmeChallengePrefix = "/.well-known/acme-challenge/"
)

// caddyParams are the host-side settings of a rendered config. Paths are the
// same inside the Caddy container, which mounts <DataDir>/caddy at the same path.
type caddyParams struct {
	Dir          string // <DataDir>/caddy
	AdminSocket  string
	ResolverAddr string
	HTTPPort     int
	HTTPSPort    int
}

func (p caddyParams) accessLog() string { return filepath.Join(p.Dir, "logs", "access.log") }
func (p caddyParams) tlsLog() string    { return filepath.Join(p.Dir, "logs", "tls.log") }
func (p caddyParams) storage() string   { return filepath.Join(p.Dir, "data") }

// notFoundPage is the catch-all answer for a domain with no service.
const notFoundPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Not found</title>
<style>
:root{color-scheme:light dark;--bg:#fafafa;--fg:#18181b;--muted:#71717a;--card:#fff;--line:#e4e4e7}
@media (prefers-color-scheme:dark){:root{--bg:#09090b;--fg:#fafafa;--muted:#a1a1aa;--card:#18181b;--line:#27272a}}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;padding:16px;box-sizing:border-box}
main{max-width:440px;width:100%;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:32px}
h1{margin:0 0 8px;font-size:20px}p{margin:0;color:var(--muted)}code{font-size:14px}
</style></head>
<body><main><h1>No service is attached to this domain</h1>
<p>If this is your domain, attach it to a service in its Settings, under Networking.</p></main></body></html>
`

type obj = map[string]any

// validDomain accepts lower-case host names without wildcards.
func validDomain(d string) bool {
	if d == "" || len(d) > 253 || strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// normRoute is a validated route.
type normRoute struct {
	proto.Route
	tls string // "acme" | "internal" | "off"
}

func normalizeRoutes(in proto.Ingress) ([]normRoute, []error) {
	var errs []error
	var out []normRoute
	seen := map[string]bool{}
	all := append([]proto.Route{}, in.Routes...)
	if in.Console != nil {
		all = append(all, *in.Console)
	}
	for _, r := range all {
		r.Domain = strings.ToLower(strings.TrimSpace(r.Domain))
		if !validDomain(r.Domain) {
			errs = append(errs, fmt.Errorf("the domain %q is not a valid host name, so its route was skipped", r.Domain))
			continue
		}
		if seen[r.Domain] {
			errs = append(errs, fmt.Errorf("the domain %s has more than one route; only the first one is served", r.Domain))
			continue
		}
		if r.Port < 1 || r.Port > 65535 {
			errs = append(errs, fmt.Errorf("the route for %s has no valid target port (%d), so it was skipped", r.Domain, r.Port))
			continue
		}
		t := r.TLS
		switch t {
		case "", "acme":
			t = "acme"
		case "internal", "off":
		default:
			errs = append(errs, fmt.Errorf("the route for %s asks for TLS mode %q, which is not acme, internal or off; using acme", r.Domain, r.TLS))
			t = "acme"
		}
		seen[r.Domain] = true
		out = append(out, normRoute{Route: r, tls: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, errs
}

func proxyHandler(r normRoute, p caddyParams, delaySec int) obj {
	h := obj{
		"handler": "reverse_proxy",
		"load_balancing": obj{
			"selection_policy": obj{"policy": "random"},
			"try_duration":     "5s",
			"try_interval":     "250ms",
		},
		"transport":          obj{"protocol": "http", "dial_timeout": "3s"},
		"stream_close_delay": fmt.Sprintf("%ds", delaySec),
		"headers": obj{"request": obj{"set": obj{
			"X-Real-Ip": []string{"{http.vars.client_ip}"},
		}}},
	}
	if r.UpstreamName == "" {
		// The console on the control host: 5050d listens on loopback only.
		h["upstreams"] = []obj{{"dial": net.JoinHostPort("127.0.0.1", strconv.Itoa(r.Port))}}
	} else {
		h["dynamic_upstreams"] = obj{
			"source":       "a",
			"name":         strings.TrimSuffix(upstreamFQDN(r.UpstreamName), "."),
			"port":         strconv.Itoa(r.Port),
			"refresh":      "1s",
			"resolver":     obj{"addresses": []string{"udp/" + p.ResolverAddr}},
			"dial_timeout": "1s",
			"versions":     obj{"ipv4": true},
		}
	}
	if !r.Panic {
		// Fail open when every replica is unhealthy (7.4): no passive checks then.
		h["health_checks"] = obj{"passive": obj{"fail_duration": "10s", "max_fails": 2}}
	}
	return h
}

func hostRoute(domains []string, handle ...obj) obj {
	return obj{"match": []obj{{"host": domains}}, "handle": handle, "terminal": true}
}

func notFoundRoute() obj {
	return obj{"handle": []obj{{
		"handler":     "static_response",
		"status_code": 404,
		"headers":     obj{"Content-Type": []string{"text/html; charset=utf-8"}, "Cache-Control": []string{"no-store"}},
		"body":        notFoundPage,
	}}, "terminal": true}
}

func acmeIssuers(in proto.Ingress) ([]obj, error) {
	withEmail := func(o obj) obj {
		if in.ACMEEmail != "" {
			o["email"] = in.ACMEEmail
		}
		return o
	}
	switch in.ACMECA {
	case "", "production":
		iss := []obj{withEmail(obj{"module": "acme"})}
		if in.ACMEEmail != "" {
			// ZeroSSL needs an email for its automatic EAB credentials.
			iss = append(iss, withEmail(obj{"module": "acme", "ca": acmeZeroSSLURL}))
		}
		return iss, nil
	case "staging":
		return []obj{withEmail(obj{"module": "acme", "ca": acmeStagingURL})}, nil
	default:
		u, err := url.Parse(in.ACMECA)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("the ACME directory %q is not production, staging or an http(s) URL; using production", in.ACMECA)
		}
		return []obj{withEmail(obj{"module": "acme", "ca": in.ACMECA})}, nil
	}
}

// renderCaddy renders the whole Caddy JSON config for an ingress state.
// Upstream addresses are not part of it: they come from the upstream
// resolver, so replica changes never change this config.
func renderCaddy(p caddyParams, in proto.Ingress) ([]byte, []error) {
	routes, errs := normalizeRoutes(in)
	delay := in.StreamCloseDelaySec
	if delay <= 0 {
		delay = defaultStreamDelay
	}
	var tlsRoutes, httpRoutes []obj
	var redirect, acmeDomains, internalDomains []string
	if !in.ACMEPrimary && in.ACMEPrimaryAddr != "" {
		if _, _, err := net.SplitHostPort(in.ACMEPrimaryAddr); err != nil {
			errs = append(errs, fmt.Errorf("the ACME primary address %q is not host:port, so challenges are not forwarded", in.ACMEPrimaryAddr))
		} else {
			httpRoutes = append(httpRoutes, obj{
				"match":    []obj{{"path": []string{acmeChallengePrefix + "*"}}},
				"handle":   []obj{{"handler": "reverse_proxy", "upstreams": []obj{{"dial": in.ACMEPrimaryAddr}}}},
				"terminal": true,
			})
		}
	}
	for _, r := range routes {
		h := proxyHandler(r, p, delay)
		switch r.tls {
		case "off":
			httpRoutes = append(httpRoutes, hostRoute([]string{r.Domain}, h))
		case "internal":
			internalDomains = append(internalDomains, r.Domain)
			tlsRoutes = append(tlsRoutes, hostRoute([]string{r.Domain}, h))
			redirect = append(redirect, r.Domain)
		default:
			acmeDomains = append(acmeDomains, r.Domain)
			tlsRoutes = append(tlsRoutes, hostRoute([]string{r.Domain}, h))
			redirect = append(redirect, r.Domain)
		}
	}
	if len(redirect) > 0 {
		httpRoutes = append(httpRoutes, hostRoute(redirect, obj{
			"handler":     "static_response",
			"status_code": 308,
			"headers":     obj{"Location": []string{"https://{http.request.host}{http.request.uri}"}},
		}))
	}
	httpRoutes = append(httpRoutes, notFoundRoute())
	tlsRoutes = append(tlsRoutes, notFoundRoute())

	logs := obj{"default_logger_name": accessLoggerName}
	https := obj{
		"listen":                  []string{":" + strconv.Itoa(p.HTTPSPort)},
		"routes":                  tlsRoutes,
		"tls_connection_policies": []obj{{}},
		"automatic_https":         obj{"disable": true},
		"logs":                    logs,
	}
	http := obj{
		"listen":          []string{":" + strconv.Itoa(p.HTTPPort)},
		"routes":          httpRoutes,
		"automatic_https": obj{"disable": true},
		"logs":            logs,
	}
	if len(in.TrustedProxies) > 0 {
		var ranges []string
		for _, c := range in.TrustedProxies {
			if _, _, err := net.ParseCIDR(c); err != nil {
				errs = append(errs, fmt.Errorf("trusted proxy range %q is not a CIDR and was skipped", c))
				continue
			}
			ranges = append(ranges, c)
		}
		if len(ranges) > 0 {
			for _, s := range []obj{https, http} {
				s["trusted_proxies"] = obj{"source": "static", "ranges": ranges}
				s["client_ip_headers"] = []string{"X-Forwarded-For"}
			}
		}
	}

	tls := obj{}
	certs := obj{}
	var policies []obj
	var automate []string
	if in.ACMEPrimary && len(acmeDomains) > 0 {
		iss, err := acmeIssuers(in)
		if err != nil {
			errs = append(errs, err)
			iss, _ = acmeIssuers(proto.Ingress{ACMEEmail: in.ACMEEmail})
		}
		policies = append(policies, obj{"subjects": acmeDomains, "issuers": iss})
		automate = append(automate, acmeDomains...)
	}
	if len(internalDomains) > 0 {
		policies = append(policies, obj{"subjects": internalDomains, "issuers": []obj{{"module": "internal"}}})
		automate = append(automate, internalDomains...)
	}
	if len(automate) > 0 {
		sort.Strings(automate)
		certs["automate"] = automate
	}
	if !in.ACMEPrimary && len(in.Certificates) > 0 {
		bundles := append([]proto.CertBundle{}, in.Certificates...)
		sort.Slice(bundles, func(i, j int) bool { return bundles[i].Domain < bundles[j].Domain })
		var pems []obj
		for _, c := range bundles {
			if c.CertPEM == "" || c.KeyPEM == "" {
				errs = append(errs, fmt.Errorf("the pushed certificate for %s has no certificate or key and was skipped", c.Domain))
				continue
			}
			pems = append(pems, obj{"certificate": c.CertPEM, "key": c.KeyPEM, "tags": []string{"5050-pushed"}})
		}
		if len(pems) > 0 {
			certs["load_pem"] = pems
		}
	}
	if len(certs) > 0 {
		tls["certificates"] = certs
	}
	if len(policies) > 0 {
		tls["automation"] = obj{"policies": policies}
	}

	apps := obj{"http": obj{"servers": obj{"https": https, "http": http}}}
	if len(tls) > 0 {
		apps["tls"] = tls
	}
	cfg := obj{
		"admin": obj{
			"listen": "unix/" + p.AdminSocket + "|0600",
			"config": obj{"persist": true},
		},
		"storage": obj{"module": "file_system", "root": p.storage()},
		"logging": obj{"logs": obj{
			"default": obj{
				"writer":  obj{"output": "stderr"},
				"exclude": []string{"http.log.access." + accessLoggerName},
			},
			accessLoggerName: obj{
				"writer":  obj{"output": "file", "filename": p.accessLog(), "roll_size_mb": 100, "roll_keep": 2, "roll_keep_days": 2},
				"encoder": obj{"format": "json"},
				"include": []string{"http.log.access." + accessLoggerName},
			},
			"tlslog": obj{
				"writer":  obj{"output": "file", "filename": p.tlsLog(), "roll_size_mb": 10, "roll_keep": 2},
				"encoder": obj{"format": "json"},
				"include": []string{"tls"},
			},
		}},
		"apps": apps,
	}
	b, err := marshalJSON(cfg)
	if err != nil {
		return nil, append(errs, fmt.Errorf("rendering the Caddy config failed: %w", err))
	}
	return b, errs
}

// marshalJSON is indented JSON without HTML escaping, ending in a newline.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bootstrapCaddy is the config Caddy starts with when it has no saved one:
// only the admin socket and storage, so the agent can load the real config.
func bootstrapCaddy(p caddyParams) []byte {
	b, _ := marshalJSON(obj{
		"admin":   obj{"listen": "unix/" + p.AdminSocket + "|0600", "config": obj{"persist": true}},
		"storage": obj{"module": "file_system", "root": p.storage()},
	})
	return b
}
