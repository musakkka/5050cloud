// SPDX-License-Identifier: MIT

package configfile

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
)

const railwayJSON = `{
  "$schema": "https://railway.com/railway.schema.json",
  "build": {
    "builder": "nixpacks",
    "buildCommand": "npm run build",
    "watchPatterns": ["src/**", "!src/**/*.test.ts"]
  },
  "deploy": {
    "startCommand": "node server.js",
    "preDeployCommand": ["npm run migrate", "npm run seed"],
    "healthcheckPath": "/health",
    "healthcheckTimeout": 120,
    "restartPolicyType": "on_failure",
    "restartPolicyMaxRetries": 5,
    "numReplicas": 2,
    "sleepApplication": true,
    "region": "us-west1"
  },
  "environments": {
    "production": {
      "deploy": {
        "numReplicas": 3,
        "startCommand": "node prod.js"
      }
    }
  }
}`

func TestParseJSONAndLines(t *testing.T) {
	f, err := Parse("railway.json", []byte(railwayJSON))
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != JSON || *f.Config.Build.Builder != "NIXPACKS" || *f.Config.Deploy.RestartPolicyType != "ON_FAILURE" {
		t.Fatalf("decoded %+v", f.Config.Build)
	}
	want := map[string]int{
		"build.builder":       4,
		"deploy.startCommand": 9,
		"environments.production.deploy.numReplicas":  22,
		"environments.production.deploy.startCommand": 23,
		"build.watchPatterns":                         6,
	}
	for k, l := range want {
		if f.Lines[k] != l {
			t.Errorf("line of %s = %d, want %d", k, f.Lines[k], l)
		}
	}
}

func TestMerge(t *testing.T) {
	f, err := Parse("railway.json", []byte(railwayJSON))
	if err != nil {
		t.Fatal(err)
	}
	base := api.ServiceConfig{Name: "web", Deploy: api.DeployConfig{StartCommand: "ui start", Replicas: 1, CronSchedule: "keep"}}
	r := Merge(base, Overlay(f, nil), "production")
	c := r.Config
	if c.Deploy.StartCommand != "node prod.js" || c.Deploy.Replicas != 3 {
		t.Fatalf("environment override not applied: %+v", c.Deploy)
	}
	if c.Deploy.PreDeployCommand != "npm run migrate && npm run seed" || c.Deploy.HealthcheckTimeoutSec != 120 || c.Deploy.RestartPolicy != "on_failure" || c.Deploy.RestartMaxRetries != 5 {
		t.Fatalf("deploy = %+v", c.Deploy)
	}
	if c.Deploy.CronSchedule != "keep" || c.Name != "web" {
		t.Fatal("fields the file does not set must keep the UI value")
	}
	if c.Build.Builder != "nixpacks" || c.Build.BuildCommand != "npm run build" {
		t.Fatalf("build = %+v", c.Build)
	}
	if r.Source["deploy.startCommand"] != "railway.json:23" || r.Source["deploy.replicas"] != "railway.json:22" || r.Source["deploy.healthcheckPath"] != "railway.json:11" {
		t.Fatalf("source = %v", r.Source)
	}
	if !reflect.DeepEqual(r.WatchPaths, []string{"src/**", "!src/**/*.test.ts"}) {
		t.Fatalf("watch paths = %v", r.WatchPaths)
	}
	joined := strings.Join(r.Notices, "\n")
	for _, want := range []string{"Nixpacks config detected, building with Railpack.", "sleepApplication is not supported in 5050cloud v0.1 and was ignored.", "region is a Railway setting"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notices miss %q: %v", want, r.Notices)
		}
	}
	staging := Merge(base, Overlay(f, nil), "staging")
	if staging.Config.Deploy.StartCommand != "node server.js" || staging.Config.Deploy.Replicas != 2 {
		t.Fatalf("staging = %+v", staging.Config.Deploy)
	}
	if got := Merge(base, nil, "production"); !reflect.DeepEqual(got.Config, base) || len(got.Source) != 0 {
		t.Fatal("nil overlay must return base")
	}
	sets := Sets(f, "production")
	if len(sets) == 0 || sets[0] != "build.buildCommand" {
		t.Fatalf("Sets = %v", sets)
	}
}

const railwayTOML = `[build]
builder = "DOCKERFILE"
dockerfilePath = "docker/Dockerfile"
railpackVersion = "0.40.1"

[deploy]
startCommand = "./run"
cronSchedule = "*/5 * * * *"
drainingSeconds = 10
overlapSeconds = 5
restartPolicyType = "NEVER"

[deploy.multiRegionConfig.us-east4]
numReplicas = 2

[deploy.multiRegionConfig.eu-west4]
numReplicas = 1

[environments.staging.deploy]
startCommand = "./run --staging"
`

func TestParseTOML(t *testing.T) {
	f, err := Parse("railway.toml", []byte(railwayTOML))
	if err != nil {
		t.Fatal(err)
	}
	if f.Lines["deploy.startCommand"] != 7 || f.Lines["environments.staging.deploy.startCommand"] != 20 || f.Lines["build.builder"] != 2 {
		t.Fatalf("lines = %v", f.Lines)
	}
	r := Merge(api.ServiceConfig{}, Overlay(f, nil), "staging")
	if r.Config.Deploy.Replicas != 3 || r.Config.Deploy.StartCommand != "./run --staging" || r.Config.Build.Builder != "dockerfile" || r.Config.Build.DockerfilePath != "docker/Dockerfile" {
		t.Fatalf("merged = %+v", r.Config)
	}
	if r.Config.Deploy.DrainingSec != 10 || r.Config.Deploy.OverlapSec != 5 || r.Config.Deploy.RestartPolicy != "never" || r.Config.Build.RailpackVersion != "0.40.1" {
		t.Fatalf("deploy = %+v", r.Config.Deploy)
	}
	if r.Source["deploy.startCommand"] != "railway.toml:20" {
		t.Fatalf("source = %v", r.Source)
	}
	if !strings.Contains(strings.Join(r.Notices, " "), "multiRegionConfig contributes only its replica count (3)") {
		t.Fatalf("notices = %v", r.Notices)
	}
}

const fiftyYAML = `# 5050.yaml
build:
  builder: RAILPACK
  watchPatterns:
    - src/**
deploy:
  startCommand: "node a.js"
  processes: auto
  singleton: true
  placement:
    roles: [app]
environments:
  production:
    deploy:
      processes: "4"
`

func TestParseYAML(t *testing.T) {
	f, err := Parse("5050.yaml", []byte(fiftyYAML))
	if err != nil {
		t.Fatal(err)
	}
	if f.Lines["deploy.startCommand"] != 7 || f.Lines["environments.production.deploy.processes"] != 15 || f.Lines["deploy.placement.roles"] != 11 {
		t.Fatalf("lines = %v", f.Lines)
	}
	r := Merge(api.ServiceConfig{}, Overlay(f, nil), "production")
	if r.Config.Deploy.Processes != "4" || r.Config.Deploy.Singleton == nil || !*r.Config.Deploy.Singleton || !reflect.DeepEqual(r.Config.Deploy.Placement.Roles, []string{"app"}) {
		t.Fatalf("merged = %+v", r.Config.Deploy)
	}
	if r.Source["deploy.processes"] != "5050.yaml:15" {
		t.Fatalf("source = %v", r.Source)
	}
	proj, err := Parse("5050.yaml", []byte("project: x\nservices: {}\n"))
	if err != nil || !proj.ProjectFile {
		t.Fatalf("project file: %v %v", proj, err)
	}
	if Sets(proj, "") != nil {
		t.Fatal("a project file sets nothing")
	}
	empty, err := Parse("5050.json", []byte(""))
	if err == nil && empty != nil {
		t.Log("empty JSON parsed")
	}
	if e, err := Parse("railway.yaml", []byte("")); err != nil || e == nil {
		t.Fatalf("empty YAML: %v", err)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name, file, body, want string
		line                   int
	}{
		{"unknown", "railway.json", "{\n \"deploy\": {\n  \"startComand\": \"x\"\n }\n}", `unknown field startComand. Did you mean startCommand?`, 3},
		{"unknown top", "railway.json", `{"deploi": {}}`, `Did you mean deploy?`, 1},
		{"far unknown", "railway.json", `{"zzzzzzzzzz": 1}`, "See the config as code reference", 1},
		{"heroku", "railway.json", `{"build": {"builder": "HEROKU"}}`, "the HEROKU builder is not available", 1},
		{"bad builder", "railway.json", `{"build": {"builder": "magic"}}`, `"magic" is not a builder`, 1},
		{"bad policy", "railway.toml", "[deploy]\nrestartPolicyType = \"sometimes\"\n", "is not a restart policy", 2},
		{"health path", "railway.json", `{"deploy": {"healthcheckPath": "health"}}`, "must start with /", 1},
		{"timeout", "railway.json", `{"deploy": {"healthcheckTimeout": 0}}`, "between 1 and 3600", 1},
		{"replicas", "railway.json", `{"deploy": {"numReplicas": 0}}`, "between 1 and 100", 1},
		{"retries", "railway.json", `{"deploy": {"restartPolicyMaxRetries": -1}}`, "cannot be negative", 1},
		{"overlap", "railway.json", `{"deploy": {"overlapSeconds": -1}}`, "cannot be negative", 1},
		{"draining", "railway.json", `{"deploy": {"drainingSeconds": -1}}`, "cannot be negative", 1},
		{"processes", "5050.json", `{"deploy": {"processes": "99"}}`, `"auto" or a number`, 1},
		{"processes word", "5050.json", `{"deploy": {"processes": "many"}}`, `"auto" or a number`, 1},
		{"type", "railway.json", `{"deploy": {"numReplicas": "two"}}`, "must be a whole number", 1},
		{"syntax", "railway.json", "{\n\"deploy\": ", "the JSON is not valid", 0},
		{"two values", "railway.json", `{} {}`, "more than one JSON value", 0},
		{"top array", "railway.json", `[1]`, "the top level must be an object", 0},
		{"not object", "railway.json", `{"deploy": 3}`, "deploy must be an object", 1},
		{"env not object", "railway.json", `{"environments": {"production": 3}}`, "must be an object", 0},
		{"yaml", "5050.yaml", "build: [\n", "the YAML is not valid", 0},
		{"toml", "railway.toml", "[deploy\n", "the TOML is not valid", 2},
		{"type ext", "railway.ini", "", "the file type is not supported", 0},
		{"env bad", "railway.json", `{"environments": {"production": {"deploy": {"numReplicas": 0}}}}`, "between 1 and 100", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.file, []byte(c.body))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.want)
			}
			var fe *Error
			if !errors.As(err, &fe) {
				t.Fatal("not a *Error")
			}
			if c.line > 0 && fe.Line != c.line {
				t.Fatalf("line = %d, want %d (%s)", fe.Line, c.line, err)
			}
		})
	}
	big := make([]byte, MaxFileBytes+1)
	if _, err := Parse("railway.json", big); err == nil {
		t.Fatal("oversized file accepted")
	}
	if _, err := ParseNixpacks("nixpacks.toml", big); err == nil {
		t.Fatal("oversized nixpacks accepted")
	}
	e := &Error{Path: "railway.json", Msg: "x."}
	if e.Error() != "railway.json: x." {
		t.Fatal(e.Error())
	}
}

const nixpacksTOML = `providers = ["node"]

[variables]
NODE_ENV = "production"

[phases.setup]
nixPkgs = ["ffmpeg"]
aptPkgs = ["libvips"]
nixLibs = ["zlib"]

[phases.build]
cmds = ["npm ci", "npm run build"]
dependsOn = ["setup"]

[phases.custom]
cmds = ["echo"]

[start]
cmd = "node dist/index.js"
runImage = "ubuntu"

[staticAssets]
"a.txt" = "x"
`

func TestNixpacks(t *testing.T) {
	h, err := ParseNixpacks("nixpacks.toml", []byte(nixpacksTOML))
	if err != nil {
		t.Fatal(err)
	}
	if h.StartCmd != "node dist/index.js" || !reflect.DeepEqual(h.BuildCmds, []string{"npm ci", "npm run build"}) {
		t.Fatalf("hints = %+v", h)
	}
	if h.Variables["NODE_ENV"] != "production" || h.NixPkgs[0] != "ffmpeg" || h.AptPkgs[0] != "libvips" || h.ProviderHint != "node" {
		t.Fatalf("hints = %+v", h)
	}
	wantIgnored := []string{"phases.build.dependsOn", "phases.custom", "phases.setup.nixLibs", "providers", "start.runImage", "staticAssets"}
	if !reflect.DeepEqual(h.IgnoredKeys, wantIgnored) {
		t.Fatalf("ignored = %v", h.IgnoredKeys)
	}
	if h.SourceLines["startCmd"] != 19 || h.SourceLines["buildCmds"] != 12 {
		t.Fatalf("lines = %v", h.SourceLines)
	}
	r := Merge(api.ServiceConfig{}, Overlay(nil, h), "production")
	if r.Config.Deploy.StartCommand != "node dist/index.js" || r.Config.Build.BuildCommand != "npm ci && npm run build" || r.Source["deploy.startCommand"] != "nixpacks.toml:19" {
		t.Fatalf("merged = %+v %v", r.Config, r.Source)
	}
	if len(r.Notices) != len(wantIgnored) {
		t.Fatalf("notices = %v", r.Notices)
	}
	off := false
	r = Merge(api.ServiceConfig{Build: api.BuildConfig{NixpacksCompat: &off}}, Overlay(nil, h), "production")
	if r.Config.Deploy.StartCommand != "" {
		t.Fatal("nixpacksCompat false must ignore hints")
	}
	r = Merge(api.ServiceConfig{Deploy: api.DeployConfig{StartCommand: "ui"}}, Overlay(nil, h), "")
	if r.Config.Deploy.StartCommand != "ui" {
		t.Fatal("hints must not override a set start command")
	}
	if _, err := ParseNixpacks("nixpacks.toml", []byte("[x")); err == nil {
		t.Fatal("bad TOML accepted")
	}
	if toStrings("a")[0] != "a" || toStrings([]string{"b"})[0] != "b" || toStrings(3) != nil {
		t.Fatal("toStrings")
	}
}

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"app/railway.json":     {Data: []byte(`{"deploy": {"startCommand": "go run ."}, "build": {"nixpacksConfigPath": "cfg/nix.toml"}}`)},
		"app/cfg/nix.toml":     {Data: []byte("[start]\ncmd = \"x\"\n")},
		"infra/5050.yaml":      {Data: []byte("deploy:\n  startCommand: go build\n")},
		"plain/nixpacks.toml":  {Data: []byte("[phases.build]\ncmds = [\"make\"]\n")},
		"bad/railway.json":     {Data: []byte(`{"nope": 1}`)},
		"badnix/nixpacks.toml": {Data: []byte(`[x`)},
	}
	ov, err := Load(fsys, "/app", "")
	if err != nil {
		t.Fatal(err)
	}
	if ov.Path != "app/railway.json" || ov.Config.Deploy == nil || *ov.Config.Deploy.StartCommand != "go run ." || ov.Nixpacks == nil || ov.Nixpacks.StartCmd != "x" {
		t.Fatalf("overlay = %+v", ov)
	}
	ov, err = Load(fsys, "/", "infra/5050.yaml")
	if err != nil || ov.Path != "infra/5050.yaml" || ov.Format != YAML {
		t.Fatalf("configPath: %+v %v", ov, err)
	}
	if _, err := Load(fsys, "/", "missing.json"); err == nil {
		t.Fatal("a missing config path must fail")
	}
	ov, err = Load(fsys, "plain", "")
	if err != nil || ov == nil || ov.Path != "" || ov.Nixpacks == nil {
		t.Fatalf("nixpacks only: %+v %v", ov, err)
	}
	ov, err = Load(fsys, "empty", "")
	if err != nil || ov != nil {
		t.Fatalf("nothing: %+v %v", ov, err)
	}
	if _, err := Load(fsys, "bad", ""); err == nil {
		t.Fatal("bad file must fail")
	}
	if _, err := Load(fsys, "badnix", ""); err == nil {
		t.Fatal("bad nixpacks must fail")
	}
	if Overlay(nil, nil) != nil {
		t.Fatal("nothing to overlay")
	}
	if clean("") != "." || clean("../x") != "x" {
		t.Fatal("clean")
	}
}

func TestHelpers(t *testing.T) {
	if FormatOf("a.yml") != YAML || FormatOf("a") != "" {
		t.Fatal("FormatOf")
	}
	if levenshtein("kitten", "sitting") != 3 {
		t.Fatal("levenshtein")
	}
	if suggest("STARTCOMMAND", []string{"startCommand"}) != "startCommand" {
		t.Fatal("case-insensitive suggestion")
	}
	if !validProcesses("auto") || validProcesses("0") || validProcesses("01") {
		t.Fatal("validProcesses")
	}
	if tomlPath(`a."b.c"`) != "a.b.c" || tomlPath("a..b") != "" {
		t.Fatalf("tomlPath = %q", tomlPath(`a."b.c"`))
	}
	l := yamlLines([]byte("\"quoted key\": 1\nurl: http://x\n'bad\n{flow: 1}\n"))
	if l["quoted key"] != 1 || l["url"] != 2 {
		t.Fatalf("yamlLines = %v", l)
	}
	tl := tomlLines([]byte("a = \"\"\"\nx = 1\n\"\"\"\nb = 2\n[t\n"))
	if tl["b"] != 4 || tl["x"] != 0 {
		t.Fatalf("tomlLines = %v", tl)
	}
	if lineAt([]byte("a\nb"), 99) != 2 || lineAt([]byte("a"), -1) != 1 {
		t.Fatal("lineAt")
	}
	if kindName(reflectTypeOf(map[string]int{})) != "an object" || kindName(reflectTypeOf(true)) != "true or false" || kindName(reflectTypeOf([]int{})) != "a list" || kindName(reflectTypeOf(1.5)) != "float64" {
		t.Fatal("kindName")
	}
	_ = manifest.RailwayConfig{}
}

func FuzzParse(f *testing.F) {
	f.Add("railway.json", railwayJSON)
	f.Add("railway.toml", railwayTOML)
	f.Add("5050.yaml", fiftyYAML)
	f.Add("nixpacks.toml", nixpacksTOML)
	f.Fuzz(func(t *testing.T, name, body string) {
		if name == "nixpacks.toml" {
			_, _ = ParseNixpacks(name, []byte(body))
			return
		}
		file, err := Parse(name, []byte(body))
		if err == nil && file != nil {
			_ = Merge(api.ServiceConfig{}, Overlay(file, nil), "production")
		}
	})
}
