// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package netw

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// linuxMesh drives the kernel WireGuard module through netlink. Each driver
// is bound to the network namespace it was created in.
type linuxMesh struct {
	mu sync.Mutex
	nl *netlink.Handle
	wg *wgctrl.Client
}

func newMeshDriver() (meshDriver, error) {
	nl, err := netlink.NewHandle()
	if err != nil {
		return nil, fmt.Errorf("opening netlink failed: %w", err)
	}
	wg, err := wgctrl.New()
	if err != nil {
		nl.Close()
		return nil, fmt.Errorf("opening the WireGuard control socket failed: %w", err)
	}
	return &linuxMesh{nl: nl, wg: wg}, nil
}

// newMeshDriverAt creates a driver whose sockets live in namespace ns (tests).
func newMeshDriverAt(ns netns.NsHandle) (meshDriver, error) {
	var d meshDriver
	err := inNetns(ns, func() error {
		var err error
		d, err = newMeshDriver()
		return err
	})
	return d, err
}

// inNetns runs fn on a locked OS thread switched into ns. Sockets created by
// fn stay in ns after the thread switches back.
func inNetns(ns netns.NsHandle, fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	orig, err := netns.Get()
	if err != nil {
		return err
	}
	defer func() { _ = orig.Close() }()
	if err := netns.Set(ns); err != nil {
		return err
	}
	defer netns.Set(orig) //nolint:errcheck // best effort; the thread is locked and dropped on failure
	return fn()
}

func (m *linuxMesh) EnsureLink(name string, mtu int, addr netip.Prefix) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, err := m.nl.LinkByName(name)
	if err != nil {
		var nf netlink.LinkNotFoundError
		if !errors.As(err, &nf) {
			return fmt.Errorf("looking up %s failed: %w", name, err)
		}
		la := netlink.NewLinkAttrs()
		la.Name = name
		la.MTU = mtu
		if err := m.nl.LinkAdd(&netlink.Wireguard{LinkAttrs: la}); err != nil {
			if errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("this kernel has no WireGuard support, so %s cannot be created; use a kernel of 5.6 or newer (or install the wireguard module)", name)
			}
			return fmt.Errorf("creating %s failed: %w", name, err)
		}
		if link, err = m.nl.LinkByName(name); err != nil {
			return fmt.Errorf("looking up %s after creating it failed: %w", name, err)
		}
	}
	if link.Type() != "wireguard" {
		return fmt.Errorf("an interface named %s already exists and is not a WireGuard interface; remove it and the agent will create its own", name)
	}
	if link.Attrs().MTU != mtu {
		if err := m.nl.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("setting the MTU of %s to %d failed: %w", name, mtu, err)
		}
	}
	addrs, err := m.nl.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("listing the addresses of %s failed: %w", name, err)
	}
	want := prefixToIPNet(addr)
	found := false
	for _, a := range addrs {
		if a.IPNet != nil && a.IPNet.String() == want.String() {
			found = true
			continue
		}
		if err := m.nl.AddrDel(link, &a); err != nil {
			return fmt.Errorf("removing the old address %s from %s failed: %w", a.IPNet, name, err)
		}
	}
	if !found {
		if err := m.nl.AddrReplace(link, &netlink.Addr{IPNet: &want}); err != nil {
			return fmt.Errorf("adding the mesh address %s to %s failed: %w", addr, name, err)
		}
	}
	if link.Attrs().Flags&net.FlagUp == 0 {
		if err := m.nl.LinkSetUp(link); err != nil {
			return fmt.Errorf("bringing %s up failed: %w", name, err)
		}
	}
	return nil
}

func (m *linuxMesh) DeleteLink(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, err := m.nl.LinkByName(name)
	if err != nil {
		var nf netlink.LinkNotFoundError
		if errors.As(err, &nf) {
			return nil
		}
		return fmt.Errorf("looking up %s failed: %w", name, err)
	}
	if link.Type() != "wireguard" {
		return nil
	}
	if err := m.nl.LinkDel(link); err != nil {
		return fmt.Errorf("removing %s failed: %w", name, err)
	}
	return nil
}

func (m *linuxMesh) Device(name string) (meshDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.wg.Device(name)
	if err != nil {
		return meshDevice{}, fmt.Errorf("reading the WireGuard device %s failed: %w", name, err)
	}
	out := meshDevice{PublicKey: d.PublicKey, ListenPort: d.ListenPort}
	for _, p := range d.Peers {
		dp := meshDevPeer{PublicKey: p.PublicKey, Endpoint: p.Endpoint, Keepalive: p.PersistentKeepaliveInterval, LastHandshake: p.LastHandshakeTime}
		for _, a := range p.AllowedIPs {
			if pf, ok := ipNetToPrefix(a); ok {
				dp.AllowedIPs = append(dp.AllowedIPs, pf)
			}
		}
		out.Peers = append(out.Peers, dp)
	}
	return out, nil
}

func (m *linuxMesh) Configure(name string, cfg wgtypes.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.wg.ConfigureDevice(name, cfg)
}

func (m *linuxMesh) Routes(name string) ([]netip.Prefix, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, err := m.nl.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("looking up %s failed: %w", name, err)
	}
	rs, err := m.nl.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("listing the routes of %s failed: %w", name, err)
	}
	var out []netip.Prefix
	for _, r := range rs {
		if r.Dst == nil || r.Table != syscall.RT_TABLE_MAIN && r.Table != 0 {
			continue
		}
		if r.Protocol == syscall.RTPROT_KERNEL {
			continue // the link's own address route
		}
		if p, ok := ipNetToPrefix(*r.Dst); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *linuxMesh) ReplaceRoute(name string, dst netip.Prefix, src netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, err := m.nl.LinkByName(name)
	if err != nil {
		return fmt.Errorf("looking up %s failed: %w", name, err)
	}
	d := prefixToIPNet(dst)
	r := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: &d, Src: net.IP(src.AsSlice()), Scope: netlink.SCOPE_LINK}
	if err := m.nl.RouteReplace(r); err != nil {
		return fmt.Errorf("adding the route %s via %s failed: %w", dst, name, err)
	}
	return nil
}

func (m *linuxMesh) DeleteRoute(name string, dst netip.Prefix) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	link, err := m.nl.LinkByName(name)
	if err != nil {
		return fmt.Errorf("looking up %s failed: %w", name, err)
	}
	d := prefixToIPNet(dst)
	if err := m.nl.RouteDel(&netlink.Route{LinkIndex: link.Attrs().Index, Dst: &d}); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("removing the route %s via %s failed: %w", dst, name, err)
	}
	return nil
}
