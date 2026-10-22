// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// A line as Caddy 2.11 writes it with the JSON encoder.
const accessLine = `{"level":"info","ts":1761100000.25,"logger":"http.log.access.fifty","msg":"handled request","request":{"remote_ip":"172.70.1.1","remote_port":"51234","client_ip":"198.51.100.20","proto":"HTTP/2.0","method":"POST","host":"API.example.com:443","uri":"/v1/orders?id=7","headers":{"User-Agent":["curl/8.5.0"],"X-Request-Id":["req-123"]},"tls":{"resumed":false,"version":772}},"bytes_read":42,"user_id":"","duration":0.0123456,"size":512,"status":502,"resp_headers":{"Content-Type":["text/plain"]}}`

func TestParseAccessLine(t *testing.T) {
	rec, ok := parseAccessLine([]byte(accessLine), func(h string) string {
		if h == "api.example.com" {
			return "svc_api"
		}
		return ""
	})
	if !ok {
		t.Fatal("line not parsed")
	}
	if rec.ServiceID != "svc_api" || rec.Stream != "http" || rec.Level != "error" || rec.TS != 1761100000250 {
		t.Errorf("record = %+v", rec)
	}
	if rec.Msg != "POST /v1/orders?id=7 502 12ms" {
		t.Errorf("msg = %q", rec.Msg)
	}
	want := map[string]any{
		"method": "POST", "path": "/v1/orders", "host": "api.example.com", "httpStatus": 502,
		"responseTime": 12.346, "durationMs": 12.346, "srcIp": "198.51.100.20", "requestId": "req-123",
		"userAgent": "curl/8.5.0", "bytes": int64(512), "proto": "HTTP/2.0",
	}
	for k, v := range want {
		if fmt.Sprint(rec.Attrs[k]) != fmt.Sprint(v) {
			t.Errorf("attr %s = %v, want %v", k, rec.Attrs[k], v)
		}
	}
	for _, c := range []struct {
		status int
		level  string
	}{{200, "info"}, {301, "info"}, {404, "warn"}, {0, "info"}} {
		line := strings.Replace(accessLine, `"status":502`, fmt.Sprintf(`"status":%d`, c.status), 1)
		r, _ := parseAccessLine([]byte(line), func(string) string { return "" })
		if r.Level != c.level {
			t.Errorf("status %d level %s, want %s", c.status, r.Level, c.level)
		}
	}
	for _, bad := range []string{"", "not json", `{"level":"info","msg":"server running"}`} {
		if _, ok := parseAccessLine([]byte(bad), func(string) string { return "" }); ok {
			t.Errorf("%q parsed as a request", bad)
		}
	}
	if formatMs(3.21) != "3.2ms" || formatMs(1234.5) != "1234ms" {
		t.Error("formatMs")
	}
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, ch <-chan string, n int) []string {
	t.Helper()
	var out []string
	timeout := time.After(3 * time.Second)
	for len(out) < n {
		select {
		case s := <-ch:
			out = append(out, s)
		case <-timeout:
			t.Fatalf("got %d lines, want %d: %v", len(out), n, out)
		}
	}
	return out
}

func TestTailerFollowsRotationAndResumes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	appendFile(t, path, "old-1\nold-2\n")
	tl := &tailer{path: path, posPath: filepath.Join(dir, "pos"), poll: 10 * time.Millisecond}

	run := func() (context.CancelFunc, chan string, chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan string, 100)
		done := make(chan error, 1)
		go func() {
			done <- tl.run(ctx, func(id string, line []byte) bool {
				ch <- id + "|" + string(line)
				return true
			})
		}()
		return cancel, ch, done
	}
	cancel, ch, done := run()
	time.Sleep(50 * time.Millisecond)
	// First start begins at the end: old lines are not replayed.
	appendFile(t, path, "a\npart")
	got := collect(t, ch, 1)
	if !strings.HasSuffix(got[0], "|a") || !strings.Contains(got[0], ":12|") {
		t.Errorf("first line = %q (offset 12 expected)", got[0])
	}
	appendFile(t, path, "ial\n")
	if got := collect(t, ch, 1); !strings.HasSuffix(got[0], "|partial") {
		t.Errorf("a line split across writes = %q", got[0])
	}
	// Rotation by rename, the way Caddy's roller does it.
	appendFile(t, path, "last-of-old\n")
	collect(t, ch, 1)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, "first-of-new\n")
	if got := collect(t, ch, 1); !strings.HasSuffix(got[0], "|first-of-new") || !strings.Contains(got[0], ":0|") {
		t.Errorf("after rotation = %q", got[0])
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Lines written while the agent was down are read after a restart.
	appendFile(t, path, "while-down\n")
	cancel, ch, done = run()
	if got := collect(t, ch, 1); !strings.HasSuffix(got[0], "|while-down") {
		t.Errorf("after restart = %q", got[0])
	}
	// Truncation starts over.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	appendFile(t, path, "after-truncate\n")
	if got := collect(t, ch, 1); !strings.HasSuffix(got[0], "|after-truncate") {
		t.Errorf("after truncate = %q", got[0])
	}
	cancel()
	<-done
}

func TestAccessLogsStreamAndAggregate(t *testing.T) {
	g, _, _, _ := newTestIngress(t)
	if err := g.Apply(context.Background(), testIngress(), nil); err != nil {
		t.Fatal(err)
	}
	if err := g.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan proto.LogRecord, 10)
	done := make(chan error, 1)
	go func() { done <- g.AccessLogs(ctx, out) }()
	time.Sleep(400 * time.Millisecond)
	ok := strings.Replace(accessLine, `"status":502`, `"status":200`, 1)
	console := strings.Replace(accessLine, "API.example.com:443", "console.example.com", 1)
	appendFile(t, g.params.accessLog(), ok+"\n"+accessLine+"\n"+console+"\n")
	var recs []proto.LogRecord
	timeout := time.After(3 * time.Second)
	for len(recs) < 2 {
		select {
		case r := <-out:
			recs = append(recs, r)
		case <-timeout:
			t.Fatalf("got %d records", len(recs))
		}
	}
	if recs[0].ServiceID != "svc_api" || recs[0].ID == "" || recs[0].ID == recs[1].ID {
		t.Errorf("records = %+v", recs)
	}
	time.Sleep(300 * time.Millisecond)
	if len(out) != 0 {
		t.Error("the console's requests were stored under a service")
	}
	s := g.HTTPSamples()
	if len(s) != 1 || s[0].ServiceID != "svc_api" || s[0].Req2xx != 1 || s[0].Req5xx != 1 || s[0].P50Ms != 12.35 {
		t.Errorf("samples = %+v", s)
	}
	if s := g.HTTPSamples(); len(s) != 0 {
		t.Error("a flushed window was reported again")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
