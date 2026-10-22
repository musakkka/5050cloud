// SPDX-License-Identifier: AGPL-3.0-only

// Package build implements agentapi.Builder (docs/SPEC.md 8.2): it fetches
// a source, reads the config-as-code file, picks Railpack or the Dockerfile,
// and builds with docker buildx on a sandboxed BuildKit, pushing the image
// and its cache to the internal registry. Image imports are copied by digest
// with go-containerregistry. Variable values never go on argv, into image
// layers or into the log.
package build

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Options configure the builder. DataDir, DockerHost and Tools are wired by
// cmd/5050-agent (see internal/agent/agentapi); the rest are optional.
type Options struct {
	DataDir    string // e.g. /var/lib/5050
	DockerHost string // "unix:///var/run/docker.sock"
	Tools      agentapi.Tools
	// MaxParallel builds on this host; 0 = max(1, CPUs/4).
	MaxParallel int

	// Network, when set, creates the 5050-build bridge and its firewall
	// rules before the BuildKit builder starts (Network.BuildNetwork).
	// Without it the bridge must already exist.
	Network agentapi.Network
	// HostToken returns the agent's current host token, used to download
	// `5050 up` uploads (BuildJob.TarballURL).
	HostToken func() string
	// Images returns HostConfig.Images. The builder reads "buildkit",
	// "railpack-frontend" and "dockerhub-mirror" (host:port of a Docker Hub
	// pull-through cache).
	Images func() map[string]string
	// BuildRegistry is the registry address as BuildKit sees it from the
	// build network. Default "<5050-build gateway>:5000", where the
	// firewall lets builds reach the registry and nothing else of ours.
	BuildRegistry string
	// BuildNetworkName defaults to "5050-build".
	BuildNetworkName string
	// DockerCLI and Git are the binaries to run; default "docker" and "git"
	// from PATH.
	DockerCLI string
	Git       string
	// HTTPClient downloads uploads and Railpack releases.
	HTTPClient *http.Client
	// ReadConfig reads the config-as-code file; default ReadConfigFile.
	ReadConfig ConfigReader
}

const (
	// builderName is the buildx builder. docs/SPEC.md 8.2 calls it "5050",
	// but buildx names must start with a letter.
	builderName        = "fifty5050"
	builderContainer   = "buildx_buildkit_" + builderName + "0"
	defaultNetworkName = "5050-build"
	defaultTimeoutSec  = 1800
	tailLines          = 400
)

// New returns the builder.
func New(o Options) agentapi.Builder {
	return newBuilder(o, execRunner{}, nil)
}

func newBuilder(o Options, r runner, e engine) *builder {
	if o.DataDir == "" {
		o.DataDir = "/var/lib/5050"
	}
	if o.DockerHost == "" {
		o.DockerHost = "unix:///var/run/docker.sock"
	}
	if o.BuildNetworkName == "" {
		o.BuildNetworkName = defaultNetworkName
	}
	if o.DockerCLI == "" {
		o.DockerCLI = "docker"
	}
	if o.Git == "" {
		o.Git = "git"
	}
	if o.ReadConfig == nil {
		o.ReadConfig = ReadConfigFile
	}
	n := o.MaxParallel
	if n <= 0 {
		n = max(1, runtime.NumCPU()/4)
	}
	if e == nil {
		e = &dockerEngine{host: o.DockerHost}
	}
	return &builder{
		opts:      o,
		run:       r,
		eng:       e,
		slots:     make(chan struct{}, n),
		dockerCLI: lookPath(o.DockerCLI),
		gitPath:   lookPath(o.Git),
		frontends: map[string]string{},
	}
}

type builder struct {
	opts      Options
	run       runner
	eng       engine
	slots     chan struct{}
	dockerCLI string
	gitPath   string

	mu        sync.Mutex // guards the fields below and the builder container
	endpoint  string     // registry address as BuildKit sees it
	lastImage string     // BuildKit image of the last EnsureRunning
	buildxVer string
	frontends map[string]string // railpack version -> frontend ref
	configMu  sync.Mutex        // guards DOCKER_CONFIG/config.json
}

var buildIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// buildFailure is an error whose text is the diagnosis sentence.
type buildFailure struct{ sentence string }

func (f *buildFailure) Error() string { return f.sentence }

func fail(format string, args ...any) error {
	return &buildFailure{sentence: fmt.Sprintf(format, args...)}
}

// Build runs one job. It never panics on bad input and always returns a
// result with Diagnosis set when OK is false.
func (b *builder) Build(ctx context.Context, job proto.BuildJob, reg proto.Registry, logf func(string)) proto.BuildResult {
	start := time.Now()
	res := proto.BuildResult{BuildID: job.BuildID}
	if logf == nil {
		logf = func(string) {}
	}
	m := newMasker(secretValues(job, reg)...)
	tail := newRing(tailLines)
	var logMu sync.Mutex
	log := func(line string) {
		line = m.Mask(line)
		logMu.Lock()
		tail.add(line)
		logMu.Unlock()
		logf(line)
	}

	timeout := job.TimeoutSec
	if timeout <= 0 {
		timeout = defaultTimeoutSec
	}
	finish := func(err error) proto.BuildResult {
		res.DurationMs = time.Since(start).Milliseconds()
		if err == nil {
			res.OK = true
			return res
		}
		res.OK = false
		res.Image, res.Digest = "", ""
		switch {
		case ctx.Err() != nil:
			res.Diagnosis = sentenceAborted
		case errors.Is(err, context.DeadlineExceeded):
			res.Diagnosis = timeoutSentence(timeout)
		default:
			var bf *buildFailure
			var ee *exitError
			switch {
			case errors.As(err, &bf):
				res.Diagnosis = bf.sentence
			case errors.As(err, &ee):
				logMu.Lock()
				lines := tail.lines()
				logMu.Unlock()
				res.Diagnosis = diagnose(lines, diagContext{RootDir: job.RootDir, DockerfilePath: job.DockerfilePath, TimeoutSec: timeout})
			default:
				res.Diagnosis = err.Error()
			}
		}
		res.Diagnosis = m.Mask(res.Diagnosis)
		log(res.Diagnosis)
		return res
	}

	if !buildIDRe.MatchString(job.BuildID) {
		return finish(fail("The build id %q is not valid.", job.BuildID))
	}
	select {
	case b.slots <- struct{}{}:
	default:
		log("Waiting for a free build slot on this host")
		select {
		case b.slots <- struct{}{}:
		case <-ctx.Done():
			return finish(ctx.Err())
		}
	}
	defer func() { <-b.slots }()

	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	err := b.build(tctx, job, reg, log, &res)
	if err != nil && tctx.Err() != nil && ctx.Err() == nil {
		err = context.DeadlineExceeded
	}
	return finish(err)
}

func (b *builder) build(ctx context.Context, job proto.BuildJob, reg proto.Registry, log func(string), res *proto.BuildResult) error {
	sources := 0
	for _, set := range []bool{job.SourceImage != "", job.Git != nil, job.TarballURL != ""} {
		if set {
			sources++
		}
	}
	if sources != 1 {
		return fail("The build needs exactly one source: a repository, an upload or an image.")
	}
	if job.Image == "" {
		return fail("The build has no target image name.")
	}
	if job.SourceImage != "" {
		return b.importImage(ctx, job, reg, log, res)
	}

	work := filepath.Join(b.opts.DataDir, "work", job.BuildID)
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return fail("The build directory %s could not be created: %v.", work, err)
	}
	defer func() { _ = os.RemoveAll(work) }()
	repo := filepath.Join(work, "src")
	planDir := filepath.Join(work, "plan")
	if err := os.MkdirAll(planDir, 0o700); err != nil {
		return fail("The build directory %s could not be created: %v.", planDir, err)
	}

	var err error
	if job.Git != nil {
		err = b.fetchGit(ctx, job.Git, repo, log)
	} else {
		err = b.fetchTarball(ctx, job.TarballURL, repo, log)
	}
	if err != nil {
		return asFailure(ctx, err)
	}

	rootDir := cleanRel(job.RootDir)
	contextDir := filepath.Join(repo, filepath.FromSlash(rootDir))
	if rootDir != "" {
		st, err := os.Stat(contextDir)
		if insideRoot(repo, rootDir) != nil || err != nil || !st.IsDir() {
			return fail("The root directory %s does not exist in this source. Check the root directory in Settings > Source.", displayRoot(rootDir))
		}
	}

	ov, notices, err := b.opts.ReadConfig(repo, rootDir, job.ConfigPath, environmentName(job.Env))
	if err != nil {
		return asFailure(ctx, err)
	}
	var nix *manifest.NixpacksHints
	var fileOV *proto.ConfigOverlay
	if ov != nil {
		res.ConfigFile = ov
		nix = ov.Nixpacks
		if ov.Path != "" {
			log("Using config file " + ov.Path)
			fileOV = ov
		}
		if nix != nil {
			log("Using hints from nixpacks.toml")
		}
	}
	plan, err := resolvePlan(job, fileOV, nix, contextDir)
	if err != nil {
		return asFailure(ctx, err)
	}
	for _, l := range plan.Log {
		log(l)
	}
	notices = append(notices, plan.Notices...)
	for _, n := range notices {
		log(n)
	}
	res.Notices = notices

	if reg.Address == "" {
		return fail("This host has no registry push credential. Give the host the build role again or restart the agent.")
	}
	if err := b.ensure(ctx, ""); err != nil {
		return asFailure(ctx, err)
	}
	b.mu.Lock()
	endpoint := b.endpoint
	b.mu.Unlock()
	if err := b.writeDockerConfig(endpoint, reg); err != nil {
		return err
	}
	target := buildTarget{
		Image:    rewriteRegistry(job.Image, reg.Address, endpoint),
		Cache:    rewriteRegistry(job.CacheRef, reg.Address, endpoint),
		Platform: platformOf(job.Platform),
		NoCache:  plan.NoCache,
		Meta:     filepath.Join(planDir, "metadata.json"),
	}

	var digest string
	switch plan.Builder {
	case builderDockerfile:
		res.Provider = "dockerfile"
		digest, err = b.buildDockerfile(ctx, job, plan, contextDir, target, log)
	default:
		var out railpackOutput
		out, err = b.buildRailpack(ctx, job, reg, endpoint, plan, contextDir, planDir, target, log)
		res.Provider = out.Provider
		res.IsNode = out.IsNode
		res.StartCommand = out.StartCommand
		digest = out.Digest
	}
	if err != nil {
		return err
	}
	res.Image = job.Image
	res.Digest = digest
	log(fmt.Sprintf("Pushed %s", job.Image))
	return nil
}

// asFailure keeps cancellation as is and turns sentences into failures.
func asFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return err
	}
	var bf *buildFailure
	if errors.As(err, &bf) {
		return err
	}
	return &buildFailure{sentence: err.Error()}
}

// secretValues lists every value the log must never show.
func secretValues(job proto.BuildJob, reg proto.Registry) []string {
	vals := make([]string, 0, len(job.Env)+4)
	for _, v := range job.Env {
		vals = append(vals, v)
	}
	if job.Git != nil && job.Git.Token != "" {
		vals = append(vals, job.Git.Token)
	}
	if job.SourceImageAuth != nil {
		vals = append(vals, job.SourceImageAuth.Password)
	}
	vals = append(vals, reg.Password)
	return vals
}

func (b *builder) httpClient() *http.Client {
	if b.opts.HTTPClient != nil {
		return b.opts.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Minute}
}

func (b *builder) images() map[string]string {
	if b.opts.Images == nil {
		return nil
	}
	return b.opts.Images()
}

func platformOf(p string) string {
	if p != "" {
		return p
	}
	return "linux/" + runtime.GOARCH
}

// ring keeps the last n log lines for diagnosis.
type ring struct {
	buf  []string
	next int
	full bool
}

func newRing(n int) *ring { return &ring{buf: make([]string, n)} }

func (r *ring) add(s string) {
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

func (r *ring) lines() []string {
	if !r.full {
		return append([]string(nil), r.buf[:r.next]...)
	}
	out := make([]string, 0, len(r.buf))
	out = append(out, r.buf[r.next:]...)
	return append(out, r.buf[:r.next]...)
}
