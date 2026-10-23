// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/sample"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// volumeWalkBudget bounds one volume usage walk (8.1).
const volumeWalkBudget = 30 * time.Second

// metricsLoop sends host, container, HTTP and database samples every 30 s
// and runs the disk guard.
func (a *Agent) metricsLoop(ctx context.Context) {
	if cur, _, err := a.roots.ReadCPU(); err == nil {
		a.hostCPU = cur
	}
	every(ctx, a.t.metricsEvery, func() {
		m := a.collectMetrics(ctx)
		a.disk.check(ctx, m.Host.Disks)
		a.send(proto.TMetrics, "", m)
	})
}

func (a *Agent) diskMounts() []string {
	mounts := []string{"/"}
	if st, err := os.Stat("/data"); err == nil && st.IsDir() && !sample.SameDevice("/", "/data") {
		mounts = append(mounts, "/data")
	}
	if a.o.DataDir != "" && !sample.SameDevice("/", a.o.DataDir) && (len(mounts) <= 1 || !sample.SameDevice("/data", a.o.DataDir)) {
		mounts = append(mounts, a.o.DataDir)
	}
	return mounts
}

func (a *Agent) collectMetrics(ctx context.Context) proto.Metrics {
	now := time.Now()
	m := proto.Metrics{At: now.UnixMilli()}
	// Host
	if cur, cpus, err := a.roots.ReadCPU(); err == nil {
		m.Host.CPUUsed = math.Round(sample.HostCPU(a.hostCPU, cur, cpus)*1000) / 1000
		m.Host.CPUs = cpus
		a.hostCPU = cur
	} else {
		m.Host.CPUs = runtime.NumCPU()
	}
	if total, used, err := a.roots.Memory(); err == nil {
		m.Host.MemTotal, m.Host.MemUsed = total, used
	}
	m.Host.Load1 = a.roots.Load1()
	if mr, ok := a.o.Network.(meshReporter); ok {
		m.Host.MeshHandshakes = mr.MeshHandshakes()
		a.checkMesh(mr, now)
	}
	for _, p := range a.diskMounts() {
		if used, total, err := sample.Statfs(p); err == nil {
			m.Host.Disks = append(m.Host.Disks, proto.Disk{Mount: p, Used: used, Total: total})
		}
	}
	// Containers
	a.mu.Lock()
	type item struct {
		id, cid string
		vols    []string
	}
	var items []item
	for id, r := range a.replicas {
		if r.cid != "" {
			var vols []string
			for _, mt := range r.spec.Mounts {
				if mt.Kind == "volume" {
					vols = append(vols, mt.Source)
				}
			}
			items = append(items, item{id: id, cid: r.cid, vols: vols})
		}
	}
	a.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	keep := map[string]bool{}
	for _, it := range items {
		c, err := a.eng.Inspect(ctx, it.cid)
		if err != nil || !c.Running() {
			continue
		}
		keep[it.id] = true
		cs := proto.ContainerSample{ReplicaID: it.id}
		got := false
		if c.Pid > 0 {
			if dir, err := a.roots.CgroupPath(c.Pid); err == nil {
				if u, err := sample.ReadCgroup(dir); err == nil {
					cs.CPU = round3(a.cpu.VCPU(it.id, u.CPUUsec, now))
					cs.MemBytes, cs.MemLimit = u.MemBytes, u.MemMax
					got = true
				}
			}
			if rx, tx, err := a.roots.NetDev(c.Pid); err == nil {
				cs.RxBytes, cs.TxBytes = rx, tx
			}
		}
		if !got {
			// The cgroup files are not visible (the agent runs in a container): ask Docker.
			if st, err := a.eng.Stats(ctx, it.cid); err == nil {
				cs.CPU = round3(a.cpu.VCPU(it.id, st.CPUNanos/1000, now))
				cs.MemBytes, cs.MemLimit = st.MemBytes, st.MemLimit
				if cs.RxBytes == 0 && cs.TxBytes == 0 {
					cs.RxBytes, cs.TxBytes = st.RxBytes, st.TxBytes
				}
			}
		}
		if cs.MemLimit == 0 {
			cs.MemLimit = c.MemLimit
		}
		a.mu.Lock()
		for _, v := range it.vols {
			cs.DiskBytes += a.volSize[v]
		}
		a.mu.Unlock()
		m.Containers = append(m.Containers, cs)
	}
	a.cpu.Forget(keep)
	m.HTTP = a.http.take()
	a.mu.Lock()
	for _, s := range a.db {
		m.DB = append(m.DB, s)
	}
	a.db = map[string]proto.DBSample{}
	a.mu.Unlock()
	sort.Slice(m.DB, func(i, j int) bool { return m.DB[i].ReplicaID < m.DB[j].ReplicaID })
	return m
}

// meshReporter is implemented by internal/agent/netw: WireGuard handshake
// times per peer host, for HostSample.MeshHandshakes and mesh.unreachable.
type meshReporter interface {
	MeshHandshakes() map[string]int64
	UnreachablePeers(now time.Time, d time.Duration) []string
}

// meshUnreachableAfter is how long a peer may go without a handshake (8.4).
const meshUnreachableAfter = 3 * time.Minute

// checkMesh raises mesh.unreachable once per peer when it has had no
// WireGuard handshake for 3 minutes, and forgets it once it is back. The
// first 3 minutes after the agent starts are skipped, since a fresh
// interface has no handshakes yet.
func (a *Agent) checkMesh(mr meshReporter, now time.Time) {
	if now.Sub(a.startedAt) < meshUnreachableAfter {
		return
	}
	down := mr.UnreachablePeers(now, meshUnreachableAfter)
	a.mu.Lock()
	if a.meshDown == nil {
		a.meshDown = map[string]bool{}
	}
	cur := map[string]bool{}
	var fresh []string
	for _, id := range down {
		cur[id] = true
		if !a.meshDown[id] {
			fresh = append(fresh, id)
		}
	}
	a.meshDown = cur
	a.mu.Unlock()
	for _, id := range fresh {
		a.send(proto.TEvent, "", proto.Event{Kind: "mesh.unreachable", Text: fmt.Sprintf("No WireGuard handshake with host %s for 3 minutes. Both hosts may be behind NAT, or UDP 51820 is blocked.", id), At: now.UnixMilli()})
	}
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// probeLoop runs each database replica's metrics probe every 60 s through
// Docker exec; its JSON output becomes a DBSample (9.10, 9.13).
func (a *Agent) probeLoop(ctx context.Context) {
	every(ctx, a.t.probeEvery, func() { a.runProbes(ctx) })
}

func (a *Agent) runProbes(ctx context.Context) {
	a.mu.Lock()
	type item struct {
		id, cid string
		probe   []string
	}
	var items []item
	for id, r := range a.replicas {
		if len(r.spec.Probe) > 0 && r.cid != "" && (r.status.State == "healthy" || r.status.State == "running") {
			items = append(items, item{id, r.cid, r.spec.Probe})
		}
	}
	a.mu.Unlock()
	for _, it := range items {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		code, out, err := a.execRun(pctx, it.cid, it.probe, nil)
		cancel()
		if err != nil || code != 0 {
			continue
		}
		s, ok := parseProbe(out)
		if !ok {
			continue
		}
		s.ReplicaID = it.id
		a.mu.Lock()
		a.db[it.id] = s
		a.mu.Unlock()
	}
}

// parseProbe reads the last JSON object line of a probe's output.
func parseProbe(out []byte) (proto.DBSample, bool) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "{") {
			continue
		}
		var s proto.DBSample
		if json.Unmarshal([]byte(l), &s) == nil {
			return s, true
		}
	}
	return proto.DBSample{}, false
}

// volumeLoop measures volume usage every 5 minutes with a 30 s budget.
func (a *Agent) volumeLoop(ctx context.Context) {
	every(ctx, a.t.volumeEvery, func() { a.measureVolumes(ctx) })
}

func (a *Agent) measureVolumes(ctx context.Context) {
	a.mu.Lock()
	seen := map[string]bool{}
	var names []string
	for _, r := range a.replicas {
		for _, m := range r.spec.Mounts {
			if m.Kind == "volume" && !seen[m.Source] {
				seen[m.Source] = true
				names = append(names, m.Source)
			}
		}
	}
	a.mu.Unlock()
	deadline := time.Now().Add(volumeWalkBudget)
	sizes := map[string]int64{}
	for _, n := range names {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		v, err := a.eng.VolumeInspect(ctx, n)
		if err != nil || v.Mountpoint == "" {
			continue
		}
		if size, _, err := sample.DirSize(v.Mountpoint, left); err == nil {
			sizes[n] = size
		}
	}
	a.mu.Lock()
	for k, v := range sizes {
		a.volSize[k] = v
	}
	a.mu.Unlock()
}

// httpAgg aggregates access logs per service over one metrics window.
type httpAgg struct {
	mu  sync.Mutex
	svc map[string]*httpWindow
}

type httpWindow struct {
	classes [4]int
	ms      []float64
}

func newHTTPAgg() *httpAgg { return &httpAgg{svc: map[string]*httpWindow{}} }

func numAttr(attrs map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := attrs[k].(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		}
	}
	return 0, false
}

func (h *httpAgg) add(r proto.LogRecord) {
	status, ok := numAttr(r.Attrs, "httpStatus", "status")
	if !ok || r.ServiceID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.svc[r.ServiceID]
	if w == nil {
		w = &httpWindow{}
		h.svc[r.ServiceID] = w
	}
	switch {
	case status >= 500:
		w.classes[3]++
	case status >= 400:
		w.classes[2]++
	case status >= 300:
		w.classes[1]++
	default:
		w.classes[0]++
	}
	if ms, ok := numAttr(r.Attrs, "durationMs", "responseTime"); ok && len(w.ms) < 100000 {
		w.ms = append(w.ms, ms)
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func (h *httpAgg) take() []proto.HTTPSample {
	h.mu.Lock()
	svc := h.svc
	h.svc = map[string]*httpWindow{}
	h.mu.Unlock()
	var out []proto.HTTPSample
	for id, w := range svc {
		sort.Float64s(w.ms)
		out = append(out, proto.HTTPSample{
			ServiceID: id, Req2xx: w.classes[0], Req3xx: w.classes[1], Req4xx: w.classes[2], Req5xx: w.classes[3],
			P50Ms: percentile(w.ms, 0.50), P90Ms: percentile(w.ms, 0.90), P95Ms: percentile(w.ms, 0.95), P99Ms: percentile(w.ms, 0.99),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceID < out[j].ServiceID })
	return out
}

var _ = engine.ErrNotFound
