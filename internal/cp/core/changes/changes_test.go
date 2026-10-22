// SPDX-License-Identifier: AGPL-3.0-only

package changes

import (
	"reflect"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/pkg/api"
)

func base() api.ServiceConfig {
	return api.ServiceConfig{
		Name:   "api",
		Source: api.SourceConfig{Type: "github", Repo: "acme/api", Branch: "main"},
		Deploy: api.DeployConfig{StartCommand: "node a.js", Replicas: 1},
		Net:    api.NetConfig{Port: 3000},
	}
}

func TestApplyAndMerge(t *testing.T) {
	c := base()
	p1 := map[string]any{"deploy": map[string]any{"startCommand": "node b.js", "replicas": 2}}
	p2 := map[string]any{"deploy": map[string]any{"replicas": 3, "healthcheckPath": "/h"}, "networking": map[string]any{"port": nil}}
	merged := MergePatch(p1, p2)
	got, err := Apply(c, merged)
	if err != nil {
		t.Fatal(err)
	}
	if got.Deploy.StartCommand != "node b.js" || got.Deploy.Replicas != 3 || got.Deploy.HealthcheckPath != "/h" || got.Net.Port != 0 {
		t.Fatalf("got %+v", got)
	}
	seq, _ := Apply(c, p1)
	seq, _ = Apply(seq, p2)
	if !reflect.DeepEqual(seq, got) {
		t.Fatal("merging patches must equal applying them in order")
	}
	if c.Deploy.StartCommand != "node a.js" {
		t.Fatal("Apply modified its input")
	}
	if same, err := Apply(c, nil); err != nil || !reflect.DeepEqual(same, c) {
		t.Fatal("empty patch")
	}
	_, err = Apply(c, map[string]any{"deploy": map[string]any{"startComand": "x"}})
	if e := fail.As(err); e == nil || e.Field != "deploy.startComand" {
		t.Fatalf("unknown field: %v", err)
	}
	_, err = Apply(c, map[string]any{"deploy": map[string]any{"replicas": "two"}})
	if e := fail.As(err); e == nil || !strings.Contains(e.Msg, "whole number") {
		t.Fatalf("type error: %v", err)
	}
	if _, err := FromMap(map[string]any{"name": map[string]any{"x": 1}}); err == nil {
		t.Fatal("object for a string accepted")
	}
	if _, err := FromMap(map[string]any{"bogus": 1}); err == nil {
		t.Fatal("unknown top-level field accepted")
	}
	pb := PatchBetween(c, got)
	again, err := Apply(c, pb)
	if err != nil || !reflect.DeepEqual(again, got) {
		t.Fatalf("PatchBetween round trip: %v %+v", err, pb)
	}
	if paths := Paths(merged); !reflect.DeepEqual(paths, []string{"deploy.healthcheckPath", "deploy.replicas", "deploy.startCommand", "networking.port"}) {
		t.Fatalf("Paths = %v", paths)
	}
	if !reflect.DeepEqual(Clone(got), got) {
		t.Fatal("Clone")
	}
}

func TestDiff(t *testing.T) {
	a := base()
	b := Clone(a)
	b.Deploy.StartCommand = "node b.js"
	b.Build.WatchPaths = []string{"src/**"}
	d := Diff(a, b)
	if len(d) != 2 || d[0].Path != "build.watchPaths" || d[1].Path != "deploy.startCommand" || d[1].Old != "node a.js" || d[1].New != "node b.js" {
		t.Fatalf("diff = %+v", d)
	}
	if len(Diff(a, a)) != 0 {
		t.Fatal("no diff for equal configs")
	}
	c := Clone(a)
	c.Deploy.Replicas = 4
	if !OnlyReplicas(a, c) || OnlyReplicas(a, b) {
		t.Fatal("OnlyReplicas")
	}
}

func TestVarOps(t *testing.T) {
	cur := map[string]Var{"A": {Value: "1"}, "B": {Value: "2"}, "S": {Value: "x", Sealed: true}}
	o := VarOps{Set: map[string]string{"A": "10", "C": "3"}, Unset: []string{"B"}}
	o = o.Merge(VarOps{Set: map[string]string{"B": "20"}, Seal: []string{"A"}})
	o = o.Merge(VarOps{Unset: []string{"C"}, NeedsValue: []string{"D"}})
	if o.Set["A"] != "10" || o.Set["B"] != "20" || o.Set["C"] != "" || !reflect.DeepEqual(o.Unset, []string{"C"}) || !reflect.DeepEqual(o.Seal, []string{"A"}) {
		t.Fatalf("merged = %+v", o)
	}
	n := o.Normalize(cur)
	if len(n.Unset) != 0 || n.Set["A"] != "10" {
		t.Fatalf("normalized = %+v", n)
	}
	noop := VarOps{Set: map[string]string{"A": "1"}, Seal: []string{"S"}, Unset: []string{"Z"}}.Normalize(cur)
	if !noop.Empty() {
		t.Fatalf("no-op ops not dropped: %+v", noop)
	}
	next := o.Apply(cur)
	if next["A"].Value != "10" || !next["A"].Sealed || next["B"].Value != "20" {
		t.Fatalf("applied = %+v", next)
	}
	if _, ok := next["C"]; ok {
		t.Fatal("C was unset")
	}
	if o.StagedState("A", cur) != "update" || o.StagedState("C", cur) != "" || o.StagedState("Z", cur) != "" {
		t.Fatal("StagedState")
	}
	add := VarOps{Set: map[string]string{"N": "1"}}
	if add.StagedState("N", cur) != "add" {
		t.Fatal("add")
	}
	del := VarOps{Unset: []string{"B"}}
	if del.StagedState("B", cur) != "delete" {
		t.Fatal("delete")
	}
	seal := VarOps{Seal: []string{"B"}}
	if seal.StagedState("B", cur) != "update" {
		t.Fatal("seal is an update")
	}
	same := VarOps{Set: map[string]string{"A": "1"}}
	if same.StagedState("A", cur) != "" {
		t.Fatal("same value is not staged")
	}
	if VarOps.Normalize(VarOps{Set: map[string]string{"A": "1"}, NeedsValue: []string{"A"}}, cur).Empty() {
		t.Fatal("a needs-value marker keeps the op")
	}
}

func TestDiffVars(t *testing.T) {
	cur := map[string]Var{
		"DATABASE_URL": {Value: "${{Postgres.DATABASE_URL}}"},
		"API_KEY":      {Value: "old-key"},
		"PLAIN":        {Value: "hello"},
		"GONE":         {Value: "bye"},
	}
	o := VarOps{
		Set:   map[string]string{"DATABASE_URL": "${{db.DATABASE_URL}}", "API_KEY": "new-key", "NEW": "x", "PLAIN": "hello"},
		Unset: []string{"GONE"},
		Seal:  []string{"PLAIN"},
	}
	d := DiffVars("variables", cur, o)
	got := map[string]api.DiffEntry{}
	for _, e := range d {
		got[e.Path] = e
	}
	if got["variables.API_KEY"].Old != MaskedValue || got["variables.API_KEY"].New != MaskedValue {
		t.Fatal("secret values must be masked")
	}
	if got["variables.DATABASE_URL"].New != "${{db.DATABASE_URL}}" {
		t.Fatal("references are shown")
	}
	if got["variables.GONE"].New != nil || got["variables.NEW"].Old != nil || got["variables.NEW"].New != MaskedValue {
		t.Fatalf("add/remove = %+v", d)
	}
	if _, ok := got["variables.PLAIN (sealed)"]; !ok {
		t.Fatalf("seal not listed: %+v", d)
	}
	if Shown("X", "", false) != "" {
		t.Fatal("empty stays empty")
	}
}

func TestValidate(t *testing.T) {
	ok := base()
	if err := Validate(ok, false); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		field string
		mod   func(*api.ServiceConfig)
		vol   bool
	}{
		{"name", func(c *api.ServiceConfig) { c.Name = strings.Repeat("a", 33) }, false},
		{"name", func(c *api.ServiceConfig) { c.Name = "" }, false},
		{"name", func(c *api.ServiceConfig) { c.Name = "bad/name" }, false},
		{"name", func(c *api.ServiceConfig) { c.Name = " pad" }, false},
		{"networking.privateName", func(c *api.ServiceConfig) { c.Net.PrivateName = "Bad_Name" }, false},
		{"source.type", func(c *api.ServiceConfig) { c.Source.Type = "ftp" }, false},
		{"source.image", func(c *api.ServiceConfig) { c.Source = api.SourceConfig{Type: "image"} }, false},
		{"source.repo", func(c *api.ServiceConfig) { c.Source.Repo = "" }, false},
		{"source.waitForCi", func(c *api.ServiceConfig) { c.Source.WaitForCI = true }, false},
		{"source.rootDir", func(c *api.ServiceConfig) { c.Source.RootDir = "app" }, false},
		{"build.builder", func(c *api.ServiceConfig) { c.Build.Builder = "heroku" }, false},
		{"build.timeoutMin", func(c *api.ServiceConfig) { c.Build.TimeoutMin = 9999 }, false},
		{"deploy.replicas", func(c *api.ServiceConfig) { c.Deploy.Replicas = -1 }, false},
		{"deploy.replicas", func(c *api.ServiceConfig) { c.Deploy.Replicas = 101 }, false},
		{"deploy.replicas", func(c *api.ServiceConfig) { c.Deploy.Replicas = 2 }, true},
		{"deploy.cronSchedule", func(c *api.ServiceConfig) { c.Deploy.CronSchedule = "every day" }, false},
		{"deploy.cronSchedule", func(c *api.ServiceConfig) { c.Deploy.CronSchedule = "@every 5m" }, false},
		{"deploy.cronTimezone", func(c *api.ServiceConfig) { c.Deploy.CronSchedule = "@daily"; c.Deploy.CronTimezone = "Mars/Base" }, false},
		{"deploy.cronOverlap", func(c *api.ServiceConfig) { c.Deploy.CronOverlap = "both" }, false},
		{"deploy.healthcheckPath", func(c *api.ServiceConfig) { c.Deploy.HealthcheckPath = "health" }, false},
		{"deploy.healthcheckTimeoutSec", func(c *api.ServiceConfig) { c.Deploy.HealthcheckTimeoutSec = 4000 }, false},
		{"deploy.preDeployTimeoutSec", func(c *api.ServiceConfig) { c.Deploy.PreDeployTimeoutSec = -1 }, false},
		{"deploy.restartPolicy", func(c *api.ServiceConfig) { c.Deploy.RestartPolicy = "sometimes" }, false},
		{"deploy", func(c *api.ServiceConfig) { c.Deploy.DrainingSec = -1 }, false},
		{"deploy.maxSurge", func(c *api.ServiceConfig) { c.Deploy.MaxSurge = "lots" }, false},
		{"deploy.maxSurge", func(c *api.ServiceConfig) { c.Deploy.MaxSurge = "200%" }, false},
		{"deploy.processes", func(c *api.ServiceConfig) { c.Deploy.Processes = "64" }, false},
		{"deploy.cpuLimit", func(c *api.ServiceConfig) { c.Deploy.CPULimit = -1 }, false},
		{"deploy.memoryLimitMb", func(c *api.ServiceConfig) { c.Deploy.MemoryLimitMB = 32 }, false},
		{"deploy.memoryReservationMb", func(c *api.ServiceConfig) { c.Deploy.MemoryReservationMB = 10 }, false},
		{"deploy.memoryReservationMb", func(c *api.ServiceConfig) { c.Deploy.MemoryLimitMB = 128; c.Deploy.MemoryReservationMB = 256 }, false},
		{"deploy.shmSizeMb", func(c *api.ServiceConfig) { c.Deploy.ShmSizeMB = -1 }, false},
		{"deploy.placement.roles", func(c *api.ServiceConfig) { c.Deploy.Placement.Roles = []string{"gpu"} }, false},
		{"networking.port", func(c *api.ServiceConfig) { c.Net.Port = 70000 }, false},
	}
	for _, tc := range cases {
		c := Clone(ok)
		tc.mod(&c)
		err := Validate(c, tc.vol)
		e := fail.As(err)
		if e == nil || e.Field != tc.field {
			t.Errorf("expected a validation error on %s, got %v", tc.field, err)
		}
	}
	good := Clone(ok)
	good.Deploy.CronSchedule, good.Deploy.CronTimezone, good.Deploy.MaxSurge, good.Deploy.Processes = "*/5 * * * *", "Europe/Berlin", "25%", "auto"
	if err := Validate(good, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"", "data", "/", "//", "/a/../b"} {
		if CheckMountPath(p) == nil {
			t.Errorf("mount path %q accepted", p)
		}
	}
	if CheckMountPath("/data") != nil {
		t.Fatal("/data rejected")
	}
	if CheckVariables(map[string]string{"OK": "${{x"}) == nil || CheckVariables(map[string]string{"1X": "a"}) == nil || CheckVariables(map[string]string{"A": "${{B}}"}) != nil {
		t.Fatal("CheckVariables")
	}
}

func TestSlugAndNames(t *testing.T) {
	for in, want := range map[string]string{"My API": "my-api", "api": "api", "--x--": "x", "!!!": "service", strings.Repeat("ab", 40): strings.Repeat("ab", 31) + "a"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	c := base()
	if PrivateName(c) != "api" {
		t.Fatal("derived private name")
	}
	c.Net.PrivateName = "backend"
	if PrivateName(c) != "backend" {
		t.Fatal("explicit private name")
	}
	taken := map[string]bool{"api": true, "api-2": true}
	if UniqueName("api", taken) != "api-3" || UniqueName("web", taken) != "web" {
		t.Fatal("UniqueName")
	}
	long := strings.Repeat("x", 32)
	if n := UniqueName(long, map[string]bool{long: true}); len(n) != 32 || !strings.HasSuffix(n, "-2") {
		t.Fatalf("UniqueName of a long name = %q", n)
	}
	if itoa(0) != "0" || itoa(120) != "120" {
		t.Fatal("itoa")
	}
}

func TestClassify(t *testing.T) {
	a := base()
	cases := []struct {
		name          string
		old, new      *api.ServiceConfig
		vars          bool
		action        string
		rebuild, okay bool
	}{
		{"nothing", nil, nil, false, "", false, false},
		{"create", nil, &a, false, ActionCreate, true, true},
		{"delete", &a, nil, false, ActionDelete, false, true},
		{"same", &a, &a, false, "", false, false},
		{"vars only", &a, &a, true, ActionUpdate, false, true},
	}
	b := Clone(a)
	b.Source.Branch = "dev"
	cases = append(cases, struct {
		name          string
		old, new      *api.ServiceConfig
		vars          bool
		action        string
		rebuild, okay bool
	}{"source", &a, &b, false, ActionUpdate, true, true})
	c := Clone(a)
	c.Deploy.Replicas = 3
	cases = append(cases, struct {
		name          string
		old, new      *api.ServiceConfig
		vars          bool
		action        string
		rebuild, okay bool
	}{"scale", &a, &c, false, ActionScale, false, true}, struct {
		name          string
		old, new      *api.ServiceConfig
		vars          bool
		action        string
		rebuild, okay bool
	}{"scale with vars", &a, &c, true, ActionUpdate, false, true})
	d := Clone(a)
	d.Deploy.StartCommand = "x"
	cases = append(cases, struct {
		name          string
		old, new      *api.ServiceConfig
		vars          bool
		action        string
		rebuild, okay bool
	}{"deploy", &a, &d, false, ActionUpdate, false, true})
	for _, tc := range cases {
		act, rb, ok := Classify(tc.old, tc.new, tc.vars)
		if act != tc.action || rb != tc.rebuild || ok != tc.okay {
			t.Errorf("%s: got %q %v %v", tc.name, act, rb, ok)
		}
	}
}

func TestSync(t *testing.T) {
	cfgA := base()
	cfgB := Clone(cfgA)
	cfgB.Deploy.StartCommand = "node prod.js"
	worker := Clone(cfgA)
	worker.Name = "worker"
	src := map[string]EnvService{
		"svc_a": {ID: "svc_a", Config: cfgA, Vars: map[string]Var{"LOG": {Value: "debug"}, "API_KEY": {Value: "staging-key"}, "NEWVAR": {Value: "n"}, "NEWSECRET": {Value: "s", Sealed: true}}},
		"svc_w": {ID: "svc_w", Config: worker, Vars: map[string]Var{"Q": {Value: "jobs"}, "TOKEN": {Value: "t"}}},
		"svc_s": {ID: "svc_s", Config: cfgA, Vars: map[string]Var{"X": {Value: "1"}}},
	}
	renamed := Clone(cfgA)
	renamed.Name = "api-prod"
	dst := map[string]EnvService{
		"svc_a": {ID: "svc_a", Config: cfgB, Vars: map[string]Var{"LOG": {Value: "info"}, "API_KEY": {Value: "prod-key"}, "OLD": {Value: "o"}}},
		"svc_s": {ID: "svc_s", Config: renamed, Vars: map[string]Var{"X": {Value: "1"}}},
		"svc_r": {ID: "svc_r", Config: cfgA},
	}
	r := Sync(src, dst, map[string]Var{"SHARED": {Value: "1"}, "SHARED_SECRET": {Value: "s"}}, map[string]Var{"SHARED": {Value: "2"}, "ONLY_HERE": {Value: "x"}})
	if len(r.Creates) != 1 || r.Creates[0].Service.ID != "svc_w" {
		t.Fatalf("creates = %+v", r.Creates)
	}
	cw := r.Creates[0].Vars
	if cw.Set["Q"] != "jobs" || cw.Set["TOKEN"] != "" || !reflect.DeepEqual(cw.NeedsValue, []string{"TOKEN"}) {
		t.Fatalf("create vars = %+v", cw)
	}
	if len(r.Edits) != 1 || r.Edits[0].ServiceID != "svc_a" {
		t.Fatalf("edits = %+v (renamed service with equal config must not be edited)", r.Edits)
	}
	e := r.Edits[0]
	if e.Patch["deploy"].(map[string]any)["startCommand"] != "node a.js" {
		t.Fatalf("patch = %+v", e.Patch)
	}
	if e.Vars.Set["LOG"] != "debug" || e.Vars.Set["NEWVAR"] != "n" || e.Vars.Set["NEWSECRET"] != "" {
		t.Fatalf("edit vars = %+v", e.Vars)
	}
	if _, copied := e.Vars.Set["API_KEY"]; copied {
		t.Fatal("secret values must never be copied")
	}
	if !reflect.DeepEqual(e.Vars.Unset, []string{"OLD"}) || !reflect.DeepEqual(e.Vars.NeedsValue, []string{"NEWSECRET"}) {
		t.Fatalf("edit vars = %+v", e.Vars)
	}
	if !reflect.DeepEqual(r.Removed, []string{"svc_r"}) {
		t.Fatalf("removed = %v", r.Removed)
	}
	if r.Shared.Set["SHARED"] != "1" || r.Shared.Set["SHARED_SECRET"] != "" || !reflect.DeepEqual(r.Shared.Unset, []string{"ONLY_HERE"}) {
		t.Fatalf("shared = %+v", r.Shared)
	}
	dup := CopyVarsDropSealed(map[string]Var{"A": {Value: "1"}, "S": {Value: "2", Sealed: true}, "API_KEY": {Value: "k"}})
	if dup.Set["A"] != "1" || dup.Set["API_KEY"] != "k" || len(dup.Set) != 2 {
		t.Fatalf("duplicate vars = %+v", dup)
	}
	if CopyVarsDropSealed(map[string]Var{"S": {Value: "x", Sealed: true}}).Set != nil || CopyVars(nil).Set != nil {
		t.Fatal("empty copies")
	}
}
