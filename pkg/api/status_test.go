// SPDX-License-Identifier: MIT

package api

import "testing"

// TestStatusLabel is the table in docs/SPEC.md 9.1.1.
func TestStatusLabel(t *testing.T) {
	cases := []struct {
		status DeploymentStatus
		latest bool
		want   string
	}{
		{DepQueued, false, "Initializing"},
		{DepWaiting, false, "Waiting"},
		{DepBuilding, false, "Building"},
		{DepDeploying, false, "Deploying"},
		{DepSuccess, true, "Active"},
		{DepSuccess, false, "Success"},
		{DepCompleted, false, "Completed"},
		{DepCrashed, false, "Crashed"},
		{DepFailed, false, "Failed"},
		{DepRemoving, false, "Removing"},
		{DepRemoved, false, "Removed"},
		{DepCancelled, false, "Removed"},
		{DepSkipped, false, "Skipped"},
		{DeploymentStatus("SLEEPING"), false, "SLEEPING"},
	}
	for _, c := range cases {
		if got := StatusLabel(c.status, c.latest); got != c.want {
			t.Errorf("StatusLabel(%s, %v) = %q, want %q", c.status, c.latest, got, c.want)
		}
	}
}
