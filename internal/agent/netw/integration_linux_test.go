// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration && linux

// Integration tests for WP5 (docs/SPEC.md 19, WP5 acceptance). They need
// root, iptables and iproute2, a kernel with WireGuard, and for the Caddy
// test network access to Docker Hub. `make test-linux` runs them in a
// privileged container; inside a Debian container without iptables or
// iproute2 they install both first (set NETW_NO_INSTALL=1 to skip instead).

package netw

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/miekg/dns"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/musakkka/5050cloud/pkg/proto"
)

var installOnce sync.Once

func requireTools(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root (run through make test-linux)")
	}
	missing := func() []string {
		var m []string
		for _, b := range []string{"iptables-restore", "iptables-save", "iptables", "ip"} {
			if _, err := exec.LookPath(b); err != nil {
				m = append(m, b)
			}
		}
		return m
	}
	if len(missing()) > 0 {
		installOnce.Do(func() {
			if os.Getenv("NETW_NO_INSTALL") != "" {
				return
			}
			if _, err := os.Stat("/.dockerenv"); err != nil {
				return
			}
			if _, err := exec.LookPath("apt-get"); err != nil {
				return
			}
			t.Log("installing iptables and iproute2 into the test container")
			for _, args := range [][]string{
				{"apt-get", "-o", "DPkg::Lock::Timeout=300", "update", "-qq"},
				{"apt-get", "-o", "DPkg::Lock::Timeout=300", "install", "-y", "-qq", "--no-install-recommends", "iptables", "iproute2"},
			} {
				c := exec.Command(args[0], args[1:]...)
				c.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
				if out, err := c.CombinedOutput(); err != nil {
					t.Logf("%v: %v\n%s", args, err, out)
				}
			}
		})
	}
	if m := missing(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
}

func sh(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func shOK(args ...string) { _ = exec.Command(args[0], args[1:]...).Run() }

func nsHandle(t *testing.T, name string) netns.NsHandle {
	t.Helper()
	h, err := netns.GetFromName(name)
	if err != nil {
		t.Fatalf("netns %s: %v", name, err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// nsSockets opens sockets inside a namespace, with IP_FREEBIND like production.
type nsSockets struct{ ns netns.NsHandle }

func (s nsSockets) ListenPacket(ctx context.Context, network, address string) (pc net.PacketConn, err error) {
	err = inNetns(s.ns, func() error {
		pc, err = defaultSockets().ListenPacket(ctx, network, address)
		return err
	})
	return
}

func (s nsSockets) Listen(ctx context.Context, network, address string) (l net.Listener, err error) {
	err = inNetns(s.ns, func() error {
		l, err = defaultSockets().Listen(ctx, network, address)
		return err
	})
	return
}

// nsDocker stands in for Docker inside a namespace: a network is a Linux
// bridge with the gateway address, the way Docker's bridge driver makes it.
type nsDocker struct {
	nl   *netlink.Handle
	mu   sync.Mutex
	nets map[string]network.Inspect
}

func (d *nsDocker) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []network.Summary
	for _, n := range d.nets {
		out = append(out, n)
	}
	return out, nil
}

func (d *nsDocker) NetworkInspect(_ context.Context, id string, _ network.InspectOptions) (network.Inspect, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, n := range d.nets {
		if n.ID == id {
			return n, nil
		}
	}
	return network.Inspect{}, cerrdefs.ErrNotFound
}

func (d *nsDocker) NetworkCreate(_ context.Context, name string, o network.CreateOptions) (network.CreateResponse, error) {
	br := o.Options[optBridgeName]
	la := netlink.NewLinkAttrs()
	la.Name = br
	if err := d.nl.LinkAdd(&netlink.Bridge{LinkAttrs: la}); err != nil && !errors.Is(err, syscall.EEXIST) {
		return network.CreateResponse{}, err
	}
	link, err := d.nl.LinkByName(br)
	if err != nil {
		return network.CreateResponse{}, err
	}
	gw := o.IPAM.Config[0].Gateway
	_, sub, _ := net.ParseCIDR(o.IPAM.Config[0].Subnet)
	ones, _ := sub.Mask.Size()
	addr, _ := netlink.ParseAddr(fmt.Sprintf("%s/%d", gw, ones))
	if err := d.nl.AddrReplace(link, addr); err != nil {
		return network.CreateResponse{}, err
	}
	if err := d.nl.LinkSetUp(link); err != nil {
		return network.CreateResponse{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nets[name] = network.Inspect{Name: name, ID: name, Driver: "bridge", IPAM: *o.IPAM, Options: o.Options, Labels: o.Labels}
	return network.CreateResponse{ID: name}, nil
}

func (d *nsDocker) NetworkRemove(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.nets, id)
	return nil
}

func (d *nsDocker) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{}, errors.New("not used")
}

func (d *nsDocker) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error) {
	return container.CreateResponse{}, errors.New("not used")
}
func (d *nsDocker) ContainerStart(context.Context, string, container.StartOptions) error {
	return errors.New("not used")
}
func (d *nsDocker) ContainerRemove(context.Context, string, container.RemoveOptions) error {
	return errors.New("not used")
}
func (d *nsDocker) ImageInspect(context.Context, string, ...client.ImageInspectOption) (image.InspectResponse, error) {
	return image.InspectResponse{}, errors.New("not used")
}
func (d *nsDocker) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}

// hostNet is our Net bound to a namespace that plays one host.
func hostNet(t *testing.T, name string) *Net {
	t.Helper()
	ns := nsHandle(t, name)
	n := newNet(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	n.ipt = newExecIPTables([]string{"ip", "netns", "exec", name})
	n.dns = newPrivateDNS(nsSockets{ns}, newForwarder(), n.o.Logger)
	nl, err := netlink.NewHandleAt(ns)
	if err != nil {
		t.Fatal(err)
	}
	d := &nsDocker{nl: nl, nets: map[string]network.Inspect{}}
	n.newDock = func() (dockerAPI, error) { return d, nil }
	n.newMesh = func() (meshDriver, error) { return newMeshDriverAt(ns) }
	t.Cleanup(func() { _ = n.Close() })
	return n
}

// serveTCP starts an echo server inside a namespace that answers "<tag>".
func serveTCP(t *testing.T, ns, addr, tag string) {
	t.Helper()
	var l net.Listener
	if err := inNetns(nsHandle(t, ns), func() error {
		var err error
		l, err = net.Listen("tcp", addr)
		return err
	}); err != nil {
		t.Fatalf("listen %s in %s: %v", addr, ns, err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, tag+"\n")
			c.Close()
		}
	}()
}

// reach dials addr from inside a namespace and returns the first line.
func reach(t *testing.T, ns, addr string) (string, error) {
	t.Helper()
	var c net.Conn
	err := inNetns(nsHandle(t, ns), func() error {
		var err error
		c, err = net.DialTimeout("tcp", addr, 1500*time.Millisecond)
		return err
	})
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line), err
}

// resolve asks a DNS server from inside a namespace.
func resolve(t *testing.T, ns, server, name string) (*dns.Msg, error) {
	t.Helper()
	var conn net.Conn
	err := inNetns(nsHandle(t, ns), func() error {
		var err error
		conn, err = net.DialTimeout("udp", server, time.Second)
		return err
	})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	c := &dns.Client{Timeout: 1500 * time.Millisecond}
	r, _, err := c.ExchangeWithConn(m, &dns.Conn{Conn: conn})
	return r, err
}

func addContainer(t *testing.T, host, bridge, name, cidr, gw string) {
	t.Helper()
	shOK("ip", "netns", "del", name)
	sh(t, "ip", "netns", "add", name)
	t.Cleanup(func() { shOK("ip", "netns", "del", name) })
	sh(t, "ip", "-n", host, "link", "add", "v-"+name, "type", "veth", "peer", "name", "eth0", "netns", name)
	sh(t, "ip", "-n", host, "link", "set", "v-"+name, "master", bridge, "up")
	sh(t, "ip", "-n", name, "addr", "add", cidr, "dev", "eth0")
	sh(t, "ip", "-n", name, "link", "set", "eth0", "up")
	sh(t, "ip", "-n", name, "link", "set", "lo", "up")
	sh(t, "ip", "-n", name, "route", "add", "default", "via", gw)
}

func TestIntegrationTwoHostsOverWireGuard(t *testing.T) {
	requireTools(t)
	for _, ns := range []string{"nw-h1", "nw-h2"} {
		shOK("ip", "netns", "del", ns)
		sh(t, "ip", "netns", "add", ns)
		t.Cleanup(func() { shOK("ip", "netns", "del", ns) })
		sh(t, "ip", "netns", "exec", ns, "sysctl", "-qw", "net.ipv4.ip_forward=1")
		sh(t, "ip", "-n", ns, "link", "set", "lo", "up")
		// What Docker sets up on a host: FORWARD drops by default, jumps to
		// DOCKER-USER first, accepts traffic leaving its bridges and replies.
		sh(t, "ip", "netns", "exec", ns, "iptables", "-N", "DOCKER-USER")
		sh(t, "ip", "netns", "exec", ns, "iptables", "-P", "FORWARD", "DROP")
		sh(t, "ip", "netns", "exec", ns, "iptables", "-A", "FORWARD", "-j", "DOCKER-USER")
		sh(t, "ip", "netns", "exec", ns, "iptables", "-A", "FORWARD", "-i", "f5+", "-j", "ACCEPT")
		sh(t, "ip", "netns", "exec", ns, "iptables", "-A", "FORWARD", "-o", "f5+", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
		sh(t, "ip", "netns", "exec", ns, "iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.128.0.0/9", "!", "-o", "f5+", "-j", "MASQUERADE")
	}
	// The underlay between the two hosts, plus a fake metadata endpoint and
	// two "internet" addresses on host 2.
	sh(t, "ip", "-n", "nw-h1", "link", "add", "ul1", "type", "veth", "peer", "name", "ul2", "netns", "nw-h2")
	sh(t, "ip", "-n", "nw-h1", "addr", "add", "192.168.77.1/24", "dev", "ul1")
	sh(t, "ip", "-n", "nw-h1", "link", "set", "ul1", "up")
	sh(t, "ip", "-n", "nw-h2", "addr", "add", "192.168.77.2/24", "dev", "ul2")
	sh(t, "ip", "-n", "nw-h2", "addr", "add", "192.168.77.3/24", "dev", "ul2")
	sh(t, "ip", "-n", "nw-h2", "addr", "add", "169.254.169.254/32", "dev", "ul2")
	sh(t, "ip", "-n", "nw-h2", "link", "set", "ul2", "up")
	sh(t, "ip", "-n", "nw-h1", "route", "add", "169.254.169.254/32", "via", "192.168.77.2")

	h1, h2 := hostNet(t, "nw-h1"), hostNet(t, "nw-h2")
	k1, err := h1.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := h2.PublicKey()
	mesh1 := proto.Mesh{Enabled: true, MTU: 1420,
		Self:  proto.MeshSelf{Index: 1, Address: "100.96.0.1/32", ListenPort: 51820, HostSubnet: "10.129.0.0/16"},
		Peers: []proto.MeshPeer{{HostID: "h2", PublicKey: k2, Endpoint: "192.168.77.2:51820", AllowedIPs: []string{"100.96.0.2/32", "10.130.0.0/16"}}}}
	mesh2 := proto.Mesh{Enabled: true, MTU: 1420,
		Self: proto.MeshSelf{Index: 2, Address: "100.96.0.2/32", ListenPort: 51820, HostSubnet: "10.130.0.0/16"},
		// Host 2 is "behind NAT": it knows no endpoint for host 1 until host 1 talks to it.
		Peers: []proto.MeshPeer{{HostID: "h1", PublicKey: k1, Endpoint: "192.168.77.1:51820", AllowedIPs: []string{"100.96.0.1/32", "10.129.0.0/16"}}}}

	// Environment A spans both hosts; B is quarantined on host 1.
	netsH1 := []proto.Network{
		{EnvironmentID: "env_a", Subnet: "10.129.4.0/24", Gateway: "10.129.4.1", Egress: "allowed", PeerSubnets: []string{"10.130.4.0/24"}, AllowMetadata: []string{"10.129.4.9"}, MTU: 1420},
		{EnvironmentID: "env_b", Subnet: "10.129.5.0/24", Gateway: "10.129.5.1", Egress: "blocked", EgressAllow: []string{"192.168.77.3/32"}, MTU: 1420},
	}
	netsH2 := []proto.Network{
		{EnvironmentID: "env_a", Subnet: "10.130.4.0/24", Gateway: "10.130.4.1", Egress: "allowed", PeerSubnets: []string{"10.129.4.0/24"}, MTU: 1420},
	}
	dnsAll := []proto.DNSEntry{
		{EnvironmentID: "env_a", Name: "api.internal", IPs: []string{"10.130.4.7"}},
		{EnvironmentID: "env_a", Name: "web.internal", Aliases: []string{"old-web.internal"}, IPs: []string{"10.129.4.7"}, Draining: []string{"10.129.4.8"}},
		{EnvironmentID: "env_b", Name: "worker.internal", IPs: []string{"10.129.5.7"}},
	}
	ctx := context.Background()
	if err := h1.Apply(ctx, proto.HostConfig{}, mesh1, netsH1, dnsAll); err != nil {
		t.Fatalf("apply host 1: %v", err)
	}
	if err := h2.Apply(ctx, proto.HostConfig{}, mesh2, netsH2, dnsAll); err != nil {
		t.Fatalf("apply host 2: %v", err)
	}
	// Apply twice: idempotent, and nothing changes.
	before := sh(t, "ip", "netns", "exec", "nw-h1", "iptables-save")
	if err := h1.Apply(ctx, proto.HostConfig{}, mesh1, netsH1, dnsAll); err != nil {
		t.Fatal(err)
	}
	if after := sh(t, "ip", "netns", "exec", "nw-h1", "iptables-save"); stripCounters(after) != stripCounters(before) {
		t.Errorf("a repeated apply changed the rules:\n%s", after)
	}

	addContainer(t, "nw-h1", bridgeName("env_a"), "nw-c1a", "10.129.4.7/24", "10.129.4.1")
	addContainer(t, "nw-h1", bridgeName("env_a"), "nw-c1m", "10.129.4.9/24", "10.129.4.1") // metadata allowed
	addContainer(t, "nw-h2", bridgeName("env_a"), "nw-c2a", "10.130.4.7/24", "10.130.4.1")
	addContainer(t, "nw-h1", bridgeName("env_b"), "nw-c1b", "10.129.5.7/24", "10.129.5.1")

	serveTCP(t, "nw-c1a", ":8080", "web")
	serveTCP(t, "nw-c2a", ":8080", "api")
	serveTCP(t, "nw-c1b", ":8080", "worker")
	serveTCP(t, "nw-h2", "192.168.77.2:9000", "internet-2")
	serveTCP(t, "nw-h2", "192.168.77.3:9000", "internet-3")
	serveTCP(t, "nw-h2", "169.254.169.254:80", "metadata")
	serveTCP(t, "nw-h1", ":2222", "host-ssh")
	serveTCP(t, "nw-h1", "100.96.0.1:5000", "host-registry")

	// Same environment across hosts by name, through WireGuard.
	var r *dns.Msg
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, err = resolve(t, "nw-c1a", "10.129.4.1:53", "api.internal")
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || len(r.Answer) != 1 || r.Answer[0].(*dns.A).A.String() != "10.130.4.7" {
		t.Fatalf("api.internal from host 1 = %v, %v", r, err)
	}
	if r, _ := resolve(t, "nw-c1a", "10.129.4.1:53", "old-web.railway.internal"); r == nil || len(r.Answer) != 1 || r.Answer[0].(*dns.A).A.String() != "10.129.4.7" {
		t.Errorf("rename alias over the railway suffix = %v (the draining address must be left out)", r)
	}
	var got string
	for time.Now().Before(deadline) {
		if got, err = reach(t, "nw-c1a", "10.130.4.7:8080"); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got != "api" {
		t.Fatalf("host 1 to host 2 in one environment: %q, %v", got, err)
	}
	if got, err := reach(t, "nw-c2a", "10.129.4.7:8080"); got != "web" {
		t.Errorf("host 2 to host 1 in one environment: %q, %v", got, err)
	}
	hs := h1.MeshHandshakes()
	if hs["h2"] == 0 {
		t.Errorf("no handshake recorded with host 2: %v", hs)
	}

	// Other environments are blocked, on the same host and across hosts.
	for _, target := range []string{"10.129.4.7:8080", "10.130.4.7:8080"} {
		if got, err := reach(t, "nw-c1b", target); err == nil {
			t.Errorf("env_b reached env_a at %s: %q", target, got)
		}
	}
	if got, err := reach(t, "nw-c1a", "10.129.5.7:8080"); err == nil {
		t.Errorf("env_a reached env_b: %q", got)
	}
	if r, err := resolve(t, "nw-c1b", "10.129.5.1:53", "api.internal"); err != nil || r.Rcode != dns.RcodeNameError {
		t.Errorf("env_b resolves env_a's name: %v %v", r, err)
	}
	// Host addresses: other gateways' DNS, a host service, the mesh (local and remote).
	if _, err := resolve(t, "nw-c1a", "10.129.5.1:53", "web.internal"); err == nil {
		t.Error("env_a used env_b's gateway DNS")
	}
	for _, target := range []string{"10.129.4.1:2222", "192.168.77.1:2222", "100.96.0.1:5000", "100.96.0.2:51820"} {
		if got, err := reach(t, "nw-c1a", target); err == nil {
			t.Errorf("a container reached host address %s: %q", target, got)
		}
	}
	// Metadata: blocked unless the replica has the opt-in.
	if got, err := reach(t, "nw-c1a", "169.254.169.254:80"); err == nil {
		t.Errorf("metadata reachable: %q", got)
	}
	if got, err := reach(t, "nw-c1m", "169.254.169.254:80"); got != "metadata" {
		t.Errorf("opted-in replica cannot reach metadata: %q %v", got, err)
	}
	// Egress: allowed environment reaches the internet; quarantined one only its allowlist.
	if got, err := reach(t, "nw-c1a", "192.168.77.2:9000"); got != "internet-2" {
		t.Errorf("allowed egress: %q %v", got, err)
	}
	if got, err := reach(t, "nw-c1b", "192.168.77.3:9000"); got != "internet-3" {
		t.Errorf("allowlisted egress: %q %v", got, err)
	}
	if got, err := reach(t, "nw-c1b", "192.168.77.2:9000"); err == nil {
		t.Errorf("quarantined environment reached a non-allowlisted address: %q", got)
	}

	// MSS clamping and the mesh MTU are in place.
	mangle := sh(t, "ip", "netns", "exec", "nw-h1", "iptables-save", "-t", "mangle")
	if !strings.Contains(mangle, "--clamp-mss-to-pmtu") || !strings.Contains(mangle, "-A FORWARD -j 5050-MSS") {
		t.Errorf("MSS clamp missing:\n%s", mangle)
	}
	if link := sh(t, "ip", "-n", "nw-h1", "link", "show", "wg5050"); !strings.Contains(link, "mtu 1420") {
		t.Errorf("wg5050 = %s", link)
	}
	if routes := sh(t, "ip", "-n", "nw-h1", "route", "show", "dev", "wg5050"); !strings.Contains(routes, "10.130.0.0/16") || !strings.Contains(routes, "src 100.96.0.1") {
		t.Errorf("mesh routes = %s", routes)
	}

	// Quarantine lifted and the environment removed: rules follow, nothing else is touched.
	if err := h1.Apply(ctx, proto.HostConfig{}, mesh1, netsH1[:1], dnsAll); err != nil {
		t.Fatal(err)
	}
	save := sh(t, "ip", "netns", "exec", "nw-h1", "iptables-save")
	if strings.Contains(save, egrChain("env_b")) || strings.Contains(save, envChain("env_b")) {
		t.Error("removed environment's chains left behind")
	}
	if !strings.Contains(save, "-A FORWARD -i f5+ -j ACCEPT") || !strings.Contains(save, "MASQUERADE") {
		t.Error("rules outside our chains were changed")
	}
}

func stripCounters(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, ":") {
			if i := strings.LastIndex(l, " ["); i > 0 {
				l = l[:i]
			}
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

func TestIntegrationRestoreBeforeDocker(t *testing.T) {
	requireTools(t)
	const ns = "nw-boot"
	shOK("ip", "netns", "del", ns)
	sh(t, "ip", "netns", "add", ns)
	t.Cleanup(func() { shOK("ip", "netns", "del", ns) })
	n := hostNet(t, ns)
	n.o.RestoreOnly = true
	other, _ := hostNet(t, ns).PublicKey()
	st := proto.DesiredState{
		Networks: []proto.Network{{EnvironmentID: "env_q", Subnet: "10.129.9.0/24", Egress: "blocked"}},
		Mesh: proto.Mesh{Enabled: true, Self: proto.MeshSelf{Address: "100.96.0.1/32"},
			Peers: []proto.MeshPeer{{HostID: "h2", PublicKey: other, AllowedIPs: []string{"100.96.0.2/32", "10.130.0.0/16"}}}},
	}
	// No Docker yet: DOCKER-USER does not exist and no bridge exists.
	if err := n.Apply(context.Background(), st.Host, st.Mesh, st.Networks, nil); err != nil {
		t.Fatal(err)
	}
	save := sh(t, "ip", "netns", "exec", ns, "iptables-save")
	for _, want := range []string{":DOCKER-USER", "-A DOCKER-USER -j 5050-ISOLATE", "-j REJECT --reject-with icmp-admin-prohibited"} {
		if !strings.Contains(save, want) {
			t.Errorf("restore lacks %q:\n%s", want, save)
		}
	}
	if out := sh(t, "ip", "-n", ns, "link", "show", "wg5050"); !strings.Contains(out, "wg5050") {
		t.Error("wg5050 not restored")
	}
	n.dns.mu.RLock()
	defer n.dns.mu.RUnlock()
	if len(n.dns.servers) != 0 {
		t.Error("restore started DNS")
	}
}

// ---------------------------------------------------------------- Caddy

// caddyBinary extracts /usr/bin/caddy from the pinned image (cached).
func caddyBinary(t *testing.T) string {
	t.Helper()
	cache := filepath.Join(os.TempDir(), "5050-netw-test")
	digest := CaddyImage[strings.LastIndex(CaddyImage, ":")+1:]
	bin := filepath.Join(cache, "caddy-"+digest[:12]+"-"+runtime.GOARCH)
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	img, err := crane.Pull(CaddyImage, crane.WithPlatform(&v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
	if err != nil {
		t.Skipf("cannot pull %s (no network?): %v", CaddyImage, err)
	}
	rc := mutate.Extract(img)
	defer rc.Close()
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if err != nil {
			t.Fatalf("caddy binary not found in %s: %v", CaddyImage, err)
		}
		if strings.TrimPrefix(h.Name, "/") != "usr/bin/caddy" {
			continue
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(bin+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if err := os.Rename(bin+".tmp", bin); err != nil {
			t.Fatal(err)
		}
		return bin
	}
}

// backend serves "<name>" on / and echoes on /ws.
func backend(t *testing.T, ip, name string, port int) {
	t.Helper()
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, name) })
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, append([]byte(name+":"), b...)); err != nil {
				return
			}
		}
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func TestIntegrationCaddyUpstreamSwapAndReload(t *testing.T) {
	requireTools(t)
	bin := caddyBinary(t)
	dir, err := os.MkdirTemp("", "nwc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	const httpPort, httpsPort, appPort = 18080, 18443, 18090
	g := newIngress(IngressOptions{DataDir: dir, ResolverAddr: "127.0.0.1:15354", HTTPPort: httpPort, HTTPSPort: httpsPort,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(func() { _ = g.Close() })
	if err := g.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := writeIfChanged(g.bootstrapPath(), bootstrapCaddy(g.params), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "--resume", "--config", g.bootstrapPath())
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+filepath.Join(dir, "caddy", "config"), "XDG_DATA_HOME="+filepath.Join(dir, "caddy", "xdg"))
	logf, _ := os.Create(filepath.Join(dir, "caddy.out"))
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			b, _ := os.ReadFile(filepath.Join(dir, "caddy.out"))
			t.Logf("caddy output:\n%s", b)
		}
	})
	if err := g.waitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(g.o.AdminSocket)
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("admin socket %s: mode %v err %v", g.o.AdminSocket, fi.Mode(), err)
	}

	backend(t, "127.0.0.2", "blue", appPort)
	backend(t, "127.0.0.3", "green", appPort)
	in := proto.Ingress{
		ACMEPrimary: true,
		Routes: []proto.Route{
			{Domain: "app.test", UpstreamName: "svc-app", Port: appPort, TLS: "off", ServiceID: "svc_app"},
			{Domain: "secure.test", UpstreamName: "svc-app", Port: appPort, TLS: "internal", ServiceID: "svc_app"},
		},
		Upstreams: []proto.UpstreamSet{{Name: "svc-app", IPs: []string{"127.0.0.2"}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logs := make(chan proto.LogRecord, 100000)
	go func() { _ = g.AccessLogs(ctx, logs) }()
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	if g.AdminLoads() != 1 {
		t.Fatalf("loads = %d", g.AdminLoads())
	}

	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64}}
	get := func(host string) (string, int, error) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), nil)
		req.Host = host
		resp, err := hc.Do(req)
		if err != nil {
			return "", 0, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.StatusCode, nil
	}
	if body, code, err := get("app.test"); body != "blue" || code != 200 {
		t.Fatalf("first request: %q %d %v", body, code, err)
	}
	if body, code, _ := get("unknown.test"); code != 404 || !strings.Contains(body, "No service is attached to this domain") {
		t.Errorf("catch-all = %d", code)
	}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/x?y=1", httpPort), nil)
	req.Host = "secure.test"
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, err := noRedirect.Do(req); err != nil {
		t.Errorf("redirect: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != "https://secure.test/x?y=1" {
			t.Errorf("redirect = %v", resp)
		}
	}

	// Open a websocket before the swap and the reload.
	ws, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", httpPort), &websocket.DialOptions{Host: "app.test"}) //nolint:bodyclose // the websocket library owns the response body
	if err != nil {
		t.Fatalf("websocket: %v", err)
	}
	defer ws.CloseNow()
	echo := func(msg string) (string, error) {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := ws.Write(c, websocket.MessageText, []byte(msg)); err != nil {
			return "", err
		}
		_, b, err := ws.Read(c)
		return string(b), err
	}
	if got, err := echo("hello"); got != "blue:hello" {
		t.Fatalf("websocket echo = %q %v", got, err)
	}

	// Load during upstream swaps: zero errors, both backends serve, no reload.
	var errCount, okCount atomic.Int64
	seen := sync.Map{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				body, code, err := get("app.test")
				if err != nil || code != 200 {
					errCount.Add(1)
					continue
				}
				okCount.Add(1)
				seen.Store(body, true)
			}
		}()
	}
	time.Sleep(time.Second)
	if err := g.SetUpstreams("svc-app", []string{"127.0.0.3"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if err := g.SetUpstreams("svc-app", []string{"127.0.0.2", "127.0.0.3"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	close(stop)
	wg.Wait()
	_, blue := seen.Load("blue")
	_, green := seen.Load("green")
	t.Logf("requests during swap: %d ok, %d errors", okCount.Load(), errCount.Load())
	if errCount.Load() != 0 || !blue || !green {
		t.Errorf("swap under load: %d errors, blue %v green %v", errCount.Load(), blue, green)
	}
	if g.AdminLoads() != 1 {
		t.Errorf("an upstream swap reloaded Caddy (%d loads)", g.AdminLoads())
	}

	// Same state again: no reload.
	in.Upstreams = []proto.UpstreamSet{{Name: "svc-app", IPs: []string{"127.0.0.2", "127.0.0.3"}}}
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	if g.AdminLoads() != 1 {
		t.Errorf("an unchanged config reloaded Caddy (%d loads)", g.AdminLoads())
	}
	// A new route reloads, and the open websocket survives it.
	in.Routes = append(in.Routes, proto.Route{Domain: "other.test", UpstreamName: "svc-app", Port: appPort, TLS: "off", ServiceID: "svc_other"})
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	if g.AdminLoads() != 2 {
		t.Fatalf("route change loads = %d, want 2", g.AdminLoads())
	}
	time.Sleep(500 * time.Millisecond)
	if got, err := echo("after-reload"); got != "blue:after-reload" {
		t.Errorf("websocket after a route set reload = %q %v", got, err)
	}
	if body, code, err := get("other.test"); code != 200 || (body != "blue" && body != "green") {
		t.Errorf("new route: %q %d %v", body, code, err)
	}

	// Internal TLS on the https port.
	tc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: "secure.test", InsecureSkipVerify: true}}} // #nosec G402 -- internal CA in a test
	var resp *http.Response
	for range 50 {
		r2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/", httpsPort), nil)
		r2.Host = "secure.test"
		if resp, err = tc.Do(r2); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("internal TLS: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}
	st, _ := g.CertStatus(ctx)
	if st["secure.test"].Status != "active" || st["secure.test"].NotAfter == 0 {
		t.Errorf("cert status = %+v", st)
	}

	// Access logs reach the right service, and feed HTTP metrics.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case rec := <-logs:
			if rec.ServiceID == "svc_app" && rec.Stream == "http" && rec.Attrs["host"] == "app.test" {
				s := g.HTTPSamples()
				if len(s) == 0 || s[0].Req2xx == 0 {
					t.Errorf("HTTP samples = %+v", s)
				}
				return
			}
		case <-deadline:
			t.Fatal("no access log record arrived")
		}
	}
}
