// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"math"
	"math/rand/v2"
	"sort"
	"sync"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxLatencySamples bounds memory per service and window; beyond it the
// aggregator keeps a uniform reservoir sample, so percentiles stay unbiased.
const maxLatencySamples = 20000

// HTTPAggregator turns access log lines into one proto.HTTPSample per
// service and window (docs/SPEC.md 9.10). It is safe for concurrent use.
type HTTPAggregator struct {
	mu  sync.Mutex
	per map[string]*httpWindow
}

type httpWindow struct {
	codes [6]int // index status/100
	lat   []float64
	seen  int
}

// NewHTTPAggregator returns an empty aggregator.
func NewHTTPAggregator() *HTTPAggregator { return &HTTPAggregator{per: map[string]*httpWindow{}} }

// Add records one request of a service.
func (a *HTTPAggregator) Add(serviceID string, status int, durationMs float64) {
	if serviceID == "" || status < 100 || status > 599 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.per[serviceID]
	if w == nil {
		w = &httpWindow{}
		a.per[serviceID] = w
	}
	w.codes[status/100]++
	w.seen++
	if math.IsNaN(durationMs) || durationMs < 0 {
		durationMs = 0
	}
	if len(w.lat) < maxLatencySamples {
		w.lat = append(w.lat, durationMs)
	} else if j := rand.IntN(w.seen); j < maxLatencySamples {
		w.lat[j] = durationMs
	}
}

// Flush returns the samples of the window that just ended, sorted by
// service, and starts a new window. Services without requests are left out.
func (a *HTTPAggregator) Flush() []proto.HTTPSample {
	a.mu.Lock()
	per := a.per
	a.per = map[string]*httpWindow{}
	a.mu.Unlock()
	out := make([]proto.HTTPSample, 0, len(per))
	for svc, w := range per {
		sort.Float64s(w.lat)
		out = append(out, proto.HTTPSample{
			ServiceID: svc,
			Req2xx:    w.codes[2] + w.codes[1],
			Req3xx:    w.codes[3],
			Req4xx:    w.codes[4],
			Req5xx:    w.codes[5],
			P50Ms:     percentile(w.lat, 50),
			P90Ms:     percentile(w.lat, 90),
			P95Ms:     percentile(w.lat, 95),
			P99Ms:     percentile(w.lat, 99),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceID < out[j].ServiceID })
	return out
}

// percentile is the nearest-rank percentile of sorted values.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	rank = min(max(rank, 1), len(sorted))
	return math.Round(sorted[rank-1]*100) / 100
}
