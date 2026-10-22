// SPDX-License-Identifier: AGPL-3.0-only

// Package logs is the control plane side of logs (docs/SPEC.md 9.4):
//
//   - Builds stores build logs as <DataDir>/builds/<deployment>.log.gz and
//     lets clients follow a running build.
//   - HostTail keeps the error and deploy lines each agent ships every 10 s
//     (o_host_logs, 15 minutes and at most 5 MB per host) so queries can
//     still show something while a host is offline.
//   - Fanout answers queries and live tails by asking every host that ran
//     the services in the window, merging by timestamp and capping at the
//     limit. Offline hosts contribute their kept lines and a notice; a
//     result is never empty without saying why.
package logs

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/logfilter"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Limits of a query (9.4).
const (
	DefaultLimit = 500
	MaxLimit     = 5000
	// MaxLineBytes caps one stored build log line.
	MaxLineBytes = 8 << 10
)

// ---------------------------------------------------------------- build logs

// Builds stores and follows build logs.
type Builds struct {
	dir string

	mu   sync.Mutex
	live map[string]*buildLog
}

type buildLog struct {
	mu    sync.Mutex
	f     *os.File
	gz    *gzip.Writer
	w     *bufio.Writer
	lines []api.LogLine
	n     int
	done  bool
	subs  map[chan struct{}]struct{}
}

// NewBuilds stores build logs in dir (created when missing).
func NewBuilds(dir string) (*Builds, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create the build log directory %s: %w", dir, err)
	}
	return &Builds{dir: dir, live: map[string]*buildLog{}}, nil
}

func (b *Builds) path(dep string) string { return filepath.Join(b.dir, dep+".log.gz") }

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		ok := r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func (b *Builds) open(dep string) (*buildLog, error) {
	if !validID(dep) {
		return nil, errors.New("invalid deployment id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if l := b.live[dep]; l != nil {
		return l, nil
	}
	f, err := os.OpenFile(b.path(dep), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	l := &buildLog{f: f, gz: gz, w: bufio.NewWriter(gz), subs: map[chan struct{}]struct{}{}}
	b.live[dep] = l
	return l, nil
}

// Append adds lines to a deployment's build log (masking is the caller's job).
func (b *Builds) Append(dep string, ts int64, lines []string) error {
	l, err := b.open(dep)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return nil
	}
	for _, s := range lines {
		if len(s) > MaxLineBytes {
			s = s[:MaxLineBytes]
		}
		l.n++
		line := api.LogLine{ID: fmt.Sprintf("b:%s:%d", dep, l.n), TS: ts, Msg: s, Stream: "build", Level: buildLevel(s), DeploymentID: dep}
		l.lines = append(l.lines, line)
		rec, _ := json.Marshal(line)
		_, _ = l.w.Write(rec)
		_ = l.w.WriteByte('\n')
	}
	_ = l.w.Flush()
	_ = l.gz.Flush()
	for ch := range l.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

func buildLevel(s string) string {
	ls := strings.ToLower(s)
	if strings.HasPrefix(ls, "error") || strings.Contains(ls, " error:") || strings.HasPrefix(ls, "failed") {
		return "error"
	}
	if strings.HasPrefix(ls, "warn") {
		return "warn"
	}
	return "info"
}

// Finish closes a build log; followers stop after the last line.
func (b *Builds) Finish(dep string) {
	b.mu.Lock()
	l := b.live[dep]
	delete(b.live, dep)
	b.mu.Unlock()
	if l == nil {
		return
	}
	l.mu.Lock()
	l.done = true
	_ = l.w.Flush()
	_ = l.gz.Close()
	_ = l.f.Close()
	for ch := range l.subs {
		close(ch)
	}
	l.subs = nil
	l.mu.Unlock()
}

// Running reports whether a build log is still open.
func (b *Builds) Running(dep string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live[dep] != nil
}

// Read returns every stored line of a build log (none when there is no log).
func (b *Builds) Read(dep string) ([]api.LogLine, error) {
	b.mu.Lock()
	l := b.live[dep]
	b.mu.Unlock()
	if l != nil {
		l.mu.Lock()
		out := append([]api.LogLine(nil), l.lines...)
		l.mu.Unlock()
		return out, nil
	}
	if !validID(dep) {
		return nil, nil
	}
	f, err := os.Open(b.path(dep))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	gz.Multistream(true)
	var out []api.LogLine
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var line api.LogLine
		if json.Unmarshal(sc.Bytes(), &line) == nil {
			out = append(out, line)
		}
	}
	// A log cut short by a crash ends with an unexpected EOF: keep what was read.
	if err := sc.Err(); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return out, err
	}
	return out, nil
}

// Follow emits the stored lines of a build log, then new ones as they are
// appended, until the build finishes or ctx is done.
func (b *Builds) Follow(ctx context.Context, dep string, emit func([]api.LogLine) error) error {
	b.mu.Lock()
	l := b.live[dep]
	b.mu.Unlock()
	if l == nil {
		lines, err := b.Read(dep)
		if err != nil {
			return err
		}
		if len(lines) > 0 {
			return emit(lines)
		}
		return nil
	}
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	sent := len(l.lines)
	first := append([]api.LogLine(nil), l.lines...)
	done := l.done
	if !done {
		l.subs[ch] = struct{}{}
	}
	l.mu.Unlock()
	if len(first) > 0 {
		if err := emit(first); err != nil {
			return err
		}
	}
	if done {
		return nil
	}
	defer func() {
		l.mu.Lock()
		if l.subs != nil {
			delete(l.subs, ch)
		}
		l.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ch:
			l.mu.Lock()
			next := append([]api.LogLine(nil), l.lines[sent:]...)
			sent = len(l.lines)
			l.mu.Unlock()
			if len(next) > 0 {
				if err := emit(next); err != nil {
					return err
				}
			}
			if !ok {
				return nil
			}
		}
	}
}

// Delete removes a build log file (retention).
func (b *Builds) Delete(dep string) error {
	if !validID(dep) {
		return nil
	}
	err := os.Remove(b.path(dep))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------- conversions

// ToLine converts an agent record to the API shape.
func ToLine(r proto.LogRecord) api.LogLine {
	return api.LogLine{ID: r.ID, TS: r.TS, Msg: r.Msg, Stream: r.Stream, Level: r.Level, ServiceID: r.ServiceID,
		Replica: r.ReplicaID, DeploymentID: r.DeploymentID, Attrs: r.Attrs}
}

// toRecord converts back for filtering.
func toRecord(l api.LogLine) proto.LogRecord {
	return proto.LogRecord{ID: l.ID, TS: l.TS, ServiceID: l.ServiceID, DeploymentID: l.DeploymentID, ReplicaID: l.Replica,
		Stream: l.Stream, Level: l.Level, Msg: l.Msg, Attrs: l.Attrs}
}

// Match reports whether a line matches a parsed filter.
func Match(f *logfilter.Filter, l api.LogLine) bool {
	if f.Empty() {
		return true
	}
	r := toRecord(l)
	return f.MatchRecord(&r)
}

// sortLines orders lines by time, then id.
func sortLines(ls []api.LogLine) {
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].TS != ls[j].TS {
			return ls[i].TS < ls[j].TS
		}
		return ls[i].ID < ls[j].ID
	})
}

// Newest keeps the newest limit lines in time order; truncated is true when
// lines were cut.
func Newest(ls []api.LogLine, limit int) ([]api.LogLine, bool) {
	sortLines(ls)
	if limit <= 0 || len(ls) <= limit {
		return ls, false
	}
	return ls[len(ls)-limit:], true
}

// Around keeps limit lines centred on the line with id (View in Context).
func Around(ls []api.LogLine, id string, limit int) []api.LogLine {
	sortLines(ls)
	idx := -1
	for i, l := range ls {
		if l.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 || limit <= 0 || len(ls) <= limit {
		return ls
	}
	start := idx - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(ls) {
		end = len(ls)
		start = end - limit
	}
	return ls[start:end]
}

// ClampLimit applies the default and maximum limit.
func ClampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }

// DroppedLine is the marker a live tail inserts when its queue overflowed.
func DroppedLine(n int, ts time.Time) api.LogLine {
	return api.LogLine{ID: "dropped:" + itoa(int(ts.UnixNano())), TS: ts.UnixMilli(), Stream: "system", Level: "warn",
		Msg: itoa(n) + " lines dropped: the live tail could not keep up."}
}
