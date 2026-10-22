// SPDX-License-Identifier: AGPL-3.0-only

package monitors

import (
	"testing"
	"time"

	"github.com/musakkka/5050cloud/pkg/api"
)

func TestEvaluateFiresAndRecovers(t *testing.T) {
	m := api.Monitor{Metric: "cpu", Threshold: 80, ForMinutes: 5}
	now := time.Unix(1_700_000_000, 0)
	var hot, cold []Point
	for i := 10; i >= 0; i-- {
		ts := now.Add(-time.Duration(i) * 30 * time.Second).UnixMilli()
		hot = append(hot, Point{TS: ts, Value: 95})
		cold = append(cold, Point{TS: ts, Value: 10})
	}
	if d := Evaluate(m, false, hot, now); d != Fire {
		t.Fatalf("hot window: %v, want Fire", d)
	}
	if d := Evaluate(m, false, cold, now); d != Hold {
		t.Fatalf("cold window: %v, want Hold", d)
	}
	if d := Evaluate(m, true, cold, now); d != Recover {
		t.Fatalf("recovered window: %v, want Recover", d)
	}
	if d := Evaluate(m, false, hot[len(hot)-1:], now); d != Hold {
		t.Fatalf("one point must not fire: %v", d)
	}
}
