// SPDX-License-Identifier: AGPL-3.0-only

package usage

import (
	"testing"

	"github.com/musakkka/5050cloud/pkg/api"
)

func TestEstimate(t *testing.T) {
	got := Estimate(api.UsageRow{VCPUHours: 730, MemGBHours: 730, EgressGB: 100}, Railway)
	if got != 35 {
		t.Fatalf("estimate = %v, want 35", got)
	}
}
