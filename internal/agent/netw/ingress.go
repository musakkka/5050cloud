// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// CaddyContainer is the name of the Caddy container on ingress hosts.
const CaddyContainer = "5050-caddy"

// IngressOptions configures NewIngress.
type IngressOptions struct {
	DataDir    string
	DockerHost string
	// AdminSocket is Caddy's admin API, default "<DataDir>/caddy/admin.sock" (root, 0600).
	AdminSocket string
	// ResolverAddr is the upstream resolver Caddy's dynamic upstreams query, default "127.0.0.1:5354".
	ResolverAddr string
	// HTTPPort and HTTPSPort default to 80 and 443.
	HTTPPort  int
	HTTPSPort int
	// Logger receives warnings; default slog.Default().
	Logger *slog.Logger
}

// Ingress implements agentapi.Ingress: Caddy on the host network with its
// admin API on a unix socket, the upstream resolver Caddy reads replica
// addresses from, the TCP proxy, certificate status and access logs.
type Ingress struct {
	o       IngressOptions
	params  caddyParams
	admin   *caddyAdmin
	ups     *upstreamDNS
	sockets socketMaker
	tcp     *tcpProxy
	stats   *HTTPAggregator
	newDock func() (dockerAPI, error)
	now     func() time.Time

	mu         sync.Mutex
	dock       dockerAPI
	loadedHash string
	last       proto.Ingress
	hosts      map[string]string // domain -> service id
	tlsDomains map[string]string // domain -> "acme" | "internal"
}

var _ agentapi.Ingress = (*Ingress)(nil)

// NewIngress returns the ingress of an ingress host. Nothing runs until
// EnsureRunning or Apply.
func NewIngress(o IngressOptions) agentapi.Ingress { return newIngress(o) }

func newIngress(o IngressOptions) *Ingress {
	dir := filepath.Join(o.DataDir, "caddy")
	if o.AdminSocket == "" {
		o.AdminSocket = filepath.Join(dir, "admin.sock")
	}
	if o.ResolverAddr == "" {
		o.ResolverAddr = DefaultResolverAddr
	}
	if o.HTTPPort == 0 {
		o.HTTPPort = 80
	}
	if o.HTTPSPort == 0 {
		o.HTTPSPort = 443
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	g := &Ingress{
		o:       o,
		params:  caddyParams{Dir: dir, AdminSocket: o.AdminSocket, ResolverAddr: o.ResolverAddr, HTTPPort: o.HTTPPort, HTTPSPort: o.HTTPSPort},
		admin:   newCaddyAdmin(o.AdminSocket),
		ups:     &upstreamDNS{},
		sockets: defaultSockets(),
		tcp:     newTCPProxy(o.Logger),
		stats:   NewHTTPAggregator(),
		now:     time.Now,
		hosts:   map[string]string{},
	}
	g.newDock = func() (dockerAPI, error) { return newDockerClient(o.DockerHost) }
	return g
}

func (g *Ingress) docker() (dockerAPI, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dock != nil {
		return g.dock, nil
	}
	d, err := g.newDock()
	if err != nil {
		return nil, err
	}
	g.dock = d
	return d, nil
}

func (g *Ingress) ensureDirs() error {
	for _, d := range []string{g.params.Dir, g.params.storage(), filepath.Join(g.params.Dir, "logs"), filepath.Join(g.params.Dir, "config")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("creating the Caddy directory %s failed: %w", d, err)
		}
	}
	// The admin socket lives here; only root may enter.
	if err := os.Chmod(g.params.Dir, 0o700); err != nil {
		return fmt.Errorf("restricting the Caddy directory %s failed: %w", g.params.Dir, err)
	}
	return nil
}

func (g *Ingress) bootstrapPath() string { return filepath.Join(g.params.Dir, "bootstrap.json") }

// EnsureRunning starts or upgrades the Caddy container: host networking,
// --resume (the last loaded config survives restarts), data and logs in
// <DataDir>/caddy, admin API on the root-only unix socket. It also starts the
// upstream resolver Caddy depends on.
func (g *Ingress) EnsureRunning(ctx context.Context, img string) error {
	if img == "" {
		img = CaddyImage
	}
	if err := g.ensureDirs(); err != nil {
		return err
	}
	if err := writeIfChanged(g.bootstrapPath(), bootstrapCaddy(g.params), 0o600); err != nil {
		return err
	}
	if err := g.ups.start(ctx, g.sockets, g.o.ResolverAddr); err != nil {
		return err
	}
	d, err := g.docker()
	if err != nil {
		return err
	}
	insp, err := d.ContainerInspect(ctx, CaddyContainer)
	switch {
	case err == nil && insp.Config != nil && insp.Config.Image == img:
		if insp.State == nil || !insp.State.Running {
			if err := d.ContainerStart(ctx, insp.ID, container.StartOptions{}); err != nil {
				return fmt.Errorf("starting the Caddy container failed: %w", err)
			}
		}
		return g.waitAdmin(ctx)
	case err == nil:
		// A different image: pull the new one first, so a failed pull leaves Caddy running.
		if err := ensureImage(ctx, d, img); err != nil {
			return err
		}
		if err := d.ContainerRemove(ctx, insp.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("removing the old Caddy container failed: %w", err)
		}
	case errdefsNotFound(err):
		if err := ensureImage(ctx, d, img); err != nil {
			return err
		}
	default:
		return fmt.Errorf("inspecting the Caddy container failed: %w", err)
	}
	cfg, hc := g.containerSpec(img)
	created, err := d.ContainerCreate(ctx, cfg, hc, nil, nil, CaddyContainer)
	if err != nil {
		return fmt.Errorf("creating the Caddy container failed: %w", err)
	}
	if err := d.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting the Caddy container failed: %w", err)
	}
	g.mu.Lock()
	g.loadedHash = "" // compare against what --resume loaded
	g.mu.Unlock()
	return g.waitAdmin(ctx)
}

func (g *Ingress) containerSpec(img string) (*container.Config, *container.HostConfig) {
	dir := g.params.Dir
	oom := -800
	cfg := &container.Config{
		Image: img,
		Cmd:   []string{"caddy", "run", "--resume", "--config", g.bootstrapPath()},
		Env: []string{
			"XDG_CONFIG_HOME=" + filepath.Join(dir, "config"),
			"XDG_DATA_HOME=" + filepath.Join(dir, "xdg-data"),
		},
		Labels: map[string]string{labelInfra: "caddy"},
	}
	hc := &container.HostConfig{
		NetworkMode:   "host",
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		OomScoreAdj:   oom,
		Mounts:        []mount.Mount{{Type: mount.TypeBind, Source: dir, Target: dir}},
		SecurityOpt:   []string{"no-new-privileges"},
		LogConfig:     container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}},
	}
	return cfg, hc
}

func (g *Ingress) waitAdmin(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if last = g.admin.ping(ctx); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("Caddy started but its admin API on %s did not answer within 30 seconds (%w); see docker logs %s", g.o.AdminSocket, last, CaddyContainer) //nolint:staticcheck // product name first
}

func ensureImage(ctx context.Context, d dockerAPI, img string) error {
	if _, err := d.ImageInspect(ctx, img); err == nil {
		return nil
	}
	rc, err := d.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling the Caddy image %s failed: %w", img, err)
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var m struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) == nil && m.Error != "" {
			return fmt.Errorf("pulling the Caddy image %s failed: %s", img, m.Error)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("pulling the Caddy image %s failed: %w", img, err)
	}
	return nil
}

func writeIfChanged(path string, b []byte, mode os.FileMode) error {
	if cur, err := os.ReadFile(path); err == nil && string(cur) == string(b) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return fmt.Errorf("writing %s failed: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("writing %s failed: %w", path, err)
	}
	return nil
}

// Apply replaces the route set, certificates, ACME settings, upstream
// answers and TCP proxies. Caddy's /load is called only when the rendered
// config differs from the one it runs.
func (g *Ingress) Apply(ctx context.Context, in proto.Ingress, tcp []proto.TCPProxy) error {
	var errs []error
	if err := g.ups.start(ctx, g.sockets, g.o.ResolverAddr); err != nil {
		errs = append(errs, err)
	}
	t, uerrs := upstreamTable(in.Upstreams)
	errs = append(errs, uerrs...)
	g.ups.replace(t)
	if in.Private {
		tcp = nil // a TCP proxy would be a public listener
	}
	if err := g.tcp.apply(tcp); err != nil {
		errs = append(errs, err)
	}

	cfg, rerrs := renderCaddy(g.params, in)
	errs = append(errs, rerrs...)
	routes, _ := normalizeRoutes(in)
	hosts := map[string]string{}
	tlsDomains := map[string]string{}
	for _, r := range routes {
		if r.ServiceID != "" {
			hosts[r.Domain] = r.ServiceID
		}
		if r.tls != "off" && r.tls != "pending" {
			tlsDomains[r.Domain] = r.tls
		}
	}
	g.mu.Lock()
	g.hosts = hosts
	g.tlsDomains = tlsDomains
	g.last = in
	g.mu.Unlock()

	if cfg != nil {
		if err := g.loadIfChanged(ctx, cfg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (g *Ingress) loadIfChanged(ctx context.Context, cfg []byte) error {
	sum := sha256.Sum256(cfg)
	h := hex.EncodeToString(sum[:])
	g.mu.Lock()
	loaded := g.loadedHash
	g.mu.Unlock()
	if h == loaded {
		return nil
	}
	if loaded == "" {
		// First apply in this process: Caddy may already run this config (--resume).
		if cur, err := g.admin.config(ctx); err == nil && cur != nil && sameJSON(cur, cfg) {
			g.mu.Lock()
			g.loadedHash = h
			g.mu.Unlock()
			return nil
		}
	}
	if err := g.admin.load(ctx, cfg); err != nil {
		return fmt.Errorf("the new route set was not loaded, so the previous one is still served: %w", err)
	}
	g.mu.Lock()
	g.loadedHash = h
	g.mu.Unlock()
	return nil
}

func upstreamTable(sets []proto.UpstreamSet) (nameTable, []error) {
	t := nameTable{}
	var errs []error
	for _, s := range sets {
		if s.Name == "" {
			errs = append(errs, errors.New("an upstream set has no name and was skipped"))
			continue
		}
		ips, err := parseIPv4s(s.IPs)
		if err != nil {
			errs = append(errs, fmt.Errorf("upstream %s: %w", s.Name, err))
		}
		t[upstreamFQDN(s.Name)] = ips
	}
	return t, errs
}

func parseIPv4s(in []string) ([]netip.Addr, error) {
	var out []netip.Addr
	var bad []string
	for _, s := range in {
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() {
			bad = append(bad, s)
			continue
		}
		out = append(out, a)
	}
	if len(bad) > 0 {
		return out, fmt.Errorf("these addresses are not IPv4 and were left out: %v", bad)
	}
	return out, nil
}

// SetUpstreams changes what the upstream resolver answers for
// "<name>.upstream.5050". Caddy picks it up within a second; it never reloads.
func (g *Ingress) SetUpstreams(name string, ips []string) error {
	if name == "" {
		return errors.New("an upstream name is required")
	}
	addrs, err := parseIPv4s(ips)
	if err != nil {
		return fmt.Errorf("upstream %s was not changed: %w", name, err)
	}
	g.ups.set(name, addrs)
	return nil
}

// HTTPSamples returns the HTTP request counts and latency percentiles per
// service since the previous call, from the access logs AccessLogs read. The
// runtime sends them in Metrics.HTTP every 30 s.
func (g *Ingress) HTTPSamples() []proto.HTTPSample { return g.stats.Flush() }

// AdminLoads reports how many times this process loaded a config into Caddy.
func (g *Ingress) AdminLoads() int64 { return g.admin.loads.Load() }

// Close stops the upstream resolver and the TCP proxy listeners. Caddy keeps
// running with its config, so public routes survive an agent restart.
func (g *Ingress) Close() error {
	g.ups.stop()
	g.tcp.close()
	return nil
}

func (g *Ingress) serviceFor(host string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hosts[host]
}

func (g *Ingress) snapshot() (proto.Ingress, map[string]string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	d := make(map[string]string, len(g.tlsDomains))
	for k, v := range g.tlsDomains {
		d[k] = v
	}
	return g.last, d
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
