// SPDX-License-Identifier: AGPL-3.0-only

// Package logstore is the agent's log store (docs/SPEC.md 8.1, 9.4, 9.19):
// processing of raw container output, hourly JSON-lines files per service
// under <DataDir>/logs/<env>/<svc>/<YYYYMMDDHH>.jsonl (gzipped after two
// hours), queries with pkg/logfilter, live subscriptions, the byte budgets
// and the tail of error and deploy lines shipped to the control plane.
package logstore

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/pkg/logfilter"
	"github.com/musakkka/5050cloud/pkg/proto"
)

const hourLayout = "2006010215"

// DefaultLimit and MaxLimit bound query results (9.4).
const (
	DefaultLimit = 500
	MaxLimit     = 5000
)

// Budgets are the retention limits (9.19).
type Budgets struct {
	RetentionDays   int           // default 7
	MaxBytesPerSvc  int64         // default 1 GiB
	MaxPctFS        int           // default 10
	FilesystemBytes int64         // size of the filesystem holding the store; 0 skips the percentage cap
	GzipAfter       time.Duration // default 2 hours
}

// Defaults fills zero fields with the spec defaults.
func (b Budgets) Defaults() Budgets {
	if b.RetentionDays <= 0 {
		b.RetentionDays = 7
	}
	if b.MaxBytesPerSvc <= 0 {
		b.MaxBytesPerSvc = 1 << 30
	}
	if b.MaxPctFS <= 0 {
		b.MaxPctFS = 10
	}
	if b.GzipAfter <= 0 {
		b.GzipAfter = 2 * time.Hour
	}
	return b
}

// Store is the on-disk log store of one host.
type Store struct {
	root string
	now  func() time.Time

	mu     sync.Mutex
	open   map[string]*openFile // by relative path without extension
	svcEnv map[string]string    // service id -> environment id
	subs   map[string]*Sub
	tail   *Tail
	closed bool
}

type openFile struct {
	f    *os.File
	w    *bufio.Writer
	size int64
	hour time.Time
	last time.Time
}

// Open opens (and creates) the store under root, normally <DataDir>/logs.
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("could not create the log directory %s: %w", root, err)
	}
	s := &Store{root: root, now: time.Now, open: map[string]*openFile{}, svcEnv: map[string]string{}, subs: map[string]*Sub{}, tail: NewTail(1000)}
	envs, _ := os.ReadDir(root)
	for _, e := range envs {
		if !e.IsDir() {
			continue
		}
		svcs, _ := os.ReadDir(filepath.Join(root, e.Name()))
		for _, sv := range svcs {
			if sv.IsDir() {
				s.svcEnv[sv.Name()] = e.Name()
			}
		}
	}
	return s, nil
}

// SetClock replaces the clock (tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Tail returns the buffer of lines shipped to the control plane.
func (s *Store) Tail() *Tail { return s.tail }

func safeName(s string) string {
	if s == "" {
		return "_"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
}

// Append stores records of one environment. It assigns each record its ID
// ("<env>/<svc>/<hour>:<offset>"), feeds live subscribers and the tail
// buffer. Records of the http stream are stored under their ServiceID.
func (s *Store) Append(envID string, recs ...proto.LogRecord) error {
	if len(recs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("the log store is closed")
	}
	env := safeName(envID)
	var firstErr error
	for i := range recs {
		r := &recs[i]
		svc := safeName(r.ServiceID)
		s.svcEnv[svc] = env
		ts := time.UnixMilli(r.TS).UTC()
		hour := ts.Truncate(time.Hour)
		// A line older than the gzip horizon goes into the current hour's
		// file instead of reopening a compressed one.
		if s.now().Sub(hour) > 2*time.Hour-time.Minute {
			hour = s.now().UTC().Truncate(time.Hour)
		}
		rel := filepath.Join(env, svc, hour.Format(hourLayout))
		of, err := s.fileLocked(rel, hour)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		r.ID = filepath.ToSlash(rel) + ":" + strconv.FormatInt(of.size, 10)
		b, err := json.Marshal(r)
		if err != nil {
			continue
		}
		b = append(b, '\n')
		n, err := of.w.Write(b)
		of.size += int64(n)
		of.last = s.now()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("could not write logs for service %s: %w", r.ServiceID, err)
		}
		for _, sub := range s.subs {
			sub.offer(*r)
		}
		if r.Stream != "http" {
			s.tail.Add(*r)
		}
	}
	return firstErr
}

func (s *Store) fileLocked(rel string, hour time.Time) (*openFile, error) {
	if of, ok := s.open[rel]; ok {
		return of, nil
	}
	p := filepath.Join(s.root, rel+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fmt.Errorf("could not create the log directory %s: %w", filepath.Dir(p), err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("could not open the log file %s: %w", p, err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	of := &openFile{f: f, w: bufio.NewWriterSize(f, 64<<10), size: st.Size(), hour: hour, last: s.now()}
	s.open[rel] = of
	return of, nil
}

// Flush writes buffered lines to disk and closes files of past hours that
// have been idle for a minute.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked(false)
}

func (s *Store) flushLocked(closeAll bool) error {
	var firstErr error
	cur := s.now().UTC().Truncate(time.Hour)
	for rel, of := range s.open {
		if err := of.w.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
		if closeAll || (of.hour.Before(cur) && s.now().Sub(of.last) > time.Minute) {
			of.f.Close()
			delete(s.open, rel)
		}
	}
	return firstErr
}

// Close flushes and closes every file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.flushLocked(true)
	s.closed = true
	return err
}

// ---------------------------------------------------------------- queries

type fileRef struct {
	env, svc string
	hour     time.Time
	path     string
	gz       bool
	size     int64
}

// files lists stored hour files of a service, oldest first.
func (s *Store) filesOf(env, svc string) []fileRef {
	dir := filepath.Join(s.root, env, svc)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byHour := map[string]fileRef{}
	for _, e := range ents {
		name := e.Name()
		var base string
		gz := false
		switch {
		case strings.HasSuffix(name, ".jsonl.gz"):
			base, gz = strings.TrimSuffix(name, ".jsonl.gz"), true
		case strings.HasSuffix(name, ".jsonl"):
			base = strings.TrimSuffix(name, ".jsonl")
		default:
			continue
		}
		h, err := time.Parse(hourLayout, base)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fr := fileRef{env: env, svc: svc, hour: h, path: filepath.Join(dir, name), gz: gz, size: info.Size()}
		if prev, ok := byHour[base]; ok && !prev.gz {
			continue // plain file wins while both exist during compression
		}
		byHour[base] = fr
	}
	out := make([]fileRef, 0, len(byHour))
	for _, fr := range byHour {
		out = append(out, fr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].hour.Before(out[j].hour) })
	return out
}

// readFile calls fn for every record of a file in order.
func readFile(fr fileRef, fn func(proto.LogRecord) bool) error {
	f, err := os.Open(fr.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if fr.gz {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("the log file %s is damaged: %w", fr.path, err)
		}
		defer func() { _ = gz.Close() }()
		r = gz
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var rec proto.LogRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if !fn(rec) {
			return nil
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	return nil
}

func streamOK(streams []string, s string) bool {
	if len(streams) == 0 {
		return s == "stdout" || s == "stderr" || s == "system"
	}
	for _, x := range streams {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// matcher tests one record against a query.
type matcher struct {
	q      proto.LogsQuery
	f      *logfilter.Filter
	svcs   map[string]bool
	window bool
}

func newMatcher(q proto.LogsQuery, window bool) (*matcher, error) {
	f, err := logfilter.Parse(q.Filter)
	if err != nil {
		return nil, err
	}
	m := &matcher{q: q, f: f, svcs: map[string]bool{}, window: window}
	for _, s := range q.ServiceIDs {
		m.svcs[s] = true
	}
	return m, nil
}

func (m *matcher) match(r *proto.LogRecord) bool {
	if len(m.svcs) > 0 && !m.svcs[r.ServiceID] {
		return false
	}
	if m.q.DeploymentID != "" && r.DeploymentID != m.q.DeploymentID {
		return false
	}
	if !streamOK(m.q.Streams, r.Stream) {
		return false
	}
	if m.window {
		if m.q.Since > 0 && r.TS < m.q.Since {
			return false
		}
		if m.q.Until > 0 && r.TS > m.q.Until {
			return false
		}
	}
	return m.f.MatchRecord(r)
}

// Query returns the newest Limit matching records in the window, oldest
// first. It scans hour files newest first and stops once it has enough.
func (s *Store) Query(q proto.LogsQuery) (proto.LogsResult, error) {
	if err := s.Flush(); err != nil {
		return proto.LogsResult{}, err
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	if q.Around != "" {
		return s.around(q)
	}
	m, err := newMatcher(q, true)
	if err != nil {
		return proto.LogsResult{}, err
	}
	// Files grouped by hour across the requested services.
	byHour := map[time.Time][]fileRef{}
	for _, fr := range s.candidateFiles(q.ServiceIDs) {
		if q.Since > 0 && fr.hour.Add(time.Hour+2*time.Hour).UnixMilli() < q.Since {
			// Late lines can land up to 2 hours after their hour; keep a margin.
			continue
		}
		if q.Until > 0 && fr.hour.UnixMilli() > q.Until+int64(time.Hour/time.Millisecond) {
			continue
		}
		byHour[fr.hour] = append(byHour[fr.hour], fr)
	}
	hours := make([]time.Time, 0, len(byHour))
	for h := range byHour {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].After(hours[j]) })
	var got []proto.LogRecord
	truncated := false
	for hi, h := range hours {
		var batch []proto.LogRecord
		for _, fr := range byHour[h] {
			ring := newRing(q.Limit)
			if err := readFile(fr, func(r proto.LogRecord) bool {
				if m.match(&r) {
					ring.add(r)
				}
				return true
			}); err != nil {
				return proto.LogsResult{}, err
			}
			if ring.dropped > 0 {
				truncated = true
			}
			batch = append(batch, ring.items()...)
		}
		got = append(got, batch...)
		if len(got) >= q.Limit {
			if hi < len(hours)-1 {
				truncated = true
			}
			// A late line in an older file could still be newer; one more hour covers the margin.
			if hi+1 < len(hours) {
				for _, fr := range byHour[hours[hi+1]] {
					ring := newRing(q.Limit)
					_ = readFile(fr, func(r proto.LogRecord) bool {
						if m.match(&r) {
							ring.add(r)
						}
						return true
					})
					got = append(got, ring.items()...)
				}
			}
			break
		}
	}
	sortRecords(got)
	if len(got) > q.Limit {
		got = got[len(got)-q.Limit:]
		truncated = true
	}
	return proto.LogsResult{Lines: got, Truncated: truncated}, nil
}

func (s *Store) candidateFiles(svcs []string) []fileRef {
	s.mu.Lock()
	type pair struct{ env, svc string }
	var pairs []pair
	if len(svcs) == 0 {
		for svc, env := range s.svcEnv {
			pairs = append(pairs, pair{env, svc})
		}
	} else {
		for _, id := range svcs {
			svc := safeName(id)
			if env, ok := s.svcEnv[svc]; ok {
				pairs = append(pairs, pair{env, svc})
			}
		}
	}
	s.mu.Unlock()
	var out []fileRef
	for _, p := range pairs {
		out = append(out, s.filesOf(p.env, p.svc)...)
	}
	return out
}

func sortRecords(recs []proto.LogRecord) {
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].TS != recs[j].TS {
			return recs[i].TS < recs[j].TS
		}
		return recs[i].ID < recs[j].ID
	})
}

// around returns Limit records centred on the record q.Around (View in
// Context), ignoring the filter and the time window.
func (s *Store) around(q proto.LogsQuery) (proto.LogsResult, error) {
	colon := strings.LastIndexByte(q.Around, ':')
	if colon < 0 {
		return proto.LogsResult{}, fmt.Errorf("the log line id %q is not valid", q.Around)
	}
	rel := q.Around[:colon]
	parts := strings.Split(rel, "/")
	if len(parts) != 3 {
		return proto.LogsResult{}, fmt.Errorf("the log line id %q is not valid", q.Around)
	}
	env, svc := safeName(parts[0]), safeName(parts[1])
	hour, err := time.Parse(hourLayout, parts[2])
	if err != nil {
		return proto.LogsResult{}, fmt.Errorf("the log line id %q is not valid", q.Around)
	}
	m := &matcher{q: proto.LogsQuery{Streams: q.Streams}, f: nil}
	var all []proto.LogRecord
	for _, fr := range s.filesOf(env, svc) {
		if fr.hour.Before(hour.Add(-time.Hour)) || fr.hour.After(hour.Add(time.Hour)) {
			continue
		}
		if err := readFile(fr, func(r proto.LogRecord) bool {
			if m.match(&r) || r.ID == q.Around {
				all = append(all, r)
			}
			return true
		}); err != nil {
			return proto.LogsResult{}, err
		}
	}
	sortRecords(all)
	idx := -1
	for i := range all {
		if all[i].ID == q.Around {
			idx = i
			break
		}
	}
	if idx < 0 {
		return proto.LogsResult{}, fmt.Errorf("the log line %s is no longer stored on this host", q.Around)
	}
	lo := idx - q.Limit/2
	if lo < 0 {
		lo = 0
	}
	hi := lo + q.Limit
	if hi > len(all) {
		hi = len(all)
		lo = max(0, hi-q.Limit)
	}
	return proto.LogsResult{Lines: all[lo:hi], Truncated: lo > 0 || hi < len(all)}, nil
}

// ring keeps the last n items.
type ring struct {
	buf     []proto.LogRecord
	n       int
	start   int
	dropped int
}

func newRing(n int) *ring { return &ring{n: n} }

func (r *ring) add(x proto.LogRecord) {
	if len(r.buf) < r.n {
		r.buf = append(r.buf, x)
		return
	}
	r.buf[r.start] = x
	r.start = (r.start + 1) % r.n
	r.dropped++
}

func (r *ring) items() []proto.LogRecord {
	out := make([]proto.LogRecord, 0, len(r.buf))
	out = append(out, r.buf[r.start:]...)
	return append(out, r.buf[:r.start]...)
}

// ---------------------------------------------------------------- subscriptions

// Sub is a live subscription. Lines arrive on C; when C is full the line is
// dropped and counted, so a slow reader never blocks the store.
type Sub struct {
	ID      string
	C       chan proto.LogRecord
	m       *matcher
	mu      sync.Mutex
	dropped int
}

func (sub *Sub) offer(r proto.LogRecord) {
	if !sub.m.match(&r) {
		return
	}
	select {
	case sub.C <- r:
	default:
		sub.mu.Lock()
		sub.dropped++
		sub.mu.Unlock()
	}
}

// TakeDropped returns and resets the number of dropped lines.
func (sub *Sub) TakeDropped() int {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	n := sub.dropped
	sub.dropped = 0
	return n
}

// Subscribe starts a live tail. Since, Until, Limit and Around are ignored.
func (s *Store) Subscribe(id string, q proto.LogsQuery, buffer int) (*Sub, error) {
	q.Since, q.Until, q.Limit, q.Around = 0, 0, 0, ""
	m, err := newMatcher(q, false)
	if err != nil {
		return nil, err
	}
	if buffer <= 0 {
		buffer = 1024
	}
	sub := &Sub{ID: id, C: make(chan proto.LogRecord, buffer), m: m}
	s.mu.Lock()
	if old, ok := s.subs[id]; ok {
		close(old.C)
	}
	s.subs[id] = sub
	s.mu.Unlock()
	return sub, nil
}

// Unsubscribe ends a live tail and closes its channel.
func (s *Store) Unsubscribe(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(sub.C)
	}
}

// UnsubscribeAll ends every live tail (the socket closed).
func (s *Store) UnsubscribeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sub := range s.subs {
		delete(s.subs, id)
		close(sub.C)
	}
}
