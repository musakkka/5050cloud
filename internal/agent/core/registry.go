// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// The internal registry (docs/SPEC.md 3.1, 9.6, 15).
//
// On the control host the agent runs distribution's registry in the
// container 5050-registry with host networking, listening on
// 127.0.0.1:5099 only. The agent itself listens on 127.0.0.1:5000, the
// mesh address and the build network gateway (port 5000) and proxies to
// it. The proxy is where two rules live that htpasswd alone cannot express:
// the pull user may only read, and while garbage collection runs the
// registry is read-only for everybody while pulls keep working.
//
// On other hosts with the build role the agent forwards <build gateway>:5000
// to the control host's registry over the mesh, so BuildKit reaches the
// registry the same way everywhere and the firewall (8.4) only has to
// allow the build network to its own gateway.
const (
	RegistryContainer = "5050-registry"
	// registryBackend is where the registry container listens (loopback only).
	registryBackend = "127.0.0.1:5099"
	// RegistryPort is the port the agent serves the registry on.
	RegistryPort = "5000"
	// registryConfigPath is the config path of registry 3.x images.
	registryConfigPath = "/etc/distribution/config.yml"
	registryAuthPath   = "/etc/distribution/htpasswd"
	buildNetworkName   = "5050-build"
)

// Commands of RegistryJob (internal/cp/orch/data) that the agent handles
// itself. A Job of kind "maintenance" with Cmd ["registry-readonly", "on"]
// or ["registry-readonly", "off"] switches the read-only mode; a Job of kind
// "exec" with ReplicaID "registry" runs its Cmd inside the registry
// container, for example
// ["registry", "garbage-collect", "--delete-untagged", "/etc/distribution/config.yml"].
const (
	RegistryReplicaID   = "registry"
	RegistryReadOnlyCmd = "registry-readonly"
)

// registryFront owns the agent's registry listeners.
type registryFront struct {
	a        *Agent
	mu       sync.Mutex
	mode     string // "proxy" | "forward" | ""
	upstream string // forward mode: the control host registry
	lns      map[string]net.Listener
	srv      *http.Server
	readOnly atomic.Bool
	pullUser atomic.Value // string
	pushUser atomic.Value // string
}

func newRegistryFront(a *Agent) *registryFront {
	f := &registryFront{a: a, lns: map[string]net.Listener{}}
	f.pullUser.Store("")
	f.pushUser.Store("")
	return f
}

// SetReadOnly switches the registry's read-only mode (garbage collection, 9.6).
func (f *registryFront) SetReadOnly(on bool) { f.readOnly.Store(on) }

// ensureRegistry makes the registry container and the agent's listeners
// match the desired state. It runs on every apply.
func (a *Agent) ensureRegistry(ctx context.Context, ds *proto.DesiredState) error {
	control := hasRole(ds.Host, "control")
	build := hasRole(ds.Host, "build")
	if !control {
		if build && ds.Host.RegistryPush != nil && ds.Host.RegistryPush.Address != "" {
			return a.regFront.reconcile(ctx, "forward", ds.Host.RegistryPush.Address, a.registryAddrs(ctx, ds, false))
		}
		return a.regFront.reconcile(ctx, "", "", nil)
	}
	pull := ds.Host.RegistryPull
	if pull.Username == "" || pull.Password == "" {
		return errors.New("the control plane sent no registry credentials, so the registry was not started")
	}
	var push proto.Registry
	if ds.Host.RegistryPush != nil {
		push = *ds.Host.RegistryPush
	}
	a.regFront.pullUser.Store(pull.Username)
	a.regFront.pushUser.Store(push.Username)
	if err := a.ensureRegistryContainer(ctx, ds.Host, pull, push); err != nil {
		return err
	}
	return a.regFront.reconcile(ctx, "proxy", registryBackend, a.registryAddrs(ctx, ds, true))
}

// registryAddrs lists the addresses the agent serves the registry on.
func (a *Agent) registryAddrs(ctx context.Context, ds *proto.DesiredState, control bool) []string {
	var out []string
	if control {
		out = append(out, net.JoinHostPort("127.0.0.1", RegistryPort))
		if ds.Mesh.Enabled {
			if p, err := netip.ParsePrefix(ds.Mesh.Self.Address); err == nil {
				out = append(out, net.JoinHostPort(p.Addr().String(), RegistryPort))
			} else if ip, err := netip.ParseAddr(ds.Mesh.Self.Address); err == nil {
				out = append(out, net.JoinHostPort(ip.String(), RegistryPort))
			}
		}
	}
	if hasRole(ds.Host, "build") {
		if gw, err := a.eng.NetworkGateway(ctx, buildNetworkName); err == nil && gw != "" {
			out = append(out, net.JoinHostPort(gw, RegistryPort))
		}
	}
	sort.Strings(out)
	return out
}

// registryFiles writes config.yml and htpasswd and returns their paths and
// a fingerprint of everything that requires a container restart.
func (a *Agent) registryFiles(pull, push proto.Registry) (cfgPath, authPath, fingerprint string, err error) {
	dir := filepath.Join(a.o.DataDir, "registry-config")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", fmt.Errorf("could not create %s: %w", dir, err)
	}
	secret, err := loadOrCreateSecret(filepath.Join(dir, "http-secret"))
	if err != nil {
		return "", "", "", err
	}
	cfg := RegistryConfig(registryBackend, secret)
	cfgPath = filepath.Join(dir, "config.yml")
	if err := writeIfChanged(cfgPath, []byte(cfg), 0o600); err != nil {
		return "", "", "", err
	}
	users := map[string]string{pull.Username: pull.Password}
	if push.Username != "" {
		users[push.Username] = push.Password
	}
	authPath = filepath.Join(dir, "htpasswd")
	if !htpasswdMatches(authPath, users) {
		b, err := Htpasswd(users)
		if err != nil {
			return "", "", "", err
		}
		if err := writeAtomic(authPath, b, 0o600); err != nil {
			return "", "", "", err
		}
	}
	h := sha256.New()
	h.Write([]byte(cfg))
	names := make([]string, 0, len(users))
	for u := range users {
		names = append(names, u)
	}
	sort.Strings(names)
	for _, u := range names {
		fmt.Fprintf(h, "\x00%s\x00%s", u, users[u])
	}
	return cfgPath, authPath, hex.EncodeToString(h.Sum(nil)[:12]), nil
}

func (a *Agent) ensureRegistryContainer(ctx context.Context, host proto.HostConfig, pull, push proto.Registry) error {
	img := pinnedImage(host, ImageKeyRegistry, RegistryImage)
	cfgPath, authPath, fp, err := a.registryFiles(pull, push)
	if err != nil {
		return err
	}
	dataDir := filepath.Join(a.o.DataDir, "registry")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", dataDir, err)
	}
	want := fp + "-" + shortHash(img)
	c, err := a.eng.Inspect(ctx, RegistryContainer)
	switch {
	case err == nil && c.Labels[engine.LabelInfra] != RegistryReplicaID:
		return fmt.Errorf("a container named %s exists that the agent did not create; remove it so the registry can start", RegistryContainer)
	case err == nil && c.Labels[engine.LabelConfig] == want:
		if !c.Running() {
			if err := a.eng.Start(ctx, c.ID); err != nil {
				return fmt.Errorf("the registry container could not start: %w", err)
			}
		}
		return nil
	case err == nil:
		a.log.Info("replacing the registry container (image or settings changed)")
		if err := a.eng.Stop(ctx, c.ID, 10*time.Second); err != nil && !errors.Is(err, engine.ErrNotFound) {
			return fmt.Errorf("the old registry container could not be stopped: %w", err)
		}
		if err := a.eng.Remove(ctx, c.ID, true); err != nil && !errors.Is(err, engine.ErrNotFound) {
			return fmt.Errorf("the old registry container could not be removed: %w", err)
		}
	case !errors.Is(err, engine.ErrNotFound):
		return fmt.Errorf("the registry container could not be inspected: %w", err)
	}
	have, err := a.eng.ImageExists(ctx, img)
	if err != nil {
		return err
	}
	if !have {
		a.log.Info("pulling the registry image", "image", img)
		if err := a.eng.Pull(ctx, img, nil); err != nil {
			return errors.New(PullDiagnosis(img, err))
		}
	}
	id, err := a.eng.Create(ctx, engine.Spec{
		Name:    RegistryContainer,
		Image:   img,
		Cmd:     []string{registryConfigPath},
		Network: "host",
		Labels:  map[string]string{engine.LabelInfra: RegistryReplicaID, engine.LabelConfig: want},
		Mounts: []engine.Mount{
			{Type: engine.MountBind, Source: dataDir, Target: "/var/lib/registry"},
			{Type: engine.MountBind, Source: cfgPath, Target: registryConfigPath, ReadOnly: true},
			{Type: engine.MountBind, Source: authPath, Target: registryAuthPath, ReadOnly: true},
		},
		Restart:         engine.Restart{Name: "unless-stopped"},
		StopTimeoutSec:  10,
		LogMaxSize:      "20m",
		LogMaxFile:      3,
		NoNewPrivileges: true,
	})
	if err != nil {
		return fmt.Errorf("the registry container could not be created: %w", err)
	}
	if err := a.eng.Start(ctx, id); err != nil {
		return fmt.Errorf("the registry container could not start: %w", err)
	}
	return nil
}

// RegistryConfig renders the registry's config.yml. Deletes are enabled
// for retention, URLs are relative so the proxy can sit in front, and
// there is no blob descriptor cache, so a garbage collection never leaves
// stale entries that would make a later push skip a deleted layer.
func RegistryConfig(addr, secret string) string {
	return "# Written by the 5050cloud agent. Changes are overwritten.\n" +
		"version: 0.1\n" +
		"log:\n  level: warn\n  formatter: text\n" +
		"storage:\n" +
		"  filesystem:\n    rootdirectory: /var/lib/registry\n" +
		"  delete:\n    enabled: true\n" +
		"  maintenance:\n    uploadpurging:\n      enabled: true\n      age: 168h\n      interval: 24h\n      dryrun: false\n" +
		"http:\n" +
		"  addr: " + addr + "\n" +
		"  secret: " + secret + "\n" +
		"  relativeurls: true\n" +
		"  headers:\n    X-Content-Type-Options: [nosniff]\n" +
		"auth:\n  htpasswd:\n    realm: 5050cloud\n    path: " + registryAuthPath + "\n"
}

// Htpasswd renders a bcrypt htpasswd file for the users, sorted by name.
func Htpasswd(users map[string]string) ([]byte, error) {
	names := make([]string, 0, len(users))
	for u := range users {
		if u == "" || strings.ContainsAny(u, ":\n") {
			return nil, fmt.Errorf("the registry user name %q is not valid", u)
		}
		names = append(names, u)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, u := range names {
		h, err := bcrypt.GenerateFromPassword([]byte(users[u]), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("could not hash the registry password for %s: %w", u, err)
		}
		b.WriteString(u + ":" + string(h) + "\n")
	}
	return []byte(b.String()), nil
}

// htpasswdMatches reports whether the file holds exactly these users with
// these passwords (bcrypt salts differ per render, so compare, not equal).
func htpasswdMatches(path string, users map[string]string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		u, h, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok {
			continue
		}
		pw, known := users[u]
		if !known || bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)) != nil {
			return false
		}
		seen++
	}
	return seen == len(users)
}

func loadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := writeAtomic(path, []byte(s+"\n"), 0o600); err != nil {
		return "", err
	}
	return s, nil
}

func writeIfChanged(path string, data []byte, mode os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	return writeAtomic(path, data, mode)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// reconcile opens and closes listeners so exactly addrs are served in the
// given mode. A listener that cannot be opened (an address not yet on the
// host) is reported and retried on the next pass.
func (f *registryFront) reconcile(ctx context.Context, mode, upstream string, addrs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if mode != f.mode || upstream != f.upstream {
		f.closeAllLocked()
		f.mode, f.upstream = mode, upstream
		if mode == "proxy" {
			f.srv = &http.Server{Handler: f.proxyHandler(upstream), ReadHeaderTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
		}
	}
	if mode == "" {
		return nil
	}
	want := map[string]bool{}
	for _, a := range addrs {
		want[a] = true
	}
	for addr, ln := range f.lns {
		if !want[addr] {
			_ = ln.Close()
			delete(f.lns, addr)
		}
	}
	var errs []string
	for _, addr := range addrs {
		if _, ok := f.lns[addr]; ok {
			continue
		}
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("could not listen on %s: %v", addr, err))
			continue
		}
		f.lns[addr] = ln
		switch mode {
		case "proxy":
			go func(srv *http.Server) { _ = srv.Serve(ln) }(f.srv)
		case "forward":
			go f.forward(ln, upstream)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (f *registryFront) closeAllLocked() {
	for addr, ln := range f.lns {
		_ = ln.Close()
		delete(f.lns, addr)
	}
	if f.srv != nil {
		_ = f.srv.Close()
		f.srv = nil
	}
}

// close stops every listener (agent shutdown).
func (f *registryFront) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeAllLocked()
	f.mode = ""
}

// registryError writes an error in the registry API's JSON shape, which
// docker and buildx print to the user.
func registryError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"code": code, "message": msg}}})
}

func (f *registryFront) proxyHandler(backend string) http.Handler {
	target := &url.URL{Scheme: "http", Host: backend}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host
		},
		FlushInterval: -1,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			MaxIdleConnsPerHost: 64,
			IdleConnTimeout:     90 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			registryError(w, http.StatusBadGateway, "UNAVAILABLE", "The registry on the control host is not answering. The agent restarts it; try again in a minute.")
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if f.readOnly.Load() {
				registryError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "The registry is read-only while garbage collection runs. Try again in a few minutes.")
				return
			}
			pull, _ := f.pullUser.Load().(string)
			push, _ := f.pushUser.Load().(string)
			if u, _, ok := r.BasicAuth(); ok && u == pull && pull != push {
				registryError(w, http.StatusForbidden, "DENIED", "This registry credential can only pull images.")
				return
			}
		}
		rp.ServeHTTP(w, r)
	})
}

// forward copies TCP connections from ln to upstream until ln closes.
func (f *registryFront) forward(ln net.Listener, upstream string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			u, err := net.DialTimeout("tcp", upstream, 10*time.Second)
			if err != nil {
				f.a.log.Warn("could not reach the registry on the control host", "upstream", upstream, "err", err)
				return
			}
			defer u.Close()
			pipe(c, u)
		}(c)
	}
}

// pipe copies both ways and half-closes, until both directions end.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}
