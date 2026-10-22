// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"fmt"
	"net/netip"
)

// Address plan defaults and limits (docs/SPEC.md 8.4).
const (
	// DefaultPodCIDR holds every container address of the install.
	DefaultPodCIDR = "10.128.0.0/9"
	// DefaultMeshCIDR holds the WireGuard tunnel addresses (100.96.0.k/32).
	DefaultMeshCIDR = "100.96.0.0/16"
	// MetadataAddr is the cloud metadata endpoint containers may not reach by default.
	MetadataAddr = "169.254.169.254"
	// MaxHosts is the number of /16 host ranges a /9 pod range holds (index 0 is reserved).
	MaxHosts = 127
	// MaxEnvSlots is the number of environment /24s one host can carry; slot 255 is the build network.
	MaxEnvSlots = 254
	// BuildSlot is the /24 of host range 0 used by every host's build network.
	BuildSlot = 255
	// MeshInterface is the WireGuard interface name.
	MeshInterface = "wg5050"
	// DefaultMeshPort is the WireGuard UDP port.
	DefaultMeshPort = 51820
	// DefaultMeshMTU is the mesh and bridge MTU when the install has two or more hosts.
	DefaultMeshMTU = 1420
	// BuildNetworkName is the Docker bridge BuildKit runs on.
	BuildNetworkName = "5050-build"
	// BuildBridgeName is the Linux interface name of the build bridge.
	BuildBridgeName = "f5-build"
	// RegistryPort is the internal registry port the build network may reach on its gateway.
	RegistryPort = 5000
)

// AddressPlan turns host and slot indexes into subnets. The zero value is
// not usable; build one with NewAddressPlan.
type AddressPlan struct {
	Pod  netip.Prefix
	Mesh netip.Prefix
}

// NewAddressPlan parses the pod and mesh ranges, using the defaults for empty
// strings. The pod range must be IPv4 and at least a /9, so 127 hosts of a
// /16 each fit; the mesh range must be IPv4 and at least a /24.
func NewAddressPlan(podCIDR, meshCIDR string) (AddressPlan, error) {
	if podCIDR == "" {
		podCIDR = DefaultPodCIDR
	}
	if meshCIDR == "" {
		meshCIDR = DefaultMeshCIDR
	}
	pod, err := netip.ParsePrefix(podCIDR)
	if err != nil {
		return AddressPlan{}, fmt.Errorf("the pod range %q is not a valid CIDR: %w", podCIDR, err)
	}
	mesh, err := netip.ParsePrefix(meshCIDR)
	if err != nil {
		return AddressPlan{}, fmt.Errorf("the mesh range %q is not a valid CIDR: %w", meshCIDR, err)
	}
	if !pod.Addr().Is4() || pod.Bits() > 9 {
		return AddressPlan{}, fmt.Errorf("the pod range %s must be an IPv4 range of /9 or larger, so 127 hosts of a /16 each fit", pod)
	}
	if !mesh.Addr().Is4() || mesh.Bits() > 24 {
		return AddressPlan{}, fmt.Errorf("the mesh range %s must be an IPv4 range of /24 or larger", mesh)
	}
	if pod.Overlaps(mesh) {
		return AddressPlan{}, fmt.Errorf("the pod range %s and the mesh range %s overlap; pick ranges that do not", pod, mesh)
	}
	return AddressPlan{Pod: pod.Masked(), Mesh: mesh.Masked()}, nil
}

// HostSubnet returns the /16 container range of host k (1..127).
func (p AddressPlan) HostSubnet(k int) (netip.Prefix, error) {
	if k < 1 || k > MaxHosts {
		return netip.Prefix{}, fmt.Errorf("host index %d is out of range: it must be between 1 and %d", k, MaxHosts)
	}
	return netip.PrefixFrom(addOffset(p.Pod.Addr(), uint32(k)<<16), 16), nil
}

// EnvSubnet returns the /24 of environment slot n (1..254) on host k.
func (p AddressPlan) EnvSubnet(k, slot int) (netip.Prefix, error) {
	host, err := p.HostSubnet(k)
	if err != nil {
		return netip.Prefix{}, err
	}
	if slot < 1 || slot > MaxEnvSlots {
		return netip.Prefix{}, fmt.Errorf("network slot %d is out of range: it must be between 1 and %d", slot, MaxEnvSlots)
	}
	return netip.PrefixFrom(addOffset(host.Addr(), uint32(slot)<<8), 24), nil
}

// BuildSubnet returns the build network, slot 255 of the reserved host range 0
// (10.128.255.0/24 by default). It is the same on every host and never routed
// over the mesh.
func (p AddressPlan) BuildSubnet() netip.Prefix {
	return netip.PrefixFrom(addOffset(p.Pod.Addr(), uint32(BuildSlot)<<8), 24)
}

// MeshAddress returns the tunnel address of host k as a /32.
func (p AddressPlan) MeshAddress(k int) (netip.Prefix, error) {
	if k < 1 || k > MaxHosts {
		return netip.Prefix{}, fmt.Errorf("host index %d is out of range: it must be between 1 and %d", k, MaxHosts)
	}
	return netip.PrefixFrom(addOffset(p.Mesh.Addr(), uint32(k)), 32), nil
}

// Gateway returns the first address of a subnet (".1"), where the bridge
// and the agent's DNS server listen.
func Gateway(subnet netip.Prefix) netip.Addr {
	return addOffset(subnet.Masked().Addr(), 1)
}

// SlotExhaustedError is the placement error of docs/SPEC.md 9.18 for a host
// that already carries MaxEnvSlots environments.
func SlotExhaustedError(host string) error {
	return fmt.Errorf("No network slot left on host %s: it already runs %d environments.", host, MaxEnvSlots) //nolint:staticcheck // the sentence is user-facing copy from the spec
}

func addOffset(a netip.Addr, off uint32) netip.Addr {
	b := a.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v += off
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
