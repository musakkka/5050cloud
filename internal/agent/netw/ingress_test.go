// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/miekg/dns"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// fakeCaddy is an admin API on a unix socket that keeps the loaded config.
type fakeCaddy struct {
	mu     sync.Mutex
	config []byte
	loads  int
	refuse string
	srv    *http.Server
}

func startFakeCaddy(t *testing.T, socket string) *fakeCaddy {
	t.Helper()
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCaddy{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /config/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.config == nil {
			_, _ = w.Write([]byte("null\n"))
			return
		}
		_, _ = w.Write(f.config)
	})
	mux.HandleFunc("POST /load", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.refuse != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + f.refuse + `"}`))
			return
		}
		f.config = b
		f.loads++
	})
	f.srv = &http.Server{Handler: mux}
	go func() { _ = f.srv.Serve(l) }()
	t.Cleanup(func() { _ = f.srv.Close() })
	return f
}

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "nw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func newTestIngress(t *testing.T) (*Ingress, *fakeCaddy, *fakeDocker, *loopSockets) {
	t.Helper()
	dir := shortDir(t)
	g := newIngress(IngressOptions{DataDir: dir, AdminSocket: filepath.Join(dir, "a.sock"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	fc := startFakeCaddy(t, g.o.AdminSocket)
	d := newFakeDocker()
	g.newDock = func() (dockerAPI, error) { return d, nil }
	ls := newLoopSockets()
	g.sockets = ls
	t.Cleanup(func() { _ = g.Close() })
	return g, fc, d, ls
}

func TestIngressApplyLoadsOnlyOnChange(t *testing.T) {
	g, fc, _, ls := newTestIngress(t)
	ctx := context.Background()
	in := testIngress()
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	if fc.loads != 1 {
		t.Fatalf("loads = %d", fc.loads)
	}
	// Upstream changes go to the resolver, not to Caddy.
	in.Upstreams = []proto.UpstreamSet{{Name: "svc-web", IPs: []string{"10.129.4.8"}}}
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	if err := g.SetUpstreams("svc-web", []string{"10.129.4.9", "10.130.4.2"}); err != nil {
		t.Fatal(err)
	}
	if fc.loads != 1 || g.AdminLoads() != 1 {
		t.Fatalf("an upstream change reloaded Caddy: %d loads", fc.loads)
	}
	r := query(t, "udp", ls.addr(DefaultResolverAddr), "svc-web.upstream.5050", dns.TypeA)
	if got := strings.Join(aRecords(r), ","); got != "10.129.4.9,10.130.4.2" {
		t.Errorf("resolver answers = %s", got)
	}
	// A route change reloads.
	in.Routes = append(in.Routes, proto.Route{Domain: "new.example.com", UpstreamName: "svc-new", Port: 80, ServiceID: "svc_new"})
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	if fc.loads != 2 {
		t.Fatalf("loads = %d, want 2", fc.loads)
	}
	if g.serviceFor("new.example.com") != "svc_new" {
		t.Error("host map not updated")
	}
	// A refused config is reported and the next apply tries again.
	in.Routes = in.Routes[:1]
	fc.mu.Lock()
	fc.refuse = "loading new config: bad things"
	fc.mu.Unlock()
	err := g.Apply(ctx, in, nil)
	if err == nil || !strings.Contains(err.Error(), "previous one is still served") || !strings.Contains(err.Error(), "bad things") {
		t.Fatalf("err = %v", err)
	}
	fc.mu.Lock()
	fc.refuse = ""
	fc.mu.Unlock()
	if err := g.Apply(ctx, in, nil); err != nil || fc.loads != 3 {
		t.Fatalf("retry: err %v loads %d", err, fc.loads)
	}
}

func TestIngressResumeSkipsIdenticalLoad(t *testing.T) {
	g, fc, _, _ := newTestIngress(t)
	cfg, _ := renderCaddy(g.params, testIngress())
	// Caddy came back with --resume and already runs the config, formatted differently.
	fc.config = []byte(strings.ReplaceAll(string(cfg), "\n", ""))
	if err := g.Apply(context.Background(), testIngress(), nil); err != nil {
		t.Fatal(err)
	}
	if fc.loads != 0 {
		t.Errorf("an identical resumed config was loaded again (%d loads)", fc.loads)
	}
}

func TestIngressApplyErrors(t *testing.T) {
	g, _, _, _ := newTestIngress(t)
	in := testIngress()
	in.Upstreams = append(in.Upstreams, proto.UpstreamSet{Name: "", IPs: nil}, proto.UpstreamSet{Name: "x", IPs: []string{"fd00::1"}})
	err := g.Apply(context.Background(), in, []proto.TCPProxy{{HostPort: 80}})
	if err == nil {
		t.Fatal("errors not reported")
	}
	for _, want := range []string{"has no name", "not IPv4", "outside 20000 to 29999"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if err := g.SetUpstreams("", nil); err == nil {
		t.Error("empty name accepted")
	}
	if err := g.SetUpstreams("a", []string{"bad"}); err == nil {
		t.Error("bad address accepted")
	}
}

func TestIngressAdminDown(t *testing.T) {
	dir := shortDir(t)
	g := newIngress(IngressOptions{DataDir: dir, AdminSocket: filepath.Join(dir, "none.sock")})
	g.sockets = newLoopSockets()
	defer g.Close()
	err := g.Apply(context.Background(), testIngress(), nil)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureRunningCreatesCaddy(t *testing.T) {
	g, _, d, _ := newTestIngress(t)
	ctx := context.Background()
	if err := g.EnsureRunning(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(d.pulls) != 1 || d.pulls[0] != CaddyImage {
		t.Errorf("pulls = %v", d.pulls)
	}
	c := d.created[CaddyContainer]
	if c.cfg == nil {
		t.Fatal("container not created")
	}
	if c.hc.NetworkMode != "host" || c.hc.OomScoreAdj != -800 || c.hc.RestartPolicy.Name != container.RestartPolicyUnlessStopped {
		t.Errorf("host config = %+v", c.hc)
	}
	if strings.Join(c.cfg.Cmd, " ") != "caddy run --resume --config "+g.bootstrapPath() {
		t.Errorf("cmd = %v", c.cfg.Cmd)
	}
	if len(c.hc.Mounts) != 1 || c.hc.Mounts[0].Type != mount.TypeBind || c.hc.Mounts[0].Source != g.params.Dir || c.hc.Mounts[0].Target != g.params.Dir {
		t.Errorf("mounts = %+v", c.hc.Mounts)
	}
	fi, err := os.Stat(g.params.Dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("caddy dir mode = %v %v", fi.Mode().Perm(), err)
	}
	if b, err := os.ReadFile(g.bootstrapPath()); err != nil || !strings.Contains(string(b), "|0600") {
		t.Errorf("bootstrap = %s %v", b, err)
	}
	// Running with the same image: nothing happens.
	creates := len(d.creates)
	if err := g.EnsureRunning(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(d.creates) != creates || len(d.pulls) != 1 {
		t.Error("a running Caddy was recreated")
	}
	// Stopped: started again, not recreated.
	d.containers[CaddyContainer].State.Running = false
	if err := g.EnsureRunning(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if len(d.creates) != creates || !d.containers[CaddyContainer].State.Running {
		t.Error("a stopped Caddy was not started in place")
	}
	// A new image: pulled first, then replaced.
	if err := g.EnsureRunning(ctx, "mirror.example/caddy:2.11.7"); err != nil {
		t.Fatal(err)
	}
	if d.pulls[len(d.pulls)-1] != "mirror.example/caddy:2.11.7" || d.created[CaddyContainer].cfg.Image != "mirror.example/caddy:2.11.7" {
		t.Error("image upgrade did not recreate the container")
	}
	// A failed pull leaves the running container alone.
	d.pullBody = `{"error":"manifest unknown"}` + "\n"
	err = g.EnsureRunning(ctx, "mirror.example/caddy:9.9.9")
	if err == nil || !strings.Contains(err.Error(), "manifest unknown") {
		t.Fatalf("err = %v", err)
	}
	if d.created[CaddyContainer].cfg.Image != "mirror.example/caddy:2.11.7" {
		t.Error("a failed pull replaced the running Caddy")
	}
	d.createErr = errors.New("no space left on device")
	delete(d.containers, CaddyContainer)
	if err := g.EnsureRunning(ctx, "mirror.example/caddy:2.11.7"); err == nil || !strings.Contains(err.Error(), "creating the Caddy container failed") {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------- certificates

func writeCert(t *testing.T, dir, issuer, domain string, notAfter time.Time) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	p := filepath.Join(dir, "certificates", issuer, domain)
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(p, domain+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(filepath.Join(p, domain+".key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600)
}

func TestCertStatusAndIssued(t *testing.T) {
	g, _, _, _ := newTestIngress(t)
	ctx := context.Background()
	in := testIngress()
	if err := g.Apply(ctx, in, nil); err != nil {
		t.Fatal(err)
	}
	store := g.params.storage()
	exp := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Second)
	writeCert(t, store, "acme-v02.api.letsencrypt.org-directory", "web-ab12.203-0-113-5.sslip.io", exp.Add(-24*time.Hour))
	writeCert(t, store, "acme.zerossl.com-v2-dv90", "web-ab12.203-0-113-5.sslip.io", exp) // newer one wins
	writeCert(t, store, "local", "dev.internal.test", exp)
	writeCert(t, store, "acme-v02.api.letsencrypt.org-directory", "console.example.com", time.Now().Add(-time.Hour)) // expired
	if err := os.MkdirAll(filepath.Dir(g.params.tlsLog()), 0o700); err != nil {
		t.Fatal(err)
	}
	logLines := strings.Join([]string{
		`{"level":"error","logger":"tls.obtain","msg":"could not get certificate from issuer","identifier":"api.example.com","error":"HTTP 400 urn:ietf:params:acme:error:dns - DNS problem: NXDOMAIN looking up A for api.example.com"}`,
		`{"level":"error","logger":"tls.obtain","msg":"will retry","identifier":"console.example.com","error":"timeout"}`,
		`{"level":"info","logger":"tls.obtain","msg":"certificate obtained successfully","identifier":"console.example.com"}`,
		`not json`,
	}, "\n") + "\n"
	if err := os.WriteFile(g.params.tlsLog(), []byte(logLines), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := g.CertStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s := st["web-ab12.203-0-113-5.sslip.io"]; s.Status != "active" || s.NotAfter != exp.UnixMilli() {
		t.Errorf("web = %+v, want active until %v", s, exp)
	}
	if s := st["dev.internal.test"]; s.Status != "active" {
		t.Errorf("internal = %+v", s)
	}
	if s := st["api.example.com"]; s.Status != "error" || !strings.Contains(s.Error, "NXDOMAIN") || !strings.HasPrefix(s.Error, "The certificate for api.example.com") {
		t.Errorf("api = %+v", s)
	}
	if s := st["console.example.com"]; s.Status != "issuing" {
		t.Errorf("console (expired, error cleared by a success) = %+v", s)
	}
	if _, ok := st["plain.example.com"]; ok {
		t.Error("a TLS-off domain has a certificate state")
	}

	// IssuedCerts streams ACME certificates once per expiry, never internal ones.
	old := certScanInterval
	certScanInterval = 20 * time.Millisecond
	defer func() { certScanInterval = old }()
	cctx, cancel := context.WithCancel(ctx)
	out := make(chan proto.CertBundle, 8)
	done := make(chan error, 1)
	go func() { done <- g.IssuedCerts(cctx, out) }()
	var got []proto.CertBundle
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case b := <-out:
			got = append(got, b)
		case <-timeout:
			t.Fatalf("got %d bundles", len(got))
		}
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("a certificate was sent twice")
	}
	domains := map[string]bool{}
	for _, b := range got {
		domains[b.Domain] = true
		if !strings.Contains(b.KeyPEM, "PRIVATE KEY") || !strings.Contains(b.CertPEM, "CERTIFICATE") {
			t.Errorf("bundle %s incomplete", b.Domain)
		}
	}
	if !domains["web-ab12.203-0-113-5.sslip.io"] || !domains["console.example.com"] || domains["dev.internal.test"] {
		t.Errorf("bundles = %v", domains)
	}

	// A secondary ingress host reports pushed certificates.
	in.ACMEPrimary = false
	in.Certificates = []proto.CertBundle{{Domain: "api.example.com", NotAfter: exp.UnixMilli(), CertPEM: "x", KeyPEM: "y"}}
	_ = g.Apply(ctx, in, nil)
	st, _ = g.CertStatus(ctx)
	if s := st["api.example.com"]; s.Status != "active" || s.NotAfter != exp.UnixMilli() {
		t.Errorf("pushed = %+v", s)
	}
	if s := st["web-ab12.203-0-113-5.sslip.io"]; s.Status != "issuing" {
		t.Errorf("not pushed yet = %+v", s)
	}
	if s := certErrorSentence("a.b", strings.Repeat("x", 400)); len(s) > 360 {
		t.Error("long errors are not shortened")
	}
}

func TestNewIngressDefaults(t *testing.T) {
	g := NewIngress(IngressOptions{DataDir: "/var/lib/5050"}).(*Ingress)
	if g.o.AdminSocket != "/var/lib/5050/caddy/admin.sock" || g.o.ResolverAddr != "127.0.0.1:5354" || g.o.HTTPPort != 80 || g.o.HTTPSPort != 443 {
		t.Errorf("defaults = %+v", g.o)
	}
}
