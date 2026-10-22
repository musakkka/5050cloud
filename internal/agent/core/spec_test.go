// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/internal/agent/assets"
	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestWrapCluster(t *testing.T) {
	cases := []struct {
		argv []string
		want []string
		ok   bool
	}{
		{[]string{"node", "server.js"}, []string{"node", assets.ClusterTarget, "4", "server.js"}, true},
		{[]string{"/usr/local/bin/node", "--max-old-space-size=512", "dist/index.js", "--port", "3000"},
			[]string{"/usr/local/bin/node", "--max-old-space-size=512", assets.ClusterTarget, "4", "dist/index.js", "--port", "3000"}, true},
		{[]string{"node", "-r", "dotenv/config", "app.js"}, []string{"node", "-r", "dotenv/config", assets.ClusterTarget, "4", "app.js"}, true},
		{[]string{"node", "--import", "tsx", "app.ts"}, []string{"node", "--import", "tsx", assets.ClusterTarget, "4", "app.ts"}, true},
		{[]string{"nodejs", "app.js"}, []string{"nodejs", assets.ClusterTarget, "4", "app.js"}, true},
		{[]string{"node", "-e", "console.log(1)"}, nil, false},
		{[]string{"node", "--inspect"}, nil, false},
		{[]string{"node"}, nil, false},
		{[]string{"npm", "start"}, nil, false},
		{[]string{"sh", "-c", "node app.js"}, nil, false},
		{[]string{"bun", "app.js"}, nil, false},
	}
	for _, c := range cases {
		got, ok := WrapCluster(c.argv, 4)
		if ok != c.ok || !slices.Equal(got, c.want) {
			t.Errorf("WrapCluster(%q) = %q %v, want %q %v", c.argv, got, ok, c.want, c.ok)
		}
	}
	if _, ok := WrapCluster([]string{"node", "a.js"}, 0); ok {
		t.Error("n=0 must not wrap")
	}
}

func TestBuildSpecClusterWrap(t *testing.T) {
	s := worker("rep_1")
	s.Processes = 3
	s.ClusterWrap = true
	s.Cmd = nil
	in := specInput{Epoch: "e", DataDir: "/var/lib/5050", AssetPaths: map[string]string{assets.ClusterName: "/var/lib/5050/assets/cluster.cjs"}, ImageArgv: []string{"node", "index.js"}}
	sp, notes, err := buildSpec(s, in)
	if err != nil || len(notes) != 0 {
		t.Fatal(err, notes)
	}
	if !slices.Equal(sp.Entrypoint, []string{"node", assets.ClusterTarget, "3", "index.js"}) || len(sp.Cmd) != 0 || sp.Cmd == nil {
		t.Fatalf("entrypoint %q cmd %q", sp.Entrypoint, sp.Cmd)
	}
	m := sp.Mounts[0]
	if m.Source != "/var/lib/5050/assets/cluster.cjs" || m.Target != assets.ClusterTarget || !m.ReadOnly {
		t.Fatalf("mount %+v", m)
	}
	for _, kv := range []string{"FIFTY_PROCESSES=3", "WEB_CONCURRENCY=3"} {
		if !slices.Contains(sp.Env, kv) {
			t.Errorf("env lacks %s: %v", kv, sp.Env)
		}
	}
	// A shell start command cannot be wrapped; the replica gets a note.
	s.Entrypoint = []string{"sh", "-c", "node index.js && echo"}
	sp, notes, err = buildSpec(s, in)
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0], "WEB_CONCURRENCY=3") {
		t.Fatalf("notes %v err %v", notes, err)
	}
	if !slices.Equal(sp.Entrypoint, s.Entrypoint) {
		t.Fatalf("entrypoint changed: %v", sp.Entrypoint)
	}
}

func TestBuildSpecMapping(t *testing.T) {
	s := worker("rep_1")
	s.Env = map[string]string{"WEB_CONCURRENCY": "7", "RAILWAY_RUN_UID": "0"}
	s.MemLimitMB = 512
	s.MemoryReservation = 256 << 20
	s.CPULimit = 1.5
	s.ShmSizeMB = 64
	s.Processes = 2
	s.StopGraceSec = 20
	s.Cmd = []string{"./server"}
	s.Labels = map[string]string{"extra": "1"}
	s.Mounts = []proto.Mount{
		{Kind: "volume", Source: "5050-vol_a", Target: "/data"},
		{Kind: "tool", Source: "wal-g", Target: "/opt/5050/bin/wal-g"},
		{Kind: "asset", Source: assets.ClusterName, Target: "/x.cjs"},
		{Kind: "tmpfs", Target: "/tmp"},
	}
	s.Files = []proto.FileSpec{{Path: "/etc/app.conf", Content: []byte("x")}}
	in := specInput{
		Epoch: "ep9", DNS: "10.129.4.1",
		ToolPaths:  map[string]string{"wal-g": "/var/lib/5050/tools/wal-g-3"},
		AssetPaths: map[string]string{assets.ClusterName: "/a/cluster.cjs"},
		FilePaths:  map[string]string{"/etc/app.conf": "/f/app.conf"},
	}
	sp, _, err := buildSpec(s, in)
	if err != nil {
		t.Fatal(err)
	}
	if sp.MemoryBytes != 512<<20 || sp.MemorySwap != 512<<20 || sp.MemoryReservation != 256<<20 || sp.NanoCPUs != 1_500_000_000 || sp.ShmSize != 64<<20 {
		t.Errorf("limits %+v", sp)
	}
	if sp.User != "0" || sp.StopTimeoutSec != 20 || sp.Labels[engine.LabelStopGrace] != "20" || sp.Labels["extra"] != "1" || sp.Labels[engine.LabelEpoch] != "ep9" {
		t.Errorf("user/grace/labels %+v", sp)
	}
	if !slices.Contains(sp.Env, "WEB_CONCURRENCY=7") || !slices.Contains(sp.Env, "FIFTY_PROCESSES=2") {
		t.Errorf("a WEB_CONCURRENCY the user set must win: %v", sp.Env)
	}
	if sp.Entrypoint != nil || !slices.Equal(sp.Cmd, []string{"./server"}) {
		t.Errorf("a Cmd alone keeps the image entrypoint: %q %q", sp.Entrypoint, sp.Cmd)
	}
	if !slices.Equal(sp.DNS, []string{"10.129.4.1"}) || sp.LogMaxSize != "20m" || sp.LogMaxFile != 3 {
		t.Errorf("dns/logs %+v", sp)
	}
	want := []engine.Mount{
		{Type: engine.MountVolume, Source: "5050-vol_a", Target: "/data"},
		{Type: engine.MountBind, Source: "/var/lib/5050/tools/wal-g-3", Target: "/opt/5050/bin/wal-g", ReadOnly: true},
		{Type: engine.MountBind, Source: "/a/cluster.cjs", Target: "/x.cjs", ReadOnly: true},
		{Type: engine.MountTmpfs, Target: "/tmp"},
		{Type: engine.MountBind, Source: "/f/app.conf", Target: "/etc/app.conf", ReadOnly: true},
	}
	if !slices.Equal(sp.Mounts, want) {
		t.Errorf("mounts\n got %+v\nwant %+v", sp.Mounts, want)
	}
	// Entrypoint replaces both ENTRYPOINT and CMD.
	s.Entrypoint = []string{"/bin/app"}
	s.Cmd = nil
	sp, _, _ = buildSpec(s, in)
	if !slices.Equal(sp.Entrypoint, []string{"/bin/app"}) || sp.Cmd == nil || len(sp.Cmd) != 0 {
		t.Errorf("entrypoint %q cmd %#v", sp.Entrypoint, sp.Cmd)
	}
	// Memory swap below the limit is raised to the limit.
	s.MemorySwap = 1
	sp, _, _ = buildSpec(s, in)
	if sp.MemorySwap != sp.MemoryBytes {
		t.Errorf("swap %d", sp.MemorySwap)
	}
}

func TestBuildSpecErrors(t *testing.T) {
	for name, m := range map[string]proto.Mount{
		"tool":  {Kind: "tool", Source: "restic", Target: "/r"},
		"asset": {Kind: "asset", Source: "nope", Target: "/n"},
		"kind":  {Kind: "nfs", Target: "/n"},
	} {
		s := worker("rep_1")
		s.Mounts = []proto.Mount{m}
		if _, _, err := buildSpec(s, specInput{}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	s := worker("rep_1")
	s.Files = []proto.FileSpec{{Path: "/etc/x"}}
	if _, _, err := buildSpec(s, specInput{}); err == nil {
		t.Error("an unwritten file must fail")
	}
}

func TestSpecHashIgnoresRuntimeOnlyFields(t *testing.T) {
	a := worker("rep_1")
	b := a
	b.Health = proto.HealthCheck{Path: "/x", IntervalSec: 1}
	b.Desired = "stopped"
	b.Probe = []string{"probe"}
	b.StopGraceSec = 99
	if SpecHash(a) != SpecHash(b) {
		t.Fatal("health, desired, probe and grace must not change the hash")
	}
	b.Image = "other"
	if SpecHash(a) == SpecHash(b) {
		t.Fatal("the image must change the hash")
	}
}

func TestRestartPolicyMapping(t *testing.T) {
	cases := map[string]engine.Restart{
		"always":     {Name: "unless-stopped"},
		"never":      {Name: "no"},
		"on_failure": {Name: "on-failure", MaxRetries: 10},
		"":           {Name: "on-failure", MaxRetries: 10},
	}
	for p, want := range cases {
		if got := RestartPolicy(proto.RestartPolicy{Policy: p}); got != want {
			t.Errorf("%q: %+v", p, got)
		}
	}
	if got := RestartPolicy(proto.RestartPolicy{Policy: "on_failure", MaxRetries: 4}); got.MaxRetries != 4 {
		t.Errorf("max retries %+v", got)
	}
}

func TestFileValidationAndNames(t *testing.T) {
	for _, p := range []string{"relative", "/a/../b", "/", "/a/"} {
		if validateFile(proto.FileSpec{Path: p}) == nil {
			t.Errorf("%q must be refused", p)
		}
	}
	if validateFile(proto.FileSpec{Path: "/a", Content: make([]byte, maxFileBytes+1)}) == nil {
		t.Error("a large file must be refused")
	}
	if validateFile(proto.FileSpec{Path: "/etc/postgresql/pg.conf"}) != nil {
		t.Error("a valid path was refused")
	}
	a, b := fileName("/etc/a.conf"), fileName("/var/a.conf")
	if a == b || !strings.HasSuffix(a, "-a.conf") || strings.Contains(fileName("/x/.."), "/") {
		t.Errorf("names %s %s", a, b)
	}
	if !strings.HasSuffix(fileName("/x/a b$"), "-a_b_") {
		t.Errorf("unsafe characters: %s", fileName("/x/a b$"))
	}
}

func TestWriteFilesAtomicModes(t *testing.T) {
	ta := newTestAgent(t)
	dir := t.TempDir()
	out, err := ta.writeFiles(dir, []proto.FileSpec{{Path: "/etc/a", Content: []byte("one"), Mode: 0o600}, {Path: "/etc/b", Content: []byte("two")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatal(out)
	}
	if _, err := ta.writeFiles(dir, []proto.FileSpec{{Path: "bad"}}); err == nil {
		t.Fatal("bad path accepted")
	}
}
