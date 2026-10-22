// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

type fakeTools struct{ path string }

func (f fakeTools) Path(context.Context, string) (string, error) { return f.path, nil }
func (fakeTools) Set([]proto.Tool)                               {}

var testReg = proto.Registry{Address: "registry.invalid:5000", Username: "push", Password: "push-password-xyz", Insecure: true}

// serveTar serves one archive per path.
func serveTar(t *testing.T, archives map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := archives[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func baseJob(url string) proto.BuildJob {
	return proto.BuildJob{
		BuildID:    "dep_1",
		ServiceID:  "svc_1",
		TarballURL: url,
		Builder:    "auto",
		Image:      "registry.invalid:5000/svc_1:dep_1",
		CacheRef:   "registry.invalid:5000/svc_1:buildcache",
		Platform:   "linux/amd64",
		Env:        map[string]string{"API_KEY": "sk_live_secretvalue_1"},
	}
}

func TestBuildRailpackEndToEnd(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{"package.json": `{"scripts":{"start":"node i.js"}}`, "i.js": ""})})
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, line func(string)) (bool, error) {
		if isBuildxBuild(c) {
			// A build step that prints a secret: the log must mask it.
			line("#7 0.1 key is sk_live_secretvalue_1")
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, func(o *Options) { o.Tools = fakeTools{path: "/opt/railpack"} })
	log, lines := collect()
	res := b.Build(context.Background(), baseJob(srv.URL+"/up"), testReg, log)
	if !res.OK {
		t.Fatalf("build failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	if res.Image != "registry.invalid:5000/svc_1:dep_1" || !strings.HasPrefix(res.Digest, "sha256:") {
		t.Errorf("image %q digest %q", res.Image, res.Digest)
	}
	if res.Provider != "node" || !res.IsNode || res.StartCommand != "npm run start" || res.BuildID != "dep_1" {
		t.Errorf("result = %+v", res)
	}
	out := strings.Join(lines(), "\n")
	if strings.Contains(out, "sk_live_secretvalue_1") || !strings.Contains(out, "key is *****") {
		t.Errorf("the secret must be masked in the log:\n%s", out)
	}
	if strings.Contains(out, testReg.Password) {
		t.Errorf("the registry password is in the log")
	}

	prep := r.find(isPrepare)
	if len(prep) != 1 || prep[0].Path != "/opt/railpack" {
		t.Fatalf("prepare calls = %+v", prep)
	}
	builds := r.find(isBuildxBuild)
	if len(builds) != 1 {
		t.Fatalf("buildx build calls = %d", len(builds))
	}
	bargs := strings.Join(builds[0].Args, " ")
	if !strings.Contains(bargs, "-t 172.30.0.1:5000/svc_1:dep_1") || !strings.Contains(bargs, "ref=172.30.0.1:5000/svc_1:buildcache") {
		t.Errorf("BuildKit must push through the build network gateway: %s", bargs)
	}
	if !strings.Contains(bargs, "BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend:v0.40.1@sha256:") {
		t.Errorf("frontend must be pinned by digest: %s", bargs)
	}
	for _, c := range append(prep, builds...) {
		if strings.Contains(strings.Join(c.Args, " "), "sk_live_secretvalue_1") {
			t.Errorf("a value is on argv: %v", c.Args)
		}
	}
	// The push credential lives in the builder's DOCKER_CONFIG, 0600.
	cfg := filepath.Join(b.opts.DataDir, "build", "docker", "config.json")
	st, err := os.Stat(cfg)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("docker config: %v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(b.opts.DataDir, "work", "dep_1")); !os.IsNotExist(err) {
		t.Errorf("the work directory must be deleted after the build")
	}
	if res.DurationMs < 0 {
		t.Errorf("duration = %d", res.DurationMs)
	}
}

func TestBuildDockerfileEndToEnd(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{
		"api/Dockerfile": "FROM alpine:3.20\nARG API_KEY\nRUN echo build\n",
	})})
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, nil)
	job := baseJob(srv.URL + "/up")
	job.RootDir = "/api"
	log, lines := collect()
	res := b.Build(context.Background(), job, testReg, log)
	if !res.OK {
		t.Fatalf("build failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	if res.Provider != "dockerfile" || res.IsNode {
		t.Errorf("result = %+v", res)
	}
	out := strings.Join(lines(), "\n")
	for _, want := range []string{"Using detected Dockerfile", "Variables visible at build time (named by ARG): API_KEY"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	builds := r.find(isBuildxBuild)
	if len(builds) != 1 {
		t.Fatalf("buildx calls = %d", len(builds))
	}
	if v, _ := envValue(builds[0].Env, "API_KEY"); v != "sk_live_secretvalue_1" {
		t.Errorf("ARG value must come from the child environment")
	}
	if !strings.HasSuffix(strings.Join(builds[0].Args, " "), "/src/api") {
		t.Errorf("the context must be the root directory: %v", builds[0].Args)
	}
	if len(r.find(isPrepare)) != 0 {
		t.Errorf("Railpack must not run for Dockerfile builds")
	}
}

func TestBuildConfigFile(t *testing.T) {
	srv := serveTar(t, map[string][]byte{
		"/good": makeTarGz(t, map[string]string{
			"Dockerfile":    "FROM alpine\n",
			"package.json":  "{}",
			"railway.json":  `{"build": {"builder": "NIXPACKS"}, "deploy": {"startCommand": "node server.js", "sleepApplication": true}}`,
			"nixpacks.toml": "[phases.install]\ncmds = [\"npm ci\"]\n",
		}),
		"/bad": makeTarGz(t, map[string]string{
			"railway.json": "{\n\"build\": {\n\"buildComand\": \"x\"\n}\n}",
		}),
	})
	r := &fakeRunner{}
	b := testBuilder(t, r, nil, func(o *Options) { o.Tools = fakeTools{path: "/opt/railpack"} })
	log, lines := collect()
	res := b.Build(context.Background(), baseJob(srv.URL+"/good"), testReg, log)
	if !res.OK {
		t.Fatalf("build failed: %s", res.Diagnosis)
	}
	if res.ConfigFile == nil || res.ConfigFile.Path != "railway.json" || res.ConfigFile.Lines["deploy.startCommand"] != 1 {
		t.Errorf("config overlay = %+v", res.ConfigFile)
	}
	if res.ConfigFile.Nixpacks == nil || len(res.ConfigFile.Nixpacks.IgnoredKeys) != 1 {
		t.Errorf("nixpacks hints = %+v", res.ConfigFile.Nixpacks)
	}
	out := strings.Join(lines(), "\n")
	for _, want := range []string{
		"Using config file railway.json",
		"Nixpacks config detected, building with Railpack.",
		"railway.json: sleepApplication is not supported in 5050cloud v0.1 and was ignored.",
		"nixpacks.toml: phases.install is not supported by Railpack and was ignored.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q", want)
		}
	}
	if len(res.Notices) != 2 {
		t.Errorf("notices = %q", res.Notices)
	}
	prep := r.find(isPrepare)
	if len(prep) != 1 || argAfter(prep[0].Args, "--start-cmd") != "node server.js" {
		t.Errorf("start command from the file must reach Railpack: %v", prep)
	}

	job := baseJob(srv.URL + "/bad")
	job.BuildID = "dep_2"
	res = b.Build(context.Background(), job, testReg, func(string) {})
	if res.OK || !strings.HasPrefix(res.Diagnosis, "railway.json line 3: unknown field buildComand") || !strings.HasSuffix(res.Diagnosis, "Did you mean buildCommand?") {
		t.Errorf("diagnosis = %q", res.Diagnosis)
	}
}

func TestBuildFailureIsDiagnosed(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{"Dockerfile": "FROM node:22\nRUN npm ci\n"})})
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, line func(string)) (bool, error) {
		if isBuildxBuild(c) {
			line("#8 1.2 npm error The `npm ci` command can only install with an existing package-lock.json or")
			line("#8 ERROR: process \"/bin/sh -c npm ci\" did not complete successfully: exit code: 1")
			return true, &exitError{Cmd: "docker", Code: 1}
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, nil)
	log, lines := collect()
	res := b.Build(context.Background(), baseJob(srv.URL+"/up"), testReg, log)
	want := "npm ci needs a package-lock.json. Commit one or set a custom install command."
	if res.OK || res.Diagnosis != want {
		t.Errorf("diagnosis = %q, want %q", res.Diagnosis, want)
	}
	if res.Image != "" || res.Digest != "" {
		t.Errorf("a failed build has no image")
	}
	all := lines()
	if all[len(all)-1] != want {
		t.Errorf("the diagnosis must be the last log line, got %q", all[len(all)-1])
	}
}

func TestBuildAbortStopsQuickly(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{"Dockerfile": "FROM alpine\nRUN sleep 600\n"})})
	started := make(chan struct{})
	r := &fakeRunner{hook: func(ctx context.Context, c cmdSpec, _ func(string)) (bool, error) {
		if isBuildxBuild(c) {
			close(started)
			<-ctx.Done()
			return true, ctx.Err()
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan proto.BuildResult, 1)
	go func() { done <- b.Build(ctx, baseJob(srv.URL+"/up"), testReg, func(string) {}) }()
	<-started
	t0 := time.Now()
	cancel()
	select {
	case res := <-done:
		if res.OK || res.Diagnosis != "The build was cancelled." {
			t.Errorf("result = %+v", res)
		}
		if d := time.Since(t0); d > 5*time.Second {
			t.Errorf("abort took %s", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not stop the build within 5 s")
	}
	if _, err := os.Stat(filepath.Join(b.opts.DataDir, "work", "dep_1")); !os.IsNotExist(err) {
		t.Errorf("the work directory must be deleted after an abort")
	}
}

func TestBuildTimeout(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{"Dockerfile": "FROM alpine\n"})})
	r := &fakeRunner{hook: func(ctx context.Context, c cmdSpec, _ func(string)) (bool, error) {
		if isBuildxBuild(c) {
			<-ctx.Done()
			return true, ctx.Err()
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, nil)
	job := baseJob(srv.URL + "/up")
	job.TimeoutSec = 1
	res := b.Build(context.Background(), job, testReg, func(string) {})
	want := "The build took longer than 1 second and was stopped. Raise the build timeout in Settings > Build."
	if res.OK || res.Diagnosis != want {
		t.Errorf("diagnosis = %q", res.Diagnosis)
	}
}

func TestBuildRejectsBadJobs(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{"package.json": "{}"})})
	b := testBuilder(t, &fakeRunner{}, nil, nil)
	cases := map[string]struct {
		mod  func(*proto.BuildJob, *proto.Registry)
		want string
	}{
		"bad id":       {func(j *proto.BuildJob, _ *proto.Registry) { j.BuildID = "../x" }, `The build id "../x" is not valid.`},
		"no source":    {func(j *proto.BuildJob, _ *proto.Registry) { j.TarballURL = "" }, "The build needs exactly one source: a repository, an upload or an image."},
		"two sources":  {func(j *proto.BuildJob, _ *proto.Registry) { j.SourceImage = "nginx" }, "The build needs exactly one source: a repository, an upload or an image."},
		"no image":     {func(j *proto.BuildJob, _ *proto.Registry) { j.Image = "" }, "The build has no target image name."},
		"missing root": {func(j *proto.BuildJob, _ *proto.Registry) { j.RootDir = "web" }, "The root directory /web does not exist in this source. Check the root directory in Settings > Source."},
		"no registry":  {func(_ *proto.BuildJob, r *proto.Registry) { r.Address = "" }, "This host has no registry push credential. Give the host the build role again or restart the agent."},
		"bad version":  {func(j *proto.BuildJob, _ *proto.Registry) { j.RailpackVersion = "9.9.9" }, "Railpack 9.9.9 is not available on this install. Available: 0.40.1, 0.40.0, 0.39.0."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			job := baseJob(srv.URL + "/up")
			reg := testReg
			c.mod(&job, &reg)
			res := b.Build(context.Background(), job, reg, nil)
			if res.OK || res.Diagnosis != c.want {
				t.Errorf("diagnosis = %q, want %q", res.Diagnosis, c.want)
			}
		})
	}
}

func TestBuildWaitsForASlot(t *testing.T) {
	srv := serveTar(t, map[string][]byte{"/up": makeTarGz(t, map[string]string{"Dockerfile": "FROM alpine\n"})})
	release := make(chan struct{})
	var once sync.Once
	inBuild := make(chan struct{})
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, _ func(string)) (bool, error) {
		if isBuildxBuild(c) && strings.Contains(strings.Join(c.Args, " "), "dep_a") {
			once.Do(func() { close(inBuild) })
			<-release
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, func(o *Options) { o.MaxParallel = 1 })
	jobA := baseJob(srv.URL + "/up")
	jobA.BuildID, jobA.Image = "dep_a", "registry.invalid:5000/svc_1:dep_a"
	jobB := baseJob(srv.URL + "/up")
	jobB.BuildID, jobB.Image = "dep_b", "registry.invalid:5000/svc_1:dep_b"
	resA := make(chan proto.BuildResult, 1)
	go func() { resA <- b.Build(context.Background(), jobA, testReg, nil) }()
	<-inBuild
	logB, linesB := collect()
	resB := make(chan proto.BuildResult, 1)
	go func() { resB <- b.Build(context.Background(), jobB, testReg, logB) }()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(strings.Join(linesB(), "\n"), "Waiting for a free build slot") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if res := <-resA; !res.OK {
		t.Errorf("A: %s", res.Diagnosis)
	}
	if res := <-resB; !res.OK {
		t.Errorf("B: %s", res.Diagnosis)
	}
	if !strings.Contains(strings.Join(linesB(), "\n"), "Waiting for a free build slot on this host") {
		t.Errorf("B did not wait: %v", linesB())
	}
}

func TestBuildGitSource(t *testing.T) {
	r := &fakeRunner{hook: func(_ context.Context, c cmdSpec, _ func(string)) (bool, error) {
		if len(c.Args) > 2 && c.Args[2] == "checkout" || len(c.Args) > 4 && c.Args[4] == "checkout" {
			dir := c.Args[1]
			writeTree(t, dir, map[string]string{"Dockerfile": "FROM alpine\n"})
		}
		return false, nil
	}}
	b := testBuilder(t, r, nil, nil)
	job := baseJob("")
	job.TarballURL = ""
	job.Git = &proto.GitSource{URL: "https://github.com/acme/app", Commit: strings.Repeat("c", 40), Branch: "main", Token: "ghs_tok_123456"}
	log, lines := collect()
	res := b.Build(context.Background(), job, testReg, log)
	if !res.OK {
		t.Fatalf("build failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	if strings.Contains(strings.Join(lines(), "\n"), "ghs_tok_123456") {
		t.Errorf("token in log")
	}
}

func TestAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(asFailure(ctx, errors.New("x")), context.Canceled) {
		t.Errorf("cancellation must win")
	}
	var bf *buildFailure
	if !errors.As(asFailure(context.Background(), errors.New("A sentence.")), &bf) || bf.sentence != "A sentence." {
		t.Errorf("plain errors become failures")
	}
	ee := &exitError{Cmd: "docker", Code: 2}
	if !errors.Is(asFailure(context.Background(), ee), ee) {
		t.Errorf("exit errors stay as they are")
	}
}

func TestRing(t *testing.T) {
	r := newRing(3)
	for _, s := range []string{"a", "b"} {
		r.add(s)
	}
	if strings.Join(r.lines(), "") != "ab" {
		t.Errorf("partial = %v", r.lines())
	}
	for _, s := range []string{"c", "d", "e"} {
		r.add(s)
	}
	if strings.Join(r.lines(), "") != "cde" {
		t.Errorf("wrapped = %v", r.lines())
	}
}
