// SPDX-License-Identifier: AGPL-3.0-only

package logs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/logfilter"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// ---------------------------------------------------------------- host tail (o_host_logs)

// Tail limits (9.4, 9.19).
const (
	TailKeep         = 15 * time.Minute
	TailMaxHostBytes = 5 << 20
)

// DB is a *sql.DB or *sql.Tx.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// StoreTail inserts a host_logs_tail batch and trims the host's tail to 15
// minutes and 5 MB.
func StoreTail(ctx context.Context, tx DB, hostID string, lines []proto.LogRecord, now time.Time) error {
	for _, l := range lines {
		attrs := ""
		if len(l.Attrs) > 0 {
			b, _ := json.Marshal(l.Attrs)
			attrs = string(b)
		}
		msg := l.Msg
		if len(msg) > MaxLineBytes {
			msg = msg[:MaxLineBytes]
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO o_host_logs (host_id, ts, line_id, service_id, deployment_id, replica_id, stream, level, line, attrs)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, hostID, l.TS, l.ID, l.ServiceID, l.DeploymentID, l.ReplicaID, l.Stream, l.Level, msg, attrs); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM o_host_logs WHERE host_id = ? AND ts < ?`, hostID, now.Add(-TailKeep).UnixMilli()); err != nil {
		return err
	}
	var size int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(LENGTH(line) + LENGTH(attrs) + 64), 0) FROM o_host_logs WHERE host_id = ?`, hostID).Scan(&size); err != nil {
		return err
	}
	if size > TailMaxHostBytes {
		// delete the oldest tenth until under budget
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM o_host_logs WHERE host_id = ?`, hostID).Scan(&n); err != nil {
			return err
		}
		drop := n/10 + 1
		if _, err := tx.ExecContext(ctx, `DELETE FROM o_host_logs WHERE id IN (SELECT id FROM o_host_logs WHERE host_id = ? ORDER BY ts LIMIT ?)`, hostID, drop); err != nil {
			return err
		}
	}
	return nil
}

// ReadTail returns the kept lines of a host for some services within a window.
func ReadTail(ctx context.Context, db DB, hostID string, services map[string]bool, since, until int64) ([]api.LogLine, error) {
	q := `SELECT line_id, ts, service_id, deployment_id, replica_id, stream, level, line, attrs FROM o_host_logs WHERE host_id = ?`
	args := []any{hostID}
	if since > 0 {
		q += ` AND ts >= ?`
		args = append(args, since)
	}
	if until > 0 {
		q += ` AND ts <= ?`
		args = append(args, until)
	}
	q += ` ORDER BY ts`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []api.LogLine
	for rows.Next() {
		var l api.LogLine
		var attrs string
		if err := rows.Scan(&l.ID, &l.TS, &l.ServiceID, &l.DeploymentID, &l.Replica, &l.Stream, &l.Level, &l.Msg, &attrs); err != nil {
			return nil, err
		}
		if len(services) > 0 && !services[l.ServiceID] {
			continue
		}
		if attrs != "" {
			_ = json.Unmarshal([]byte(attrs), &l.Attrs)
		}
		if l.ID == "" {
			l.ID = fmt.Sprintf("tail:%s:%d", hostID, l.TS)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- fan-out

// Source is what the fan-out needs from the engine.
type Source interface {
	// Services lists the service ids of an environment (an empty query means all of them).
	Services(ctx context.Context, envID string) ([]string, error)
	// Hosts lists the hosts that ran replicas of these services (or of one
	// deployment) during the window, with their names.
	Hosts(ctx context.Context, envID string, serviceIDs []string, deploymentID string, since, until int64) ([]HostRef, error)
	// Builds lists the deployments of these services whose build ran in the window.
	Builds(ctx context.Context, envID string, serviceIDs []string, deploymentID string, since, until int64) ([]string, error)
	// Online reports whether a host is connected.
	Online(hostID string) bool
	// QueryHost asks one agent.
	QueryHost(ctx context.Context, hostID string, q proto.LogsQuery) (proto.LogsResult, error)
	// Subscribe starts a live tail on one host until ctx is done.
	Subscribe(ctx context.Context, hostID string, sub proto.LogsSubscribe, fn func(proto.LogLines)) error
	// Tail returns the o_host_logs lines of a host.
	Tail(ctx context.Context, hostID string, services map[string]bool, since, until int64) ([]api.LogLine, error)
}

// HostRef names a host.
type HostRef struct {
	ID   string
	Name string
}

// Fanout answers log queries and live tails.
type Fanout struct {
	Src    Source
	Builds *Builds
	// HostTimeout bounds one host's answer.
	HostTimeout time.Duration
}

func streamsFor(types []string) (runtime, http, build bool) {
	if len(types) == 0 {
		return true, false, false
	}
	for _, t := range types {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "deploy":
			runtime = true
		case "http":
			http = true
		case "build":
			build = true
		}
	}
	return
}

// OfflineNotice is the sentence for an offline host (9.4).
func OfflineNotice(name string) string {
	return fmt.Sprintf("Host %s is offline. Showing its last error and deploy lines; full logs return when it is back.", name)
}

// Query answers GET /logs.
func (f *Fanout) Query(ctx context.Context, q cpapi.LogsQuery) (api.LogsResponse, error) {
	limit := ClampLimit(q.Limit)
	filter, err := logfilter.Parse(q.Filter)
	if err != nil {
		return api.LogsResponse{}, err
	}
	services := q.ServiceIDs
	if len(services) == 0 && q.DeploymentID == "" {
		services, err = f.Src.Services(ctx, q.EnvID)
		if err != nil {
			return api.LogsResponse{}, err
		}
	}
	svcSet := map[string]bool{}
	for _, s := range services {
		svcSet[s] = true
	}
	runtime, httpLogs, build := streamsFor(q.Types)
	var resp api.LogsResponse
	var mu sync.Mutex
	var all []api.LogLine
	add := func(ls []api.LogLine) {
		mu.Lock()
		all = append(all, ls...)
		mu.Unlock()
	}
	if runtime || httpLogs {
		var streams []string
		if runtime && httpLogs {
			streams = []string{"stdout", "stderr", "system", "http"}
		} else if httpLogs {
			streams = []string{"http"}
		}
		hosts, err := f.Src.Hosts(ctx, q.EnvID, services, q.DeploymentID, q.Since, q.Until)
		if err != nil {
			return resp, err
		}
		pq := proto.LogsQuery{ServiceIDs: services, DeploymentID: q.DeploymentID, Streams: streams, Since: q.Since, Until: q.Until,
			Filter: q.Filter, Limit: limit, Around: q.Around}
		var wg sync.WaitGroup
		for _, h := range hosts {
			if !f.Src.Online(h.ID) {
				tail, err := f.Src.Tail(ctx, h.ID, svcSet, q.Since, q.Until)
				if err == nil {
					var keep []api.LogLine
					for _, l := range tail {
						if q.DeploymentID != "" && l.DeploymentID != q.DeploymentID {
							continue
						}
						if !streamWanted(l.Stream, runtime, httpLogs) || !Match(filter, l) {
							continue
						}
						keep = append(keep, l)
					}
					add(keep)
				}
				mu.Lock()
				resp.Notices = append(resp.Notices, OfflineNotice(h.Name))
				mu.Unlock()
				continue
			}
			wg.Add(1)
			go func(h HostRef) {
				defer wg.Done()
				hctx := ctx
				if f.HostTimeout > 0 {
					var cancel context.CancelFunc
					hctx, cancel = context.WithTimeout(ctx, f.HostTimeout)
					defer cancel()
				}
				res, err := f.Src.QueryHost(hctx, h.ID, pq)
				if err != nil {
					mu.Lock()
					resp.Notices = append(resp.Notices, fmt.Sprintf("Host %s did not answer the log query: %s", h.Name, strings.TrimSuffix(err.Error(), ".")+"."))
					mu.Unlock()
					return
				}
				ls := make([]api.LogLine, 0, len(res.Lines))
				for _, r := range res.Lines {
					ls = append(ls, ToLine(r))
				}
				add(ls)
				if res.Truncated {
					mu.Lock()
					resp.Truncated = true
					mu.Unlock()
				}
			}(h)
		}
		wg.Wait()
	}
	if build && f.Builds != nil {
		deps, err := f.Src.Builds(ctx, q.EnvID, services, q.DeploymentID, q.Since, q.Until)
		if err != nil {
			return resp, err
		}
		for _, d := range deps {
			ls, err := f.Builds.Read(d)
			if err != nil {
				continue
			}
			var keep []api.LogLine
			for _, l := range ls {
				if (q.Since > 0 && l.TS < q.Since) || (q.Until > 0 && l.TS > q.Until) || !Match(filter, l) {
					continue
				}
				keep = append(keep, l)
			}
			add(keep)
		}
	}
	all = dedupe(all)
	if q.Around != "" {
		resp.Lines = Around(all, q.Around, limit)
	} else {
		var cut bool
		resp.Lines, cut = Newest(all, limit)
		resp.Truncated = resp.Truncated || cut
	}
	if resp.Lines == nil {
		resp.Lines = []api.LogLine{}
	}
	sort.Strings(resp.Notices)
	if len(resp.Lines) == 0 && len(resp.Notices) == 0 {
		if q.Filter != "" {
			resp.Notices = []string{"No log lines match this filter in the selected window."}
		} else {
			resp.Notices = []string{"No log lines in the selected window."}
		}
	}
	return resp, nil
}

func streamWanted(stream string, runtime, http bool) bool {
	if stream == "http" {
		return http
	}
	return runtime
}

func dedupe(ls []api.LogLine) []api.LogLine {
	seen := make(map[string]bool, len(ls))
	out := ls[:0]
	for _, l := range ls {
		k := l.ID + "|" + l.Replica
		if l.ID != "" && seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, l)
	}
	return out
}

// TailQueue is the bounded queue of a live tail.
const TailQueue = 5000

// Tail streams live lines until ctx is done. It subscribes on every online
// host that runs the services and re-checks the host set every
// resubscribe interval, so a host that comes back joins the tail. Lines are
// emitted in batches; when the client cannot keep up, lines are dropped with
// a marker instead of blocking the agents.
func (f *Fanout) Tail(ctx context.Context, q cpapi.LogsQuery, emit func([]api.LogLine) error) error {
	filter, err := logfilter.Parse(q.Filter)
	if err != nil {
		return err
	}
	services := q.ServiceIDs
	if len(services) == 0 && q.DeploymentID == "" {
		services, err = f.Src.Services(ctx, q.EnvID)
		if err != nil {
			return err
		}
	}
	runtime, httpLogs, build := streamsFor(q.Types)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	queue := make(chan api.LogLine, TailQueue)
	var dropped int
	var dmu sync.Mutex
	push := func(l api.LogLine) {
		select {
		case queue <- l:
		default:
			dmu.Lock()
			dropped++
			dmu.Unlock()
		}
	}
	var streams []string
	if runtime && httpLogs {
		streams = []string{"stdout", "stderr", "system", "http"}
	} else if httpLogs {
		streams = []string{"http"}
	}
	subs := map[string]context.CancelFunc{}
	var smu sync.Mutex
	resubscribe := func() {
		if !runtime && !httpLogs {
			return
		}
		now := time.Now().UnixMilli()
		hosts, err := f.Src.Hosts(ctx, q.EnvID, services, q.DeploymentID, now-time.Hour.Milliseconds(), now)
		if err != nil {
			return
		}
		smu.Lock()
		defer smu.Unlock()
		want := map[string]bool{}
		for _, h := range hosts {
			if !f.Src.Online(h.ID) {
				continue
			}
			want[h.ID] = true
			if subs[h.ID] != nil {
				continue
			}
			sctx, scancel := context.WithCancel(ctx)
			subs[h.ID] = scancel
			go func(hostID string) {
				_ = f.Src.Subscribe(sctx, hostID, proto.LogsSubscribe{Query: proto.LogsQuery{ServiceIDs: services,
					DeploymentID: q.DeploymentID, Streams: streams, Filter: q.Filter}}, func(ll proto.LogLines) {
					if ll.Dropped > 0 {
						dmu.Lock()
						dropped += ll.Dropped
						dmu.Unlock()
					}
					for _, r := range ll.Lines {
						push(ToLine(r))
					}
				})
				smu.Lock()
				if subs[hostID] != nil {
					delete(subs, hostID)
				}
				smu.Unlock()
				scancel()
			}(h.ID)
		}
		for id, c := range subs {
			if !want[id] {
				c()
				delete(subs, id)
			}
		}
	}
	resubscribe()
	if build && f.Builds != nil {
		deps, err := f.Src.Builds(ctx, q.EnvID, services, q.DeploymentID, time.Now().Add(-time.Hour).UnixMilli(), 0)
		if err == nil {
			for _, d := range deps {
				if !f.Builds.Running(d) {
					continue
				}
				go func(dep string) {
					first := true
					_ = f.Builds.Follow(ctx, dep, func(ls []api.LogLine) error {
						if first {
							first = false
							return nil // the client already loaded the stored lines
						}
						for _, l := range ls {
							if Match(filter, l) {
								push(l)
							}
						}
						return nil
					})
				}(d)
			}
		}
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	re := time.NewTicker(5 * time.Second)
	defer re.Stop()
	var batch []api.LogLine
	for {
		select {
		case <-ctx.Done():
			return nil
		case l := <-queue:
			batch = append(batch, l)
			if len(batch) >= 500 {
				if err := f.flush(&batch, emit, &dmu, &dropped); err != nil {
					return err
				}
			}
		case <-tick.C:
			if err := f.flush(&batch, emit, &dmu, &dropped); err != nil {
				return err
			}
		case <-re.C:
			resubscribe()
		}
	}
}

func (f *Fanout) flush(batch *[]api.LogLine, emit func([]api.LogLine) error, mu *sync.Mutex, dropped *int) error {
	mu.Lock()
	d := *dropped
	*dropped = 0
	mu.Unlock()
	if d > 0 {
		*batch = append(*batch, DroppedLine(d, time.Now()))
	}
	if len(*batch) == 0 {
		return nil
	}
	sortLines(*batch)
	err := emit(*batch)
	*batch = nil
	return err
}
