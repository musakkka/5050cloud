// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// DNS answer settings (docs/SPEC.md 8.4).
const (
	privateTTL  = 5 // seconds, private names
	upstreamTTL = 1 // seconds, upstream resolver answers (Caddy refreshes every second)
	// UpstreamSuffix is the zone the upstream resolver answers.
	UpstreamSuffix = "upstream.5050"
	// DefaultResolverAddr is where the upstream resolver listens on ingress hosts.
	DefaultResolverAddr = "127.0.0.1:5354"
	forwardTimeout      = 2 * time.Second
	rebindInterval      = 2 * time.Second
)

// socketMaker opens sockets; *net.ListenConfig satisfies it. Tests swap in
// one that opens sockets inside a network namespace.
type socketMaker interface {
	ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error)
	Listen(ctx context.Context, network, address string) (net.Listener, error)
}

// nameTable maps a lower-case fully qualified name ("api.internal.") to its addresses.
type nameTable map[string][]netip.Addr

// privateNames returns the names an entry answers: "<base>.internal." and
// "<base>.railway.internal." for the name and each alias kept after a rename.
func privateNames(name string) []string {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	base := strings.TrimSuffix(n, ".internal")
	base = strings.TrimSuffix(base, ".railway")
	if base == "" || strings.ContainsAny(base, " \t") {
		return nil
	}
	return []string{base + ".internal.", base + ".railway.internal."}
}

// buildNameTables groups DNS entries by environment. Draining addresses are
// left out: they stay routable but are never handed to new queries.
func buildNameTables(entries []proto.DNSEntry) (map[string]nameTable, []error) {
	out := map[string]nameTable{}
	var errs []error
	for _, e := range entries {
		var ips []netip.Addr
		draining := map[netip.Addr]bool{}
		for _, s := range e.Draining {
			if a, err := netip.ParseAddr(s); err == nil {
				draining[a] = true
			}
		}
		for _, s := range e.IPs {
			a, err := netip.ParseAddr(s)
			if err != nil || !a.Is4() {
				errs = append(errs, fmt.Errorf("address %q of private name %s is not an IPv4 address and was left out", s, e.Name))
				continue
			}
			if !draining[a] {
				ips = append(ips, a)
			}
		}
		names := privateNames(e.Name)
		if names == nil {
			errs = append(errs, fmt.Errorf("private name %q of environment %s is not a valid name and was skipped", e.Name, e.EnvironmentID))
			continue
		}
		for _, al := range e.Aliases {
			names = append(names, privateNames(al)...)
		}
		t := out[e.EnvironmentID]
		if t == nil {
			t = nameTable{}
			out[e.EnvironmentID] = t
		}
		for _, n := range names {
			t[n] = append(t[n], ips...)
		}
	}
	return out, errs
}

// answerA fills an A answer with the addresses in random order.
func answerA(req *dns.Msg, name string, ips []netip.Addr, ttl uint32) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = true
	m.RecursionAvailable = true
	q := req.Question[0]
	if q.Qtype != dns.TypeA && q.Qtype != dns.TypeANY {
		return m // NOERROR with no answers: the name exists, it has no records of that type
	}
	order := rand.Perm(len(ips))
	for _, i := range order {
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   net.IP(ips[i].AsSlice()),
		})
	}
	return m
}

func nxdomain(req *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeNameError)
	m.Authoritative = true
	m.RecursionAvailable = true
	return m
}

// writeMsg truncates to what the transport allows and writes.
func writeMsg(w dns.ResponseWriter, req, m *dns.Msg) {
	size := dns.MinMsgSize
	if _, ok := w.RemoteAddr().(*net.TCPAddr); ok {
		size = dns.MaxMsgSize
	} else if opt := req.IsEdns0(); opt != nil {
		size = max(int(opt.UDPSize()), dns.MinMsgSize)
		// A forwarded reply already carries the upstream's OPT. A second one is
		// malformed (RFC 6891 6.1.1) and c-ares rejects the whole reply.
		if m.IsEdns0() == nil {
			m.SetEdns0(opt.UDPSize(), false)
		}
	}
	m.Truncate(size)
	_ = w.WriteMsg(m)
}

// ---------------------------------------------------------------- servers

// dnsServer is one UDP and one TCP miekg/dns server on an address.
type dnsServer struct {
	addr string
	udp  *dns.Server
	tcp  *dns.Server
}

func startDNSServer(ctx context.Context, sm socketMaker, addr string, h dns.Handler) (*dnsServer, error) {
	pc, err := sm.ListenPacket(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}
	l, err := sm.Listen(ctx, "tcp", addr)
	if err != nil {
		pc.Close()
		return nil, err
	}
	s := &dnsServer{
		addr: addr,
		udp:  &dns.Server{PacketConn: pc, Handler: h},
		tcp:  &dns.Server{Listener: l, Handler: h, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second},
	}
	started := make(chan struct{}, 2)
	s.udp.NotifyStartedFunc = func() { started <- struct{}{} }
	s.tcp.NotifyStartedFunc = func() { started <- struct{}{} }
	go func() { _ = s.udp.ActivateAndServe() }()
	go func() { _ = s.tcp.ActivateAndServe() }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			s.stop()
			return nil, fmt.Errorf("the DNS server on %s did not start within 5 seconds", addr)
		}
	}
	return s, nil
}

func (s *dnsServer) stop() {
	_ = s.udp.Shutdown()
	_ = s.tcp.Shutdown()
}

// privateDNS answers <name>.internal on every environment gateway of this
// host and forwards everything else to the host's resolvers.
type privateDNS struct {
	sm  socketMaker
	fwd *forwarder
	log *slog.Logger

	mu      sync.RWMutex
	tables  map[string]nameTable // environment id -> names
	servers map[string]*dnsServer
	wanted  map[string]string // listen address -> environment id
	failed  map[string]string // listen address -> last bind error
	loop    sync.Once
	closed  chan struct{}
}

func newPrivateDNS(sm socketMaker, fwd *forwarder, log *slog.Logger) *privateDNS {
	return &privateDNS{
		sm: sm, fwd: fwd, log: log,
		tables:  map[string]nameTable{},
		servers: map[string]*dnsServer{},
		wanted:  map[string]string{},
		failed:  map[string]string{},
		closed:  make(chan struct{}),
	}
}

// update replaces the answers and the set of gateways to listen on. Gateways
// that cannot be bound yet (the bridge does not exist) are retried every 2 s.
func (p *privateDNS) update(tables map[string]nameTable, gateways map[string]string) error {
	p.mu.Lock()
	p.tables = tables
	p.wanted = map[string]string{}
	for gw, env := range gateways {
		p.wanted[net.JoinHostPort(gw, "53")] = env
	}
	p.mu.Unlock()
	p.loop.Do(func() { go p.rebindLoop() })
	return p.reconcile()
}

func (p *privateDNS) reconcile() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, s := range p.servers {
		if _, ok := p.wanted[addr]; !ok {
			s.stop()
			delete(p.servers, addr)
		}
	}
	for addr := range p.failed {
		if _, ok := p.wanted[addr]; !ok {
			delete(p.failed, addr)
		}
	}
	var errs []error
	addrs := make([]string, 0, len(p.wanted))
	for a := range p.wanted {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		env := p.wanted[addr]
		if s, ok := p.servers[addr]; ok && s != nil {
			continue
		}
		s, err := startDNSServer(context.Background(), p.sm, addr, p.handler(env))
		if err != nil {
			msg := fmt.Sprintf("the private DNS server for environment %s cannot listen on %s yet (%v); retrying every 2 seconds", env, addr, err)
			if p.failed[addr] != msg && p.log != nil {
				p.log.Warn(msg)
			}
			p.failed[addr] = msg
			errs = append(errs, errors.New(msg))
			continue
		}
		delete(p.failed, addr)
		p.servers[addr] = s
	}
	return errors.Join(errs...)
}

func (p *privateDNS) rebindLoop() {
	t := time.NewTicker(rebindInterval)
	defer t.Stop()
	for {
		select {
		case <-p.closed:
			return
		case <-t.C:
			p.mu.RLock()
			pending := len(p.failed) > 0
			p.mu.RUnlock()
			if pending {
				_ = p.reconcile()
			}
		}
	}
}

func (p *privateDNS) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	for addr, s := range p.servers {
		s.stop()
		delete(p.servers, addr)
	}
}

func (p *privateDNS) handler(env string) dns.Handler {
	return dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		if len(req.Question) != 1 || req.Opcode != dns.OpcodeQuery {
			m := new(dns.Msg)
			m.SetRcode(req, dns.RcodeNotImplemented)
			writeMsg(w, req, m)
			return
		}
		q := req.Question[0]
		name := strings.ToLower(q.Name)
		if name == "internal." || strings.HasSuffix(name, ".internal.") {
			p.mu.RLock()
			ips, ok := p.tables[env][name]
			p.mu.RUnlock()
			if !ok {
				writeMsg(w, req, nxdomain(req))
				return
			}
			writeMsg(w, req, answerA(req, q.Name, ips, privateTTL))
			return
		}
		_, tcp := w.RemoteAddr().(*net.TCPAddr)
		writeMsg(w, req, p.fwd.forward(req, tcp))
	})
}

// upstreamDNS answers <UpstreamName>.upstream.5050 on 127.0.0.1:5354 for
// Caddy's dynamic upstreams. Changing its answers never reloads Caddy.
type upstreamDNS struct {
	mu    sync.RWMutex
	names nameTable
	srv   *dnsServer
}

func upstreamFQDN(name string) string {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	n = strings.TrimSuffix(n, "."+UpstreamSuffix)
	return n + "." + UpstreamSuffix + "."
}

func (u *upstreamDNS) set(name string, ips []netip.Addr) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.names == nil {
		u.names = nameTable{}
	}
	u.names[upstreamFQDN(name)] = ips
}

func (u *upstreamDNS) replace(t nameTable) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.names = t
}

func (u *upstreamDNS) lookup(name string) ([]netip.Addr, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	ips, ok := u.names[upstreamFQDN(name)]
	return ips, ok
}

func (u *upstreamDNS) start(ctx context.Context, sm socketMaker, addr string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.srv != nil {
		return nil
	}
	s, err := startDNSServer(ctx, sm, addr, dns.HandlerFunc(u.serve))
	if err != nil {
		return fmt.Errorf("the upstream resolver cannot listen on %s: %w", addr, err)
	}
	u.srv = s
	return nil
}

func (u *upstreamDNS) stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.srv != nil {
		u.srv.stop()
		u.srv = nil
	}
}

func (u *upstreamDNS) serve(w dns.ResponseWriter, req *dns.Msg) {
	if len(req.Question) != 1 {
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeFormatError)
		writeMsg(w, req, m)
		return
	}
	name := strings.ToLower(req.Question[0].Name)
	if !strings.HasSuffix(name, "."+UpstreamSuffix+".") {
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeRefused)
		writeMsg(w, req, m)
		return
	}
	u.mu.RLock()
	ips, ok := u.names[name]
	u.mu.RUnlock()
	if !ok {
		writeMsg(w, req, nxdomain(req))
		return
	}
	writeMsg(w, req, answerA(req, req.Question[0].Name, ips, upstreamTTL))
}
