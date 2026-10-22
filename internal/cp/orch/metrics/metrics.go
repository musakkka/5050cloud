// SPDX-License-Identifier: AGPL-3.0-only

// Package metrics stores the samples agents send every 30 s in metrics.db
// and answers the Metrics tab, the host page, the Postgres panel, monitors
// and usage (docs/SPEC.md 6.3, 9.10, 9.21, 9.23).
//
// Layout: series(id, kind, key, env_id, service_id, host_id) where kind is
// replica, host, http or pg, and points(series_id, ts, n, ...) WITHOUT ROWID
// with raw 30 s points kept 48 hours, plus points_5m (14 days) and points_1h
// (30 days) rollups. Network counters arrive as totals; the store keeps the
// bytes moved in each window (a delta, robust to counter resets) and turns
// them into rates when it reads. A service's series is the sum of its
// replicas', so old and new replicas add up during a deploy.
//
// All writes go through one writer goroutine; reads never hold a
// transaction while they compute (docs/SPEC.md 6.5).
package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Retention and resolution (docs/SPEC.md 6.3).
const (
	RawStep      = 30 * time.Second
	RawKeep      = 48 * time.Hour
	FiveMinKeep  = 14 * 24 * time.Hour
	HourKeep     = 30 * 24 * time.Hour
	TargetPoints = 240
	janitorBatch = 5000
)

// Series kinds.
const (
	KindReplica = "replica"
	KindHost    = "host"
	KindHTTP    = "http"
	KindPG      = "pg"
)

// For pg series the generic columns hold: cpu = cache hit ratio, mem =
// pg_wal bytes, r2 = active connections, r3 = idle, r4 = idle in
// transaction, r5 = every other state.

const schema = `
CREATE TABLE IF NOT EXISTS series (
	id         INTEGER PRIMARY KEY,
	kind       TEXT NOT NULL,
	key        TEXT NOT NULL,
	env_id     TEXT NOT NULL DEFAULT '',
	service_id TEXT NOT NULL DEFAULT '',
	host_id    TEXT NOT NULL DEFAULT '',
	UNIQUE (kind, key)
);
CREATE INDEX IF NOT EXISTS series_service ON series (service_id, env_id, kind);
CREATE INDEX IF NOT EXISTS series_host ON series (host_id, kind);
`

const pointCols = `n, cpu, mem, mem_limit, rx, tx, tx_ext, disk, r2, r3, r4, r5, p50, p90, p95, p99`

func pointsDDL(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	series_id INTEGER NOT NULL,
	ts        INTEGER NOT NULL,
	n         INTEGER NOT NULL DEFAULT 1,
	cpu REAL, mem REAL, mem_limit REAL, rx REAL, tx REAL, tx_ext REAL, disk REAL,
	r2 REAL, r3 REAL, r4 REAL, r5 REAL, p50 REAL, p90 REAL, p95 REAL, p99 REAL,
	PRIMARY KEY (series_id, ts)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS %s_ts ON %s (ts);`, table, table, table)
}

// Store is metrics.db.
type Store struct {
	db  *sql.DB
	log *slog.Logger
	now func() time.Time

	writes chan writeReq
	wg     sync.WaitGroup
	stop   chan struct{}

	mu     sync.Mutex
	series map[string]int64 // kind|key -> id
	last   map[string]counters
	topQ   map[string][]api.QueryStat // env/svc -> latest top queries
	hostFS map[string]proto.HostSample
}

type counters struct{ rx, tx, txExt int64 }

type writeReq struct {
	ctx  context.Context
	fn   func(*sql.Tx) error
	done chan error
}

// Options configures Open.
type Options struct {
	Log *slog.Logger
	Now func() time.Time
}

// Open opens (creating when needed) metrics.db at path.
func Open(path string, o Options) (*Store, error) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=wal_autocheckpoint(1000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	ctx := context.Background()
	for _, ddl := range []string{schema, pointsDDL("points"), pointsDDL("points_5m"), pointsDDL("points_1h")} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("metrics.db schema: %w", err)
		}
	}
	s := &Store{
		db: db, log: o.Log.With("component", "metrics"), now: o.Now,
		writes: make(chan writeReq, 64), stop: make(chan struct{}),
		series: map[string]int64{}, last: map[string]counters{},
		topQ: map[string][]api.QueryStat{}, hostFS: map[string]proto.HostSample{},
	}
	s.wg.Add(1)
	go s.writer()
	return s, nil
}

// Close stops the writer and closes the database.
func (s *Store) Close() error {
	close(s.stop)
	s.wg.Wait()
	return s.db.Close()
}

func (s *Store) writer() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case r := <-s.writes:
			r.done <- s.runTx(r.ctx, r.fn)
		}
	}
}

func (s *Store) runTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error {
	r := writeReq{ctx: ctx, fn: fn, done: make(chan error, 1)}
	select {
	case s.writes <- r:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stop:
		return errors.New("metrics store closed")
	}
	select {
	case err := <-r.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) seriesID(ctx context.Context, tx *sql.Tx, kind, key, env, svc, host string) (int64, error) {
	k := kind + "|" + key
	s.mu.Lock()
	id, ok := s.series[k]
	s.mu.Unlock()
	if ok {
		return id, nil
	}
	err := tx.QueryRowContext(ctx, `SELECT id FROM series WHERE kind = ? AND key = ?`, kind, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO series (kind, key, env_id, service_id, host_id) VALUES (?, ?, ?, ?, ?)`, kind, key, env, svc, host)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.series[k] = id
	s.mu.Unlock()
	return id, nil
}

// point is one row of a points table; nil fields are NULL.
type point struct {
	n                                 int64
	cpu, mem, memLimit, rx, tx, txExt *float64
	disk, r2, r3, r4, r5              *float64
	p50, p90, p95, p99                *float64
}

func f(v float64) *float64 { return &v }

func insertPoint(ctx context.Context, tx *sql.Tx, table string, sid, ts int64, p point) error {
	if p.n == 0 {
		p.n = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (series_id, ts, `+pointCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(series_id, ts) DO UPDATE SET n = excluded.n, cpu = excluded.cpu, mem = excluded.mem,
		mem_limit = excluded.mem_limit, rx = excluded.rx, tx = excluded.tx, tx_ext = excluded.tx_ext, disk = excluded.disk,
		r2 = excluded.r2, r3 = excluded.r3, r4 = excluded.r4, r5 = excluded.r5,
		p50 = excluded.p50, p90 = excluded.p90, p95 = excluded.p95, p99 = excluded.p99`,
		sid, ts, p.n, p.cpu, p.mem, p.memLimit, p.rx, p.tx, p.txExt, p.disk, p.r2, p.r3, p.r4, p.r5, p.p50, p.p90, p.p95, p.p99)
	return err
}

// ReplicaInfo locates a replica for ingest.
type ReplicaInfo struct {
	EnvID     string
	ServiceID string
}

// Ingest stores one metrics message from a host. replica maps a replica id
// to its service instance; httpEnv maps an HTTP sample's service id to the
// environment it belongs to on that ingress host (the sample carries only
// the service id).
func (s *Store) Ingest(ctx context.Context, hostID string, m proto.Metrics, replica func(id string) (ReplicaInfo, bool), httpEnv func(serviceID string) string) error {
	at := m.At
	if at == 0 {
		at = s.now().UnixMilli()
	}
	ts := at - at%RawStep.Milliseconds()
	s.mu.Lock()
	s.hostFS[hostID] = m.Host
	type cdelta struct {
		rx, tx, ext int64
	}
	deltas := map[string]cdelta{}
	for _, c := range m.Containers {
		prev, ok := s.last[c.ReplicaID]
		d := cdelta{}
		if ok {
			d.rx, d.tx, d.ext = delta(prev.rx, c.RxBytes), delta(prev.tx, c.TxBytes), delta(prev.txExt, c.TxExternalBytes)
		}
		s.last[c.ReplicaID] = counters{rx: c.RxBytes, tx: c.TxBytes, txExt: c.TxExternalBytes}
		deltas[c.ReplicaID] = d
	}
	for _, d := range m.DB {
		if info, ok := replica(d.ReplicaID); ok && len(d.TopQueries) > 0 {
			qs := make([]api.QueryStat, 0, len(d.TopQueries))
			for _, q := range d.TopQueries {
				qs = append(qs, api.QueryStat{Query: q.Query, Calls: q.Calls, TotalMs: q.TotalMs, MeanMs: q.MeanMs})
			}
			s.topQ[info.EnvID+"/"+info.ServiceID] = qs
		}
	}
	s.mu.Unlock()

	return s.write(ctx, func(tx *sql.Tx) error {
		hid, err := s.seriesID(ctx, tx, KindHost, hostID, "", "", hostID)
		if err != nil {
			return err
		}
		var used float64
		for i, d := range m.Host.Disks {
			if d.Mount == "/" || i == 0 {
				used = float64(d.Used)
			}
		}
		if err := insertPoint(ctx, tx, "points", hid, ts, point{cpu: f(m.Host.CPUUsed), mem: f(float64(m.Host.MemUsed)),
			memLimit: f(float64(m.Host.MemTotal)), disk: f(used)}); err != nil {
			return err
		}
		for _, c := range m.Containers {
			info, ok := replica(c.ReplicaID)
			if !ok {
				continue
			}
			sid, err := s.seriesID(ctx, tx, KindReplica, c.ReplicaID, info.EnvID, info.ServiceID, hostID)
			if err != nil {
				return err
			}
			d := deltas[c.ReplicaID]
			p := point{cpu: f(c.CPU), mem: f(float64(c.MemBytes)), rx: f(float64(d.rx)), tx: f(float64(d.tx)), txExt: f(float64(d.ext))}
			if c.MemLimit > 0 {
				p.memLimit = f(float64(c.MemLimit))
			}
			if c.DiskBytes > 0 {
				p.disk = f(float64(c.DiskBytes))
			}
			if err := insertPoint(ctx, tx, "points", sid, ts, p); err != nil {
				return err
			}
		}
		for _, hs := range m.HTTP {
			env := ""
			if httpEnv != nil {
				env = httpEnv(hs.ServiceID)
			}
			sid, err := s.seriesID(ctx, tx, KindHTTP, env+"/"+hs.ServiceID+"/"+hostID, env, hs.ServiceID, hostID)
			if err != nil {
				return err
			}
			if err := insertPoint(ctx, tx, "points", sid, ts, point{r2: f(float64(hs.Req2xx)), r3: f(float64(hs.Req3xx)),
				r4: f(float64(hs.Req4xx)), r5: f(float64(hs.Req5xx)), p50: f(hs.P50Ms), p90: f(hs.P90Ms), p95: f(hs.P95Ms), p99: f(hs.P99Ms)}); err != nil {
				return err
			}
		}
		for _, d := range m.DB {
			info, ok := replica(d.ReplicaID)
			if !ok {
				continue
			}
			sid, err := s.seriesID(ctx, tx, KindPG, info.EnvID+"/"+info.ServiceID, info.EnvID, info.ServiceID, hostID)
			if err != nil {
				return err
			}
			var active, idle, idleTx, other float64
			for st, n := range d.Connections {
				switch st {
				case "active":
					active += float64(n)
				case "idle":
					idle += float64(n)
				case "idle in transaction", "idle in transaction (aborted)":
					idleTx += float64(n)
				default:
					other += float64(n)
				}
			}
			if err := insertPoint(ctx, tx, "points", sid, ts, point{cpu: f(d.CacheHitRatio), mem: f(float64(d.WALBytes)),
				r2: f(active), r3: f(idle), r4: f(idleTx), r5: f(other)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func delta(prev, cur int64) int64 {
	if cur >= prev {
		return cur - prev
	}
	return cur // counter reset (container restart)
}

// HostSample returns the latest host sample (disk totals for monitors and the Hosts page).
func (s *Store) HostSample(hostID string) (proto.HostSample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hostFS[hostID]
	return h, ok
}

// Forget drops in-memory counter state of a removed replica.
func (s *Store) Forget(replicaID string) {
	s.mu.Lock()
	delete(s.last, replicaID)
	s.mu.Unlock()
}

// ---------------------------------------------------------------- reading

// Resolution picks the table and step for a window so a chart gets about
// 240 points (docs/SPEC.md 9.10).
func (s *Store) Resolution(from, until int64) (table string, step int64) {
	span := until - from
	if span <= 0 {
		span = RawStep.Milliseconds()
	}
	step = span / TargetPoints
	raw := RawStep.Milliseconds()
	if step < raw {
		step = raw
	}
	step = (step + raw - 1) / raw * raw
	now := s.now().UnixMilli()
	switch {
	case step < (5*time.Minute).Milliseconds() && from >= now-RawKeep.Milliseconds():
		return "points", step
	case step < time.Hour.Milliseconds() && from >= now-FiveMinKeep.Milliseconds():
		if step < (5 * time.Minute).Milliseconds() {
			step = (5 * time.Minute).Milliseconds()
		}
		step = roundUp(step, (5 * time.Minute).Milliseconds())
		return "points_5m", step
	default:
		if step < time.Hour.Milliseconds() {
			step = time.Hour.Milliseconds()
		}
		step = roundUp(step, time.Hour.Milliseconds())
		return "points_1h", step
	}
}

func roundUp(v, m int64) int64 { return (v + m - 1) / m * m }

type row struct {
	sid int64
	ts  int64
	n   float64
	v   [15]sql.NullFloat64 // cpu..p99 in pointCols order after n
}

const (
	cCPU = iota
	cMem
	cMemLimit
	cRx
	cTx
	cTxExt
	cDisk
	cR2
	cR3
	cR4
	cR5
	cP50
	cP90
	cP95
	cP99
)

// sumCols are deltas (summed within a bucket); the rest are averaged.
var sumCols = map[int]bool{cRx: true, cTx: true, cTxExt: true, cR2: true, cR3: true, cR4: true, cR5: true}

func (s *Store) readRows(ctx context.Context, table string, sids []int64, from, until int64) ([]row, error) {
	if len(sids) == 0 {
		return nil, nil
	}
	args := []any{from, until}
	q := `SELECT series_id, ts, ` + pointCols + ` FROM ` + table + ` WHERE ts >= ? AND ts < ? AND series_id IN (`
	for i, id := range sids {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, id)
	}
	q += `) ORDER BY ts`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []row
	for rows.Next() {
		var r row
		dest := []any{&r.sid, &r.ts, &r.n}
		for i := range r.v {
			dest = append(dest, &r.v[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type seriesRef struct {
	id   int64
	key  string
	host string
}

func (s *Store) findSeries(ctx context.Context, kind, envID, serviceID, hostID string) ([]seriesRef, error) {
	var rows *sql.Rows
	var err error
	if hostID != "" {
		rows, err = s.db.QueryContext(ctx, `SELECT id, key, host_id FROM series WHERE kind = ? AND host_id = ?`, kind, hostID)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id, key, host_id FROM series WHERE kind = ? AND service_id = ? AND env_id = ?`, kind, serviceID, envID)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []seriesRef
	for rows.Next() {
		var r seriesRef
		if err := rows.Scan(&r.id, &r.key, &r.host); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// bucketAgg aggregates one series' rows into buckets.
type bucketAgg struct {
	n    float64
	sum  [15]float64
	wsum [15]float64 // weight for averaged columns
	has  [15]bool
}

func aggregate(rows []row, from, step int64) map[int64]map[int64]*bucketAgg {
	out := map[int64]map[int64]*bucketAgg{} // sid -> bucket -> agg
	for _, r := range rows {
		b := from + (r.ts-from)/step*step
		m := out[r.sid]
		if m == nil {
			m = map[int64]*bucketAgg{}
			out[r.sid] = m
		}
		a := m[b]
		if a == nil {
			a = &bucketAgg{}
			m[b] = a
		}
		n := r.n
		if n <= 0 {
			n = 1
		}
		a.n += n
		for i, v := range r.v {
			if !v.Valid {
				continue
			}
			a.has[i] = true
			if sumCols[i] {
				a.sum[i] += v.Float64
			} else {
				a.sum[i] += v.Float64 * n
				a.wsum[i] += n
			}
		}
	}
	return out
}

func (a *bucketAgg) avg(i int) (float64, bool) {
	if !a.has[i] || a.wsum[i] == 0 {
		return 0, false
	}
	return a.sum[i] / a.wsum[i], true
}

// ServiceMetrics answers GET .../services/{sid}/metrics.
func (s *Store) ServiceMetrics(ctx context.Context, envID, serviceID string, from, until int64, perReplica bool) (api.MetricsResponse, error) {
	table, step := s.Resolution(from, until)
	resp := api.MetricsResponse{Step: step, Series: map[string][][2]float64{}, Deployments: []api.DeployMarker{}}
	reps, err := s.findSeries(ctx, KindReplica, envID, serviceID, "")
	if err != nil {
		return resp, err
	}
	ids := make([]int64, len(reps))
	keyOf := map[int64]string{}
	for i, r := range reps {
		ids[i] = r.id
		keyOf[r.id] = r.key
	}
	rows, err := s.readRows(ctx, table, ids, from, until)
	if err != nil {
		return resp, err
	}
	agg := aggregate(rows, from, step)
	stepSec := float64(step) / 1000
	type sums struct{ cpu, mem, rx, tx, disk float64 }
	total := map[int64]*sums{}
	if perReplica {
		resp.PerReplica = map[string]map[string][][2]float64{}
	}
	for sid, buckets := range agg {
		for b, a := range buckets {
			t := total[b]
			if t == nil {
				t = &sums{}
				total[b] = t
			}
			cpu, _ := a.avg(cCPU)
			mem, _ := a.avg(cMem)
			disk, _ := a.avg(cDisk)
			rx := a.sum[cRx] / stepSec
			tx := a.sum[cTx] / stepSec
			t.cpu += cpu
			t.mem += mem
			t.rx += rx
			t.tx += tx
			if disk > t.disk {
				t.disk = disk // one volume per service: replicas report the same volume
			}
			if perReplica {
				k := keyOf[sid]
				m := resp.PerReplica[k]
				if m == nil {
					m = map[string][][2]float64{}
					resp.PerReplica[k] = m
				}
				ts := float64(b)
				m["cpu"] = append(m["cpu"], [2]float64{ts, cpu})
				m["mem"] = append(m["mem"], [2]float64{ts, mem})
				m["rx"] = append(m["rx"], [2]float64{ts, rx})
				m["tx"] = append(m["tx"], [2]float64{ts, tx})
			}
		}
	}
	for _, b := range sortedKeys(total) {
		t := total[b]
		ts := float64(b)
		resp.Series["cpu"] = append(resp.Series["cpu"], [2]float64{ts, t.cpu})
		resp.Series["mem"] = append(resp.Series["mem"], [2]float64{ts, t.mem})
		resp.Series["rx"] = append(resp.Series["rx"], [2]float64{ts, t.rx})
		resp.Series["tx"] = append(resp.Series["tx"], [2]float64{ts, t.tx})
		resp.Series["disk"] = append(resp.Series["disk"], [2]float64{ts, t.disk})
	}
	for _, m := range resp.PerReplica {
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i][0] < m[k][j][0] })
		}
	}
	if err := s.httpSeries(ctx, table, envID, serviceID, from, step, until, resp.Series); err != nil {
		return resp, err
	}
	return resp, nil
}

func (s *Store) httpSeries(ctx context.Context, table, envID, serviceID string, from, step, until int64, out map[string][][2]float64) error {
	refs, err := s.findSeries(ctx, KindHTTP, envID, serviceID, "")
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	ids := make([]int64, len(refs))
	for i, r := range refs {
		ids[i] = r.id
	}
	rows, err := s.readRows(ctx, table, ids, from, until)
	if err != nil {
		return err
	}
	agg := aggregate(rows, from, step)
	type hb struct {
		r     [4]float64
		pw    float64
		p     [4]float64
		valid bool
	}
	buckets := map[int64]*hb{}
	for _, bs := range agg {
		for b, a := range bs {
			x := buckets[b]
			if x == nil {
				x = &hb{}
				buckets[b] = x
			}
			reqs := 0.0
			for i, c := range []int{cR2, cR3, cR4, cR5} {
				x.r[i] += a.sum[c]
				reqs += a.sum[c]
			}
			w := reqs
			for i, c := range []int{cP50, cP90, cP95, cP99} {
				if v, ok := a.avg(c); ok && w > 0 {
					x.p[i] += v * w
				}
			}
			x.pw += w
			x.valid = true
		}
	}
	stepSec := float64(step) / 1000
	for _, b := range sortedKeys(buckets) {
		x := buckets[b]
		ts := float64(b)
		all := 0.0
		for i, name := range []string{"req_2xx", "req_3xx", "req_4xx", "req_5xx"} {
			out[name] = append(out[name], [2]float64{ts, x.r[i] / stepSec})
			all += x.r[i]
		}
		rate := 0.0
		if all > 0 {
			rate = x.r[3] / all
		}
		out["error_rate"] = append(out["error_rate"], [2]float64{ts, rate})
		for i, name := range []string{"p50", "p90", "p95", "p99"} {
			v := 0.0
			if x.pw > 0 {
				v = x.p[i] / x.pw
			}
			out[name] = append(out[name], [2]float64{ts, v})
		}
	}
	return nil
}

// HostMetrics answers GET /hosts/{id}/metrics: cpu (vCPU), mem and disk (bytes).
func (s *Store) HostMetrics(ctx context.Context, hostID string, from, until int64) (api.MetricsResponse, error) {
	table, step := s.Resolution(from, until)
	resp := api.MetricsResponse{Step: step, Series: map[string][][2]float64{}, Deployments: []api.DeployMarker{}}
	refs, err := s.findSeries(ctx, KindHost, "", "", hostID)
	if err != nil || len(refs) == 0 {
		return resp, err
	}
	rows, err := s.readRows(ctx, table, []int64{refs[0].id}, from, until)
	if err != nil {
		return resp, err
	}
	agg := aggregate(rows, from, step)[refs[0].id]
	for _, b := range sortedKeys(agg) {
		a := agg[b]
		ts := float64(b)
		cpu, _ := a.avg(cCPU)
		mem, _ := a.avg(cMem)
		disk, _ := a.avg(cDisk)
		lim, _ := a.avg(cMemLimit)
		resp.Series["cpu"] = append(resp.Series["cpu"], [2]float64{ts, cpu})
		resp.Series["mem"] = append(resp.Series["mem"], [2]float64{ts, mem})
		resp.Series["disk"] = append(resp.Series["disk"], [2]float64{ts, disk})
		if lim > 0 {
			resp.MemLimit = int64(lim)
		}
	}
	return resp, nil
}

// DBMetrics answers the Postgres panel's Metrics tab.
func (s *Store) DBMetrics(ctx context.Context, envID, serviceID string, from, until int64) (api.DBMetricsResponse, error) {
	table, step := s.Resolution(from, until)
	resp := api.DBMetricsResponse{Step: step, Series: map[string][][2]float64{}, TopQueries: []api.QueryStat{}}
	s.mu.Lock()
	if q := s.topQ[envID+"/"+serviceID]; q != nil {
		resp.TopQueries = append(resp.TopQueries, q...)
	}
	s.mu.Unlock()
	sort.Slice(resp.TopQueries, func(i, j int) bool { return resp.TopQueries[i].TotalMs > resp.TopQueries[j].TotalMs })
	if len(resp.TopQueries) > 10 {
		resp.TopQueries = resp.TopQueries[:10]
	}
	refs, err := s.findSeries(ctx, KindPG, envID, serviceID, "")
	if err != nil || len(refs) == 0 {
		return resp, err
	}
	rows, err := s.readRows(ctx, table, []int64{refs[0].id}, from, until)
	if err != nil {
		return resp, err
	}
	agg := aggregate(rows, from, step)[refs[0].id]
	for _, b := range sortedKeys(agg) {
		a := agg[b]
		ts := float64(b)
		n := a.n
		if n <= 0 {
			n = 1
		}
		hit, _ := a.avg(cCPU)
		wal, _ := a.avg(cMem)
		resp.Series["cache_hit_ratio"] = append(resp.Series["cache_hit_ratio"], [2]float64{ts, hit})
		resp.Series["wal_bytes"] = append(resp.Series["wal_bytes"], [2]float64{ts, wal})
		// connection counts are gauges stored in summed columns: average them per sample
		resp.Series["connections_active"] = append(resp.Series["connections_active"], [2]float64{ts, a.sum[cR2] / n})
		resp.Series["connections_idle"] = append(resp.Series["connections_idle"], [2]float64{ts, a.sum[cR3] / n})
		resp.Series["connections_idle_in_transaction"] = append(resp.Series["connections_idle_in_transaction"], [2]float64{ts, a.sum[cR4] / n})
		resp.Series["connections_other"] = append(resp.Series["connections_other"], [2]float64{ts, a.sum[cR5] / n})
	}
	return resp, nil
}

// Point is one raw value for monitors.
type Point struct {
	TS    int64
	Value float64
	// Limit is the memory limit (memory) or host total (disk) when known.
	Limit float64
}

// Raw returns per-30 s service totals of one metric at raw resolution:
// "cpu" (vCPU), "memory" (bytes, Limit = summed limits), "disk" (bytes),
// "http_5xx_rate" (0..1, Limit = request count).
func (s *Store) Raw(ctx context.Context, envID, serviceID, metric string, from, until int64) ([]Point, error) {
	step := RawStep.Milliseconds()
	switch metric {
	case "http_5xx_rate":
		refs, err := s.findSeries(ctx, KindHTTP, envID, serviceID, "")
		if err != nil {
			return nil, err
		}
		ids := refIDs(refs)
		rows, err := s.readRows(ctx, "points", ids, from, until)
		if err != nil {
			return nil, err
		}
		agg := aggregate(rows, from, step)
		tot := map[int64][2]float64{}
		for _, bs := range agg {
			for b, a := range bs {
				x := tot[b]
				x[0] += a.sum[cR5]
				x[1] += a.sum[cR2] + a.sum[cR3] + a.sum[cR4] + a.sum[cR5]
				tot[b] = x
			}
		}
		var out []Point
		for _, b := range sortedKeys(tot) {
			x := tot[b]
			v := 0.0
			if x[1] > 0 {
				v = x[0] / x[1]
			}
			out = append(out, Point{TS: b, Value: v, Limit: x[1]})
		}
		return out, nil
	default:
		refs, err := s.findSeries(ctx, KindReplica, envID, serviceID, "")
		if err != nil {
			return nil, err
		}
		rows, err := s.readRows(ctx, "points", refIDs(refs), from, until)
		if err != nil {
			return nil, err
		}
		agg := aggregate(rows, from, step)
		tot := map[int64]*Point{}
		for _, bs := range agg {
			for b, a := range bs {
				p := tot[b]
				if p == nil {
					p = &Point{TS: b}
					tot[b] = p
				}
				switch metric {
				case "cpu":
					v, _ := a.avg(cCPU)
					p.Value += v
				case "memory":
					v, _ := a.avg(cMem)
					l, _ := a.avg(cMemLimit)
					p.Value += v
					p.Limit += l
				case "disk":
					v, _ := a.avg(cDisk)
					if v > p.Value {
						p.Value = v
					}
				}
			}
		}
		out := make([]Point, 0, len(tot))
		for _, b := range sortedKeys(tot) {
			out = append(out, *tot[b])
		}
		return out, nil
	}
}

func refIDs(refs []seriesRef) []int64 {
	out := make([]int64, len(refs))
	for i, r := range refs {
		out[i] = r.id
	}
	return out
}

func sortedKeys[V any](m map[int64]V) []int64 {
	ks := make([]int64, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i] < ks[j] })
	return ks
}

// ---------------------------------------------------------------- usage

// Usage is a service instance's resource use over a window (9.23).
type Usage struct {
	VCPUHours      float64
	MemGBHours     float64
	EgressGB       float64
	VolumeGBMonths float64
}

const gib = float64(1 << 30)

// UsageFor sums the 1 hour rollups of a service instance's replicas.
func (s *Store) UsageFor(ctx context.Context, envID, serviceID string, from, until int64) (Usage, error) {
	refs, err := s.findSeries(ctx, KindReplica, envID, serviceID, "")
	if err != nil {
		return Usage{}, err
	}
	rows, err := s.readRows(ctx, "points_1h", refIDs(refs), from, until)
	if err != nil {
		return Usage{}, err
	}
	var u Usage
	sample := RawStep.Seconds()
	diskByHour := map[int64]float64{}
	diskSamples := map[int64]float64{}
	for _, r := range rows {
		n := r.n
		if n <= 0 {
			n = 1
		}
		hours := n * sample / 3600
		if r.v[cCPU].Valid {
			u.VCPUHours += r.v[cCPU].Float64 * hours
		}
		if r.v[cMem].Valid {
			u.MemGBHours += r.v[cMem].Float64 / gib * hours
		}
		if r.v[cTxExt].Valid {
			u.EgressGB += r.v[cTxExt].Float64 / gib
		}
		if r.v[cDisk].Valid {
			// replicas of one service share one volume: keep the largest per hour
			if r.v[cDisk].Float64 > diskByHour[r.ts] {
				diskByHour[r.ts] = r.v[cDisk].Float64
				diskSamples[r.ts] = hours
			}
		}
	}
	for ts, d := range diskByHour {
		u.VolumeGBMonths += d / gib * diskSamples[ts] / 730
	}
	return u, nil
}

// ---------------------------------------------------------------- maintenance

// Rollup recomputes the 5 minute and 1 hour rollups of the last two hours
// (the current partial buckets included) from raw points.
func (s *Store) Rollup(ctx context.Context) error {
	now := s.now().UnixMilli()
	for _, r := range []struct {
		table string
		step  int64
	}{{"points_5m", (5 * time.Minute).Milliseconds()}, {"points_1h", time.Hour.Milliseconds()}} {
		from := now - 2*time.Hour.Milliseconds()
		from -= from % r.step
		q := fmt.Sprintf(`INSERT INTO %s (series_id, ts, %s)
			SELECT series_id, (ts / %d) * %d, SUM(n),
				AVG(cpu), AVG(mem), AVG(mem_limit), SUM(rx), SUM(tx), SUM(tx_ext), AVG(disk),
				SUM(r2), SUM(r3), SUM(r4), SUM(r5), AVG(p50), AVG(p90), AVG(p95), AVG(p99)
			FROM points WHERE ts >= ? GROUP BY series_id, ts / %d
			ON CONFLICT(series_id, ts) DO UPDATE SET n = excluded.n, cpu = excluded.cpu, mem = excluded.mem,
				mem_limit = excluded.mem_limit, rx = excluded.rx, tx = excluded.tx, tx_ext = excluded.tx_ext,
				disk = excluded.disk, r2 = excluded.r2, r3 = excluded.r3, r4 = excluded.r4, r5 = excluded.r5,
				p50 = excluded.p50, p90 = excluded.p90, p95 = excluded.p95, p99 = excluded.p99`,
			r.table, pointCols, r.step, r.step, r.step)
		if err := s.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, q, from)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// Janitor deletes expired points in batches of 5,000, each in its own short
// transaction, and returns how many rows it removed.
func (s *Store) Janitor(ctx context.Context) (int64, error) {
	now := s.now().UnixMilli()
	var total int64
	for _, r := range []struct {
		table string
		keep  time.Duration
	}{{"points", RawKeep}, {"points_5m", FiveMinKeep}, {"points_1h", HourKeep}} {
		cut := now - r.keep.Milliseconds()
		for {
			var n int64
			err := s.write(ctx, func(tx *sql.Tx) error {
				res, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (series_id, ts) IN
					(SELECT series_id, ts FROM %s WHERE ts < ? LIMIT %d)`, r.table, r.table, janitorBatch), cut)
				if err != nil {
					return err
				}
				n, err = res.RowsAffected()
				return err
			})
			if err != nil {
				return total, err
			}
			total += n
			if n < janitorBatch {
				break
			}
		}
	}
	return total, nil
}

// Run performs rollups every 5 minutes and the janitor every 10 minutes
// until ctx is done.
func (s *Store) Run(ctx context.Context) {
	roll := time.NewTicker(5 * time.Minute)
	jan := time.NewTicker(10 * time.Minute)
	defer roll.Stop()
	defer jan.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-roll.C:
			if err := s.Rollup(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("metrics rollup", "err", err)
			}
		case <-jan.C:
			if _, err := s.Janitor(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("metrics janitor", "err", err)
			}
		}
	}
}
