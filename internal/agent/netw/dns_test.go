// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func query(t *testing.T, network, addr, name string, qtype uint16) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	c := &dns.Client{Net: network, Timeout: 2 * time.Second}
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s %s: %v", network, name, err)
	}
	return r
}

func aRecords(r *dns.Msg) []string {
	var out []string
	for _, rr := range r.Answer {
		if a, ok := rr.(*dns.A); ok {
			out = append(out, a.A.String())
		}
	}
	sort.Strings(out)
	return out
}

func testEntries() []proto.DNSEntry {
	return []proto.DNSEntry{
		{EnvironmentID: "env_a", Name: "api.internal", Aliases: []string{"backend.internal"}, IPs: []string{"10.129.4.7", "10.130.4.3", "10.129.4.8"}, Draining: []string{"10.129.4.8"}},
		{EnvironmentID: "env_a", Name: "Postgres.internal", IPs: []string{"10.129.4.20"}},
		{EnvironmentID: "env_b", Name: "api.internal", IPs: []string{"10.129.5.7"}},
	}
}

func TestPrivateNames(t *testing.T) {
	cases := map[string][]string{
		"api.internal":          {"api.internal.", "api.railway.internal."},
		"API.railway.internal.": {"api.internal.", "api.railway.internal."},
		"worker":                {"worker.internal.", "worker.railway.internal."},
		"":                      nil,
		"a b":                   nil,
	}
	for in, want := range cases {
		if got := privateNames(in); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("privateNames(%q) = %v, want %v", in, got, want)
		}
	}
	_, errs := buildNameTables([]proto.DNSEntry{{EnvironmentID: "e", Name: "", IPs: []string{"x"}}})
	if len(errs) != 2 {
		t.Errorf("errors = %v", errs)
	}
}

func TestPrivateDNSAnswers(t *testing.T) {
	ls := newLoopSockets()
	fwd := newForwarder()
	fwd.servers, fwd.checked = []string{"192.0.2.53:53"}, time.Now()
	fwd.exchange = func(_ context.Context, m *dns.Msg, network, server string) (*dns.Msg, error) {
		r := new(dns.Msg)
		r.SetReply(m)
		r.Answer = append(r.Answer, &dns.A{Hdr: dns.RR_Header{Name: m.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: []byte{93, 184, 215, 14}})
		return r, nil
	}
	p := newPrivateDNS(ls, fwd, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer p.close()
	tables, errs := buildNameTables(testEntries())
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := p.update(tables, map[string]string{"10.129.4.1": "env_a", "10.129.5.1": "env_b"}); err != nil {
		t.Fatal(err)
	}
	a, b := ls.addr("10.129.4.1:53"), ls.addr("10.129.5.1:53")

	for _, name := range []string{"api.internal", "api.railway.internal", "backend.internal", "BACKEND.railway.internal"} {
		r := query(t, "udp", a, name, dns.TypeA)
		if got := strings.Join(aRecords(r), ","); got != "10.129.4.7,10.130.4.3" {
			t.Errorf("%s = %s (draining address must be left out)", name, got)
		}
		if !r.Authoritative || r.Answer[0].Header().Ttl != privateTTL {
			t.Errorf("%s: authoritative %v ttl %d", name, r.Authoritative, r.Answer[0].Header().Ttl)
		}
	}
	if got := aRecords(query(t, "tcp", a, "postgres.internal", dns.TypeA)); len(got) != 1 {
		t.Errorf("postgres over TCP = %v", got)
	}
	// Environments see only their own names.
	if got := aRecords(query(t, "udp", b, "api.internal", dns.TypeA)); len(got) != 1 || got[0] != "10.129.5.7" {
		t.Errorf("env_b api = %v", got)
	}
	if r := query(t, "udp", b, "postgres.internal", dns.TypeA); r.Rcode != dns.RcodeNameError {
		t.Errorf("env_b sees env_a's postgres: rcode %d", r.Rcode)
	}
	if r := query(t, "udp", a, "api.internal", dns.TypeAAAA); r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 {
		t.Errorf("AAAA = rcode %d answers %d", r.Rcode, len(r.Answer))
	}
	// Other names go to the host's resolvers.
	if got := aRecords(query(t, "udp", a, "example.com", dns.TypeA)); len(got) != 1 || got[0] != "93.184.215.14" {
		t.Errorf("forwarded = %v", got)
	}
	// Shuffled: both orders show up.
	orders := map[string]bool{}
	for range 40 {
		r := query(t, "udp", a, "api.internal", dns.TypeA)
		orders[r.Answer[0].(*dns.A).A.String()] = true
	}
	if len(orders) != 2 {
		t.Errorf("answers are not shuffled: %v", orders)
	}

	// A new state drops env_b's listener and changes answers in place.
	tables, _ = buildNameTables([]proto.DNSEntry{{EnvironmentID: "env_a", Name: "api.internal", IPs: []string{"10.129.4.9"}}})
	if err := p.update(tables, map[string]string{"10.129.4.1": "env_a"}); err != nil {
		t.Fatal(err)
	}
	if got := aRecords(query(t, "udp", a, "api.internal", dns.TypeA)); len(got) != 1 || got[0] != "10.129.4.9" {
		t.Errorf("after update = %v", got)
	}
	p.mu.RLock()
	n := len(p.servers)
	p.mu.RUnlock()
	if n != 1 {
		t.Errorf("servers = %d, want 1", n)
	}
}

func TestPrivateDNSLargeAnswerTruncates(t *testing.T) {
	ls := newLoopSockets()
	p := newPrivateDNS(ls, newForwarder(), nil)
	defer p.close()
	var ips []string
	for i := 2; i < 200; i++ {
		ips = append(ips, fmt.Sprintf("10.129.4.%d", i))
	}
	tables, _ := buildNameTables([]proto.DNSEntry{{EnvironmentID: "e", Name: "big.internal", IPs: ips}})
	if err := p.update(tables, map[string]string{"10.129.4.1": "e"}); err != nil {
		t.Fatal(err)
	}
	m := new(dns.Msg)
	m.SetQuestion("big.internal.", dns.TypeA)
	c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	r, _, err := c.Exchange(m, ls.addr("10.129.4.1:53"))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated {
		t.Error("a 198-address answer over plain UDP must be truncated")
	}
	if got := query(t, "tcp", ls.addr("10.129.4.1:53"), "big.internal", dns.TypeA); len(got.Answer) != 198 {
		t.Errorf("TCP answers = %d", len(got.Answer))
	}
}

func TestPrivateDNSRetriesBind(t *testing.T) {
	ls := newLoopSockets()
	ls.fail["10.129.4.1:53"] = true
	p := newPrivateDNS(ls, newForwarder(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer p.close()
	tables, _ := buildNameTables(testEntries())
	err := p.update(tables, map[string]string{"10.129.4.1": "env_a"})
	if err == nil || !strings.Contains(err.Error(), "retrying") {
		t.Fatalf("err = %v", err)
	}
	ls.mu.Lock()
	ls.fail = map[string]bool{}
	ls.mu.Unlock()
	deadline := time.Now().Add(3 * rebindInterval)
	for time.Now().Before(deadline) {
		if a := ls.addr("10.129.4.1:53"); a != "" {
			if got := aRecords(query(t, "udp", a, "api.internal", dns.TypeA)); len(got) == 2 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the DNS server did not start after the address appeared")
}

func TestForwarderResolvConf(t *testing.T) {
	dir := t.TempDir()
	etc, sysd := filepath.Join(dir, "resolv.conf"), filepath.Join(dir, "systemd.conf")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(etc, "# stub\nnameserver 127.0.0.53\noptions edns0 trust-ad\nsearch example\n")
	write(sysd, "nameserver 10.0.0.2\nnameserver fe80::1%eth0\nnameserver nonsense\n")
	f := newForwarder()
	f.files = []string{etc, sysd}
	got := f.upstreams()
	if strings.Join(got, ",") != "10.0.0.2:53,[fe80::1]:53" {
		t.Errorf("stub resolv.conf: upstreams = %v", got)
	}
	write(etc, "nameserver 1.1.1.1\nnameserver 8.8.8.8\n")
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(etc, future, future)
	f.checked = time.Time{}
	if got := f.upstreams(); strings.Join(got, ",") != "1.1.1.1:53,8.8.8.8:53" {
		t.Errorf("plain resolv.conf: upstreams = %v", got)
	}
}

func TestForwarderFallbacks(t *testing.T) {
	f := newForwarder()
	f.servers, f.checked = []string{"a:53", "b:53", "c:53"}, time.Now()
	var tried []string
	f.exchange = func(_ context.Context, m *dns.Msg, network, server string) (*dns.Msg, error) {
		tried = append(tried, network+" "+server)
		switch {
		case server == "a:53":
			return nil, errors.New("timeout")
		case server == "b:53" && network == "udp":
			r := new(dns.Msg)
			r.SetRcode(m, dns.RcodeServerFailure)
			return r, nil
		case server == "c:53" && network == "udp":
			r := new(dns.Msg)
			r.SetReply(m)
			r.Truncated = true
			return r, nil
		}
		r := new(dns.Msg)
		r.SetReply(m)
		r.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: m.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET}, A: []byte{1, 2, 3, 4}}}
		return r, nil
	}
	req := new(dns.Msg)
	req.SetQuestion("example.org.", dns.TypeA)
	r := f.forward(req, false)
	if len(r.Answer) != 1 || r.Id != req.Id {
		t.Fatalf("answer = %v", r)
	}
	if strings.Join(tried, ",") != "udp a:53,udp b:53,udp c:53,tcp c:53" {
		t.Errorf("tried = %v", tried)
	}
	f.servers = []string{}
	if r := f.forward(req, false); r.Rcode != dns.RcodeServerFailure {
		t.Errorf("no upstreams rcode = %d", r.Rcode)
	}
}

func TestUpstreamResolver(t *testing.T) {
	ls := newLoopSockets()
	u := &upstreamDNS{}
	if err := u.start(context.Background(), ls, "127.0.0.1:5354"); err != nil {
		t.Fatal(err)
	}
	defer u.stop()
	if err := u.start(context.Background(), ls, "127.0.0.1:5354"); err != nil {
		t.Fatal("second start must be a no-op")
	}
	addr := ls.addr("127.0.0.1:5354")
	t1, _ := upstreamTable([]proto.UpstreamSet{{Name: "svc-abc", IPs: []string{"10.129.4.7", "10.130.4.2"}}})
	u.replace(t1)
	r := query(t, "udp", addr, "svc-abc.upstream.5050", dns.TypeA)
	if got := strings.Join(aRecords(r), ","); got != "10.129.4.7,10.130.4.2" || r.Answer[0].Header().Ttl != upstreamTTL {
		t.Errorf("answers = %s", got)
	}
	u.set("svc-abc", nil)
	if r := query(t, "udp", addr, "svc-abc.upstream.5050", dns.TypeA); r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 {
		t.Errorf("empty set: rcode %d answers %d", r.Rcode, len(r.Answer))
	}
	if r := query(t, "udp", addr, "missing.upstream.5050", dns.TypeA); r.Rcode != dns.RcodeNameError {
		t.Errorf("missing rcode = %d", r.Rcode)
	}
	if r := query(t, "udp", addr, "example.com", dns.TypeA); r.Rcode != dns.RcodeRefused {
		t.Errorf("outside zone rcode = %d", r.Rcode)
	}
	if ips, ok := u.lookup("SVC-ABC.upstream.5050."); !ok || len(ips) != 0 {
		t.Errorf("lookup = %v %v", ips, ok)
	}
}
