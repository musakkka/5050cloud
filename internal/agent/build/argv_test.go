// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// secretEnv has values that must never appear on argv.
var secretEnv = map[string]string{
	"DATABASE_URL": "postgres://user:hunter2-db-pass@db:5432/app",
	"API_KEY":      "sk_live_0123456789abcdef",
	"NPM_TOKEN":    "npm_tokenvalue_987654321",
	"PATH":         "/evil/bin",
	"LD_PRELOAD":   "/tmp/evil.so",
	"EMPTY":        "",
	"BAD=NAME":     "x",
}

func assertNoValues(t *testing.T, argv []string, env map[string]string) {
	t.Helper()
	joined := strings.Join(argv, " ")
	for k, v := range env {
		if len(v) >= 4 && strings.Contains(joined, v) {
			t.Errorf("the value of %s is on argv: %s", k, joined)
		}
	}
}

func TestRailpackPrepareArgvHasNoValues(t *testing.T) {
	p := buildPlan{Env: secretEnv, BuildCommand: "npm run build", StartCommand: "node server.js"}
	argv, env, reserved := railpackPrepareArgv(p, "/w/src", "/w/plan/plan.json", "/w/plan/info.json")
	assertNoValues(t, argv, secretEnv)
	want := []string{"prepare", "/w/src", "--plan-out", "/w/plan/plan.json", "--info-out", "/w/plan/info.json",
		"--build-cmd", "npm run build", "--start-cmd", "node server.js",
		"--env", "API_KEY", "--env", "DATABASE_URL", "--env", "NPM_TOKEN"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %q\nwant  %q", argv, want)
	}
	if v, ok := envValue(env, "API_KEY"); !ok || v != secretEnv["API_KEY"] {
		t.Errorf("API_KEY must be in the child environment")
	}
	if _, ok := envValue(env, "PATH"); ok {
		t.Errorf("a service variable named PATH must not reach Railpack")
	}
	if strings.Join(reserved, ",") != "LD_PRELOAD,PATH" {
		t.Errorf("reserved = %v", reserved)
	}
}

func TestRailpackBuildArgvHasNoValues(t *testing.T) {
	b := testBuilder(t, nil, nil, nil)
	p := buildPlan{Env: secretEnv}
	plan := railpackPlanFile{Secrets: []string{"API_KEY", "DATABASE_URL", "NPM_TOKEN", "PATH", "MISSING"}}
	tgt := buildTarget{Image: "172.30.0.1:5000/svc:dep", Cache: "172.30.0.1:5000/svc:buildcache", Platform: "linux/amd64", Meta: "/w/plan/metadata.json"}
	argv, env, err := b.railpackBuildArgv(proto.BuildJob{ServiceID: "svc_123"}, p, plan, "ghcr.io/railwayapp/railpack-frontend:v0.40.1@sha256:abc", "/w/plan/plan.json", "/w/src", tgt)
	if err != nil {
		t.Fatal(err)
	}
	assertNoValues(t, argv, secretEnv)
	s := strings.Join(argv, " ")
	for _, want := range []string{
		"buildx build --builder fifty5050 --progress plain --platform linux/amd64 --provenance=false --sbom=false",
		"-t 172.30.0.1:5000/svc:dep --output type=image,push=true",
		"--cache-from type=registry,ref=172.30.0.1:5000/svc:buildcache",
		"--cache-to type=registry,ref=172.30.0.1:5000/svc:buildcache,mode=max,ignore-error=true",
		"-f /w/plan/plan.json --build-arg BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend:v0.40.1@sha256:abc",
		"--build-arg cache-key=svc_123",
		"--secret id=API_KEY,env=FIFTY_BUILD_SECRET_0",
		"--secret id=DATABASE_URL,env=FIFTY_BUILD_SECRET_1",
		"--secret id=NPM_TOKEN,env=FIFTY_BUILD_SECRET_2",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("argv lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "id=PATH") || strings.Contains(s, "id=MISSING") {
		t.Errorf("reserved or unset secrets must be skipped: %s", s)
	}
	if !strings.HasSuffix(s, " /w/src") {
		t.Errorf("the context must come last: %s", s)
	}
	if v, _ := envValue(env, "FIFTY_BUILD_SECRET_1"); v != secretEnv["DATABASE_URL"] {
		t.Errorf("secret alias carries the wrong value")
	}
	// The secrets hash is an HMAC: stable for the same values, different
	// when one changes, and not the plain SHA-256 of the values.
	h1, _ := b.secretsHash([]string{"API_KEY"}, secretEnv)
	h2, _ := b.secretsHash([]string{"API_KEY"}, secretEnv)
	h3, _ := b.secretsHash([]string{"API_KEY"}, map[string]string{"API_KEY": "other"})
	if h1 != h2 || h1 == h3 || len(h1) != 64 {
		t.Errorf("secrets hash: %s %s %s", h1, h2, h3)
	}
}

func TestDockerfileArgvOnlyARGNames(t *testing.T) {
	info := dockerfileInfo{Args: []string{"API_KEY", "NODE_VERSION", "PATH"}, Secrets: []string{"NPM_TOKEN"}}
	p := buildPlan{Env: secretEnv}
	tgt := buildTarget{Image: "r:5000/s:d", Platform: "linux/arm64", Meta: "/m.json", NoCache: true, Cache: "r:5000/s:buildcache"}
	log, lines := collect()
	argv, env := dockerfileArgv(info, p, "/w/src/Dockerfile", "/w/src", tgt, log)
	assertNoValues(t, argv, secretEnv)
	s := strings.Join(argv, " ")
	if !strings.Contains(s, "--build-arg API_KEY") || strings.Contains(s, "DATABASE_URL") {
		t.Errorf("only ARG names become build arguments: %s", s)
	}
	if strings.Contains(s, "--build-arg PATH") {
		t.Errorf("PATH must not be passed: %s", s)
	}
	if !strings.Contains(s, "--secret id=NPM_TOKEN,env=FIFTY_BUILD_SECRET_0") {
		t.Errorf("secret mounts are passed as secrets: %s", s)
	}
	if !strings.Contains(s, "--no-cache") || strings.Contains(s, "--cache-from") {
		t.Errorf("no cache: %s", s)
	}
	if v, _ := envValue(env, "API_KEY"); v != secretEnv["API_KEY"] {
		t.Errorf("API_KEY value must be in the child environment")
	}
	out := strings.Join(lines(), "\n")
	if !strings.Contains(out, "Variables visible at build time (named by ARG): API_KEY") {
		t.Errorf("log = %s", out)
	}
	if !strings.Contains(out, "Not passed as build arguments because the build tools use these names themselves: PATH") {
		t.Errorf("log = %s", out)
	}
}

func TestRewriteRegistry(t *testing.T) {
	cases := [][4]string{
		{"127.0.0.1:5000/svc:1", "127.0.0.1:5000", "172.30.0.1:5000", "172.30.0.1:5000/svc:1"},
		{"ghcr.io/x/y:1", "127.0.0.1:5000", "172.30.0.1:5000", "ghcr.io/x/y:1"},
		{"127.0.0.1:50001/svc:1", "127.0.0.1:5000", "172.30.0.1:5000", "127.0.0.1:50001/svc:1"},
		{"", "a", "b", ""},
	}
	for _, c := range cases {
		if got := rewriteRegistry(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("rewriteRegistry(%q) = %q, want %q", c[0], got, c[3])
		}
	}
}

func TestUsableEnv(t *testing.T) {
	names, reserved := usableEnv(map[string]string{
		"OK": "1", "DOCKER_HOST": "tcp://evil", "BUILDKIT_X": "1", "with space": "1", "NUL": "a\x00b", "lower": "1",
	})
	if strings.Join(names, ",") != "OK,lower" || strings.Join(reserved, ",") != "BUILDKIT_X,DOCKER_HOST" {
		t.Errorf("names %v reserved %v", names, reserved)
	}
}
