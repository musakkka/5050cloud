// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/statecache"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestReconcileCreatesReplica(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	s := ta.attach()
	c := collect(s)
	ta.deliver(s, baseState(1, worker("rep_1")))

	waitFor(t, "container running", func() bool {
		ct, ok := ta.eng.Container("5050-rep_1")
		return ok && ct.Running()
	})
	sp, _ := ta.eng.SpecOf("5050-rep_1")
	for k, want := range map[string]string{
		engine.LabelReplica: "rep_1", engine.LabelDeployment: "dep_1", engine.LabelService: "svc_a",
		engine.LabelEnvironment: "env_1", engine.LabelEpoch: "ep1",
	} {
		if sp.Labels[k] != want {
			t.Errorf("label %s = %q, want %q", k, sp.Labels[k], want)
		}
	}
	if sp.IP != "10.129.4.7" || sp.Network != "5050-env_1" || sp.CgroupParent != "5050-apps.slice" || !sp.NoNewPrivileges {
		t.Errorf("spec network/ip/cgroup/nnp wrong: %+v", sp)
	}
	if sp.Restart.Name != "on-failure" || sp.Restart.MaxRetries != 3 {
		t.Errorf("restart policy = %+v", sp.Restart)
	}
	if !slices.Contains(sp.DNS, "10.129.0.1") {
		t.Errorf("DNS = %v", sp.DNS)
	}
	if !slices.Contains(ta.net.egress, "10.129.4.7") {
		t.Errorf("egress rules were not applied before start: %v", ta.net.egress)
	}
	// A worker without a port is healthy after 10 s (10 units here).
	waitFor(t, "healthy status", func() bool {
		st, ok := c.lastStatus(t, "rep_1")
		return ok && st.State == "healthy"
	})
	applied := c.of(proto.TStateApplied)
	if len(applied) != 1 || decodeAs[proto.StateApplied](t, applied[0]).Generation != 1 {
		t.Fatalf("state_applied = %+v", applied)
	}
	if g, e := ta.AppliedGeneration(); g != 1 || e != "ep1" {
		t.Errorf("applied = %d %s", g, e)
	}
}

func TestPullsMissingImageWithRegistryAuth(t *testing.T) {
	ta := newTestAgent(t)
	s := ta.attach()
	ta.deliver(s, baseState(1, worker("rep_1")))
	waitFor(t, "container running", func() bool {
		ct, ok := ta.eng.Container("5050-rep_1")
		return ok && ct.Running()
	})
	if !slices.Contains(ta.eng.CallsSnapshot(), "pull "+testImage) {
		t.Fatalf("no pull: %v", ta.eng.CallsSnapshot())
	}
}

func TestPullFailureReportsDiagnosis(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.PullErr[testImage] = engine.ErrNotFound
	s := ta.attach()
	c := collect(s)
	ta.deliver(s, baseState(1, worker("rep_1")))
	waitFor(t, "crashed status", func() bool {
		st, ok := c.lastStatus(t, "rep_1")
		return ok && st.State == "crashed" && strings.Contains(st.Diagnosis, "was not found")
	})
}

func TestUnlabelledContainersAreNeverTouched(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddContainer(engine.Container{Name: "someone-else", Labels: map[string]string{"app": "x"}})
	ta.eng.AddContainer(engine.Container{Name: "untagged"})
	s := ta.attach()
	ta.deliver(s, baseState(1))
	time.Sleep(20 * time.Millisecond)
	for _, call := range ta.eng.CallsSnapshot() {
		if strings.Contains(call, "someone-else") || strings.Contains(call, "untagged") {
			t.Fatalf("touched a foreign container: %s", call)
		}
	}
}

func TestStaleReplicaRemovedOnlyAfterFreshState(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	ta.eng.AddContainer(engine.Container{Name: "5050-rep_old", Labels: map[string]string{engine.LabelReplica: "rep_old", engine.LabelStopGrace: "1"}})

	// Without a connection (cache mode) nothing is removed.
	ds := baseState(1)
	errs, _ := ta.apply(context.Background(), &ds, ta.removalAllowedLocked())
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := ta.eng.Container("5050-rep_old"); !ok {
		t.Fatal("removed a container before a fresh desired state")
	}
	// A state from an older connection does not allow removal either.
	old := ta.attach()
	ta.attach()
	ta.deliver(old, baseState(2))
	time.Sleep(20 * time.Millisecond)
	if _, ok := ta.eng.Container("5050-rep_old"); !ok {
		t.Fatal("removed a container on a state from an old connection")
	}
	// A state on the current connection removes it: SIGTERM first, then remove.
	cur := ta.attach()
	ta.deliver(cur, baseState(3))
	waitFor(t, "stale container removed", func() bool {
		_, ok := ta.eng.Container("5050-rep_old")
		return !ok
	})
	calls := strings.Join(ta.eng.CallsSnapshot(), ",")
	if !strings.Contains(calls, "stop 5050-rep_old,remove 5050-rep_old") {
		t.Errorf("expected stop then remove, got %s", calls)
	}
}

func TestGenerationAndEpochGuard(t *testing.T) {
	ta := newTestAgent(t)
	s := ta.attach()
	c := collect(s)
	ta.deliver(s, baseState(5))
	low := baseState(3)
	ta.deliver(s, low)
	if g, _ := ta.AppliedGeneration(); g != 5 {
		t.Fatalf("a lower generation in the same epoch was applied: %d", g)
	}
	waitFor(t, "refusal", func() bool {
		for _, e := range c.of(proto.TStateApplied) {
			sa := decodeAs[proto.StateApplied](t, e)
			if sa.Generation == 3 && len(sa.Errors) == 1 && strings.Contains(sa.Errors[0], "Refused") {
				return true
			}
		}
		return false
	})
	// A new epoch (state restored) is accepted even with a lower generation.
	restored := baseState(2)
	restored.Epoch = "ep2"
	ta.deliver(s, restored)
	if g, e := ta.AppliedGeneration(); g != 2 || e != "ep2" {
		t.Fatalf("restored state not applied: %d %s", g, e)
	}
}

func TestAdoptRemovesNothingAndReportsUnknown(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	ta.eng.AddContainer(engine.Container{Name: "5050-rep_z", Image: "img:z", Labels: map[string]string{engine.LabelReplica: "rep_z", engine.LabelDeployment: "dep_z"}})
	s := ta.attach()
	c := collect(s)
	ds := baseState(1, worker("rep_1"))
	ds.Adopt = true
	ta.deliver(s, ds)
	waitFor(t, "state_applied", func() bool { return len(c.of(proto.TStateApplied)) == 1 })
	sa := decodeAs[proto.StateApplied](t, c.of(proto.TStateApplied)[0])
	if len(sa.Unknown) != 1 || sa.Unknown[0].ReplicaID != "rep_z" || sa.Unknown[0].DeploymentID != "dep_z" || sa.Unknown[0].Labels[engine.LabelReplica] != "rep_z" {
		t.Fatalf("unknown = %+v", sa.Unknown)
	}
	waitFor(t, "addition applied", func() bool { _, ok := ta.eng.Container("5050-rep_1"); return ok })
	time.Sleep(20 * time.Millisecond)
	if _, ok := ta.eng.Container("5050-rep_z"); !ok {
		t.Fatal("adopt mode removed an unknown container")
	}
}

func TestSpecChangeRecreatesContainer(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	s := ta.attach()
	ta.deliver(s, baseState(1, worker("rep_1")))
	waitFor(t, "created", func() bool { ct, ok := ta.eng.Container("5050-rep_1"); return ok && ct.Running() })
	first, _ := ta.eng.Container("5050-rep_1")

	// Health settings do not change the container.
	same := worker("rep_1")
	same.Health.IntervalSec = 9
	ta.deliver(s, baseState(2, same))
	time.Sleep(20 * time.Millisecond)
	if ct, _ := ta.eng.Container("5050-rep_1"); ct.ID != first.ID {
		t.Fatal("a health-only change recreated the container")
	}
	changed := worker("rep_1")
	changed.Env = map[string]string{"FOO": "baz"}
	ta.deliver(s, baseState(3, changed))
	waitFor(t, "recreated", func() bool {
		ct, ok := ta.eng.Container("5050-rep_1")
		return ok && ct.ID != first.ID && ct.Running()
	})
	sp, _ := ta.eng.SpecOf("5050-rep_1")
	if !slices.Contains(sp.Env, "FOO=baz") {
		t.Fatalf("env = %v", sp.Env)
	}
}

func TestDesiredStoppedStopsAndRunningStartsAgain(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	s := ta.attach()
	c := collect(s)
	ta.deliver(s, baseState(1, worker("rep_1")))
	waitFor(t, "running", func() bool { ct, ok := ta.eng.Container("5050-rep_1"); return ok && ct.Running() })
	stopped := worker("rep_1")
	stopped.Desired = "stopped"
	ta.deliver(s, baseState(2, stopped))
	waitFor(t, "stopped", func() bool {
		st, ok := c.lastStatus(t, "rep_1")
		ct, _ := ta.eng.Container("5050-rep_1")
		return ok && st.State == "stopped" && !ct.Running()
	})
	ta.deliver(s, baseState(3, worker("rep_1")))
	waitFor(t, "started again", func() bool { ct, ok := ta.eng.Container("5050-rep_1"); return ok && ct.Running() })
}

func TestCrashedContainerIsLeftToDockerRestartPolicy(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	s := ta.attach()
	c := collect(s)
	ta.deliver(s, baseState(1, worker("rep_1")))
	waitFor(t, "running", func() bool { ct, ok := ta.eng.Container("5050-rep_1"); return ok && ct.Running() })
	go ta.eventLoop(ta.ctx)
	time.Sleep(20 * time.Millisecond) // subscribed
	ta.eng.SetRestartCount("5050-rep_1", 3)
	ta.eng.ExitContainer("5050-rep_1", 2, false)
	waitFor(t, "crashed", func() bool {
		st, ok := c.lastStatus(t, "rep_1")
		return ok && st.State == "crashed" && st.Restarts == 3 && st.ExitCode != nil && *st.ExitCode == 2
	})
	ta.eng.ResetCalls()
	ta.reconcileCurrent(context.Background())
	time.Sleep(20 * time.Millisecond)
	for _, call := range ta.eng.CallsSnapshot() {
		if strings.HasPrefix(call, "start ") {
			t.Fatalf("the agent restarted a container Docker gave up on: %v", ta.eng.CallsSnapshot())
		}
	}
}

func TestBootFromCacheAppliesNetworkingWithoutRemovals(t *testing.T) {
	dir := t.TempDir()
	key := []byte("0123456789abcdef0123456789abcdef")
	cache, err := statecache.New(statecache.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	ds := baseState(7, worker("rep_1"))
	ds.Ingress = &proto.Ingress{Routes: []proto.Route{{Domain: "a.example.com", UpstreamName: "svc-a"}}, Upstreams: []proto.UpstreamSet{{Name: "svc-a", IPs: []string{"10.129.4.7"}}}}
	ds.DNS = []proto.DNSEntry{{EnvironmentID: "env_1", Name: "api.internal", IPs: []string{"10.129.4.7"}}}
	if err := cache.Save(statecache.Entry{State: ds}); err != nil {
		t.Fatal(err)
	}
	eng := engine.NewFake()
	eng.AddImage(testImage, engine.ImageConfig{})
	eng.AddContainer(engine.Container{Name: "5050-rep_gone", Labels: map[string]string{engine.LabelReplica: "rep_gone"}})
	ta := newTestAgentIn(t, dir, eng, key)

	ta.bootFromCache(context.Background())
	if ta.net.applies() != 1 || len(ta.net.applied[0].DNS) != 1 || ta.net.applied[0].Networks[0].Name != "5050-env_1" {
		t.Fatalf("networking not restored from the cache: %+v", ta.net.applied)
	}
	if len(ta.ing.applied) != 1 || ta.ing.applied[0].Routes[0].Domain != "a.example.com" {
		t.Fatalf("routes not restored: %+v", ta.ing.applied)
	}
	if got := ta.ing.ups("svc-a"); len(got) != 1 {
		t.Fatalf("upstreams not restored: %v", got)
	}
	waitFor(t, "cached replica started", func() bool { ct, ok := ta.eng.Container("5050-rep_1"); return ok && ct.Running() })
	time.Sleep(20 * time.Millisecond)
	if _, ok := ta.eng.Container("5050-rep_gone"); !ok {
		t.Fatal("boot from cache removed a container")
	}
	h := ta.buildHello(context.Background())
	if !h.Cached || h.Applied != 7 || h.Epoch != "ep1" || h.WGPublicKey != "wgpub=" {
		t.Fatalf("hello = %+v", h)
	}
	if len(h.Replicas) != 2 {
		t.Fatalf("hello must list every replica container: %+v", h.Replicas)
	}
}

func TestDesiredStateIsCachedEncrypted(t *testing.T) {
	ta := newTestAgent(t)
	s := ta.attach()
	rep := worker("rep_1")
	rep.Env = map[string]string{"SECRET_TOKEN": "s3cr3t-value"}
	ta.eng.AddImage(testImage, engine.ImageConfig{})
	ta.deliver(s, baseState(4, rep))
	b, err := os.ReadFile(statecache.Path(ta.dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "s3cr3t-value") {
		t.Fatal("the cache holds a variable value in clear text")
	}
	st, _ := os.Stat(statecache.Path(ta.dir))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %v", st.Mode())
	}
	c, _ := statecache.New(statecache.Path(ta.dir), ta.key)
	e, err := c.Load()
	if err != nil || e.State.Generation != 4 || e.State.Replicas[0].Env["SECRET_TOKEN"] != "s3cr3t-value" {
		t.Fatalf("cache round trip: %v %+v", err, e.State)
	}
}

func TestVolumesCreatedAndTrashed(t *testing.T) {
	ta := newTestAgent(t)
	oldDir := filepath.Join(t.TempDir(), "old")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "data.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	ta.eng.SetVolume(engine.Volume{Name: "5050-vol_b", Mountpoint: oldDir})
	ds := baseState(1)
	ds.Volumes = []proto.VolumeSpec{{Name: "5050-vol_a", ServiceID: "svc_a"}, {Name: "5050-vol_b", Trash: true}}
	trashRoot := t.TempDir()
	errs := ta.applyVolumes(context.Background(), &ds, false)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if _, err := ta.eng.VolumeInspect(context.Background(), "5050-vol_a"); err != nil {
		t.Fatal("volume not created")
	}
	if _, err := ta.eng.VolumeInspect(context.Background(), "5050-vol_b"); err != nil {
		t.Fatal("trash must wait for a fresh state")
	}
	// Trash moves the data into 5050-trash-<id>-<ts>.
	trash := TrashName("5050-vol_b", time.Now())
	ta.eng.SetVolume(engine.Volume{Name: trash, Mountpoint: filepath.Join(trashRoot, "t")})
	if err := ta.trashVolume(context.Background(), "5050-vol_b"); err != nil {
		t.Fatal(err)
	}
	if _, err := ta.eng.VolumeInspect(context.Background(), "5050-vol_b"); err == nil {
		t.Fatal("old volume still there")
	}
	if b, err := os.ReadFile(filepath.Join(trashRoot, "t", "data.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("data not moved to the trash volume: %v", err)
	}
	if !strings.HasPrefix(trash, "5050-trash-vol_b-") {
		t.Fatalf("trash name = %s", trash)
	}
}

func TestTrashRefusesMountedVolume(t *testing.T) {
	ta := newTestAgent(t)
	ta.eng.SetVolume(engine.Volume{Name: "5050-vol_b", Mountpoint: t.TempDir()})
	ta.eng.AddContainer(engine.Container{Name: "5050-rep_1", Labels: map[string]string{engine.LabelReplica: "rep_1"}, Volumes: []string{"5050-vol_b"}})
	if err := ta.trashVolume(context.Background(), "5050-vol_b"); err == nil || !strings.Contains(err.Error(), "still mounted") {
		t.Fatalf("err = %v", err)
	}
	if err := ta.trashVolume(context.Background(), "5050-vol_missing"); err != nil {
		t.Fatalf("missing volume: %v", err)
	}
}

func TestFailOpen(t *testing.T) {
	ips := []string{"a", "b", "c"}
	got, panic := FailOpen(ips, map[string]bool{"b": true})
	if panic || !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("got %v %v", got, panic)
	}
	got, panic = FailOpen(ips, map[string]bool{"a": true, "b": true, "c": true})
	if !panic || !slices.Equal(got, ips) {
		t.Fatalf("all unhealthy must keep every replica: %v %v", got, panic)
	}
	got, panic = FailOpen(ips, nil)
	if panic || !slices.Equal(got, ips) {
		t.Fatalf("got %v", got)
	}
}

func TestUpstreamsFollowLocalHealthWhileCached(t *testing.T) {
	ta := newTestAgent(t)
	ta.mu.Lock()
	ta.cached = true
	ta.replicas["rep_1"] = &replica{spec: proto.ReplicaSpec{ID: "rep_1", IP: "10.0.0.1"}, localUnhealthy: true}
	ta.replicas["rep_2"] = &replica{spec: proto.ReplicaSpec{ID: "rep_2", IP: "10.0.0.2"}}
	ta.mu.Unlock()
	errs := ta.applyUpstreams([]proto.UpstreamSet{{Name: "u", IPs: []string{"10.0.0.1", "10.0.0.2"}}, {Name: "v", IPs: []string{"10.0.0.1"}}})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := ta.ing.ups("u"); !slices.Equal(got, []string{"10.0.0.2"}) {
		t.Fatalf("u = %v", got)
	}
	if got := ta.ing.ups("v"); !slices.Equal(got, []string{"10.0.0.1"}) {
		t.Fatalf("v must fail open: %v", got)
	}
	calls := ta.ing.setCalls
	ta.applyUpstreams([]proto.UpstreamSet{{Name: "u", IPs: []string{"10.0.0.1", "10.0.0.2"}}})
	if ta.ing.setCalls != calls+1 {
		t.Fatalf("unchanged upstreams must not be pushed again; removed ones must be cleared (%d -> %d)", calls, ta.ing.setCalls)
	}
	if keys := sortedKeys(ta.ing.upstreams); !slices.Equal(keys, []string{"u"}) {
		t.Fatalf("upstreams left: %v", keys)
	}
}

func TestInfraEnsuredPerRole(t *testing.T) {
	ta := newTestAgent(t)
	ds := baseState(1)
	ds.Host.Roles = []string{"app", "build"}
	ds.Host.Images = map[string]string{ImageKeyBuildKit: "bk:1"}
	ds.Ingress = &proto.Ingress{}
	ta.ensureInfra(context.Background(), &ds)
	ta.ensureInfra(context.Background(), &ds)
	if !slices.Equal(ta.bld.ensured, []string{"bk:1"}) || len(ta.ing.ensured) != 1 {
		t.Fatalf("builder %v, caddy %v: each must be ensured once while unchanged", ta.bld.ensured, ta.ing.ensured)
	}
	ds.Host.Images[ImageKeyBuildKit] = "bk:2"
	ta.ensureInfra(context.Background(), &ds)
	if !slices.Equal(ta.bld.ensured, []string{"bk:1", "bk:2"}) {
		t.Fatalf("a changed pin must re-ensure the builder: %v", ta.bld.ensured)
	}
}
