// SPDX-License-Identifier: AGPL-3.0-only

// Package routes computes the public and private entry points of every
// service from committed config and replica health (docs/SPEC.md 7.3, 7.4,
// 8.4, 9.11): Caddy routes (one per domain, pointing at a dynamic upstream
// name), the upstream resolver's answers, TCP proxies, private DNS entries
// with draining addresses, the panic (fail open) flag, and which ingress
// host runs ACME.
//
// It is pure: the engine passes snapshots and replica states in and puts
// the result into each host's desired state. Replica changes only change
// upstream answers and DNS, never the route set, so they never reload Caddy.
package routes

import (
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Replica is one container as routing sees it.
type Replica struct {
	ID        string
	ServiceID string
	EnvID     string
	HostID    string
	IP        string
	// State is the agent's replica state ("healthy", "unhealthy", "running", ...).
	State string
	// Serving: the replica belongs to the deployment that takes traffic
	// (the active one, or the new one after cutover).
	Serving bool
	// Draining: the replica left the routes and stays in private DNS for
	// in-flight connections until the drain ends (7.3).
	Draining bool
	// HostOnline is false once the host has been offline for 60 s; its
	// replicas are withdrawn from every route and DNS answer (7.1).
	HostOnline bool
}

// Service is one committed service instance with its replicas.
type Service struct {
	Snap     cpapi.ServiceSnapshot
	Replicas []Replica
	// Port is the effective target port.
	Port int
}

// Output is what the ingress hosts and every host's resolvers need.
type Output struct {
	Routes    []proto.Route
	Upstreams []proto.UpstreamSet
	TCP       []proto.TCPProxy
	// DNS holds private names per environment.
	DNS map[string][]proto.DNSEntry
	// Panic lists service instances ("env/svc") routed in fail-open mode.
	Panic map[string]bool
	// Serving lists the replica ids that take traffic, per service instance.
	Serving map[string][]string
}

// Key identifies a service instance.
func Key(envID, serviceID string) string { return envID + "/" + serviceID }

// UpstreamName is the dynamic upstream a service instance's routes use; the
// upstream resolver answers "<name>.upstream.5050".
func UpstreamName(envID, serviceID string) string {
	n := strings.ToLower("u-" + trimPrefix(serviceID) + "-" + trimPrefix(envID))
	return strings.ReplaceAll(n, "_", "-")
}

func trimPrefix(id string) string {
	if i := strings.IndexByte(id, '_'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// Healthy states take traffic; running states are used in panic mode.
func healthy(state string) bool { return state == "healthy" }

func running(state string) bool {
	switch state {
	case "healthy", "unhealthy", "running":
		return true
	}
	return false
}

// TLSMode picks the issuer for a domain: Caddy's internal CA for names that
// can never get a public certificate, ACME otherwise.
func TLSMode(domain string) string {
	d := strings.ToLower(strings.TrimSuffix(domain, "."))
	for _, s := range []string{".localhost", ".internal", ".local", ".test"} {
		if strings.HasSuffix(d, s) || d == strings.TrimPrefix(s, ".") {
			return "internal"
		}
	}
	return "acme"
}

// Compute builds the routes, upstreams, TCP proxies and DNS entries.
func Compute(services []Service) Output {
	out := Output{DNS: map[string][]proto.DNSEntry{}, Panic: map[string]bool{}, Serving: map[string][]string{}}
	sorted := append([]Service(nil), services...)
	sort.Slice(sorted, func(i, j int) bool {
		return Key(sorted[i].Snap.EnvID, sorted[i].Snap.ServiceID) < Key(sorted[j].Snap.EnvID, sorted[j].Snap.ServiceID)
	})
	for _, s := range sorted {
		if s.Snap.Deleted {
			continue
		}
		key := Key(s.Snap.EnvID, s.Snap.ServiceID)
		ips, ids, panic := servingSet(s.Replicas)
		if panic {
			out.Panic[key] = true
		}
		out.Serving[key] = ids
		name := UpstreamName(s.Snap.EnvID, s.Snap.ServiceID)
		hasRoute := false
		for _, d := range s.Snap.Domains {
			if d.Domain == "" {
				continue
			}
			port := d.Port
			if port == 0 {
				port = s.Port
			}
			out.Routes = append(out.Routes, proto.Route{
				Domain:       strings.ToLower(d.Domain),
				UpstreamName: name,
				Port:         port,
				TLS:          TLSMode(d.Domain),
				ServiceID:    s.Snap.ServiceID,
				Panic:        panic,
			})
			hasRoute = true
		}
		if hasRoute {
			out.Upstreams = append(out.Upstreams, proto.UpstreamSet{Name: name, IPs: ips})
		}
		for _, t := range s.Snap.TCPProxies {
			if t.HostPort == 0 {
				continue
			}
			ups := make([]string, 0, len(ips))
			target := t.TargetPort
			if target == 0 {
				target = s.Port
			}
			for _, ip := range ips {
				ups = append(ups, joinHostPort(ip, target))
			}
			out.TCP = append(out.TCP, proto.TCPProxy{HostPort: t.HostPort, Upstreams: ups})
		}
		entry := proto.DNSEntry{
			EnvironmentID: s.Snap.EnvID,
			Name:          PrivateDomain(s.Snap),
			IPs:           ips,
		}
		for _, a := range s.Snap.PrivateAliases {
			if a = strings.TrimSpace(a); a != "" {
				entry.Aliases = append(entry.Aliases, a+".internal")
			}
		}
		for _, r := range s.Replicas {
			if r.Draining && r.HostOnline && r.IP != "" {
				entry.Draining = append(entry.Draining, r.IP)
			}
		}
		sort.Strings(entry.Draining)
		out.DNS[s.Snap.EnvID] = append(out.DNS[s.Snap.EnvID], entry)
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Domain < out.Routes[j].Domain })
	sort.Slice(out.TCP, func(i, j int) bool { return out.TCP[i].HostPort < out.TCP[j].HostPort })
	return out
}

// servingSet returns the addresses that take traffic: healthy serving
// replicas on online hosts, or, when none is healthy but some are running,
// every running one with panic set (7.4: a degradation never becomes a
// guaranteed 502).
func servingSet(reps []Replica) (ips, ids []string, panic bool) {
	var runIPs, runIDs []string
	for _, r := range reps {
		if !r.Serving || r.Draining || !r.HostOnline || r.IP == "" {
			continue
		}
		if healthy(r.State) {
			ips = append(ips, r.IP)
			ids = append(ids, r.ID)
		}
		if running(r.State) {
			runIPs = append(runIPs, r.IP)
			runIDs = append(runIDs, r.ID)
		}
	}
	if len(ips) == 0 && len(runIPs) > 0 {
		ips, ids, panic = runIPs, runIDs, true
	}
	sort.Strings(ips)
	sort.Strings(ids)
	if ips == nil {
		ips = []string{}
	}
	return ips, ids, panic
}

// PrivateDomain is "<private name>.internal".
func PrivateDomain(s cpapi.ServiceSnapshot) string {
	n := s.PrivateName
	if n == "" {
		n = Slug(s.Config.Name)
	}
	return n + ".internal"
}

// Slug lower-cases a name and keeps [a-z0-9-], as private names and
// generated domains do.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func joinHostPort(ip string, port int) string {
	return net.JoinHostPort(ip, strconv.Itoa(port))
}

// IngressHost is an ingress candidate for ACME primary selection.
type IngressHost struct {
	ID        string
	Online    bool
	Control   bool
	CreatedAt int64
	MeshIP    string
}

// ACMEPrimary picks the one ingress host that runs ACME: the control host
// while it is online, else the oldest online ingress host (8.4).
func ACMEPrimary(hosts []IngressHost) (id, addr string) {
	var best *IngressHost
	for i := range hosts {
		h := &hosts[i]
		if !h.Online {
			continue
		}
		if h.Control {
			return h.ID, h.MeshIP + ":80"
		}
		if best == nil || h.CreatedAt < best.CreatedAt || (h.CreatedAt == best.CreatedAt && h.ID < best.ID) {
			best = h
		}
	}
	if best == nil {
		return "", ""
	}
	return best.ID, best.MeshIP + ":80"
}
