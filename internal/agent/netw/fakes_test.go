// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// ---------------------------------------------------------------- docker

type fakeDocker struct {
	mu         sync.Mutex
	nets       map[string]network.Inspect // by name
	containers map[string]container.InspectResponse
	images     map[string]bool
	pulls      []string
	pullBody   string
	creates    []string
	removes    []string
	started    []string
	created    map[string]struct {
		cfg *container.Config
		hc  *container.HostConfig
	}
	createErr error
	seq       int
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{
		nets:       map[string]network.Inspect{},
		containers: map[string]container.InspectResponse{},
		images:     map[string]bool{},
		created: map[string]struct {
			cfg *container.Config
			hc  *container.HostConfig
		}{},
	}
}

func (f *fakeDocker) NetworkList(_ context.Context, o network.ListOptions) ([]network.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []network.Summary
	for _, n := range f.nets {
		if o.Filters.Contains("label") {
			if _, ok := n.Labels[labelNetwork]; !ok {
				continue
			}
		}
		s := n
		s.Containers = nil // the list endpoint does not fill containers
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeDocker) NetworkInspect(_ context.Context, id string, _ network.InspectOptions) (network.Inspect, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nets {
		if n.ID == id {
			return n, nil
		}
	}
	return network.Inspect{}, cerrdefs.ErrNotFound
}

func (f *fakeDocker) NetworkCreate(_ context.Context, name string, o network.CreateOptions) (network.CreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return network.CreateResponse{}, f.createErr
	}
	for _, n := range f.nets {
		if o.IPAM != nil && len(n.IPAM.Config) > 0 && n.IPAM.Config[0].Subnet == o.IPAM.Config[0].Subnet {
			return network.CreateResponse{}, fmt.Errorf("Pool overlaps with other one on this address space")
		}
	}
	f.seq++
	id := fmt.Sprintf("net%d", f.seq)
	f.nets[name] = network.Inspect{Name: name, ID: id, Driver: o.Driver, IPAM: *o.IPAM, Options: o.Options, Labels: o.Labels}
	f.creates = append(f.creates, name)
	return network.CreateResponse{ID: id}, nil
}

func (f *fakeDocker) NetworkRemove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, n := range f.nets {
		if n.ID == id {
			delete(f.nets, name)
			f.removes = append(f.removes, name)
			return nil
		}
	}
	return cerrdefs.ErrNotFound
}

func (f *fakeDocker) ContainerInspect(_ context.Context, id string) (container.InspectResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return container.InspectResponse{}, cerrdefs.ErrNotFound
	}
	return c, nil
}

func (f *fakeDocker) ContainerCreate(_ context.Context, cfg *container.Config, hc *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return container.CreateResponse{}, f.createErr
	}
	f.containers[name] = container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: name, Name: "/" + name, State: &container.State{}},
		Config:            cfg,
	}
	f.created[name] = struct {
		cfg *container.Config
		hc  *container.HostConfig
	}{cfg, hc}
	f.creates = append(f.creates, name)
	return container.CreateResponse{ID: name}, nil
}

func (f *fakeDocker) ContainerStart(_ context.Context, id string, _ container.StartOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[id]; ok {
		c.State.Running = true
	}
	f.started = append(f.started, id)
	return nil
}

func (f *fakeDocker) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.containers, id)
	f.removes = append(f.removes, id)
	return nil
}

func (f *fakeDocker) ImageInspect(_ context.Context, id string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.images[id] {
		return image.InspectResponse{ID: id}, nil
	}
	return image.InspectResponse{}, cerrdefs.ErrNotFound
}

func (f *fakeDocker) ImagePull(_ context.Context, ref string, _ image.PullOptions) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulls = append(f.pulls, ref)
	body := f.pullBody
	if body == "" {
		body = `{"status":"Pulling"}` + "\n" + `{"status":"Done"}` + "\n"
		f.images[ref] = true
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

// ---------------------------------------------------------------- mesh

type fakeMesh struct {
	mu         sync.Mutex
	link       bool
	mtu        int
	addr       netip.Prefix
	dev        meshDevice
	routes     []netip.Prefix
	configures []wgtypes.Config
	deleted    int
	routeOps   []string
}

func (m *fakeMesh) EnsureLink(name string, mtu int, addr netip.Prefix) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.link, m.mtu, m.addr = true, mtu, addr
	return nil
}

func (m *fakeMesh) DeleteLink(string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.link {
		m.deleted++
	}
	m.link = false
	m.dev = meshDevice{}
	m.routes = nil
	return nil
}

func (m *fakeMesh) Device(string) (meshDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.dev
	d.Peers = slices.Clone(d.Peers)
	return d, nil
}

// Configure applies the config the way the kernel does.
func (m *fakeMesh) Configure(_ string, cfg wgtypes.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configures = append(m.configures, cfg)
	if cfg.PrivateKey != nil {
		m.dev.PublicKey = cfg.PrivateKey.PublicKey()
	}
	if cfg.ListenPort != nil {
		m.dev.ListenPort = *cfg.ListenPort
	}
	for _, pc := range cfg.Peers {
		idx := slices.IndexFunc(m.dev.Peers, func(p meshDevPeer) bool { return p.PublicKey == pc.PublicKey })
		if pc.Remove {
			if idx >= 0 {
				m.dev.Peers = slices.Delete(m.dev.Peers, idx, idx+1)
			}
			continue
		}
		if idx < 0 {
			m.dev.Peers = append(m.dev.Peers, meshDevPeer{PublicKey: pc.PublicKey})
			idx = len(m.dev.Peers) - 1
		}
		p := &m.dev.Peers[idx]
		if pc.Endpoint != nil {
			p.Endpoint = pc.Endpoint
		}
		if pc.PersistentKeepaliveInterval != nil {
			p.Keepalive = *pc.PersistentKeepaliveInterval
		}
		if pc.ReplaceAllowedIPs {
			p.AllowedIPs = nil
		}
		for _, a := range pc.AllowedIPs {
			pf, _ := ipNetToPrefix(a)
			p.AllowedIPs = append(p.AllowedIPs, pf)
		}
	}
	return nil
}

func (m *fakeMesh) Routes(string) ([]netip.Prefix, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.routes), nil
}

func (m *fakeMesh) ReplaceRoute(_ string, dst netip.Prefix, _ netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.Contains(m.routes, dst) {
		m.routes = append(m.routes, dst)
	}
	m.routeOps = append(m.routeOps, "add "+dst.String())
	return nil
}

func (m *fakeMesh) DeleteRoute(_ string, dst netip.Prefix) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.Index(m.routes, dst); i >= 0 {
		m.routes = slices.Delete(m.routes, i, i+1)
	}
	m.routeOps = append(m.routeOps, "del "+dst.String())
	return nil
}

// ---------------------------------------------------------------- sockets

// loopSockets binds every address on 127.0.0.1 with a random port and
// remembers the mapping, so DNS servers "on a gateway" can run in a test.
type loopSockets struct {
	mu    sync.Mutex
	addrs map[string]string // requested -> actual (udp)
	fail  map[string]bool
}

func newLoopSockets() *loopSockets {
	return &loopSockets{addrs: map[string]string{}, fail: map[string]bool{}}
}

func (l *loopSockets) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail[address] {
		return nil, fmt.Errorf("cannot assign requested address")
	}
	pc, err := (&net.ListenConfig{}).ListenPacket(ctx, network, "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l.addrs[address] = pc.LocalAddr().String()
	return pc, nil
}

func (l *loopSockets) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	l.mu.Lock()
	actual := l.addrs[address]
	l.mu.Unlock()
	return (&net.ListenConfig{}).Listen(ctx, network, actual)
}

func (l *loopSockets) addr(requested string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.addrs[requested]
}

// ---------------------------------------------------------------- net

func newTestNet(t *testing.T) (*Net, *fakeIPT, *fakeDocker, *fakeMesh) {
	t.Helper()
	n := newNet(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ipt := &fakeIPT{saved: "*filter\n:INPUT ACCEPT [0:0]\n:FORWARD DROP [0:0]\nCOMMIT\n"}
	d := newFakeDocker()
	m := &fakeMesh{}
	n.ipt = ipt
	n.nft = func(context.Context, string, ...string) error { return errors.New("no such set") }
	n.resolve = fakeResolve
	n.newDock = func() (dockerAPI, error) { return d, nil }
	n.newMesh = func() (meshDriver, error) { return m, nil }
	n.dns = newPrivateDNS(newLoopSockets(), newForwarder(), n.o.Logger)
	t.Cleanup(func() { _ = n.Close() })
	return n, ipt, d, m
}
