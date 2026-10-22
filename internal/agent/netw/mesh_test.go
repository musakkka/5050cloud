// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func mustKey(t *testing.T) wgtypes.Key {
	t.Helper()
	k, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func testMesh(t *testing.T) (proto.Mesh, wgtypes.Key, wgtypes.Key) {
	p2, p3 := mustKey(t), mustKey(t)
	return proto.Mesh{
		Enabled: true,
		Self:    proto.MeshSelf{Index: 1, Address: "100.96.0.1/32", ListenPort: 51820, HostSubnet: "10.129.0.0/16"},
		Peers: []proto.MeshPeer{
			{HostID: "host_2", PublicKey: p2.PublicKey().String(), Endpoint: "198.51.100.2:51820", AllowedIPs: []string{"100.96.0.2/32", "10.130.0.0/16"}},
			{HostID: "host_3", PublicKey: p3.PublicKey().String(), AllowedIPs: []string{"100.96.0.3/32", "10.131.0.0/16"}}, // behind NAT
		},
	}, p2, p3
}

func TestKeyStore(t *testing.T) {
	dir := t.TempDir()
	ks := keyStore{path: filepath.Join(dir, "sub", "wg.key")}
	k1, err := ks.load()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(ks.path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", fi.Mode().Perm())
	}
	k2, err := ks.load()
	if err != nil || k1 != k2 {
		t.Fatalf("key changed between loads: %v", err)
	}
	if err := os.WriteFile(ks.path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ks.load(); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("damaged key err = %v", err)
	}
}

func TestPlanMesh(t *testing.T) {
	plan, _ := NewAddressPlan("", "")
	m, _, _ := testMesh(t)
	m.Peers = append(m.Peers,
		proto.MeshPeer{HostID: "bad_key", PublicKey: "nope"},
		proto.MeshPeer{HostID: "dup", PublicKey: m.Peers[0].PublicKey},
		proto.MeshPeer{HostID: "host_4", PublicKey: mustKey(t).PublicKey().String(), Endpoint: "unresolvable:1",
			AllowedIPs: []string{"100.96.0.1/32", "fd00::/8", "x", "10.132.0.0/16"}},
	)
	mp, errs := planMesh(plan, m, DefaultMeshPort, func(s string) (*net.UDPAddr, error) {
		if strings.HasPrefix(s, "unresolvable") {
			return nil, &net.DNSError{Err: "no such host", Name: s}
		}
		return net.ResolveUDPAddr("udp4", s)
	})
	if len(errs) != 6 {
		for _, e := range errs {
			t.Log(e)
		}
		t.Fatalf("errors = %d, want 6", len(errs))
	}
	if mp.Address.String() != "100.96.0.1/32" || mp.MTU != DefaultMeshMTU || mp.Port != 51820 {
		t.Errorf("plan = %+v", mp)
	}
	if len(mp.Peers) != 3 {
		t.Fatalf("peers = %d, want 3", len(mp.Peers))
	}
	want := []string{"10.130.0.0/16", "10.131.0.0/16", "10.132.0.0/16", "100.96.0.2/32", "100.96.0.3/32"}
	var got []string
	for _, r := range mp.Routes {
		got = append(got, r.String())
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("routes = %v, want %v", got, want)
	}

	for _, bad := range []string{"", "nonsense", "10.129.0.1/32", "fd00::1"} {
		m.Self.Address = bad
		if mp, errs := planMesh(plan, m, 1, resolveUDP); mp.Address.IsValid() || len(errs) != 1 {
			t.Errorf("self address %q accepted", bad)
		}
	}
	m.Self.Address = "100.96.0.9"
	m.MTU = 100
	mp, errs = planMesh(plan, proto.Mesh{Self: m.Self, MTU: 100}, 51999, resolveUDP)
	if mp.Address.String() != "100.96.0.9/32" || mp.MTU != DefaultMeshMTU || mp.Port != 51820 || len(errs) != 1 {
		t.Errorf("plan = %+v, errs = %v", mp, errs)
	}
}

func TestApplyMeshMinimalChanges(t *testing.T) {
	plan, _ := NewAddressPlan("", "")
	key := mustKey(t)
	m, p2, p3 := testMesh(t)
	mp, errs := planMesh(plan, m, DefaultMeshPort, resolveUDP)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	d := &fakeMesh{}
	if err := applyMesh(d, key, mp); err != nil {
		t.Fatal(err)
	}
	if !d.link || d.mtu != 1420 || d.addr.String() != "100.96.0.1/32" {
		t.Errorf("link = %v mtu %d addr %s", d.link, d.mtu, d.addr)
	}
	if len(d.configures) != 1 || d.dev.PublicKey != key.PublicKey() || d.dev.ListenPort != 51820 || len(d.dev.Peers) != 2 {
		t.Fatalf("first configure wrong: %d configures, dev %+v", len(d.configures), d.dev)
	}
	for _, p := range d.dev.Peers {
		if p.Keepalive != MeshKeepalive {
			t.Errorf("peer keepalive = %v", p.Keepalive)
		}
	}
	if len(d.routes) != 4 {
		t.Errorf("routes = %v", d.routes)
	}

	// Same plan again: no configure, no route change.
	ops := len(d.routeOps)
	if err := applyMesh(d, key, mp); err != nil {
		t.Fatal(err)
	}
	if len(d.configures) != 1 || len(d.routeOps) != ops {
		t.Errorf("unchanged plan reconfigured: %d configures, %d route ops", len(d.configures), len(d.routeOps)-ops)
	}

	// The NATed peer roamed: its learned endpoint is kept, not reset.
	learned := &net.UDPAddr{IP: net.ParseIP("203.0.113.9"), Port: 40000}
	for i := range d.dev.Peers {
		if d.dev.Peers[i].PublicKey == p3.PublicKey() {
			d.dev.Peers[i].Endpoint = learned
		}
	}
	if err := applyMesh(d, key, mp); err != nil {
		t.Fatal(err)
	}
	if len(d.configures) != 1 {
		t.Error("a learned endpoint of a NATed peer caused a reconfigure")
	}

	// Remove host_3, change host_2's ranges.
	m.Peers = m.Peers[:1]
	m.Peers[0].AllowedIPs = []string{"100.96.0.2/32", "10.130.0.0/16", "10.200.0.0/16"}
	mp2, _ := planMesh(plan, m, DefaultMeshPort, resolveUDP)
	if err := applyMesh(d, key, mp2); err != nil {
		t.Fatal(err)
	}
	last := d.configures[len(d.configures)-1]
	if last.PrivateKey != nil || last.ListenPort != nil || last.ReplacePeers {
		t.Errorf("only peers should change: %+v", last)
	}
	removed, updated := 0, 0
	for _, pc := range last.Peers {
		if pc.Remove && pc.PublicKey == p3.PublicKey() {
			removed++
		}
		if !pc.Remove && pc.PublicKey == p2.PublicKey() && pc.ReplaceAllowedIPs && len(pc.AllowedIPs) == 3 {
			updated++
		}
	}
	if removed != 1 || updated != 1 {
		t.Errorf("peer changes = %+v", last.Peers)
	}
	for _, r := range []string{"10.131.0.0/16", "100.96.0.3/32"} {
		if slicesContainsPrefix(d.routes, r) {
			t.Errorf("route %s left behind", r)
		}
	}
	if !slicesContainsPrefix(d.routes, "10.200.0.0/16") {
		t.Error("new route missing")
	}
}

func slicesContainsPrefix(ps []netip.Prefix, s string) bool {
	for _, p := range ps {
		if p.String() == s {
			return true
		}
	}
	return false
}

func TestNetMeshLifecycle(t *testing.T) {
	n, _, _, fm := newTestNet(t)
	ctx := context.Background()
	m, p2, _ := testMesh(t)
	if err := n.Apply(ctx, proto.HostConfig{}, m, nil, nil); err != nil {
		t.Fatal(err)
	}
	pub, err := n.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if fm.dev.PublicKey.String() != pub {
		t.Error("the device does not use this host's key")
	}
	// Handshakes: host_2 talked 10 s ago, host_3 never did.
	now := time.Now()
	for i := range fm.dev.Peers {
		if fm.dev.Peers[i].PublicKey == p2.PublicKey() {
			fm.dev.Peers[i].LastHandshake = now.Add(-10 * time.Second)
		}
	}
	hs := n.MeshHandshakes()
	if hs["host_2"] == 0 || hs["host_3"] != 0 || len(hs) != 2 {
		t.Errorf("handshakes = %v", hs)
	}
	if got := n.UnreachablePeers(now, 3*time.Minute); len(got) != 1 || got[0] != "host_3" {
		t.Errorf("unreachable = %v", got)
	}
	// Back to one host: the interface goes away.
	if err := n.Apply(ctx, proto.HostConfig{}, proto.Mesh{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if fm.link || fm.deleted != 1 {
		t.Error("wg5050 not removed when the mesh was disabled")
	}
	if hs := n.MeshHandshakes(); hs != nil {
		t.Errorf("handshakes after disable = %v", hs)
	}
}

func TestIPNetConversions(t *testing.T) {
	p := netip.MustParsePrefix("10.130.0.0/16")
	n := prefixToIPNet(p)
	back, ok := ipNetToPrefix(n)
	if !ok || back != p {
		t.Errorf("round trip = %v %v", back, ok)
	}
	if _, ok := ipNetToPrefix(net.IPNet{}); ok {
		t.Error("empty IPNet converted")
	}
	if !udpEqual(nil, nil) || udpEqual(nil, &net.UDPAddr{}) {
		t.Error("udpEqual nil handling")
	}
}
