// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// MeshKeepalive is the WireGuard persistent keepalive, so NAT mappings stay open.
const MeshKeepalive = 25 * time.Second

// meshDriver is the kernel side of the mesh: the wg5050 link, its WireGuard
// device and its routes. The Linux implementation uses netlink and wgctrl;
// tests use a fake.
type meshDriver interface {
	EnsureLink(name string, mtu int, addr netip.Prefix) error
	DeleteLink(name string) error
	Device(name string) (meshDevice, error)
	Configure(name string, cfg wgtypes.Config) error
	Routes(name string) ([]netip.Prefix, error)
	ReplaceRoute(name string, dst netip.Prefix, src netip.Addr) error
	DeleteRoute(name string, dst netip.Prefix) error
}

type meshDevice struct {
	PublicKey  wgtypes.Key
	ListenPort int
	Peers      []meshDevPeer
}

type meshDevPeer struct {
	PublicKey     wgtypes.Key
	Endpoint      *net.UDPAddr
	AllowedIPs    []netip.Prefix
	Keepalive     time.Duration
	LastHandshake time.Time
}

// keyStore keeps the host's WireGuard private key in <DataDir>/wg.key (0600).
// The key is generated once and never leaves the host.
type keyStore struct{ path string }

func (k keyStore) load() (wgtypes.Key, error) {
	b, err := os.ReadFile(k.path)
	if err == nil {
		key, perr := wgtypes.ParseKey(strings.TrimSpace(string(b)))
		if perr != nil {
			return wgtypes.Key{}, fmt.Errorf("the WireGuard key in %s is damaged (%w); move it away to make a new one, then rejoin this host", k.path, perr)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return wgtypes.Key{}, fmt.Errorf("reading the WireGuard key %s failed: %w", k.path, err)
	}
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("generating a WireGuard key failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return wgtypes.Key{}, fmt.Errorf("creating the data directory for the WireGuard key failed: %w", err)
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(key.String()+"\n"), 0o600); err != nil {
		return wgtypes.Key{}, fmt.Errorf("writing the WireGuard key %s failed: %w", k.path, err)
	}
	if err := os.Rename(tmp, k.path); err != nil {
		return wgtypes.Key{}, fmt.Errorf("writing the WireGuard key %s failed: %w", k.path, err)
	}
	return key, nil
}

// meshPlan is a validated proto.Mesh.
type meshPlan struct {
	Address netip.Prefix
	Port    int
	MTU     int
	Peers   []meshPeerPlan
	Routes  []netip.Prefix // sorted
}

type meshPeerPlan struct {
	HostID     string
	Key        wgtypes.Key
	Endpoint   *net.UDPAddr // nil when the peer dials us (behind NAT)
	AllowedIPs []netip.Prefix
}

// planMesh validates a proto.Mesh. Bad peers are skipped with an error; a bad
// self address fails the whole mesh.
func planMesh(plan AddressPlan, m proto.Mesh, defaultPort int, resolve func(string) (*net.UDPAddr, error)) (meshPlan, []error) {
	var errs []error
	addr, err := netip.ParsePrefix(m.Self.Address)
	if err != nil {
		if a, aerr := netip.ParseAddr(m.Self.Address); aerr == nil {
			addr, err = netip.PrefixFrom(a, 32), nil
		}
	}
	if err != nil || !addr.Addr().Is4() {
		return meshPlan{}, []error{fmt.Errorf("the mesh address %q of this host is not an IPv4 address, so the mesh was not set up", m.Self.Address)}
	}
	if !plan.Mesh.Contains(addr.Addr()) {
		return meshPlan{}, []error{fmt.Errorf("the mesh address %s of this host is outside the mesh range %s, so the mesh was not set up", addr.Addr(), plan.Mesh)}
	}
	mp := meshPlan{Address: netip.PrefixFrom(addr.Addr(), 32), Port: m.Self.ListenPort, MTU: m.MTU}
	if mp.Port == 0 {
		mp.Port = defaultPort
	}
	if mp.MTU == 0 {
		mp.MTU = DefaultMeshMTU
	}
	if mp.MTU < 1280 || mp.MTU > 9000 {
		errs = append(errs, fmt.Errorf("mesh MTU %d is out of range (1280 to 9000); using %d", mp.MTU, DefaultMeshMTU))
		mp.MTU = DefaultMeshMTU
	}
	seen := map[wgtypes.Key]bool{}
	for _, p := range m.Peers {
		key, err := wgtypes.ParseKey(p.PublicKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("mesh peer %s was skipped: its public key is not a WireGuard key", p.HostID))
			continue
		}
		if seen[key] {
			errs = append(errs, fmt.Errorf("mesh peer %s was skipped: another peer has the same public key", p.HostID))
			continue
		}
		pp := meshPeerPlan{HostID: p.HostID, Key: key}
		if p.Endpoint != "" {
			ep, err := resolve(p.Endpoint)
			if err != nil {
				errs = append(errs, fmt.Errorf("the endpoint %q of mesh peer %s did not resolve (%w); waiting for that peer to reach this host instead", p.Endpoint, p.HostID, err))
			} else {
				pp.Endpoint = ep
			}
		}
		for _, s := range p.AllowedIPs {
			ap, err := netip.ParsePrefix(s)
			if err != nil || !ap.Addr().Is4() {
				errs = append(errs, fmt.Errorf("allowed range %q of mesh peer %s was skipped: it is not an IPv4 CIDR", s, p.HostID))
				continue
			}
			ap = ap.Masked()
			if ap.Overlaps(mp.Address) {
				errs = append(errs, fmt.Errorf("allowed range %s of mesh peer %s was skipped: it covers this host's own mesh address", ap, p.HostID))
				continue
			}
			pp.AllowedIPs = append(pp.AllowedIPs, ap)
		}
		pp.AllowedIPs = sortPrefixes(pp.AllowedIPs)
		seen[key] = true
		mp.Peers = append(mp.Peers, pp)
		mp.Routes = append(mp.Routes, pp.AllowedIPs...)
	}
	slices.SortFunc(mp.Peers, func(a, b meshPeerPlan) int { return strings.Compare(a.Key.String(), b.Key.String()) })
	mp.Routes = sortPrefixes(mp.Routes)
	return mp, errs
}

// applyMesh brings wg5050 to the plan with the fewest changes: only peers
// that differ are reconfigured (never ReplacePeers, which would reset every
// session), endpoints learned from NATed peers are kept, and only missing or
// extra routes are changed.
func applyMesh(d meshDriver, key wgtypes.Key, mp meshPlan) error {
	if err := d.EnsureLink(MeshInterface, mp.MTU, mp.Address); err != nil {
		return err
	}
	dev, err := d.Device(MeshInterface)
	if err != nil {
		return err
	}
	cfg := wgtypes.Config{}
	changed := false
	if dev.PublicKey != key.PublicKey() {
		cfg.PrivateKey = &key
		changed = true
	}
	if dev.ListenPort != mp.Port {
		port := mp.Port
		cfg.ListenPort = &port
		changed = true
	}
	have := map[wgtypes.Key]meshDevPeer{}
	for _, p := range dev.Peers {
		have[p.PublicKey] = p
	}
	want := map[wgtypes.Key]bool{}
	for _, p := range mp.Peers {
		want[p.Key] = true
		cur, ok := have[p.Key]
		if ok && slices.Equal(sortPrefixes(slices.Clone(cur.AllowedIPs)), p.AllowedIPs) &&
			cur.Keepalive == MeshKeepalive && (p.Endpoint == nil || udpEqual(cur.Endpoint, p.Endpoint)) {
			continue
		}
		ka := MeshKeepalive
		pc := wgtypes.PeerConfig{
			PublicKey:                   p.Key,
			Endpoint:                    p.Endpoint,
			ReplaceAllowedIPs:           true,
			PersistentKeepaliveInterval: &ka,
		}
		for _, a := range p.AllowedIPs {
			pc.AllowedIPs = append(pc.AllowedIPs, prefixToIPNet(a))
		}
		cfg.Peers = append(cfg.Peers, pc)
		changed = true
	}
	for k := range have {
		if !want[k] {
			cfg.Peers = append(cfg.Peers, wgtypes.PeerConfig{PublicKey: k, Remove: true})
			changed = true
		}
	}
	if changed {
		if err := d.Configure(MeshInterface, cfg); err != nil {
			return fmt.Errorf("configuring WireGuard on %s failed: %w", MeshInterface, err)
		}
	}
	cur, err := d.Routes(MeshInterface)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range cur {
		if !slices.Contains(mp.Routes, r) && r != mp.Address {
			if err := d.DeleteRoute(MeshInterface, r); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, r := range mp.Routes {
		if !slices.Contains(cur, r) {
			if err := d.ReplaceRoute(MeshInterface, r, mp.Address.Addr()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func udpEqual(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.IP.Equal(b.IP) && a.Port == b.Port
}

func prefixToIPNet(p netip.Prefix) net.IPNet {
	return net.IPNet{IP: net.IP(p.Addr().AsSlice()), Mask: net.CIDRMask(p.Bits(), p.Addr().BitLen())}
}

func ipNetToPrefix(n net.IPNet) (netip.Prefix, bool) {
	a, ok := netip.AddrFromSlice(n.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	ones, _ := n.Mask.Size()
	return netip.PrefixFrom(a.Unmap(), ones).Masked(), true
}

func resolveUDP(s string) (*net.UDPAddr, error) {
	return net.ResolveUDPAddr("udp4", s)
}
