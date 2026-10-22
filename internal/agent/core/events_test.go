// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestDiagnosisSentences(t *testing.T) {
	if got := OOMDiagnosis(512); got != "Out of memory at 512 MB (limit). Raise the memory limit or lower Processes." {
		t.Error(got)
	}
	if got := OOMDiagnosis(0); !strings.HasPrefix(got, "Out of memory:") {
		t.Error(got)
	}
	if got := EarlyExitDiagnosis(1, "Error: cannot find module 'x'."); got != "The app exited with code 1 right after starting: Error: cannot find module 'x'." {
		t.Error(got)
	}
	if got := EarlyExitDiagnosis(3, ""); got != "The app exited with code 3 right after starting." {
		t.Error(got)
	}
	if got := EarlyExitDiagnosis(1, strings.Repeat("x", 400)); !strings.HasSuffix(got, "....") {
		t.Errorf("long stderr is cut: %q", got[len(got)-10:])
	}
	cases := map[string]error{
		"Image nginx:1.99 was not found.": fmt.Errorf("%w: manifest unknown", engine.ErrNotFound),
		"The registry refused access to ghcr.io/acme/app. Add registry credentials in Settings > Source.": fmt.Errorf("%w", engine.ErrUnauthorized),
	}
	refs := map[string]string{
		"Image nginx:1.99 was not found.": "nginx:1.99@sha256:abc",
		"The registry refused access to ghcr.io/acme/app. Add registry credentials in Settings > Source.": "ghcr.io/acme/app:v1",
	}
	for want, err := range cases {
		if got := PullDiagnosis(refs[want], err); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	if got := PullDiagnosis("a:1", errors.New("write /x: no space left on device")); !strings.Contains(got, "ran out of disk") {
		t.Error(got)
	}
	if got := PullDiagnosis("a:1", errors.New("timeout")); got != "Image a:1 could not be pulled: Timeout." {
		t.Error(got)
	}
	if displayImage("reg:5000/a@sha256:1") != "reg:5000/a@sha256:1" || repoOf("reg:5000/a:v1@sha256:1") != "reg:5000/a" {
		t.Error("image name helpers")
	}
}

// startRunning creates and starts a replica through the agent and returns its collector.
func startRunning(t *testing.T, ta *testAgent, s proto.ReplicaSpec) *collector {
	t.Helper()
	ta.eng.AddImage(s.Image, engine.ImageConfig{})
	sess := ta.attach()
	c := collect(sess)
	go ta.eventLoop(ta.ctx)
	time.Sleep(10 * time.Millisecond)
	ta.deliver(sess, baseState(1, s))
	waitFor(t, "running", func() bool { ct, ok := ta.eng.Container(ContainerName(s.ID)); return ok && ct.Running() })
	return c
}

func TestOOMKillReported(t *testing.T) {
	ta := newTestAgent(t)
	s := worker("rep_1")
	s.MemLimitMB = 256
	c := startRunning(t, ta, s)
	time.Sleep(30 * time.Millisecond) // past "right after starting" is not needed for OOM
	ta.eng.ExitContainer("5050-rep_1", 137, true)
	waitFor(t, "oom status", func() bool {
		st, ok := c.lastStatus(t, "rep_1")
		return ok && st.OOMKilled && st.Diagnosis == "Out of memory at 256 MB (limit). Raise the memory limit or lower Processes."
	})
	waitFor(t, "oom event", func() bool {
		for _, e := range c.of(proto.TEvent) {
			if decodeAs[proto.Event](t, e).Kind == "oom" {
				return true
			}
		}
		return false
	})
}

func TestEarlyExitUsesLastStderrLine(t *testing.T) {
	ta := newTestAgent(t)
	s := worker("rep_1")
	s.Restart = proto.RestartPolicy{Policy: "never"}
	ta.eng.LogLines["5050-rep_1"] = []engine.LogLine{{Stream: "stderr", TS: time.Now(), Text: []byte("Error: listen EADDRINUSE")}}
	c := startRunning(t, ta, s)
	time.Sleep(20 * time.Millisecond)
	ta.eng.ExitContainer("5050-rep_1", 1, false)
	waitFor(t, "crashed with diagnosis", func() bool {
		st, ok := c.lastStatus(t, "rep_1")
		return ok && st.State == "crashed" && st.Diagnosis == "The app exited with code 1 right after starting: Error: listen EADDRINUSE."
	})
}

func TestRestartEventCounted(t *testing.T) {
	ta := newTestAgent(t)
	c := startRunning(t, ta, worker("rep_1"))
	ta.eng.ExitContainer("5050-rep_1", 1, false)
	waitFor(t, "exited", func() bool { st, _ := c.lastStatus(t, "rep_1"); return st.State == "exited" })
	// Docker's restart policy brings it back.
	ta.eng.SetRestartCount("5050-rep_1", 1)
	ct, _ := ta.eng.Container("5050-rep_1")
	if err := ta.eng.Start(context.Background(), ct.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restart event", func() bool {
		for _, e := range c.of(proto.TEvent) {
			if ev := decodeAs[proto.Event](t, e); ev.Kind == "restart" && strings.Contains(ev.Text, "1 restarts") {
				return true
			}
		}
		return false
	})
	waitFor(t, "healthy again", func() bool { st, _ := c.lastStatus(t, "rep_1"); return st.State == "healthy" && st.Restarts == 1 })
}

func TestAlwaysPolicyNeverCrashes(t *testing.T) {
	ta := newTestAgent(t)
	s := worker("rep_1")
	s.Restart = proto.RestartPolicy{Policy: "always"}
	c := startRunning(t, ta, s)
	time.Sleep(30 * time.Millisecond)
	ta.eng.SetRestartCount("5050-rep_1", 50)
	ta.eng.ExitContainer("5050-rep_1", 1, false)
	waitFor(t, "exited", func() bool { st, _ := c.lastStatus(t, "rep_1"); return st.State == "exited" })
}
