// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration && linux

package build

// Integration tests against the real Docker Engine (make test-linux, or
// `go test -tags integration ./internal/agent/build/` as root on a Linux
// host). They cover the WP4 acceptance list of docs/SPEC.md 19:
//
//   - a Railpack Node repo, a Dockerfile with an ARG, an image import and
//     the railway.json fixture with builder NIXPACKS, each pushed to a
//     local registry:2 with htpasswd auth;
//   - the second Railpack build reuses the cache and is at least 40% faster;
//   - no variable value in the image config, history or layers, in the log
//     or in any process's argv while a build runs;
//   - the sandbox: RUN steps cannot reach the host loopback, cannot ask for
//     the host network, and (with FIFTY_TEST_NETW=1, where the agent's
//     firewall really applies) cannot reach the gateway, the mesh registry
//     or the metadata address either;
//   - abort stops a build within 5 seconds.
//
// They need internet access (ghcr.io and Docker Hub) and take a few
// minutes the first time.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"golang.org/x/crypto/bcrypt"

	"github.com/musakkka/5050cloud/internal/agent/netw"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Pinned test images (each at least two weeks old when pinned).
const (
	itRegistryImage = "registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
	itAlpineImage   = "alpine:3.20.3@sha256:1e42bbe2508154c9126d48c2b8a75420c3544343bf86fd041fb7527e017a4b4a"
	// itDockerCLIImage provides the docker CLI and the buildx plugin when
	// the test machine has none (the golang image of make test-linux).
	itDockerCLIImage = "docker:28.5.1-cli@sha256:9190b0613792e658a7783cf14b2d5ace5941bb68ede7276922ea36ee457d76ad"

	itNetwork   = "5050-build-it"
	itRegistry  = "5050-it-registry"
	itListener  = "5050-it-listener"
	itPushUser  = "push"
	itSecretVal = "it-secret-Zq8vN3pL0xW2"
	itAdminPort = "2019"
)

type itEnv struct {
	docker   *client.Client
	b        *builder
	reg      proto.Registry // as the test process reaches it
	endpoint string         // as BuildKit reaches it
	gateway  string         // the build network gateway
	srv      *tarServer
	useNetw  bool
}

func TestIntegrationBuilder(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the integration tests run as root (make test-linux)")
	}
	ctx := context.Background()
	env := setupIntegration(t, ctx)

	var railpackFirst time.Duration
	t.Run("RailpackNode", func(t *testing.T) {
		railpackFirst = env.railpackNode(t, ctx, "dep_it_rp1")
	})
	t.Run("RailpackCachedRebuild", func(t *testing.T) {
		if railpackFirst == 0 {
			t.Skip("the first Railpack build failed")
		}
		second := env.railpackNode(t, ctx, "dep_it_rp2")
		t.Logf("first build %s, cached rebuild %s", railpackFirst, second)
		if float64(second) > 0.6*float64(railpackFirst) {
			t.Errorf("the cached rebuild took %s, more than 60%% of the first build (%s)", second, railpackFirst)
		}
	})
	var dockerfileImage string
	t.Run("DockerfileWithArg", func(t *testing.T) {
		dockerfileImage = env.dockerfileWithArg(t, ctx)
	})
	t.Run("ImageImport", func(t *testing.T) {
		if dockerfileImage == "" {
			t.Skip("the Dockerfile build failed, so there is nothing to import")
		}
		env.imageImport(t, ctx, dockerfileImage)
	})
	t.Run("RailwayJSONNixpacks", func(t *testing.T) { env.railwayJSONNixpacks(t, ctx) })
	t.Run("Sandbox", func(t *testing.T) { env.sandbox(t, ctx) })
	t.Run("HostNetworkRefused", func(t *testing.T) { env.hostNetworkRefused(t, ctx) })
	t.Run("Abort", func(t *testing.T) { env.abort(t, ctx) })
	t.Run("Prune", func(t *testing.T) {
		if err := env.b.Prune(ctx, 10<<30); err != nil {
			t.Errorf("prune: %v", err)
		}
	})
}

// ---------------------------------------------------------------- cases

func (e *itEnv) job(id, path string) proto.BuildJob {
	return proto.BuildJob{
		BuildID:    id,
		ServiceID:  "svc_it_" + strings.TrimPrefix(path, "/"),
		TarballURL: e.srv.url + path,
		Builder:    "auto",
		Image:      fmt.Sprintf("%s/svc_it/%s:%s", e.reg.Address, strings.TrimPrefix(path, "/"), id),
		CacheRef:   fmt.Sprintf("%s/svc_it/%s:buildcache", e.reg.Address, strings.TrimPrefix(path, "/")),
		Platform:   "linux/" + runtime.GOARCH,
		TimeoutSec: 900,
		Env: map[string]string{
			"SECRET_TOKEN":             itSecretVal,
			"RAILWAY_ENVIRONMENT_NAME": "production",
		},
	}
}

func (e *itEnv) railpackNode(t *testing.T, ctx context.Context, id string) time.Duration {
	e.srv.add("/node", map[string]string{
		"package.json": `{"name":"it-node","version":"1.0.0","private":true,"scripts":{"start":"node index.js"},"engines":{"node":"22"}}`,
		"index.js":     "require('http').createServer((q, s) => s.end('ok')).listen(process.env.PORT || 3000)\n",
	})
	job := e.job(id, "/node")
	watch := e.watchArgv(ctx, itSecretVal)
	log, lines := collectLog(t)
	start := time.Now()
	res := e.b.Build(ctx, job, e.reg, log)
	took := time.Since(start)
	leaks := watch()
	if !res.OK {
		t.Fatalf("Railpack build failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	if res.Provider != "node" || !res.IsNode || res.StartCommand == "" {
		t.Errorf("provider %q, isNode %v, start %q", res.Provider, res.IsNode, res.StartCommand)
	}
	if !strings.HasPrefix(res.Digest, "sha256:") {
		t.Errorf("digest = %q", res.Digest)
	}
	for _, l := range leaks {
		t.Errorf("a process had the variable value on its argv: %s", l)
	}
	e.assertNoSecret(t, job.Image, lines())
	return took
}

func (e *itEnv) dockerfileWithArg(t *testing.T, ctx context.Context) string {
	// The Dockerfile must not contain the secret itself: its text lands in the
	// image history. It compares a hash of the mounted file instead.
	sum := sha256.Sum256([]byte(itSecretVal))
	secretHash := hex.EncodeToString(sum[:])
	e.srv.add("/dockerfile", map[string]string{
		"Dockerfile": "FROM " + itAlpineImage + "\n" +
			"ARG GREETING\n" +
			"RUN echo \"$GREETING\" > /greeting\n" +
			"RUN --mount=type=secret,id=SECRET_TOKEN test \"$(sha256sum < /run/secrets/SECRET_TOKEN | cut -d' ' -f1)\" = \"" + secretHash + "\" && echo secret-mounted\n" +
			"CMD [\"cat\", \"/greeting\"]\n",
	})
	job := e.job("dep_it_df1", "/dockerfile")
	job.Env["GREETING"] = "hello-from-arg"
	job.Env["NOT_AN_ARG"] = "never-visible-value-77"
	watch := e.watchArgv(ctx, itSecretVal, "hello-from-arg", "never-visible-value-77")
	log, lines := collectLog(t)
	res := e.b.Build(ctx, job, e.reg, log)
	leaks := watch()
	if !res.OK {
		t.Fatalf("Dockerfile build failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	out := strings.Join(lines(), "\n")
	for _, want := range []string{"Using detected Dockerfile", "Variables visible at build time (named by ARG): GREETING", "Variables mounted as build secrets: SECRET_TOKEN", "secret-mounted"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q", want)
		}
	}
	for _, l := range leaks {
		t.Errorf("a process had a variable value on its argv: %s", l)
	}
	files := e.imageFiles(t, job.Image)
	if got := strings.TrimSpace(files["greeting"]); got != "hello-from-arg" {
		t.Errorf("/greeting = %q, want the ARG value", got)
	}
	e.assertNoSecret(t, job.Image, lines(), "never-visible-value-77")
	return job.Image
}

func (e *itEnv) imageImport(t *testing.T, ctx context.Context, src string) {
	job := proto.BuildJob{
		BuildID:         "dep_it_imp1",
		ServiceID:       "svc_it_import",
		SourceImage:     src,
		SourceImageAuth: &proto.RegistryAuth{Server: e.reg.Address, Username: e.reg.Username, Password: e.reg.Password},
		Image:           e.reg.Address + "/svc_it/import:dep_it_imp1",
		Platform:        "linux/" + runtime.GOARCH,
	}
	log, lines := collectLog(t)
	res := e.b.Build(ctx, job, e.reg, log)
	if !res.OK {
		t.Fatalf("import failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	want := e.digestOf(t, src)
	if res.Digest != want.String() || e.digestOf(t, job.Image) != want {
		t.Errorf("imported digest %s, source %s", res.Digest, want)
	}
	if strings.Contains(strings.Join(lines(), "\n"), e.reg.Password) {
		t.Errorf("the registry password reached the log")
	}

	// A missing image gets the catalog sentence.
	job.BuildID = "dep_it_imp2"
	job.SourceImage = e.reg.Address + "/svc_it/does-not-exist:1"
	res = e.b.Build(ctx, job, e.reg, func(string) {})
	if res.OK || !strings.HasSuffix(res.Diagnosis, "was not found.") {
		t.Errorf("missing image diagnosis = %q", res.Diagnosis)
	}
}

func (e *itEnv) railwayJSONNixpacks(t *testing.T, ctx context.Context) {
	e.srv.add("/railwayjson", map[string]string{
		"railway.json": `{
  "$schema": "https://railway.com/railway.schema.json",
  "build": { "builder": "NIXPACKS" },
  "deploy": {
    "startCommand": "node server.js",
    "preDeployCommand": ["node -e \"console.log('migrate')\""],
    "healthcheckPath": "/health",
    "restartPolicyType": "ON_FAILURE"
  },
  "environments": {
    "production": { "deploy": { "startCommand": "node server.js --production" } }
  }
}
`,
		"package.json": `{"name":"it-railway-json","version":"1.0.0","private":true}`,
		"server.js":    "require('http').createServer((q, s) => s.end('ok')).listen(process.env.PORT || 3000)\n",
	})
	job := e.job("dep_it_rj1", "/railwayjson")
	log, lines := collectLog(t)
	res := e.b.Build(ctx, job, e.reg, log)
	if !res.OK {
		t.Fatalf("railway.json build failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	out := strings.Join(lines(), "\n")
	if !strings.Contains(out, "Nixpacks config detected, building with Railpack.") {
		t.Errorf("log lacks the Nixpacks notice")
	}
	if res.ConfigFile == nil || res.ConfigFile.Path != "railway.json" || res.ConfigFile.Lines["deploy.startCommand"] == 0 {
		t.Errorf("config overlay = %+v", res.ConfigFile)
	}
	if res.StartCommand != "node server.js --production" {
		t.Errorf("start command = %q, want the production override", res.StartCommand)
	}
}

// sandbox runs E38's Dockerfile: every probe must fail, while the internet
// stays reachable.
func (e *itEnv) sandbox(t *testing.T, ctx context.Context) {
	// Positive control: from the host network the listener answers, so a
	// blocked probe means the sandbox blocked it.
	if code := e.runOnce(t, ctx, "host", "nc", "-z", "-w", "3", "127.0.0.1", itAdminPort); code != 0 {
		t.Fatalf("the host listener on 127.0.0.1:%s is not up (exit %d)", itAdminPort, code)
	}
	probes := []string{"127.0.0.1:" + itAdminPort}
	if e.useNetw {
		mesh := os.Getenv("FIFTY_TEST_MESH_REGISTRY")
		if mesh == "" {
			mesh = "100.96.0.1:5000"
		}
		probes = append(probes, e.gateway+":"+itAdminPort, mesh, netw.MetadataAddr+":80")
	} else {
		t.Logf("FIFTY_TEST_NETW is not set: the gateway, mesh and metadata probes need the agent's firewall on a real host and are skipped here")
	}
	// The probe addresses are literals in the Dockerfile, not variables: the
	// build log masks every variable value, which would hide the addresses.
	e.srv.add("/sandbox", map[string]string{
		"Dockerfile": sandboxDockerfile(probes, "1.1.1.1:443"),
	})
	job := e.job("dep_it_sb1", "/sandbox")
	job.NoCache = true
	log, lines := collectLog(t)
	res := e.b.Build(ctx, job, e.reg, log)
	out := strings.Join(lines(), "\n")
	if !res.OK {
		t.Fatalf("sandbox build failed: %s\n%s", res.Diagnosis, out)
	}
	for _, p := range probes {
		if !strings.Contains(out, "SANDBOX-BLOCKED "+p) {
			t.Errorf("probe %s was not blocked", p)
		}
	}
	if strings.Contains(out, "SANDBOX-REACHED") {
		t.Errorf("a RUN step reached something of ours:\n%s", out)
	}
	if !strings.Contains(out, "SANDBOX-INTERNET ok") {
		t.Errorf("RUN steps must still reach the internet")
	}
}

// sandboxDockerfile is the E38 probe: it reports each address it tries
// and fails only when a probe got through. The result words are split in the
// command text ("RE""ACHED") so that the build log, which echoes the command,
// only contains them when a step really printed them.
func sandboxDockerfile(probes []string, internet string) string {
	return "FROM " + itAlpineImage + "\n" +
		`RUN reached=0; for t in ` + strings.Join(probes, " ") + `; do h="${t%:*}"; p="${t##*:}"; ` +
		`if nc -z -w 3 "$h" "$p"; then echo "SANDBOX-RE""ACHED $t"; reached=1; else echo "SANDBOX-BLOCKED $t"; fi; done; ` +
		`h="` + strings.Split(internet, ":")[0] + `"; p="` + strings.Split(internet, ":")[1] + `"; if nc -z -w 5 "$h" "$p"; then echo "SANDBOX-INTERNET ok"; fi; ` +
		`test "$reached" = 0` + "\n"
}

func (e *itEnv) hostNetworkRefused(t *testing.T, ctx context.Context) {
	e.srv.add("/hostnet", map[string]string{
		"Dockerfile": "FROM " + itAlpineImage + "\nRUN --network=host nc -z -w 3 127.0.0.1 " + itAdminPort + "\n",
	})
	job := e.job("dep_it_hn1", "/hostnet")
	job.NoCache = true
	log, lines := collectLog(t)
	res := e.b.Build(ctx, job, e.reg, log)
	if res.OK {
		t.Fatalf("a RUN --network=host step must be refused:\n%s", strings.Join(lines(), "\n"))
	}
	if !strings.Contains(strings.Join(lines(), "\n"), "network.host") {
		t.Errorf("the refusal should name the network.host entitlement")
	}
}

func (e *itEnv) abort(t *testing.T, ctx context.Context) {
	e.srv.add("/slow", map[string]string{
		"Dockerfile": "FROM " + itAlpineImage + "\nRUN echo abort-test-started && sleep 317\n",
	})
	job := e.job("dep_it_ab1", "/slow")
	job.NoCache = true
	bctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started := make(chan struct{})
	var once sync.Once
	done := make(chan proto.BuildResult, 1)
	go func() {
		done <- e.b.Build(bctx, job, e.reg, func(l string) {
			if strings.Contains(l, "abort-test-started") {
				once.Do(func() { close(started) })
			}
		})
	}()
	select {
	case <-started:
	case res := <-done:
		t.Fatalf("the slow build ended before it was aborted: %s", res.Diagnosis)
	case <-time.After(5 * time.Minute):
		t.Fatal("the slow build never started")
	}
	time.Sleep(time.Second) // let the RUN step settle into sleep
	t0 := time.Now()
	cancel()
	var res proto.BuildResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Build did not return within 5 s of the abort")
	}
	if took := time.Since(t0); took > 5*time.Second {
		t.Errorf("abort took %s", took)
	}
	if res.OK || res.Diagnosis != sentenceAborted {
		t.Errorf("aborted result = %+v", res)
	}
	// The RUN step itself must be gone from BuildKit within the same budget.
	deadline := t0.Add(5 * time.Second)
	for e.buildkitRuns(ctx, "sleep 317") {
		if time.Now().After(deadline) {
			t.Errorf("the RUN step still runs in BuildKit 5 s after the abort")
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ---------------------------------------------------------------- setup

func setupIntegration(t *testing.T, ctx context.Context) *itEnv {
	t.Helper()
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	if _, err := dc.Ping(ctx); err != nil {
		t.Skipf("no Docker Engine: %v", err)
	}
	t.Cleanup(func() { _ = dc.Close() })
	e := &itEnv{docker: dc, useNetw: os.Getenv("FIFTY_TEST_NETW") == "1"}

	dataDir := t.TempDir()
	dockerCLI := ensureDockerCLI(t, ctx, filepath.Join(dataDir, "build", "docker"))

	for _, img := range []string{itRegistryImage, itAlpineImage} {
		pullImage(t, ctx, dc, img)
	}

	netName := itNetwork
	var nw agentapiNetwork
	if e.useNetw {
		n := netw.New(netw.Options{DataDir: dataDir, DockerHost: os.Getenv("DOCKER_HOST")})
		nw = n
		name, err := n.BuildNetwork(ctx)
		if err != nil {
			t.Fatalf("netw.BuildNetwork: %v", err)
		}
		netName = name
	} else {
		removeContainer(ctx, dc, itRegistry)
		_ = dc.NetworkRemove(ctx, itNetwork)
		if _, err := dc.NetworkCreate(ctx, itNetwork, network.CreateOptions{Driver: "bridge"}); err != nil {
			t.Fatalf("create network: %v", err)
		}
		t.Cleanup(func() { _ = dc.NetworkRemove(context.Background(), itNetwork) })
	}
	ni, err := dc.NetworkInspect(ctx, netName, network.InspectOptions{})
	if err != nil || len(ni.IPAM.Config) == 0 {
		t.Fatalf("inspect %s: %v", netName, err)
	}
	e.gateway = ni.IPAM.Config[0].Gateway

	password := "it-push-" + fmt.Sprint(time.Now().UnixNano())
	regIP := e.startRegistry(t, ctx, netName, password)
	if e.useNetw {
		// The production layout: the registry on the gateway port the
		// firewall opens for the build network.
		e.endpoint = e.gateway + ":5000"
	} else {
		e.endpoint = regIP + ":5000"
		e.joinNetwork(t, ctx, netName)
	}
	e.reg = proto.Registry{Address: e.endpoint, Username: itPushUser, Password: password, Insecure: true}
	waitTCP(t, e.endpoint, 30*time.Second)
	e.startListener(t, ctx)

	e.srv = newTarServer(t)
	opts := Options{
		DataDir:          dataDir,
		DockerHost:       dockerHostOr(),
		DockerCLI:        dockerCLI,
		BuildNetworkName: netName,
		BuildRegistry:    e.endpoint,
		MaxParallel:      2,
	}
	if nw != nil {
		opts.Network = nw
	}
	e.b = newBuilder(opts, execRunner{}, nil)
	if err := e.b.EnsureRunning(ctx, ""); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	t.Cleanup(func() {
		cmd := exec.Command(dockerCLI, "buildx", "rm", "--force", builderName)
		cmd.Env = e.b.baseEnv()
		_ = cmd.Run()
	})
	// A second call with the same settings must leave the builder alone.
	before := e.containerStarted(t, ctx, builderContainer)
	if err := e.b.EnsureRunning(ctx, ""); err != nil {
		t.Fatalf("EnsureRunning again: %v", err)
	}
	if after := e.containerStarted(t, ctx, builderContainer); after != before {
		t.Errorf("EnsureRunning restarted an unchanged builder (%s, then %s)", before, after)
	}
	e.assertBuilderSandboxed(t, ctx, netName)
	return e
}

// agentapiNetwork is the slice of agentapi.Network the test passes on.
type agentapiNetwork interface {
	Apply(context.Context, proto.HostConfig, proto.Mesh, []proto.Network, []proto.DNSEntry) error
	PublicKey() (string, error)
	DNSServer(string) string
	AllowEgress(context.Context, string, proto.Network) error
	BuildNetwork(context.Context) (string, error)
}

func dockerHostOr() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	return "unix:///var/run/docker.sock"
}

// assertBuilderSandboxed checks the BuildKit container EnsureRunning made.
func (e *itEnv) assertBuilderSandboxed(t *testing.T, ctx context.Context, netName string) {
	ct, err := e.docker.ContainerInspect(ctx, builderContainer)
	if err != nil {
		t.Fatalf("inspect the BuildKit container: %v", err)
	}
	if ct.Config.Image != BuildKitImage {
		t.Errorf("BuildKit image = %s, want the pin %s", ct.Config.Image, BuildKitImage)
	}
	if ct.HostConfig.NetworkMode.IsHost() {
		t.Errorf("BuildKit must not use the host network")
	}
	if _, ok := ct.NetworkSettings.Networks[netName]; !ok {
		t.Errorf("BuildKit is not on %s: %v", netName, ct.NetworkSettings.Networks)
	}
	args := strings.Join(append(ct.Config.Entrypoint, ct.Config.Cmd...), " ")
	// buildx 0.27 and newer append --allow-insecure-entitlement=network.host to
	// every docker-container builder whatever flags are passed. That only lets
	// a client ask for host networking; the agent never passes --allow, and
	// HostNetworkRefused proves such a build is refused. Anything else would be
	// ours and is an error.
	rest := strings.ReplaceAll(args, "--allow-insecure-entitlement=network.host", "")
	if !strings.Contains(args, "--oci-worker-net=bridge") || strings.Contains(rest, "insecure-entitlement") {
		t.Errorf("buildkitd flags = %s", args)
	}
	if ct.HostConfig.Memory <= 0 || ct.HostConfig.CPUQuota <= 0 {
		t.Errorf("BuildKit has no memory or CPU cap: memory %d, quota %d", ct.HostConfig.Memory, ct.HostConfig.CPUQuota)
	}
}

func (e *itEnv) containerStarted(t *testing.T, ctx context.Context, name string) string {
	ct, err := e.docker.ContainerInspect(ctx, name)
	if err != nil {
		t.Fatalf("inspect %s: %v", name, err)
	}
	return ct.State.StartedAt
}

// startRegistry runs registry:2 with htpasswd auth and returns its IP on
// the build network.
func (e *itEnv) startRegistry(t *testing.T, ctx context.Context, netName, password string) string {
	t.Helper()
	removeContainer(ctx, e.docker, itRegistry)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &container.Config{
		Image: itRegistryImage,
		Env: []string{
			"REGISTRY_AUTH=htpasswd",
			"REGISTRY_AUTH_HTPASSWD_REALM=5050cloud test",
			"REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd",
			"REGISTRY_STORAGE_DELETE_ENABLED=true",
		},
	}
	hc := &container.HostConfig{}
	nc := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{netName: {}}}
	if e.useNetw {
		// The production layout: the registry listens on the build
		// network gateway, the one port the firewall opens to builds.
		hc.NetworkMode = "host"
		nc = nil
		cfg.Env = append(cfg.Env, "REGISTRY_HTTP_ADDR="+e.gateway+":5000")
	}
	created, err := e.docker.ContainerCreate(ctx, cfg, hc, nc, nil, itRegistry)
	if err != nil {
		t.Fatalf("create the registry: %v", err)
	}
	t.Cleanup(func() { removeContainer(context.Background(), e.docker, itRegistry) })
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte(itPushUser + ":" + string(hash) + "\n")
	_ = tw.WriteHeader(&tar.Header{Name: "auth/", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: "auth/htpasswd", Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	if err := e.docker.CopyToContainer(ctx, created.ID, "/", &buf, container.CopyToContainerOptions{}); err != nil {
		t.Fatalf("copy htpasswd: %v", err)
	}
	if err := e.docker.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start the registry: %v", err)
	}
	if e.useNetw {
		return e.gateway
	}
	ct, err := e.docker.ContainerInspect(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ct.NetworkSettings.Networks[netName].IPAddress
}

// startListener runs a TCP listener on the host network at port 2019 (the
// port Caddy's admin API would use over TCP), so the sandbox probes have
// something to fail to reach.
func (e *itEnv) startListener(t *testing.T, ctx context.Context) {
	t.Helper()
	removeContainer(ctx, e.docker, itListener)
	cfg := &container.Config{
		Image: itAlpineImage,
		Cmd:   []string{"sh", "-c", "while :; do nc -l -p " + itAdminPort + " </dev/null >/dev/null 2>&1; done"},
	}
	created, err := e.docker.ContainerCreate(ctx, cfg, &container.HostConfig{NetworkMode: "host"}, nil, nil, itListener)
	if err != nil {
		t.Fatalf("create the listener: %v", err)
	}
	t.Cleanup(func() { removeContainer(context.Background(), e.docker, itListener) })
	if err := e.docker.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start the listener: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
}

// runOnce runs a short alpine command and returns its exit code.
func (e *itEnv) runOnce(t *testing.T, ctx context.Context, netMode string, cmd ...string) int64 {
	t.Helper()
	created, err := e.docker.ContainerCreate(ctx, &container.Config{Image: itAlpineImage, Cmd: cmd}, &container.HostConfig{NetworkMode: container.NetworkMode(netMode)}, nil, nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer removeContainer(context.Background(), e.docker, created.ID)
	if err := e.docker.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitC, errC := e.docker.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	select {
	case w := <-waitC:
		return w.StatusCode
	case err := <-errC:
		t.Fatalf("wait: %v", err)
	}
	return -1
}

// joinNetwork connects the test's own container (make test-linux) to the
// build network so it can reach the registry by its IP there.
func (e *itEnv) joinNetwork(t *testing.T, ctx context.Context, netName string) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return // on a host, bridge IPs are reachable directly
	}
	self, _ := os.Hostname()
	if err := e.docker.NetworkConnect(ctx, netName, self, nil); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("connect the test container to %s: %v", netName, err)
	}
	t.Cleanup(func() { _ = e.docker.NetworkDisconnect(context.Background(), netName, self, true) })
}

// buildkitRuns reports whether a process whose command line contains s
// runs in the BuildKit container.
func (e *itEnv) buildkitRuns(ctx context.Context, s string) bool {
	top, err := e.docker.ContainerTop(ctx, builderContainer, []string{"auxww"})
	if err != nil {
		return false
	}
	for _, p := range top.Processes {
		if strings.Contains(strings.Join(p, " "), s) {
			return true
		}
	}
	return false
}

// watchArgv samples every process's argv, here and inside BuildKit, while a
// build runs, and returns the command lines that contained a value.
func (e *itEnv) watchArgv(ctx context.Context, values ...string) func() []string {
	stop := make(chan struct{})
	var mu sync.Mutex
	var leaks []string
	check := func(cmdline string) {
		for _, v := range values {
			if strings.Contains(cmdline, v) {
				mu.Lock()
				leaks = append(leaks, cmdline)
				mu.Unlock()
			}
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			procs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
			for _, p := range procs {
				data, err := os.ReadFile(p)
				if err == nil {
					check(strings.ReplaceAll(string(data), "\x00", " "))
				}
			}
			if top, err := e.docker.ContainerTop(ctx, builderContainer, []string{"auxww"}); err == nil {
				for _, pr := range top.Processes {
					check(strings.Join(pr, " "))
				}
			}
		}
	}()
	return func() []string {
		close(stop)
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		return leaks
	}
}

// assertNoSecret checks the log, the manifest, the config (with its
// history) and every layer of an image for variable values.
func (e *itEnv) assertNoSecret(t *testing.T, ref string, log []string, extra ...string) {
	t.Helper()
	values := append([]string{itSecretVal, e.reg.Password}, extra...)
	joined := strings.Join(log, "\n")
	img := e.image(t, ref)
	raw, err := img.RawConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := img.RawManifest()
	if err != nil {
		t.Fatal(err)
	}
	layers, err := img.Layers()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if strings.Contains(joined, v) {
			t.Errorf("a variable value reached the build log")
		}
		if bytes.Contains(raw, []byte(v)) || bytes.Contains(manifest, []byte(v)) {
			t.Errorf("a variable value is in the image config or history of %s", ref)
		}
	}
	for i, l := range layers {
		rc, err := l.Uncompressed()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if bytes.Contains(data, []byte(v)) {
				t.Errorf("a variable value is in layer %d of %s", i, ref)
			}
		}
	}
}

func (e *itEnv) image(t *testing.T, ref string) v1.Image {
	t.Helper()
	r, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	img, err := remote.Image(r, remote.WithAuth(e.auth()), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
	if err != nil {
		t.Fatalf("read %s: %v", ref, err)
	}
	return img
}

func (e *itEnv) digestOf(t *testing.T, ref string) v1.Hash {
	t.Helper()
	d, err := e.image(t, ref).Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// imageFiles returns the small regular files of an image by path.
func (e *itEnv) imageFiles(t *testing.T, ref string) map[string]string {
	t.Helper()
	rc := mutate.Extract(e.image(t, ref))
	defer rc.Close()
	out := map[string]string{}
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg && h.Size < 4096 {
			data, _ := io.ReadAll(tr)
			out[strings.TrimPrefix(h.Name, "/")] = string(data)
		}
	}
}

func (e *itEnv) auth() authn.Authenticator {
	return authn.FromConfig(authn.AuthConfig{Username: e.reg.Username, Password: e.reg.Password})
}

// ensureDockerCLI returns a docker CLI with the buildx plugin: the host's
// when it has both, else the pinned binaries copied out of the docker CLI
// image (by digest) into the builder's DOCKER_CONFIG plugin directory.
func ensureDockerCLI(t *testing.T, ctx context.Context, dockerConfig string) string {
	t.Helper()
	if p, err := exec.LookPath("docker"); err == nil {
		if exec.Command(p, "buildx", "version").Run() == nil {
			return p
		}
	}
	cache := os.Getenv("FIFTY_TEST_CACHE")
	if cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			dir = os.TempDir()
		}
		cache = filepath.Join(dir, "5050-test")
	}
	dir := filepath.Join(cache, "docker-cli-28.5.1-"+runtime.GOARCH)
	cli := filepath.Join(dir, "docker")
	plugin := filepath.Join(dir, "docker-buildx")
	if _, err := os.Stat(plugin); err != nil {
		ref, err := name.ParseReference(itDockerCLIImage)
		if err != nil {
			t.Fatal(err)
		}
		img, err := remote.Image(ref, remote.WithContext(ctx), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
		if err != nil {
			t.Fatalf("fetch the docker CLI image: %v", err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"usr/local/bin/docker":                               cli,
			"usr/local/libexec/docker/cli-plugins/docker-buildx": plugin,
		}
		rc := mutate.Extract(img)
		tr := tar.NewReader(rc)
		for len(want) > 0 {
			h, err := tr.Next()
			if err != nil {
				_ = rc.Close()
				t.Fatalf("the docker CLI image lacks %v: %v", want, err)
			}
			dst, ok := want[strings.TrimPrefix(h.Name, "/")]
			if !ok || h.Typeflag != tar.TypeReg {
				continue
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst+".tmp", data, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(dst+".tmp", dst); err != nil {
				t.Fatal(err)
			}
			delete(want, strings.TrimPrefix(h.Name, "/"))
		}
		_ = rc.Close()
	}
	plugins := filepath.Join(dockerConfig, "cli-plugins")
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(plugin, filepath.Join(plugins, "docker-buildx")); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	return cli
}

func pullImage(t *testing.T, ctx context.Context, dc *client.Client, ref string) {
	t.Helper()
	rc, err := dc.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		t.Fatalf("pull %s: %v", ref, err)
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

func removeContainer(ctx context.Context, dc *client.Client, id string) {
	_ = dc.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: true})
}

func waitTCP(t *testing.T, addr string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not open: %v", addr, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// collectLog keeps the build log and prints it when the test fails.
func collectLog(t *testing.T) (func(string), func() []string) {
	log, lines := collect()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("build log:\n%s", strings.Join(lines(), "\n"))
		}
	})
	return log, lines
}

// tarServer serves source archives at paths, like the control plane's
// upload endpoint.
type tarServer struct {
	url string
	mu  sync.Mutex
	m   map[string][]byte
	t   *testing.T
}

func newTarServer(t *testing.T) *tarServer {
	s := &tarServer{m: map[string][]byte{}, t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		d, ok := s.m[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(d)
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

func (s *tarServer) add(p string, files map[string]string) {
	data := makeTarGz(s.t, files)
	s.mu.Lock()
	s.m[p] = data
	s.mu.Unlock()
}
