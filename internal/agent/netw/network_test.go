// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/miekg/dns"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestNetApplyDockerAndDNS(t *testing.T) {
	n, _, d, _ := newTestNet(t)
	ctx := context.Background()
	nets := testNets()
	if err := n.Apply(ctx, proto.HostConfig{}, proto.Mesh{}, nets, testEntries()); err != nil {
		t.Fatal(err)
	}
	prod, ok := d.nets["5050-env_prod"]
	if !ok {
		t.Fatalf("networks = %v", d.creates)
	}
	if prod.IPAM.Config[0].Subnet != "10.129.4.0/24" || prod.IPAM.Config[0].Gateway != "10.129.4.1" ||
		prod.Options[optBridgeName] != bridgeName("env_prod") || prod.Options[optMTU] != "1420" ||
		prod.Options[optICC] != "true" || prod.Labels[labelEnvironment] != "env_prod" {
		t.Errorf("prod network = %+v", prod)
	}
	if len(bridgeName("env_prod")) > 15 {
		t.Error("bridge name longer than the kernel allows")
	}
	if _, ok := d.nets["5050-env_quarantine"]; !ok {
		t.Error("a network without a name did not get the default name")
	}
	if got := n.DNSServer("env_prod"); got != "10.129.4.1" {
		t.Errorf("DNSServer = %q", got)
	}
	if got := n.DNSServer("nope"); got != "" {
		t.Errorf("DNSServer(unknown) = %q", got)
	}
	ls := n.dns.sm.(*loopSockets)
	if got := aRecords(query(t, "udp", ls.addr("10.129.4.1:53"), "api.internal", dns.TypeA)); len(got) != 0 {
		// env_prod has no names in testEntries (they belong to env_a)
		t.Errorf("env_prod answers = %v", got)
	}

	// Same state: nothing is created again.
	creates := len(d.creates)
	if err := n.Apply(ctx, proto.HostConfig{}, proto.Mesh{}, nets, nil); err != nil {
		t.Fatal(err)
	}
	if len(d.creates) != creates {
		t.Errorf("an unchanged state recreated networks: %v", d.creates[creates:])
	}

	// MTU change on an empty network: recreated. On a busy one: reported and kept.
	nets[0].MTU = 1380
	d.mu.Lock()
	q := d.nets["5050-env_quarantine"]
	q.Containers = map[string]network.EndpointResource{"c1": {}}
	d.nets["5050-env_quarantine"] = q
	d.mu.Unlock()
	nets[1].MTU = 1380
	err := n.Apply(ctx, proto.HostConfig{}, proto.Mesh{}, nets, nil)
	if err == nil || !strings.Contains(err.Error(), "still has 1 containers attached") {
		t.Fatalf("err = %v", err)
	}
	if d.nets["5050-env_prod"].Options[optMTU] != "1380" {
		t.Error("empty network was not recreated with the new MTU")
	}
	if d.nets["5050-env_quarantine"].Options[optMTU] == "1380" {
		t.Error("a network with containers was recreated")
	}

	// The environment leaves this host: its empty network is removed, the busy one stays.
	if err := n.Apply(ctx, proto.HostConfig{}, proto.Mesh{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.nets["5050-env_prod"]; ok {
		t.Error("stale empty network kept")
	}
	if _, ok := d.nets["5050-env_quarantine"]; !ok {
		t.Error("stale network with containers removed")
	}
}

func TestNetApplyReportsDockerErrors(t *testing.T) {
	n, _, d, _ := newTestNet(t)
	d.createErr = errors.New("Pool overlaps with other one on this address space")
	err := n.Apply(context.Background(), proto.HostConfig{}, proto.Mesh{}, testNets(), nil)
	if err == nil || !strings.Contains(err.Error(), "creating Docker network 5050-env_prod with subnet 10.129.4.0/24 failed") {
		t.Fatalf("err = %v", err)
	}
	n.newDock = func() (dockerAPI, error) { return nil, errors.New("connecting to Docker failed: no socket") }
	n.dock = nil
	if err := n.Apply(context.Background(), proto.HostConfig{}, proto.Mesh{}, testNets(), nil); err == nil {
		t.Fatal("docker failure not reported")
	}
}

func TestRestoreOnlySkipsDockerAndDNS(t *testing.T) {
	n, ipt, d, fm := newTestNet(t)
	n.o.RestoreOnly = true
	m, _, _ := testMesh(t)
	if err := n.Apply(context.Background(), proto.HostConfig{}, m, testNets(), testEntries()); err != nil {
		t.Fatal(err)
	}
	if len(ipt.restores) != 1 || !fm.link {
		t.Error("restore did not apply the firewall and the mesh")
	}
	if len(d.creates) != 0 {
		t.Error("restore touched Docker")
	}
	n.dns.mu.RLock()
	servers := len(n.dns.servers)
	n.dns.mu.RUnlock()
	if servers != 0 {
		t.Error("restore started DNS servers")
	}
}

func TestRestoreEntryPoint(t *testing.T) {
	// Without iptables on the test machine Restore must fail with a sentence,
	// not panic, and must not need Docker.
	err := Restore(context.Background(), Options{DataDir: t.TempDir()}, proto.DesiredState{Networks: testNets()})
	if err == nil {
		t.Skip("iptables is available here; the integration test covers Restore")
	}
	if !strings.Contains(err.Error(), "iptables") && !strings.Contains(err.Error(), "firewall") {
		t.Errorf("err = %v", err)
	}
}

func TestAllowEgress(t *testing.T) {
	n, ipt, _, _ := newTestNet(t)
	ctx := context.Background()
	nets := testNets()
	if err := n.AllowEgress(ctx, "nonsense", nets[0]); err == nil {
		t.Error("bad IP accepted")
	}
	if err := n.AllowEgress(ctx, "10.9.9.9", nets[0]); err == nil || !strings.Contains(err.Error(), "not inside") {
		t.Errorf("IP outside subnet: %v", err)
	}
	// Before any Apply: the network is added and rules go in at once.
	if err := n.AllowEgress(ctx, "10.129.5.7", nets[1]); err != nil {
		t.Fatal(err)
	}
	if len(ipt.restores) != 1 || !strings.Contains(ipt.restores[0], egrChain("env_quarantine")) {
		t.Fatalf("quarantine rules not applied before the container starts: %v", ipt.restores)
	}
	if got := n.DNSServer("env_quarantine"); got != "10.129.5.1" {
		t.Errorf("DNSServer = %q", got)
	}
	// Already covered: no new restore.
	if err := n.AllowEgress(ctx, "10.129.5.8", nets[1]); err != nil {
		t.Fatal(err)
	}
	if len(ipt.restores) != 1 {
		t.Errorf("restores = %d, want 1", len(ipt.restores))
	}
	// A replica that gets metadata access: the rule set changes.
	nets[1].AllowMetadata = []string{"10.129.5.9"}
	if err := n.AllowEgress(ctx, "10.129.5.9", nets[1]); err != nil {
		t.Fatal(err)
	}
	if len(ipt.restores) != 2 || !strings.Contains(ipt.restores[1], "-s 10.129.5.9/32 -d 169.254.169.254/32 -j RETURN") {
		t.Error("metadata opt-in not applied")
	}
	ipt.failNext = errors.New("boom")
	nets[1].EgressAllow = []string{"203.0.113.9"}
	if err := n.AllowEgress(ctx, "10.129.5.9", nets[1]); err == nil {
		t.Error("restore failure not reported")
	}
}

func TestBuildNetwork(t *testing.T) {
	n, ipt, d, _ := newTestNet(t)
	name, err := n.BuildNetwork(context.Background())
	if err != nil || name != BuildNetworkName {
		t.Fatalf("BuildNetwork = %q, %v", name, err)
	}
	b := d.nets[BuildNetworkName]
	if b.IPAM.Config[0].Subnet != "10.128.255.0/24" || b.Options[optBridgeName] != BuildBridgeName || b.Labels[labelNetwork] != "build" {
		t.Errorf("build network = %+v", b)
	}
	if len(ipt.restores) != 1 || !strings.Contains(ipt.restores[0], "-s 10.128.255.0/24 -d 10.128.0.0/9 -j DROP") {
		t.Error("build firewall rules missing")
	}
	// Applying a state without it keeps the build network (it is not an environment).
	if err := n.Apply(context.Background(), proto.HostConfig{}, proto.Mesh{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.nets[BuildNetworkName]; !ok {
		t.Error("build network removed by an apply")
	}
	if _, err := n.BuildNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPublicKeyStable(t *testing.T) {
	n, _, _, _ := newTestNet(t)
	a, err := n.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := n.PublicKey()
	if a != b || len(a) != 44 {
		t.Errorf("keys %q %q", a, b)
	}
}

func TestNewReturnsNetwork(t *testing.T) {
	nw := New(Options{DataDir: t.TempDir()})
	if nw.DNSServer("x") != "" {
		t.Error("fresh network knows an environment")
	}
	_ = nw.(*Net).Close()
}
