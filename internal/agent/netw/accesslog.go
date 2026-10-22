// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// caddyAccess is one line of Caddy's JSON access log.
type caddyAccess struct {
	Level   string  `json:"level"`
	TS      float64 `json:"ts"`
	Logger  string  `json:"logger"`
	Request struct {
		RemoteIP string              `json:"remote_ip"`
		ClientIP string              `json:"client_ip"`
		Proto    string              `json:"proto"`
		Method   string              `json:"method"`
		Host     string              `json:"host"`
		URI      string              `json:"uri"`
		Headers  map[string][]string `json:"headers"`
	} `json:"request"`
	BytesRead   int64               `json:"bytes_read"`
	Duration    float64             `json:"duration"` // seconds
	Size        int64               `json:"size"`
	Status      int                 `json:"status"`
	RespHeaders map[string][]string `json:"resp_headers"`
}

func firstHeader(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// parseAccessLine turns a Caddy access log line into a LogRecord of stream
// "http" with the attributes of the log filter (docs/SPEC.md 9.4). The
// service comes from the request's host.
func parseAccessLine(line []byte, serviceFor func(host string) string) (proto.LogRecord, bool) {
	var a caddyAccess
	if err := json.Unmarshal(line, &a); err != nil || a.Request.Method == "" {
		return proto.LogRecord{}, false
	}
	host := strings.ToLower(a.Request.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	path, _, _ := strings.Cut(a.Request.URI, "?")
	ms := math.Round(a.Duration*1e6) / 1e3
	status := a.Status
	if status == 0 {
		status = 200 // Caddy logs 0 when a handler wrote nothing; the client saw 200
	}
	level := "info"
	switch {
	case status >= 500:
		level = "error"
	case status >= 400:
		level = "warn"
	}
	src := a.Request.ClientIP
	if src == "" {
		src = a.Request.RemoteIP
	}
	attrs := map[string]any{
		"method":       a.Request.Method,
		"path":         path,
		"host":         host,
		"httpStatus":   status,
		"responseTime": ms,
		"durationMs":   ms,
		"srcIp":        src,
		"bytes":        a.Size,
		"proto":        a.Request.Proto,
	}
	if ua := firstHeader(a.Request.Headers, "User-Agent"); ua != "" {
		attrs["userAgent"] = ua
	}
	rid := firstHeader(a.Request.Headers, "X-Request-Id")
	if rid == "" {
		rid = firstHeader(a.RespHeaders, "X-Request-Id")
	}
	if rid != "" {
		attrs["requestId"] = rid
	}
	sec, frac := math.Modf(a.TS)
	ts := time.Unix(int64(sec), int64(frac*1e9)).UnixMilli()
	return proto.LogRecord{
		TS:        ts,
		ServiceID: serviceFor(host),
		Stream:    "http",
		Level:     level,
		Msg:       fmt.Sprintf("%s %s %d %s", a.Request.Method, a.Request.URI, status, formatMs(ms)),
		Attrs:     attrs,
	}, true
}

func formatMs(ms float64) string {
	if ms < 10 {
		return fmt.Sprintf("%.1fms", ms)
	}
	return fmt.Sprintf("%.0fms", ms)
}

// AccessLogs follows Caddy's access log and streams each request as a
// LogRecord of the service its domain belongs to. It also feeds the
// HTTPSamples aggregator. Its read position survives agent restarts; on the
// very first start it begins at the end of the file. It returns when ctx ends.
func (g *Ingress) AccessLogs(ctx context.Context, out chan<- proto.LogRecord) error {
	if err := g.ensureDirs(); err != nil {
		return err
	}
	t := &tailer{path: g.params.accessLog(), posPath: filepath.Join(g.params.Dir, "access.pos"), poll: 250 * time.Millisecond}
	return t.run(ctx, func(id string, line []byte) bool {
		rec, ok := parseAccessLine(line, g.serviceFor)
		if !ok {
			return true
		}
		g.stats.Add(rec.ServiceID, int(toFloat(rec.Attrs["httpStatus"])), toFloat(rec.Attrs["durationMs"]))
		if rec.ServiceID == "" {
			return true // the console or an unknown host
		}
		rec.ID = id
		select {
		case out <- rec:
			return true
		case <-ctx.Done():
			return false
		}
	})
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

// ---------------------------------------------------------------- tailer

// tailer follows a file that is rotated by renaming (Caddy's roller): it
// finishes the old file, then continues with the new one from its start.
type tailer struct {
	path    string
	posPath string
	poll    time.Duration
}

type tailPos struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

const maxLogLine = 1 << 20

func (t *tailer) loadPos() (tailPos, bool) {
	b, err := os.ReadFile(t.posPath)
	if err != nil {
		return tailPos{}, false
	}
	var p tailPos
	if json.Unmarshal(b, &p) != nil {
		return tailPos{}, false
	}
	return p, true
}

func (t *tailer) savePos(p tailPos) {
	b, _ := json.Marshal(p)
	_ = writeIfChanged(t.posPath, b, 0o600)
}

// run calls emit with an id ("<file id>:<offset>") and each complete line
// until ctx ends or emit returns false.
func (t *tailer) run(ctx context.Context, emit func(id string, line []byte) bool) error {
	pos, havePos := t.loadPos()
	var (
		f       *os.File
		fid     string
		off     int64 // offset of the next unread byte
		pending []byte
		rotated bool
	)
	defer func() {
		if f != nil {
			f.Close()
			t.savePos(tailPos{File: fid, Offset: off - int64(len(pending))})
		}
	}()
	lastSave := time.Now()
	buf := make([]byte, 64<<10)
	for {
		if f == nil {
			nf, err := os.Open(t.path)
			if err == nil {
				fi, serr := nf.Stat()
				if serr != nil {
					nf.Close()
				} else {
					f, fid = nf, fileID(fi)
					switch {
					case rotated:
						off = 0
					case havePos && pos.File == fid && pos.Offset <= fi.Size():
						off = pos.Offset
					case havePos:
						off = 0 // a new file since the last run
					default:
						off = fi.Size()
					}
					havePos, rotated, pending = false, false, nil
					if _, err := f.Seek(off, io.SeekStart); err != nil {
						f.Close()
						f = nil
					}
				}
			} else if errors.Is(err, os.ErrNotExist) {
				rotated = true // a file that appears later is read from its start
			} else {
				return fmt.Errorf("opening the access log %s failed: %w", t.path, err)
			}
		}
		if f != nil {
			for {
				n, err := f.Read(buf)
				if n > 0 {
					pending = append(pending, buf[:n]...)
					off += int64(n)
					start := off - int64(len(pending))
					for {
						i := bytes.IndexByte(pending, '\n')
						if i < 0 {
							break
						}
						line := bytes.TrimSpace(pending[:i])
						if len(line) > 0 && !emit(fmt.Sprintf("caddy-access-%s:%d", fid, start), line) {
							pending = pending[i+1:]
							return nil
						}
						start += int64(i + 1)
						pending = pending[i+1:]
					}
					if len(pending) > maxLogLine {
						pending = nil // a runaway line without a newline
					}
				}
				if err != nil || n == 0 {
					break
				}
			}
			// At the end: was the file rotated or truncated?
			if fi, err := os.Stat(t.path); err == nil {
				cur, _ := f.Stat()
				if cur != nil && !os.SameFile(cur, fi) {
					f.Close()
					f, rotated, pending = nil, true, nil
					continue
				}
				if fi.Size() < off {
					_, _ = f.Seek(0, io.SeekStart)
					off, pending = 0, nil
				}
			}
			if time.Since(lastSave) > 5*time.Second {
				t.savePos(tailPos{File: fid, Offset: off - int64(len(pending))})
				lastSave = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(t.poll):
		}
	}
}
