// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Options configures New.
type Options struct {
	DataDir    string
	DockerHost string
	// MeshPort is the WireGuard UDP port, default 51820.
	MeshPort int
	// RestoreOnly applies the firewall and the mesh only, without Docker and
	// without DNS. 5050-net-restore uses it before Docker starts (Restore).
	RestoreOnly bool
	// Logger receives warnings that are retried in the background; default slog.Default().
	Logger *slog.Logger
}

// Net implements agentapi.Network: Docker networks per environment, the
// WireGuard mesh, the private DNS servers and the firewall (docs/SPEC.md 8.4).
type Net struct {
	o       Options
	ipt     iptables
	keys    keyStore
	dns     *privateDNS
	resolve hostResolver
	newDock func() (dockerAPI, error)
	newMesh func() (meshDriver, error)

	mu        sync.Mutex
	dock      dockerAPI
	mesh      meshDriver
	plan      AddressPlan
	nets      []proto.Network
	gateways  map[string]string // environment id -> gateway ip
	rulesHash string
	peers     map[string]string // WireGuard public key -> host id
}

var _ agentapi.Network = (*Net)(nil)

// New returns the agent's networking. Nothing touches the host until Apply.
func New(o Options) agentapi.Network { return newNet(o) }

func newNet(o Options) *Net {
	if o.MeshPort == 0 {
		o.MeshPort = DefaultMeshPort
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	plan, _ := NewAddressPlan("", "")
	n := &Net{
		o:        o,
		ipt:      newExecIPTables(nil),
		keys:     keyStore{path: filepath.Join(o.DataDir, "wg.key")},
		dns:      newPrivateDNS(defaultSockets(), newForwarder(), o.Logger),
		resolve:  lookupIPv4,
		newMesh:  newMeshDriver,
		plan:     plan,
		gateways: map[string]string{},
		peers:    map[string]string{},
	}
	n.newDock = func() (dockerAPI, error) { return newDockerClient(o.DockerHost) }
	return n
}

func lookupIPv4(ctx context.Context, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
}

// Apply brings networks, firewall, mesh and DNS to the desired state. It is
// idempotent: a repeated state changes nothing on the host. Items that fail
// are reported as errors while the rest still apply; the firewall is always
// replaced in one iptables-restore, or not at all.
func (n *Net) Apply(ctx context.Context, host proto.HostConfig, mesh proto.Mesh, nets []proto.Network, dns []proto.DNSEntry) error {
	plan, err := NewAddressPlan(host.PodCIDR, host.MeshCIDR)
	if err != nil {
		return fmt.Errorf("networking was not applied: %w", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.plan = plan
	n.nets = slices.Clone(nets)

	var errs []error
	spec, serrs := buildFirewallSpec(ctx, plan, nets, n.resolve)
	errs = append(errs, serrs...)
	if err := n.applyFirewallLocked(ctx, spec); err != nil {
		errs = append(errs, err)
	}
	if err := n.applyMeshLocked(mesh); err != nil {
		errs = append(errs, err)
	}
	gateways := map[string]string{}
	for _, e := range spec.Envs {
		gateways[e.ID] = e.Gateway.String()
	}
	n.gateways = gateways
	if n.o.RestoreOnly {
		return errors.Join(errs...)
	}
	if err := n.applyDockerLocked(ctx, spec, nets); err != nil {
		errs = append(errs, err)
	}
	tables, derrs := buildNameTables(dns)
	errs = append(errs, derrs...)
	listen := map[string]string{}
	for env, gw := range gateways {
		listen[gw] = env
	}
	if err := n.dns.update(tables, listen); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (n *Net) applyFirewallLocked(ctx context.Context, spec firewallSpec) error {
	rs := renderRules(spec)
	saved, err := n.ipt.Save(ctx)
	if err != nil {
		return err
	}
	st := parseSave(saved)
	h := sha256.Sum256([]byte(renderRestore(rs, savedState{})))
	key := hex.EncodeToString(h[:])
	if key == n.rulesHash && inSync(rs, st) {
		return nil
	}
	if err := n.ipt.Restore(ctx, renderRestore(rs, st)); err != nil {
		return err
	}
	n.rulesHash = key
	return nil
}

func (n *Net) meshDriverLocked() (meshDriver, error) {
	if n.mesh != nil {
		return n.mesh, nil
	}
	d, err := n.newMesh()
	if err != nil {
		return nil, err
	}
	n.mesh = d
	return d, nil
}

func (n *Net) applyMeshLocked(m proto.Mesh) error {
	if !m.Enabled {
		n.peers = map[string]string{}
		d, err := n.meshDriverLocked()
		if err != nil {
			return nil // no mesh support and no mesh wanted
		}
		return d.DeleteLink(MeshInterface)
	}
	key, err := n.keys.load()
	if err != nil {
		return err
	}
	mp, errs := planMesh(n.plan, m, n.o.MeshPort, resolveUDP)
	if mp.Address.IsValid() {
		d, err := n.meshDriverLocked()
		if err != nil {
			errs = append(errs, err)
		} else if err := applyMesh(d, key, mp); err != nil {
			errs = append(errs, err)
		}
		peers := map[string]string{}
		for _, p := range mp.Peers {
			peers[p.Key.String()] = p.HostID
		}
		n.peers = peers
	}
	return errors.Join(errs...)
}

func (n *Net) dockerLocked() (dockerAPI, error) {
	if n.dock != nil {
		return n.dock, nil
	}
	d, err := n.newDock()
	if err != nil {
		return nil, err
	}
	n.dock = d
	return d, nil
}

func (n *Net) applyDockerLocked(ctx context.Context, spec firewallSpec, nets []proto.Network) error {
	d, err := n.dockerLocked()
	if err != nil {
		return err
	}
	byEnv := map[string]proto.Network{}
	for _, pn := range nets {
		byEnv[pn.EnvironmentID] = pn
	}
	want := make([]wantNet, 0, len(spec.Envs))
	for _, e := range spec.Envs {
		pn := byEnv[e.ID]
		want = append(want, wantNet{
			Name:    networkName(e.ID, pn.Name),
			Env:     e.ID,
			Subnet:  e.Local,
			Gateway: e.Gateway,
			Bridge:  bridgeName(e.ID),
			MTU:     pn.MTU,
		})
	}
	return ensureNetworks(ctx, d, want, true)
}

// PublicKey returns this host's WireGuard public key, generating the private
// key in <DataDir>/wg.key (0600) on first use.
func (n *Net) PublicKey() (string, error) {
	k, err := n.keys.load()
	if err != nil {
		return "", err
	}
	return k.PublicKey().String(), nil
}

// DNSServer returns the address containers of an environment use as their
// resolver (the environment gateway), or "" when the environment has no
// network on this host yet.
func (n *Net) DNSServer(environmentID string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.gateways[environmentID]
}

// AllowEgress makes sure the egress and metadata rules covering containerIP
// are in place before the container joins any route. Rules are per subnet,
// so for a known network this only re-checks the live rules; a network the
// last Apply did not have is added first.
func (n *Net) AllowEgress(ctx context.Context, containerIP string, nw proto.Network) error {
	ip, err := netip.ParseAddr(containerIP)
	if err != nil {
		return fmt.Errorf("the container address %q is not an IP address", containerIP)
	}
	sub, err := netip.ParsePrefix(nw.Subnet)
	if err != nil || !sub.Contains(ip) {
		return fmt.Errorf("the container address %s is not inside its network %s", ip, nw.Subnet)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	merged := slices.Clone(n.nets)
	found := false
	for i, cur := range merged {
		if cur.EnvironmentID == nw.EnvironmentID {
			found = true
			if !reflect.DeepEqual(cur, nw) {
				merged[i] = nw
			}
		}
	}
	if !found {
		merged = append(merged, nw)
	}
	spec, errs := buildFirewallSpec(ctx, n.plan, merged, n.resolve)
	for _, e := range errs {
		n.o.Logger.Warn(e.Error())
	}
	if err := n.applyFirewallLocked(ctx, spec); err != nil {
		return fmt.Errorf("the egress rules for %s could not be applied: %w", ip, err)
	}
	n.nets = merged
	if _, ok := n.gateways[nw.EnvironmentID]; !ok {
		for _, e := range spec.Envs {
			if e.ID == nw.EnvironmentID {
				n.gateways[e.ID] = e.Gateway.String()
			}
		}
	}
	return nil
}

// BuildNetwork ensures the 5050-build bridge and returns its name. Its
// firewall rules (internet only, plus the registry on its gateway) are part
// of every rule set.
func (n *Net) BuildNetwork(ctx context.Context) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.rulesHash == "" {
		spec, _ := buildFirewallSpec(ctx, n.plan, n.nets, n.resolve)
		if err := n.applyFirewallLocked(ctx, spec); err != nil {
			return "", fmt.Errorf("the build network was not created because its firewall rules failed: %w", err)
		}
	}
	d, err := n.dockerLocked()
	if err != nil {
		return "", err
	}
	sub := n.plan.BuildSubnet()
	w := wantNet{Name: BuildNetworkName, Subnet: sub, Gateway: Gateway(sub), Bridge: BuildBridgeName}
	if err := ensureNetworks(ctx, d, []wantNet{w}, false); err != nil {
		return "", err
	}
	return BuildNetworkName, nil
}

// MeshHandshakes maps each mesh peer's host id to the Unix ms of its last
// WireGuard handshake (0 when there was none). The runtime reports it in
// HostSample.MeshHandshakes; the control plane flags pairs of hosts with no
// handshake for 3 minutes as unable to reach each other.
func (n *Net) MeshHandshakes() map[string]int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.peers) == 0 || n.mesh == nil {
		return nil
	}
	dev, err := n.mesh.Device(MeshInterface)
	if err != nil {
		return nil
	}
	out := map[string]int64{}
	for _, id := range n.peers {
		out[id] = 0
	}
	for _, p := range dev.Peers {
		id, ok := n.peers[p.PublicKey.String()]
		if !ok {
			continue
		}
		if !p.LastHandshake.IsZero() {
			out[id] = p.LastHandshake.UnixMilli()
		}
	}
	return out
}

// UnreachablePeers lists the host ids of mesh peers with no handshake within
// d (3 minutes in the spec), for the "mesh.unreachable" host event.
func (n *Net) UnreachablePeers(now time.Time, d time.Duration) []string {
	var out []string
	for id, ms := range n.MeshHandshakes() {
		if ms == 0 || now.Sub(time.UnixMilli(ms)) > d {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// Close stops the DNS servers. The firewall, mesh and bridges stay, so apps
// keep working while the agent restarts.
func (n *Net) Close() error {
	n.dns.close()
	return nil
}

// Restore is what 5050-net-restore runs at boot, before Docker starts: it
// loads the firewall chains and wg5050 from the cached desired state so a
// quarantined environment never has a window of open egress. The caller
// (the agent binary) decrypts the cache; Restore touches neither Docker nor DNS.
func Restore(ctx context.Context, o Options, st proto.DesiredState) error {
	o.RestoreOnly = true
	n := newNet(o)
	defer n.Close()
	return n.Apply(ctx, st.Host, st.Mesh, st.Networks, nil)
}
