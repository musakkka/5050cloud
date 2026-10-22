// SPDX-License-Identifier: AGPL-3.0-only

package logstore

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Usage reports what the janitor found and did.
type Usage struct {
	TotalBytes int64
	// TopService uses the most bytes; TopBytes is how many.
	TopService string
	TopBytes   int64
	Deleted    int // files deleted this round
	Gzipped    int
}

// Janitor compresses files older than GzipAfter, then deletes the oldest
// hourly files until the store is within the retention days, the per-service
// cap and the share of the filesystem (docs/SPEC.md 9.19).
func (s *Store) Janitor(b Budgets) (Usage, error) {
	b = b.Defaults()
	if err := s.Flush(); err != nil {
		return Usage{}, err
	}
	now := s.now().UTC()
	var u Usage
	type svcFiles struct {
		svc   string
		files []fileRef
		bytes int64
	}
	var all []*svcFiles
	s.mu.Lock()
	pairs := make([][2]string, 0, len(s.svcEnv))
	for svc, env := range s.svcEnv {
		pairs = append(pairs, [2]string{env, svc})
	}
	openSet := map[string]bool{}
	for rel := range s.open {
		openSet[filepath.Join(s.root, rel+".jsonl")] = true
	}
	s.mu.Unlock()
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][1] < pairs[j][1] })

	var firstErr error
	for _, p := range pairs {
		sf := &svcFiles{svc: p[1]}
		for _, fr := range s.filesOf(p[0], p[1]) {
			// 1. retention by age
			if now.Sub(fr.hour) > time.Duration(b.RetentionDays)*24*time.Hour+time.Hour {
				if err := os.Remove(fr.path); err == nil {
					u.Deleted++
				}
				continue
			}
			// 2. gzip past hours
			if !fr.gz && now.Sub(fr.hour.Add(time.Hour)) >= b.GzipAfter && !openSet[fr.path] {
				nfr, err := gzipFile(fr)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
				} else {
					fr = nfr
					u.Gzipped++
				}
			}
			sf.files = append(sf.files, fr)
			sf.bytes += fr.size
		}
		all = append(all, sf)
	}
	// 3. per-service cap, oldest first, never the newest file.
	for _, sf := range all {
		for sf.bytes > b.MaxBytesPerSvc && len(sf.files) > 1 {
			fr := sf.files[0]
			if err := os.Remove(fr.path); err != nil {
				break
			}
			u.Deleted++
			sf.bytes -= fr.size
			sf.files = sf.files[1:]
		}
	}
	// 4. share of the filesystem, oldest first across services.
	for _, sf := range all {
		u.TotalBytes += sf.bytes
	}
	if b.FilesystemBytes > 0 {
		capBytes := b.FilesystemBytes * int64(b.MaxPctFS) / 100
		for u.TotalBytes > capBytes {
			var oldest *svcFiles
			for _, sf := range all {
				if len(sf.files) > 1 && (oldest == nil || sf.files[0].hour.Before(oldest.files[0].hour)) {
					oldest = sf
				}
			}
			if oldest == nil {
				break
			}
			fr := oldest.files[0]
			if err := os.Remove(fr.path); err != nil {
				break
			}
			u.Deleted++
			oldest.bytes -= fr.size
			u.TotalBytes -= fr.size
			oldest.files = oldest.files[1:]
		}
	}
	for _, sf := range all {
		if sf.bytes > u.TopBytes {
			u.TopBytes, u.TopService = sf.bytes, sf.svc
		}
	}
	return u, firstErr
}

func gzipFile(fr fileRef) (fileRef, error) {
	dst := strings.TrimSuffix(fr.path, ".jsonl") + ".jsonl.gz"
	in, err := os.Open(fr.path)
	if err != nil {
		return fr, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(fr.path), ".gz-*")
	if err != nil {
		return fr, fmt.Errorf("could not compress %s: %w", fr.path, err)
	}
	gz := gzip.NewWriter(tmp)
	_, err = io.Copy(gz, in)
	if cerr := gz.Close(); err == nil {
		err = cerr
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fr, fmt.Errorf("could not compress %s: %w", fr.path, err)
	}
	_ = os.Remove(fr.path)
	st, err := os.Stat(dst)
	if err != nil {
		return fr, err
	}
	return fileRef{env: fr.env, svc: fr.svc, hour: fr.hour, path: dst, gz: true, size: st.Size()}, nil
}

// ---------------------------------------------------------------- tail

// MaxTailBatch caps one host_logs_tail message (9.4).
const MaxTailBatch = 200

// Tail buffers recent runtime lines for host_logs_tail: every error-level
// line and every deploy-log line since the last batch, at most 200 per
// batch, error lines first.
type Tail struct {
	mu     sync.Mutex
	errs   []proto.LogRecord
	others []proto.LogRecord
	max    int
}

// NewTail keeps at most max lines of each kind between batches.
func NewTail(max int) *Tail { return &Tail{max: max} }

// Add buffers a line.
func (t *Tail) Add(r proto.LogRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r.Level == LevelError {
		t.errs = appendCapped(t.errs, r, t.max)
	} else {
		t.others = appendCapped(t.others, r, t.max)
	}
}

func appendCapped(s []proto.LogRecord, r proto.LogRecord, max int) []proto.LogRecord {
	s = append(s, r)
	if len(s) > max {
		s = append(s[:0], s[len(s)-max:]...)
	}
	return s
}

// Take returns the next batch (newest lines, error lines first, oldest
// first within the batch) and clears the buffer.
func (t *Tail) Take() []proto.LogRecord {
	t.mu.Lock()
	errs, others := t.errs, t.others
	t.errs, t.others = nil, nil
	t.mu.Unlock()
	if len(errs) > MaxTailBatch {
		errs = errs[len(errs)-MaxTailBatch:]
	}
	room := MaxTailBatch - len(errs)
	if len(others) > room {
		others = others[len(others)-room:]
	}
	out := append(append([]proto.LogRecord(nil), errs...), others...)
	sortRecords(out)
	return out
}
