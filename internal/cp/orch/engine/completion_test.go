// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/musakkka/5050cloud/pkg/api"
)

func TestRanToCompletion(t *testing.T) {
	zero, one := 0, 1
	cfg := func(policy string) api.ServiceConfig {
		var c api.ServiceConfig
		c.Deploy.RestartPolicy = policy
		return c
	}
	cases := []struct {
		name   string
		policy string
		r      repRow
		want   bool
	}{
		{"exit 0, default policy", "", repRow{State: "exited", ExitCode: &zero}, true},
		{"exit 0, never", "never", repRow{State: "exited", ExitCode: &zero}, true},
		{"exit 0, always restarts", "always", repRow{State: "exited", ExitCode: &zero}, false},
		{"exit 1", "", repRow{State: "exited", ExitCode: &one}, false},
		{"crashed", "", repRow{State: "crashed", ExitCode: &zero}, false},
		{"no exit code", "", repRow{State: "exited"}, false},
		{"healthy", "", repRow{State: "healthy"}, false},
	}
	for _, c := range cases {
		if got := ranToCompletion(cfg(c.policy), c.r); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
