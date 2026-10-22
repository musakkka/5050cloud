// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func strp(s string) *string { return &s }

func overlay(b *manifest.RailwayBuild, d *manifest.RailwayDeploy, envs map[string]manifest.RailwayEnvironment) *proto.ConfigOverlay {
	return &proto.ConfigOverlay{Path: "railway.json", Format: "json", Config: manifest.RailwayConfig{Build: b, Deploy: d, Environments: envs}}
}

func TestBuilderChoice(t *testing.T) {
	withDockerfile := map[string]string{"Dockerfile": "FROM alpine\n", "docker/Prod.Dockerfile": "FROM alpine\n"}
	noDockerfile := map[string]string{"package.json": "{}"}
	cases := []struct {
		name      string
		files     map[string]string
		job       proto.BuildJob
		ov        *proto.ConfigOverlay
		builder   string
		df        string
		nixpacks  bool
		detected  bool
		errSubstr string
	}{
		{name: "auto with Dockerfile", files: withDockerfile, job: proto.BuildJob{Builder: "auto"}, builder: builderDockerfile, df: "Dockerfile", detected: true},
		{name: "auto without Dockerfile", files: noDockerfile, job: proto.BuildJob{}, builder: builderRailpack},
		{name: "railpack forced", files: withDockerfile, job: proto.BuildJob{Builder: "railpack"}, builder: builderRailpack},
		{name: "dockerfile forced and missing", files: noDockerfile, job: proto.BuildJob{Builder: "dockerfile"}, errSubstr: "No Dockerfile at Dockerfile. Check the Dockerfile path in Settings > Build."},
		{name: "dockerfile path setting", files: withDockerfile, job: proto.BuildJob{DockerfilePath: "docker/Prod.Dockerfile"}, builder: builderDockerfile, df: "docker/Prod.Dockerfile", detected: true},
		{name: "dockerfile path setting missing", files: noDockerfile, job: proto.BuildJob{DockerfilePath: "x/Dockerfile"}, errSubstr: "No Dockerfile at x/Dockerfile."},
		{name: "RAILWAY_DOCKERFILE_PATH variable", files: withDockerfile, job: proto.BuildJob{Env: map[string]string{"RAILWAY_DOCKERFILE_PATH": "docker/Prod.Dockerfile"}}, builder: builderDockerfile, df: "docker/Prod.Dockerfile", detected: true},
		{name: "FIFTY_DOCKERFILE_PATH variable", files: withDockerfile, job: proto.BuildJob{Env: map[string]string{"FIFTY_DOCKERFILE_PATH": "docker/Prod.Dockerfile"}}, builder: builderDockerfile, df: "docker/Prod.Dockerfile", detected: true},
		{name: "setting wins over variable", files: withDockerfile, job: proto.BuildJob{DockerfilePath: "Dockerfile", Env: map[string]string{"RAILWAY_DOCKERFILE_PATH": "docker/Prod.Dockerfile"}}, builder: builderDockerfile, df: "Dockerfile", detected: true},
		{name: "nixpacks setting", files: withDockerfile, job: proto.BuildJob{Builder: "nixpacks"}, builder: builderRailpack, nixpacks: true},
		{name: "NIXPACKS in the file beats the setting", files: withDockerfile, job: proto.BuildJob{Builder: "dockerfile"}, ov: overlay(&manifest.RailwayBuild{Builder: strp("NIXPACKS")}, nil, nil), builder: builderRailpack, nixpacks: true},
		{name: "file dockerfilePath", files: withDockerfile, ov: overlay(&manifest.RailwayBuild{Builder: strp("DOCKERFILE"), DockerfilePath: strp("docker/Prod.Dockerfile")}, nil, nil), builder: builderDockerfile, df: "docker/Prod.Dockerfile"},
		{name: "environment block wins", files: withDockerfile, job: proto.BuildJob{Env: map[string]string{"RAILWAY_ENVIRONMENT_NAME": "production"}},
			ov: overlay(&manifest.RailwayBuild{Builder: strp("DOCKERFILE")}, nil, map[string]manifest.RailwayEnvironment{"production": {Build: &manifest.RailwayBuild{Builder: strp("RAILPACK")}}}), builder: builderRailpack},
		{name: "other environment block ignored", files: withDockerfile, job: proto.BuildJob{Env: map[string]string{"RAILWAY_ENVIRONMENT_NAME": "staging"}},
			ov: overlay(&manifest.RailwayBuild{Builder: strp("DOCKERFILE")}, nil, map[string]manifest.RailwayEnvironment{"production": {Build: &manifest.RailwayBuild{Builder: strp("RAILPACK")}}}), builder: builderDockerfile, df: "Dockerfile"},
		{name: "heroku refused", files: noDockerfile, job: proto.BuildJob{Builder: "HEROKU"}, errSubstr: "The HEROKU builder is not supported. Use RAILPACK or DOCKERFILE."},
		{name: "unknown builder", files: noDockerfile, job: proto.BuildJob{Builder: "bazel"}, errSubstr: "The builder bazel is not known."},
		{name: "dockerfile path escaping the root", files: withDockerfile, job: proto.BuildJob{DockerfilePath: "../../etc/passwd"}, errSubstr: "No Dockerfile at ../../etc/passwd."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, c.files)
			p, err := resolvePlan(c.job, c.ov, nil, dir)
			if c.errSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), c.errSubstr) {
					t.Fatalf("err = %v, want it to contain %q", err, c.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Builder != c.builder || p.DockerfilePath != c.df || p.FromNixpacks != c.nixpacks || p.DetectedDockerfile != c.detected {
				t.Errorf("plan = builder %q df %q nixpacks %v detected %v", p.Builder, p.DockerfilePath, p.FromNixpacks, p.DetectedDockerfile)
			}
			logs := strings.Join(p.Log, "\n")
			if c.nixpacks && !strings.Contains(logs, "Nixpacks config detected, building with Railpack.") {
				t.Errorf("missing the Nixpacks log line: %q", logs)
			}
			if c.detected && !strings.Contains(logs, "Using detected Dockerfile") {
				t.Errorf("missing the detected Dockerfile log line: %q", logs)
			}
		})
	}
}

func TestRailpackVersionChoice(t *testing.T) {
	dir := t.TempDir()
	p, err := resolvePlan(proto.BuildJob{}, nil, nil, dir)
	if err != nil || p.Railpack.Version != CurrentRailpack().Version {
		t.Fatalf("default version: %v %v", p.Railpack.Version, err)
	}
	p, err = resolvePlan(proto.BuildJob{RailpackVersion: "v0.39.0"}, nil, nil, dir)
	if err != nil || p.Railpack.Version != "0.39.0" {
		t.Fatalf("setting: %v %v", p.Railpack.Version, err)
	}
	p, err = resolvePlan(proto.BuildJob{RailpackVersion: "0.39.0"}, overlay(&manifest.RailwayBuild{RailpackVersion: strp("0.40.0")}, nil, nil), nil, dir)
	if err != nil || p.Railpack.Version != "0.40.0" {
		t.Fatalf("file wins: %v %v", p.Railpack.Version, err)
	}
	_, err = resolvePlan(proto.BuildJob{RailpackVersion: "0.1.0"}, nil, nil, dir)
	want := "Railpack 0.1.0 is not available on this install. Available: 0.40.1, 0.40.0, 0.39.0."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if len(RailpackVersions()) != 3 {
		t.Errorf("the spec pins the current version and the two before it")
	}
}

func TestCommandsPriority(t *testing.T) {
	dir := t.TempDir()
	hints := &manifest.NixpacksHints{StartCmd: "nix start", BuildCmds: []string{"a", "b"}}
	p, err := resolvePlan(proto.BuildJob{}, nil, hints, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.StartCommand != "nix start" || p.BuildCommand != "a && b" {
		t.Errorf("hints only: %q %q", p.StartCommand, p.BuildCommand)
	}
	p, _ = resolvePlan(proto.BuildJob{StartCommand: "ui start", BuildCommand: "ui build"}, nil, hints, dir)
	if p.StartCommand != "ui start" || p.BuildCommand != "ui build" {
		t.Errorf("setting over hints: %q %q", p.StartCommand, p.BuildCommand)
	}
	ov := overlay(&manifest.RailwayBuild{BuildCommand: strp("file build")}, &manifest.RailwayDeploy{StartCommand: strp("file start")},
		map[string]manifest.RailwayEnvironment{"production": {Deploy: &manifest.RailwayDeploy{StartCommand: strp("prod start")}}})
	p, _ = resolvePlan(proto.BuildJob{StartCommand: "ui start", BuildCommand: "ui build"}, ov, hints, dir)
	if p.StartCommand != "file start" || p.BuildCommand != "file build" {
		t.Errorf("file over setting: %q %q", p.StartCommand, p.BuildCommand)
	}
	p, _ = resolvePlan(proto.BuildJob{Env: map[string]string{"RAILWAY_ENVIRONMENT_NAME": "production"}}, ov, hints, dir)
	if p.StartCommand != "prod start" {
		t.Errorf("environment block: %q", p.StartCommand)
	}
}

func TestNixpacksMapping(t *testing.T) {
	dir := t.TempDir()
	hints := &manifest.NixpacksHints{
		Variables: map[string]string{"FROM_FILE": "1", "KEEP": "file"},
		NixPkgs:   []string{"ffmpeg", "imagemagick"},
		AptPkgs:   []string{"libvips"},
	}
	job := proto.BuildJob{Builder: "nixpacks", Env: map[string]string{
		"KEEP":                  "service",
		"NIXPACKS_NODE_VERSION": "20",
		"NIXPACKS_BUILD_CMD":    "make",
		"RAILPACK_BUILD_CMD":    "already set",
		"NIXPACKS_NO_MUSL":      "1",
	}}
	p, err := resolvePlan(job, nil, hints, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FROM_FILE":                    "1",
		"KEEP":                         "service",
		"RAILPACK_PACKAGES":            "ffmpeg imagemagick",
		"RAILPACK_DEPLOY_APT_PACKAGES": "libvips",
		"RAILPACK_NODE_VERSION":        "20",
		"RAILPACK_BUILD_CMD":           "already set",
	}
	for k, v := range want {
		if p.Env[k] != v {
			t.Errorf("Env[%s] = %q, want %q", k, p.Env[k], v)
		}
	}
	if len(p.Notices) != 1 || p.Notices[0] != "Variable NIXPACKS_NO_MUSL is not supported by Railpack and was ignored." {
		t.Errorf("notices = %q", p.Notices)
	}
	if !strings.Contains(strings.Join(p.Log, "\n"), "Variable NIXPACKS_NODE_VERSION was passed to Railpack as RAILPACK_NODE_VERSION.") {
		t.Errorf("log = %q", p.Log)
	}
	if job.Env["RAILPACK_NODE_VERSION"] != "" {
		t.Errorf("the job's own map must not change")
	}
}

func TestNoCacheVariable(t *testing.T) {
	dir := t.TempDir()
	for _, env := range []map[string]string{{"NO_CACHE": "1"}, {"FIFTY_NO_CACHE": "true"}} {
		p, _ := resolvePlan(proto.BuildJob{Env: env}, nil, nil, dir)
		if !p.NoCache {
			t.Errorf("%v should disable the cache", env)
		}
	}
	p, _ := resolvePlan(proto.BuildJob{Env: map[string]string{"NO_CACHE": "0"}}, nil, nil, dir)
	if p.NoCache {
		t.Errorf("NO_CACHE=0 keeps the cache")
	}
}
