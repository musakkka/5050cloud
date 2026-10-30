// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Chain names. iptables allows 28 characters.
const (
	chainIsolate = "5050-ISOLATE"
	chainEgress  = "5050-EGRESS"
	chainHostIn  = "5050-HOSTIN"
	chainNAT     = "5050-NAT"
	chainMSS     = "5050-MSS"
	chainPrefix  = "5050-"
	envChainPfx  = "5050-E-"
	egrChainPfx  = "5050-X-"
)

// firewallSpec is everything the rule set depends on, already parsed.
type firewallSpec struct {
	Pod          netip.Prefix
	Mesh         netip.Prefix
	Build        netip.Prefix
	BuildGateway netip.Addr
	Envs         []envRules // sorted by ID
}

type envRules struct {
	ID            string
	Local         netip.Prefix   // this host's /24
	Gateway       netip.Addr     // DNS listens here
	Subnets       []netip.Prefix // this host's and every peer host's /24, sorted
	EgressBlocked bool
	EgressAllow   []netip.Prefix // resolved allowlist, sorted
	AllowMetadata []netip.Addr   // sorted
}

// hostResolver resolves a hostname of an egress allowlist to addresses.
type hostResolver func(ctx context.Context, host string) ([]netip.Addr, error)

// buildFirewallSpec validates the networks of a desired state and turns them
// into a firewallSpec. A network with an invalid subnet is skipped with an
// error; the rest still apply, so one bad entry cannot open or close
// everything else.
func buildFirewallSpec(ctx context.Context, plan AddressPlan, nets []proto.Network, resolve hostResolver) (firewallSpec, []error) {
	spec := firewallSpec{
		Pod:          plan.Pod,
		Mesh:         plan.Mesh,
		Build:        plan.BuildSubnet(),
		BuildGateway: Gateway(plan.BuildSubnet()),
	}
	var errs []error
	seen := map[string]bool{}
	for _, n := range nets {
		if n.EnvironmentID == "" {
			errs = append(errs, fmt.Errorf("a network in the desired state has no environment id, so it was skipped"))
			continue
		}
		if seen[n.EnvironmentID] {
			errs = append(errs, fmt.Errorf("environment %s appears twice in the desired state networks; only the first entry was used", n.EnvironmentID))
			continue
		}
		local, err := parseEnvSubnet(plan, n.Subnet)
		if err != nil {
			errs = append(errs, fmt.Errorf("network of environment %s was skipped: %w", n.EnvironmentID, err))
			continue
		}
		seen[n.EnvironmentID] = true
		gw := Gateway(local)
		if n.Gateway != "" {
			g, err := netip.ParseAddr(n.Gateway)
			if err != nil || !local.Contains(g) {
				errs = append(errs, fmt.Errorf("network of environment %s has gateway %q, which is not an address in %s; using %s", n.EnvironmentID, n.Gateway, local, gw))
			} else {
				gw = g
			}
		}
		er := envRules{ID: n.EnvironmentID, Local: local, Gateway: gw, Subnets: []netip.Prefix{local}}
		for _, ps := range n.PeerSubnets {
			p, err := parseEnvSubnet(plan, ps)
			if err != nil {
				errs = append(errs, fmt.Errorf("peer subnet of environment %s was skipped: %w", n.EnvironmentID, err))
				continue
			}
			er.Subnets = append(er.Subnets, p)
		}
		er.Subnets = sortPrefixes(er.Subnets)
		er.EgressBlocked = n.Egress == "blocked"
		if er.EgressBlocked {
			for _, a := range n.EgressAllow {
				ps, err := parseAllowEntry(ctx, a, resolve)
				if err != nil {
					errs = append(errs, fmt.Errorf("egress allowlist entry %q of environment %s was skipped: %w", a, n.EnvironmentID, err))
					continue
				}
				er.EgressAllow = append(er.EgressAllow, ps...)
			}
			er.EgressAllow = sortPrefixes(er.EgressAllow)
		}
		for _, s := range n.AllowMetadata {
			a, err := netip.ParseAddr(s)
			if err != nil || !local.Contains(a) {
				errs = append(errs, fmt.Errorf("metadata access for %q in environment %s was skipped: it is not a replica address in %s", s, n.EnvironmentID, local))
				continue
			}
			er.AllowMetadata = append(er.AllowMetadata, a)
		}
		slices.SortFunc(er.AllowMetadata, func(a, b netip.Addr) int { return a.Compare(b) })
		er.AllowMetadata = slices.Compact(er.AllowMetadata)
		spec.Envs = append(spec.Envs, er)
	}
	sort.Slice(spec.Envs, func(i, j int) bool { return spec.Envs[i].ID < spec.Envs[j].ID })
	return spec, errs
}

func parseEnvSubnet(plan AddressPlan, s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("subnet %q is not a valid CIDR", s)
	}
	if !p.Addr().Is4() || p.Bits() != 24 {
		return netip.Prefix{}, fmt.Errorf("subnet %s is not an IPv4 /24", p)
	}
	p = p.Masked()
	if !plan.Pod.Contains(p.Addr()) {
		return netip.Prefix{}, fmt.Errorf("subnet %s is outside the pod range %s", p, plan.Pod)
	}
	if p == plan.BuildSubnet() {
		return netip.Prefix{}, fmt.Errorf("subnet %s is the build network and cannot hold an environment", p)
	}
	return p, nil
}

func parseAllowEntry(ctx context.Context, s string, resolve hostResolver) ([]netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("it is empty")
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		if !p.Addr().Is4() {
			return nil, errors.New("only IPv4 ranges are supported in v0.1")
		}
		return []netip.Prefix{p.Masked()}, nil
	}
	if a, err := netip.ParseAddr(s); err == nil {
		if !a.Is4() {
			return nil, errors.New("only IPv4 addresses are supported in v0.1")
		}
		return []netip.Prefix{netip.PrefixFrom(a, 32)}, nil
	}
	if resolve == nil {
		return nil, errors.New("hostnames cannot be resolved here")
	}
	addrs, err := resolve(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("the name did not resolve: %w", err)
	}
	var out []netip.Prefix
	for _, a := range addrs {
		a = a.Unmap()
		if a.Is4() {
			out = append(out, netip.PrefixFrom(a, 32))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the name has no IPv4 address")
	}
	return out, nil
}

func sortPrefixes(ps []netip.Prefix) []netip.Prefix {
	slices.SortFunc(ps, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	return slices.Compact(ps)
}

func envChain(id string) string { return envChainPfx + shortHash(id, 10) }
func egrChain(id string) string { return egrChainPfx + shortHash(id, 10) }

func shortHash(s string, n int) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:n]
}

// ---------------------------------------------------------------- rendering

// table is one iptables table of the rendered rule set.
type table struct {
	name   string
	chains []string            // our chains, in declaration order
	rules  map[string][]string // chain -> rule specs without "-A <chain>"
}

func newTable(name string) *table { return &table{name: name, rules: map[string][]string{}} }

func (t *table) chain(name string) {
	if !slices.Contains(t.chains, name) {
		t.chains = append(t.chains, name)
	}
}

func (t *table) add(chain, spec string) {
	t.chain(chain)
	t.rules[chain] = append(t.rules[chain], spec)
}

// hook is a jump from a built-in (or Docker) chain into one of ours.
type hook struct {
	table, chain, spec string
}

func (h hook) line() string { return "-A " + h.chain + " " + h.spec }

// ruleSet is the full desired firewall: our chains and the hooks into them.
type ruleSet struct {
	tables []*table
	hooks  []hook
}

func cidr(p netip.Prefix) string { return p.String() }
func host32(a netip.Addr) string { return a.String() + "/32" }

// renderRules builds the desired rule set from a spec (docs/SPEC.md 8.4).
func renderRules(s firewallSpec) ruleSet {
	pod, mesh, build := cidr(s.Pod), cidr(s.Mesh), cidr(s.Build)
	meta := MetadataAddr + "/32"
	wire := WireServerAddr + "/32"

	f := newTable("filter")
	f.chain(chainIsolate)
	f.chain(chainEgress)
	f.chain(chainHostIn)

	// 5050-ISOLATE, reached from DOCKER-USER for every forwarded packet.
	f.add(chainIsolate, "-s "+pod+" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT")
	f.add(chainIsolate, "-d "+pod+" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT")
	for _, e := range s.Envs {
		for _, a := range e.AllowMetadata {
			f.add(chainIsolate, "-s "+host32(a)+" -d "+meta+" -j RETURN")
			f.add(chainIsolate, "-s "+host32(a)+" -d "+wire+" -j RETURN")
			f.add(chainIsolate, "-s "+host32(a)+" -d "+mesh+" -j RETURN")
		}
	}
	// Every forwarded container, also on bridges 5050cloud did not make.
	f.add(chainIsolate, "-d "+meta+" -j DROP")
	f.add(chainIsolate, "-d "+wire+" -p udp -m udp --dport 53 -j RETURN")
	f.add(chainIsolate, "-d "+wire+" -p tcp -m tcp --dport 53 -j RETURN")
	f.add(chainIsolate, "-d "+wire+" -j DROP")
	f.add(chainIsolate, "-s "+pod+" -d "+mesh+" -j DROP")
	// Hosts of the mesh (ingress, TCP proxy, health checks) reach replicas.
	f.add(chainIsolate, "-s "+mesh+" -d "+pod+" -i "+MeshInterface+" -j ACCEPT")
	// The build network reaches the internet and nothing of ours.
	f.add(chainIsolate, "-s "+build+" -d "+build+" -j ACCEPT")
	f.add(chainIsolate, "-s "+build+" -d "+pod+" -j DROP")
	f.add(chainIsolate, "-s "+build+" -j RETURN")
	for _, e := range s.Envs {
		ch := envChain(e.ID)
		f.chain(ch)
		for _, sub := range e.Subnets {
			f.add(chainIsolate, "-s "+cidr(sub)+" -j "+ch)
		}
		for _, sub := range e.Subnets {
			f.add(ch, "-d "+cidr(sub)+" -j ACCEPT")
		}
	}
	f.add(chainIsolate, "-s "+pod+" -d "+pod+" -j DROP")
	f.add(chainIsolate, "-s "+pod+" -j "+chainEgress)

	// 5050-EGRESS: quarantined environments reach only their allowlist.
	for _, e := range s.Envs {
		if !e.EgressBlocked {
			continue
		}
		ch := egrChain(e.ID)
		f.chain(ch)
		f.add(chainEgress, "-s "+cidr(e.Local)+" -j "+ch)
		for _, a := range e.EgressAllow {
			f.add(ch, "-d "+cidr(a)+" -j RETURN")
		}
		f.add(ch, "-j REJECT --reject-with icmp-admin-prohibited")
	}

	// 5050-HOSTIN, reached from INPUT for traffic from the pod range.
	f.add(chainHostIn, "-m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT")
	for _, e := range s.Envs {
		f.add(chainHostIn, "-s "+cidr(e.Local)+" -d "+host32(e.Gateway)+" -p udp -m udp --dport 53 -j ACCEPT")
		f.add(chainHostIn, "-s "+cidr(e.Local)+" -d "+host32(e.Gateway)+" -p tcp -m tcp --dport 53 -j ACCEPT")
	}
	f.add(chainHostIn, fmt.Sprintf("-s %s -d %s -p tcp -m tcp --dport %d -j ACCEPT", build, host32(s.BuildGateway), RegistryPort))
	f.add(chainHostIn, "-j DROP")

	m := newTable("mangle")
	m.add(chainMSS, "-o "+MeshInterface+" -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu")
	m.add(chainMSS, "-i "+MeshInterface+" -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu")

	n := newTable("nat")
	// ACCEPT ends the nat chain walk, so Docker's MASQUERADE never sees
	// pod-to-pod traffic and cross-host packets keep their source address.
	n.add(chainNAT, "-s "+pod+" -d "+pod+" -j ACCEPT")

	return ruleSet{
		tables: []*table{f, m, n},
		hooks: []hook{
			{"filter", "DOCKER-USER", "-j " + chainIsolate},
			{"filter", "INPUT", "-s " + pod + " -j " + chainHostIn},
			{"mangle", "FORWARD", "-j " + chainMSS},
			{"nat", "POSTROUTING", "-j " + chainNAT},
		},
	}
}

// savedState is the part of iptables-save output we care about.
type savedState struct {
	chains map[string]map[string]bool     // table -> chain -> exists
	rules  map[string]map[string][]string // table -> chain -> full "-A" lines
}

func parseSave(out string) savedState {
	st := savedState{chains: map[string]map[string]bool{}, rules: map[string]map[string][]string{}}
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "*"):
			cur = strings.TrimPrefix(line, "*")
			if st.chains[cur] == nil {
				st.chains[cur] = map[string]bool{}
				st.rules[cur] = map[string][]string{}
			}
		case cur == "":
		case strings.HasPrefix(line, ":"):
			if f := strings.Fields(line[1:]); len(f) > 0 {
				st.chains[cur][f[0]] = true
			}
		case strings.HasPrefix(line, "-A "):
			if f := strings.Fields(line); len(f) > 1 {
				st.rules[cur][f[1]] = append(st.rules[cur][f[1]], line)
			}
		}
	}
	return st
}

func (st savedState) has(tbl, chain string) bool { return st.chains[tbl][chain] }

// renderRestore turns the desired rule set into iptables-restore --noflush
// input against the current state: our chains are declared and flushed,
// stale chains of ours are deleted, wrong or duplicate hooks are removed and
// missing hooks are inserted at the top. Nothing outside our chains and
// hooks is touched.
func renderRestore(rs ruleSet, st savedState) string {
	var b strings.Builder
	for _, t := range rs.tables {
		fmt.Fprintf(&b, "*%s\n", t.name)
		var hooks []hook
		for _, h := range rs.hooks {
			if h.table == t.name {
				hooks = append(hooks, h)
			}
		}
		for _, h := range hooks {
			if !st.has(t.name, h.chain) && !isBuiltin(t.name, h.chain) {
				// DOCKER-USER before Docker has started (5050-net-restore).
				fmt.Fprintf(&b, ":%s - [0:0]\n", h.chain)
			}
		}
		for _, c := range t.chains {
			fmt.Fprintf(&b, ":%s - [0:0]\n", c)
		}
		// Jumps into our chains from chains that are not ours and are not
		// exactly one of our hooks (an old pod range, a duplicate) go first.
		kept := map[string]bool{}
		var others []string
		for c := range st.rules[t.name] {
			if !strings.HasPrefix(c, chainPrefix) {
				others = append(others, c)
			}
		}
		sort.Strings(others)
		for _, c := range others {
			for _, line := range st.rules[t.name][c] {
				if !jumpsToOurs(line) {
					continue
				}
				if hooksInclude(hooks, line) && !kept[line] {
					kept[line] = true
					continue
				}
				fmt.Fprintf(&b, "-D%s\n", strings.TrimPrefix(line, "-A"))
			}
		}
		var missing []hook
		for _, h := range hooks {
			if !kept[h.line()] {
				missing = append(missing, h)
			}
		}
		for _, c := range t.chains {
			fmt.Fprintf(&b, "-F %s\n", c)
		}
		var stale []string
		for c := range st.chains[t.name] {
			if strings.HasPrefix(c, chainPrefix) && !slices.Contains(t.chains, c) {
				stale = append(stale, c)
			}
		}
		sort.Strings(stale)
		for _, c := range stale {
			fmt.Fprintf(&b, "-F %s\n", c)
		}
		for _, c := range stale {
			fmt.Fprintf(&b, "-X %s\n", c)
		}
		for _, c := range t.chains {
			for _, r := range t.rules[c] {
				fmt.Fprintf(&b, "-A %s %s\n", c, r)
			}
		}
		for _, h := range missing {
			fmt.Fprintf(&b, "-I %s 1 %s\n", h.chain, h.spec)
		}
		b.WriteString("COMMIT\n")
	}
	return b.String()
}

func hooksInclude(hooks []hook, line string) bool {
	for _, h := range hooks {
		if h.line() == line {
			return true
		}
	}
	return false
}

func jumpsToOurs(line string) bool {
	f := strings.Fields(line)
	for i := 0; i+1 < len(f); i++ {
		if (f[i] == "-j" || f[i] == "-g") && strings.HasPrefix(f[i+1], chainPrefix) {
			return true
		}
	}
	return false
}

func isBuiltin(tbl, chain string) bool {
	switch tbl {
	case "filter":
		return chain == "INPUT" || chain == "FORWARD" || chain == "OUTPUT"
	case "nat":
		return chain == "PREROUTING" || chain == "INPUT" || chain == "OUTPUT" || chain == "POSTROUTING"
	case "mangle":
		return chain == "PREROUTING" || chain == "INPUT" || chain == "FORWARD" || chain == "OUTPUT" || chain == "POSTROUTING"
	}
	return false
}

// inSync reports whether the live rules already match the rule set: every
// hook present exactly once, our chains present with the expected number of
// rules and no stale chain of ours. iptables-save normalizes rule text, so
// counts are compared rather than text; the rendered text itself is compared
// by the caller through its hash.
func inSync(rs ruleSet, st savedState) bool {
	for _, t := range rs.tables {
		count := map[string]int{}
		for c, lines := range st.rules[t.name] {
			if strings.HasPrefix(c, chainPrefix) {
				continue
			}
			for _, line := range lines {
				if jumpsToOurs(line) {
					count[line]++
				}
			}
		}
		want := 0
		for _, h := range rs.hooks {
			if h.table != t.name {
				continue
			}
			want++
			if count[h.line()] != 1 {
				return false
			}
		}
		if len(count) != want {
			return false
		}
		for c := range st.chains[t.name] {
			if strings.HasPrefix(c, chainPrefix) && !slices.Contains(t.chains, c) {
				return false
			}
		}
		for _, c := range t.chains {
			if !st.has(t.name, c) || len(st.rules[t.name][c]) != len(t.rules[c]) {
				return false
			}
		}
	}
	return true
}
