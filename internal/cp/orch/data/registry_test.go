// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// TestRegistryGCSequencing checks 9.6: pause the build queue, switch the
// registry read-only, collect, switch back, then resume, even on failure.
func TestRegistryGCSequencing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		e := newEnv(t)
		e.run.OnRegistryJob = func(cmd []string, env map[string]string) (proto.JobResult, error) {
			if fail && len(cmd) > 1 && cmd[1] == "garbage-collect" {
				return proto.JobResult{ExitCode: 1}, nil
			}
			return proto.JobResult{}, nil
		}
		err := e.d.RunRegistryGC(e.ctx(), 3)
		if fail != (err != nil) {
			t.Fatalf("fail=%v: err=%v", fail, err)
		}
		var seq []string
		for _, c := range e.run.Calls() {
			if strings.HasPrefix(c, "PauseBuilds") || strings.HasPrefix(c, "ResumeBuilds") || strings.HasPrefix(c, "Registry") {
				seq = append(seq, c)
			}
		}
		want := []string{"PauseBuilds", `RegistryRestart REGISTRY_STORAGE_MAINTENANCE_READONLY={"enabled":true}`,
			"RegistryJob registry garbage-collect --delete-untagged", "RegistryRestart ", "ResumeBuilds"}
		if len(seq) != len(want) {
			t.Fatalf("fail=%v: sequence %q, want %q", fail, seq, want)
		}
		for i := range want {
			if !strings.HasPrefix(seq[i], want[i]) {
				t.Fatalf("fail=%v: step %d is %q, want %q (sequence %q)", fail, i, seq[i], want[i], seq)
			}
		}
	}
}
