// SPDX-License-Identifier: AGPL-3.0-only

package logstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func TestDetectLevel(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{`{"level":"error","msg":"boom"}`, "error"},
		{`{"level":"WARNING","msg":"x"}`, "warn"},
		{`{"severity":"DEBUG","message":"x"}`, "debug"},
		{`{"level":50,"msg":"pino error"}`, "error"},
		{`{"level":40,"msg":"pino warn"}`, "warn"},
		{`{"level":30,"msg":"pino info with error word"}`, "info"},
		{`time=2026 level=warn msg="disk"`, "warn"},
		{`level="error" msg=x`, "error"},
		{`[ERROR] could not connect`, "error"},
		{`ERROR: could not connect`, "error"},
		{`[warn] retrying`, "warn"},
		{`2026-10-22T10:00:00Z INFO starting`, "info"},
		{`2026-10-22 10:00:00,123 ERROR db down`, "error"},
		{`<debug> details`, "debug"},
		{`Traceback (most recent call last):`, "error"},
		{`Unhandled exception in handler`, "error"},
		{`panic: runtime error: index out of range`, "error"},
		{`server listening on :3000`, "info"},
		{`0 errors found`, "info"},
		{`information about errorless runs`, "info"},
		{`{"msg":"no level here"}`, "info"},
	}
	for _, c := range cases {
		attrs, _, _ := ParseJSON(c.line)
		if got := DetectLevel(c.line, attrs); got != c.want {
			t.Errorf("DetectLevel(%q) = %q, want %q", c.line, got, c.want)
		}
	}
	if NormalizeLevel("bogus") != "" || NormalizeLevel("60") != "error" || NormalizeLevel("20") != "debug" || NormalizeLevel("5") != "" {
		t.Error("NormalizeLevel")
	}
}

func TestParseJSON(t *testing.T) {
	attrs, msg, ok := ParseJSON(`{"msg":"hi","n":12345678901234567890,"f":1.5,"a":[1,{"b":2}]}`)
	if !ok || msg != "hi" {
		t.Fatal(ok, msg)
	}
	if attrs["f"] != 1.5 {
		t.Fatalf("number: %#v", attrs["f"])
	}
	if a := attrs["a"].([]any); a[0] != float64(1) || a[1].(map[string]any)["b"] != float64(2) {
		t.Fatalf("nested: %#v", attrs["a"])
	}
	if _, msg, ok := ParseJSON("plain line"); ok || msg != "plain line" {
		t.Fatal("plain")
	}
	if _, _, ok := ParseJSON("{broken"); ok {
		t.Fatal("broken")
	}
	if _, msg, ok := ParseJSON(`{"x":1}`); !ok || msg != `{"x":1}` {
		t.Fatal("no msg field keeps the line")
	}
}

func TestMasking(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":  "postgres://app:hunter2pass@db.internal:5432/app",
		"STRIPE_KEY":    "sk_live_abcdef",
		"SHORT_TOKEN":   "abc",
		"API_SECRET":    "s3cr3t-value",
		"PUBLIC_NAME":   "visible-value",
		"REDIS_URL":     "redis://default:p%40ssword99@redis:6379",
		"MY_PASSWORD_2": "s3cr3t-value-longer",
	}
	m := NewMasker(env, "sealed-thing")
	line := "connect postgres://app:hunter2pass@db sk_live_abcdef abc s3cr3t-value-longer visible-value p%40ssword99 p@ssword99 sealed-thing"
	got := m.Apply(line)
	for _, secret := range []string{"hunter2pass", "sk_live_abcdef", "s3cr3t-value", "p@ssword99", "p%40ssword99", "sealed-thing"} {
		if strings.Contains(got, secret) {
			t.Fatalf("%q still in %q", secret, got)
		}
	}
	if !strings.Contains(got, "visible-value") || !strings.Contains(got, " abc ") {
		t.Fatalf("non-secrets must stay: %q", got)
	}
	if strings.Contains(got, "•••-longer") {
		t.Fatalf("a longer secret must be masked whole: %q", got)
	}
	var nilM *Masker
	if nilM.Apply("x") != "x" || NewMasker(nil).Apply("y") != "y" {
		t.Fatal("empty maskers pass lines through")
	}
}

func TestProcessor(t *testing.T) {
	p := NewProcessor(Source{ServiceID: "svc_1", DeploymentID: "dep_1", ReplicaID: "rep_1"}, NewMasker(map[string]string{"TOKEN": "tok-123456"}))
	base := time.Unix(1700000000, 0)
	recs := p.Line("stderr", base, []byte("\x1b[31m[ERROR]\x1b[0m bad tok-123456\r"))
	if len(recs) != 1 {
		t.Fatal(len(recs))
	}
	r := recs[0]
	if r.Msg != "[ERROR] bad •••" || r.Level != "error" || r.Stream != "stderr" || r.ReplicaID != "rep_1" || r.TS != base.UnixMilli() {
		t.Fatalf("%+v", r)
	}
	if p.LastStderr() != "[ERROR] bad •••" {
		t.Fatal(p.LastStderr())
	}
	long := strings.Repeat("é", MaxLineBytes)
	r = p.Line("stdout", base, []byte(long))[0]
	if len(r.Msg) > MaxLineBytes || !strings.HasPrefix(long, r.Msg) {
		t.Fatalf("cap: %d", len(r.Msg))
	}
	j := p.Line("stdout", base, []byte(`{"level":"warn","msg":"hello","user":{"id":7}}`))[0]
	if j.Msg != "hello" || j.Level != "warn" || j.Attrs["user"].(map[string]any)["id"] != float64(7) {
		t.Fatalf("%+v", j)
	}
	// Rate limit: 500 lines a second pass, the rest are dropped with one warning.
	p2 := NewProcessor(Source{ReplicaID: "rep_2"}, nil)
	kept := 0
	for i := 0; i < 700; i++ {
		kept += len(p2.Line("stdout", base.Add(time.Duration(i)*time.Microsecond), []byte("x")))
	}
	if kept != MaxLinesPerSecond {
		t.Fatalf("kept %d", kept)
	}
	next := p2.Line("stdout", base.Add(time.Second), []byte("after"))
	if len(next) != 2 || next[0].Msg != "200 log lines dropped: more than 500 lines per second" || next[0].Level != "warn" || next[1].Msg != "after" {
		t.Fatalf("%+v", next)
	}
	for i := 0; i < 600; i++ {
		p2.Line("stdout", base.Add(2*time.Second), []byte("x"))
	}
	if f := p2.Flush(base.Add(2 * time.Second)); f != nil {
		t.Fatal("same second: nothing to flush yet")
	}
	f := p2.Flush(base.Add(3 * time.Second))
	if len(f) != 1 || !strings.HasPrefix(f[0].Msg, "100 log lines dropped") {
		t.Fatalf("%+v", f)
	}
	if s := p2.System("info", "Pulling image"); s.Stream != "system" || s.ReplicaID != "rep_2" {
		t.Fatalf("%+v", s)
	}
	if got := p2.Line("stdout", time.Time{}, []byte("now")); len(got) != 1 || got[0].TS == 0 {
		t.Fatal("zero time uses the clock")
	}
}

func newStore(t *testing.T, now time.Time) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := now
	s.SetClock(func() time.Time { return clock })
	t.Cleanup(func() { _ = s.Close() })
	return s, &clock
}

func mk(svc string, ts time.Time, level, msg string) proto.LogRecord {
	return proto.LogRecord{TS: ts.UnixMilli(), ServiceID: svc, DeploymentID: "dep_" + svc, ReplicaID: "rep_" + svc, Stream: "stdout", Level: level, Msg: msg}
}

func TestStoreQuery(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC)
	s, _ := newStore(t, now)
	// Three hours of lines for two services.
	for h := 0; h < 3; h++ {
		for i := 0; i < 10; i++ {
			ts := now.Add(-time.Duration(2-h)*time.Hour + time.Duration(i)*time.Second)
			lvl := "info"
			if i%5 == 0 {
				lvl = "error"
			}
			if err := s.Append("env_1", mk("svc_a", ts, lvl, fmt.Sprintf("a h%d i%d", h, i)), mk("svc_b", ts, "info", fmt.Sprintf("b h%d i%d", h, i))); err != nil {
				t.Fatal(err)
			}
		}
	}
	httpRec := mk("svc_a", now, "info", "GET / 200")
	httpRec.Stream = "http"
	httpRec.Attrs = map[string]any{"httpStatus": float64(200)}
	if err := s.Append("env_1", httpRec); err != nil {
		t.Fatal(err)
	}

	res, err := s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 5 || !res.Truncated || res.Lines[4].Msg != "a h2 i9" || res.Lines[0].Msg != "a h2 i5" {
		t.Fatalf("newest 5 oldest first: %+v", msgs(res.Lines))
	}
	for i := 1; i < len(res.Lines); i++ {
		if res.Lines[i].TS < res.Lines[i-1].TS {
			t.Fatal("not sorted")
		}
	}
	res, _ = s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Filter: "@level:error", Limit: 100})
	if len(res.Lines) != 6 || res.Truncated {
		t.Fatalf("errors: %v", msgs(res.Lines))
	}
	res, _ = s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a", "svc_b"}, Filter: `"h1 i3"`, Limit: 100})
	if len(res.Lines) != 2 {
		t.Fatalf("both services: %v", msgs(res.Lines))
	}
	res, _ = s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Since: now.Add(-time.Hour).UnixMilli(), Until: now.Add(-time.Hour + 4*time.Second).UnixMilli(), Limit: 100})
	if len(res.Lines) != 5 || res.Lines[0].Msg != "a h1 i0" {
		t.Fatalf("window: %v", msgs(res.Lines))
	}
	res, _ = s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Streams: []string{"http"}, Limit: 100})
	if len(res.Lines) != 1 || res.Lines[0].Attrs["httpStatus"] != float64(200) {
		t.Fatalf("http: %v", msgs(res.Lines))
	}
	res, _ = s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, DeploymentID: "dep_nope", Limit: 100})
	if len(res.Lines) != 0 {
		t.Fatal("deployment filter")
	}
	res, _ = s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_missing"}})
	if len(res.Lines) != 0 {
		t.Fatal("unknown service")
	}
	if _, err := s.Query(proto.LogsQuery{Filter: `"open`}); err == nil {
		t.Fatal("bad filter must fail")
	}
	res, _ = s.Query(proto.LogsQuery{Limit: 100000})
	if len(res.Lines) != 60 {
		t.Fatalf("all runtime lines: %d", len(res.Lines))
	}

	// View in context.
	all, _ := s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Limit: 1000})
	target := all.Lines[15]
	ctx, err := s.Query(proto.LogsQuery{Around: target.ID, Limit: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Lines) != 6 || ctx.Lines[3].ID != target.ID {
		t.Fatalf("around %s: %v", target.Msg, msgs(ctx.Lines))
	}
	if _, err := s.Query(proto.LogsQuery{Around: "env_1/svc_a/2026102212:999999"}); err == nil {
		t.Fatal("unknown id")
	}
	for _, bad := range []string{"nocolon", "a/b:1", "a/b/notanhour:1"} {
		if _, err := s.Query(proto.LogsQuery{Around: bad}); err == nil {
			t.Fatalf("bad id %q", bad)
		}
	}

	// A reopened store finds the services on disk.
	_ = s.Close()
	s2, err := Open(s.root)
	if err != nil {
		t.Fatal(err)
	}
	s2.SetClock(func() time.Time { return now })
	res, _ = s2.Query(proto.LogsQuery{ServiceIDs: []string{"svc_b"}, Limit: 1000})
	if len(res.Lines) != 30 {
		t.Fatalf("reopen: %d", len(res.Lines))
	}
	if err := s.Append("env_1", mk("svc_a", now, "info", "x")); err == nil {
		t.Fatal("append after close must fail")
	}
}

func msgs(recs []proto.LogRecord) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Msg)
	}
	return out
}

func TestSubscribe(t *testing.T) {
	now := time.Now()
	s, _ := newStore(t, now)
	sub, err := s.Subscribe("s1", proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Filter: "@level:error", Since: 1, Limit: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Append("env", mk("svc_a", now, "error", "e1"), mk("svc_a", now, "info", "i1"), mk("svc_b", now, "error", "other"),
		mk("svc_a", now, "error", "e2"), mk("svc_a", now, "error", "e3"))
	if r := <-sub.C; r.Msg != "e1" {
		t.Fatal(r.Msg)
	}
	if r := <-sub.C; r.Msg != "e2" {
		t.Fatal(r.Msg)
	}
	if sub.TakeDropped() != 1 || sub.TakeDropped() != 0 {
		t.Fatal("one line should be counted as dropped")
	}
	if _, err := s.Subscribe("bad", proto.LogsQuery{Filter: "("}, 0); err == nil {
		t.Fatal("bad filter")
	}
	sub2, _ := s.Subscribe("s1", proto.LogsQuery{}, 0)
	if _, ok := <-sub.C; ok {
		t.Fatal("replaced subscription must be closed")
	}
	s.Unsubscribe("s1")
	if _, ok := <-sub2.C; ok {
		t.Fatal("unsubscribed channel must be closed")
	}
	s.Unsubscribe("nope")
	sub3, _ := s.Subscribe("s3", proto.LogsQuery{}, 0)
	s.UnsubscribeAll()
	if _, ok := <-sub3.C; ok {
		t.Fatal("closed")
	}
}

func TestJanitor(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC)
	s, clock := newStore(t, now)
	// 10 days of hourly files for svc_a, a few hours for svc_b.
	for h := 240; h >= 0; h -= 6 {
		*clock = now.Add(-time.Duration(h) * time.Hour)
		ts := *clock
		if err := s.Append("env_1", mk("svc_a", ts, "info", strings.Repeat("a", 1000))); err != nil {
			t.Fatal(err)
		}
		if h <= 12 {
			_ = s.Append("env_1", mk("svc_b", ts, "info", strings.Repeat("b", 100)))
		}
		_ = s.Flush()
	}
	*clock = now
	// Close idle past-hour files so they can be compressed.
	*clock = now.Add(2 * time.Minute)
	_ = s.Flush()
	*clock = now
	u, err := s.Janitor(Budgets{RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if u.Deleted == 0 || u.Gzipped == 0 {
		t.Fatalf("expected deletes and compression: %+v", u)
	}
	files := s.filesOf("env_1", "svc_a")
	for _, fr := range files {
		if now.Sub(fr.hour) > 7*24*time.Hour+time.Hour {
			t.Fatalf("file older than retention kept: %v", fr.hour)
		}
		if now.Sub(fr.hour.Add(time.Hour)) >= 2*time.Hour && !fr.gz {
			t.Fatalf("old file not compressed: %v", fr.path)
		}
	}
	if u.TopService != "svc_a" {
		t.Fatalf("top service %q", u.TopService)
	}
	// Compressed files are still queryable.
	res, err := s.Query(proto.LogsQuery{ServiceIDs: []string{"svc_a"}, Limit: 1000})
	if err != nil || len(res.Lines) != len(files) {
		t.Fatalf("query after gzip: %d lines, %d files, %v", len(res.Lines), len(files), err)
	}
	// Per-service byte cap: keep deleting oldest until under 3 KB (never the newest file).
	u, _ = s.Janitor(Budgets{MaxBytesPerSvc: 3000})
	if left := s.filesOf("env_1", "svc_a"); len(left) == 0 || sumSize(left) > 3000 && len(left) > 1 {
		t.Fatalf("per-service cap: %d files, %d bytes", len(left), sumSize(left))
	}
	// Filesystem share: 10% of 2 KB = 200 bytes leaves one file per service.
	_, _ = s.Janitor(Budgets{FilesystemBytes: 2000})
	if left := s.filesOf("env_1", "svc_a"); len(left) != 1 {
		t.Fatalf("fs cap left %d files", len(left))
	}
	if left := s.filesOf("env_1", "svc_b"); len(left) != 1 {
		t.Fatalf("fs cap left %d svc_b files", len(left))
	}
	// Stray files are ignored.
	_ = os.WriteFile(filepath.Join(s.root, "env_1", "svc_a", "README"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(s.root, "env_1", "svc_a", "notanhour.jsonl"), []byte("x"), 0o600)
	if _, err := s.Janitor(Budgets{}); err != nil {
		t.Fatal(err)
	}
}

func sumSize(fs []fileRef) int64 {
	var n int64
	for _, f := range fs {
		n += f.size
	}
	return n
}

func TestTail(t *testing.T) {
	tl := NewTail(1000)
	base := time.Unix(1700000000, 0)
	for i := 0; i < 300; i++ {
		tl.Add(mk("svc", base.Add(time.Duration(i)*time.Millisecond), "info", fmt.Sprint("i", i)))
	}
	for i := 0; i < 50; i++ {
		tl.Add(mk("svc", base.Add(time.Duration(i)*time.Millisecond), "error", fmt.Sprint("e", i)))
	}
	b := tl.Take()
	if len(b) != MaxTailBatch {
		t.Fatal(len(b))
	}
	errs := 0
	for _, r := range b {
		if r.Level == "error" {
			errs++
		}
	}
	if errs != 50 {
		t.Fatalf("every error line must be in the batch, got %d", errs)
	}
	if len(tl.Take()) != 0 {
		t.Fatal("take clears")
	}
	small := NewTail(2)
	for i := 0; i < 5; i++ {
		small.Add(mk("s", base, "error", fmt.Sprint(i)))
	}
	if got := small.Take(); len(got) != 2 || got[1].Msg != "4" {
		t.Fatalf("%v", msgs(got))
	}
	big := NewTail(1000)
	for i := 0; i < 300; i++ {
		big.Add(mk("s", base, "error", fmt.Sprint(i)))
	}
	if len(big.Take()) != MaxTailBatch {
		t.Fatal("errors are capped too")
	}
	s, _ := newStore(t, base)
	_ = s.Append("e", mk("svc", base, "error", "x"))
	if len(s.Tail().Take()) != 1 {
		t.Fatal("store feeds the tail")
	}
}

func TestStripAndCap(t *testing.T) {
	if string(StripANSI([]byte("\x1b]0;title\x07plain \x1b[1;32mgreen\x1b[0m"))) != "plain green" {
		t.Fatal(string(StripANSI([]byte("\x1b]0;title\x07plain \x1b[1;32mgreen\x1b[0m"))))
	}
	if CapLine("short") != "short" {
		t.Fatal("cap")
	}
	if safeName("") != "_" || safeName("a/b..c") != "a_b__c" {
		t.Fatal(safeName("a/b..c"))
	}
}
