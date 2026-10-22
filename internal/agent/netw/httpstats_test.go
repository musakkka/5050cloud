// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"math"
	"sync"
	"testing"
)

func TestHTTPAggregator(t *testing.T) {
	a := NewHTTPAggregator()
	for i := 1; i <= 100; i++ {
		a.Add("svc_a", 200, float64(i))
	}
	a.Add("svc_a", 301, 1)
	a.Add("svc_a", 404, 1)
	a.Add("svc_a", 503, 1)
	a.Add("svc_a", 101, 1) // websocket upgrades count as success
	a.Add("svc_b", 500, math.NaN())
	a.Add("", 200, 1)       // no service
	a.Add("svc_c", 999, 1)  // nonsense status
	a.Add("svc_c", 200, -5) // clock skew
	s := a.Flush()
	if len(s) != 3 {
		t.Fatalf("samples = %+v", s)
	}
	x := s[0]
	if x.ServiceID != "svc_a" || x.Req2xx != 101 || x.Req3xx != 1 || x.Req4xx != 1 || x.Req5xx != 1 {
		t.Errorf("counts = %+v", x)
	}
	// 104 values: 100 of 1..100 plus four 1s.
	if x.P50Ms != 48 || x.P90Ms != 90 || x.P95Ms != 95 || x.P99Ms != 99 {
		t.Errorf("percentiles = %v %v %v %v", x.P50Ms, x.P90Ms, x.P95Ms, x.P99Ms)
	}
	if s[1].ServiceID != "svc_b" || s[1].P99Ms != 0 || s[2].ServiceID != "svc_c" || s[2].P50Ms != 0 {
		t.Errorf("edge samples = %+v", s[1:])
	}
	if len(a.Flush()) != 0 {
		t.Error("window not reset")
	}
	if percentile(nil, 50) != 0 {
		t.Error("empty percentile")
	}
}

func TestHTTPAggregatorReservoir(t *testing.T) {
	a := NewHTTPAggregator()
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range maxLatencySamples {
				a.Add("svc", 200, float64((i+g)%1000))
			}
		}()
	}
	wg.Wait()
	s := a.Flush()
	if s[0].Req2xx != 4*maxLatencySamples {
		t.Errorf("count = %d", s[0].Req2xx)
	}
	if s[0].P50Ms < 400 || s[0].P50Ms > 600 {
		t.Errorf("reservoir p50 = %v, want about 500", s[0].P50Ms)
	}
}
