// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Resolver files: /etc/resolv.conf, or the real upstreams of systemd-resolved
// when /etc/resolv.conf only points at its stub (docs/SPEC.md 8.4).
const (
	etcResolvConf     = "/etc/resolv.conf"
	systemdResolvConf = "/run/systemd/resolve/resolv.conf"
)

// forwarder sends queries for names that are not private to the host's resolvers.
type forwarder struct {
	files []string // tried in order; see upstreams
	stat  func(string) (os.FileInfo, error)
	read  func(string) ([]byte, error)

	mu      sync.Mutex
	servers []string
	checked time.Time
	mtimes  map[string]time.Time

	// exchange is replaceable in tests.
	exchange func(ctx context.Context, m *dns.Msg, network, server string) (*dns.Msg, error)
}

func newForwarder() *forwarder {
	return &forwarder{
		files:    []string{etcResolvConf, systemdResolvConf},
		stat:     os.Stat,
		read:     os.ReadFile,
		mtimes:   map[string]time.Time{},
		exchange: exchangeDNS,
	}
}

func exchangeDNS(ctx context.Context, m *dns.Msg, network, server string) (*dns.Msg, error) {
	c := &dns.Client{Net: network, Timeout: forwardTimeout}
	r, _, err := c.ExchangeContext(ctx, m, server)
	return r, err
}

// parseResolvConf returns the nameserver addresses as host:53 strings.
func parseResolvConf(b []byte) []string {
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			ip := f[1]
			if i := strings.IndexByte(ip, '%'); i > 0 {
				ip = ip[:i] // zone of a link-local IPv6 address
			}
			if net.ParseIP(ip) != nil {
				out = append(out, net.JoinHostPort(ip, "53"))
			}
		}
	}
	return out
}

func onlyStub(servers []string) bool {
	if len(servers) == 0 {
		return false
	}
	for _, s := range servers {
		h, _, _ := net.SplitHostPort(s)
		if h != "127.0.0.53" && h != "127.0.0.54" {
			return false
		}
	}
	return true
}

// upstreams reloads the resolver files at most every 5 seconds and only when
// they changed.
func (f *forwarder) upstreams() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.checked) < 5*time.Second && f.servers != nil {
		return f.servers
	}
	f.checked = time.Now()
	changed := f.servers == nil
	for _, p := range f.files {
		fi, err := f.stat(p)
		var mt time.Time
		if err == nil {
			mt = fi.ModTime()
		}
		if !mt.Equal(f.mtimes[p]) {
			changed = true
			f.mtimes[p] = mt
		}
	}
	if !changed {
		return f.servers
	}
	var servers []string
	if b, err := f.read(f.files[0]); err == nil {
		servers = parseResolvConf(b)
	}
	if onlyStub(servers) && len(f.files) > 1 {
		if b, err := f.read(f.files[1]); err == nil {
			if real := parseResolvConf(b); len(real) > 0 {
				servers = real
			}
		}
	}
	f.servers = servers
	if f.servers == nil {
		f.servers = []string{}
	}
	return f.servers
}

// forward asks each upstream in turn and returns the first answer, retrying
// over TCP when a UDP answer is truncated. With no answer it returns SERVFAIL.
func (f *forwarder) forward(req *dns.Msg, tcp bool) *dns.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 2*forwardTimeout)
	defer cancel()
	q := req.Copy()
	for _, s := range f.upstreams() {
		network := "udp"
		if tcp {
			network = "tcp"
		}
		r, err := f.exchange(ctx, q, network, s)
		if err == nil && r != nil && r.Truncated && !tcp {
			if r2, err2 := f.exchange(ctx, q, "tcp", s); err2 == nil && r2 != nil {
				r = r2
			}
		}
		if err != nil || r == nil {
			continue
		}
		if r.Rcode == dns.RcodeServerFailure || r.Rcode == dns.RcodeRefused {
			continue
		}
		r.Id = req.Id
		return r
	}
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeServerFailure)
	m.RecursionAvailable = true
	return m
}
