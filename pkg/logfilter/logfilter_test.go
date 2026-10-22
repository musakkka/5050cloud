// SPDX-License-Identifier: MIT

package logfilter

import (
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

func rec(msg string, attrs map[string]any) *proto.LogRecord {
	return &proto.LogRecord{
		ID: "f:1", TS: 1, ServiceID: "svc_a", DeploymentID: "dep_1", ReplicaID: "rep_1",
		Stream: "stdout", Level: "info", Msg: msg, Attrs: attrs,
	}
}

func TestMatch(t *testing.T) {
	errRec := rec("connection refused by upstream", nil)
	errRec.Level = "error"
	http := rec("GET /api/users 503", map[string]any{
		"method": "GET", "path": "/api/users", "httpStatus": float64(503), "durationMs": float64(812),
		"host": "app.example.com", "srcIp": "198.51.100.7",
	})
	http.Stream = "http"
	nested := rec("job done", map[string]any{
		"retries": float64(2),
		"user":    map[string]any{"id": "u_42", "roles": []any{"admin", "dev"}},
		"items":   []any{map[string]any{"sku": "A1"}, map[string]any{"sku": "B2"}},
		"ok":      true,
		"count":   "17",
	})
	hc := rec("GET /healthcheck 200", nil)
	hc.Level = "error"

	cases := []struct {
		filter string
		r      *proto.LogRecord
		want   bool
	}{
		{"", errRec, true},
		{"   ", errRec, true},
		{"refused", errRec, true},
		{"REFUSED", errRec, true},
		{"timeout", errRec, false},
		{`"connection refused"`, errRec, true},
		{`"refused connection"`, errRec, false},
		{`"connection refused" OR timeout`, errRec, true},
		{`timeout OR "connection refused"`, errRec, true},
		{`timeout or nope`, errRec, false},
		{"@level:error", errRec, true},
		{"@level:ERROR", errRec, true},
		{"@level:info", errRec, false},
		{"@level:error -healthcheck", errRec, true},
		{"@level:error -healthcheck", hc, false},
		{"@level:error AND -healthcheck", hc, false},
		{"connection upstream", errRec, true},
		{"connection AND nothere", errRec, false},
		{"connection nothere OR upstream", errRec, true},
		{"connection (nothere OR upstream)", errRec, true},
		{"-(nothere OR upstream)", errRec, false},
		{"-(nothere OR nope)", errRec, true},
		{"@httpStatus:>=500 @path:/api", http, true},
		{"@httpStatus:>=500 @path:/ap", http, false},
		{"@path:/api/users", http, true},
		{"@path:/api/", http, true},
		{"@path:/api/*", http, true},
		{"@httpStatus:>503", http, false},
		{"@httpStatus:<504", http, true},
		{"@httpStatus:<=503", http, true},
		{"@httpStatus:503", http, true},
		{"@httpStatus:500..599", http, true},
		{"@httpStatus:200..299", http, false},
		{"@responseTime:>800", http, true},
		{"@durationMs:>800", http, true},
		{"@method:get", http, true},
		{"@host:*.example.com", http, true},
		{"@srcIp:198.51.100.7", http, true},
		{"@stream:http", http, true},
		{"@service:svc_a", http, true},
		{"@deployment:dep_1", http, true},
		{"replica:rep_1", http, true},
		{"replica:rep_2", http, false},
		{"@replica:rep_1", http, true},
		{"@retries:1..3", nested, true},
		{"@retries:3..5", nested, false},
		{"@user.id:u_42", nested, true},
		{"@user.roles[1]:dev", nested, true},
		{"@user.roles[5]:dev", nested, false},
		{"@user.roles:admin", nested, true},
		{"@items[1].sku:b2", nested, true},
		{"@items[0].sku:b2", nested, false},
		{"@ok:true", nested, true},
		{"@count:>10", nested, true},
		{"@count:17", nested, true},
		{"@missing:x", nested, false},
		{"-@missing:x", nested, true},
		{`@user.id:"u_42"`, nested, true},
		{`@msg:"job done"`, nested, true},
		{`"job \"done"`, rec(`job "done" now`, nil), true},
		{"a-b", rec("x a-b y", nil), true},
		{"@level:error OR @level:warn", errRec, true},
		{"(@level:warn OR @level:error) refused", errRec, true},
	}
	for _, c := range cases {
		f, err := Parse(c.filter)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.filter, err)
		}
		if got := f.MatchRecord(c.r); got != c.want {
			t.Errorf("filter %q on %q: got %v, want %v (parsed as %s)", c.filter, c.r.Msg, got, c.want, f)
		}
		// The canonical form must mean the same thing.
		g, err := Parse(f.String())
		if err != nil {
			t.Fatalf("re-parse of %q (%q): %v", c.filter, f.String(), err)
		}
		if got := g.MatchRecord(c.r); got != c.want {
			t.Errorf("canonical %q of %q: got %v, want %v", f.String(), c.filter, got, c.want)
		}
	}
}

func TestPrecedence(t *testing.T) {
	f := MustParse("a b OR c")
	if f.String() != "((a AND b) OR c)" {
		t.Fatalf("got %s", f)
	}
	f = MustParse("a OR b c")
	if f.String() != "(a OR (b AND c))" {
		t.Fatalf("got %s", f)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		`"open`:               "never closed",
		`(a OR b`:             "never closed",
		`a)`:                  "closing parenthesis",
		`()`:                  "empty parentheses",
		`a OR`:                "nothing after it",
		`a AND`:               "nothing after it",
		`OR a`:                "put a term before it",
		`@level:`:             "no value",
		`@:x`:                 "@name:value",
		`@x:>abc`:             "not a number",
		`@x:5..1`:             "starts above",
		`a"b`:                 "quote inside a word",
		`@a..b:1`:             "empty part",
		`@a[x]:1`:             "array index",
		`(a OR (b AND c)) OR`: "nothing after it",
	}
	for in, want := range cases {
		_, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q): want an error containing %q", in, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %q, want it to contain %q", in, err, want)
		}
		if !strings.HasSuffix(err.Error(), ".") || err.Error()[0] < 'A' || err.Error()[0] > 'Z' {
			t.Errorf("Parse(%q) error %q is not a full sentence", in, err)
		}
	}
}

func TestNilFilter(t *testing.T) {
	var f *Filter
	if !f.MatchRecord(rec("x", nil)) || !f.Empty() || f.String() != "" {
		t.Fatal("nil filter must match everything")
	}
	if !(&Filter{}).Match(recordFields{rec("x", nil)}) {
		t.Fatal("zero filter must match everything")
	}
}

type mapFields map[string]any

func (m mapFields) Message() string { s, _ := m["msg"].(string); return s }
func (m mapFields) Attr(n string) (any, bool) {
	v, ok := m[n]
	return v, ok
}

func TestCustomFields(t *testing.T) {
	f := MustParse("@n:>1 hello")
	if !f.Match(mapFields{"msg": "hello world", "n": 2}) {
		t.Fatal("custom fields should match")
	}
	if f.Match(mapFields{"msg": "hello world", "n": int64(1)}) {
		t.Fatal("n=1 must not match >1")
	}
}

func TestLookup(t *testing.T) {
	attrs := map[string]any{"http.status": float64(200), "A": map[string]any{"B": []any{float64(1), []any{"x"}}}}
	if v, ok := Lookup(attrs, "http.status"); !ok || v != float64(200) {
		t.Fatalf("flat dotted key: %v %v", v, ok)
	}
	if v, ok := Lookup(attrs, "a.b[1][0]"); !ok || v != "x" {
		t.Fatalf("nested: %v %v", v, ok)
	}
	for _, p := range []string{"", "a.c", "a.b[9]", "a.b[x]", "a.b[1", "a.b.c"} {
		if _, ok := Lookup(attrs, p); ok {
			t.Errorf("Lookup(%q) should miss", p)
		}
	}
	if _, ok := Lookup(nil, "a"); ok {
		t.Fatal("nil attrs")
	}
}

func TestStringify(t *testing.T) {
	for v, want := range map[any]string{nil: "", true: "true", 3: "3", int64(4): "4", 1.5: "1.5", "s": "s", uint8(7): "7"} {
		if got := stringify(v); got != want {
			t.Errorf("stringify(%v) = %q", v, got)
		}
	}
	for _, v := range []any{float32(1), 2, int64(3), int32(4), uint64(5), "6"} {
		if _, ok := toFloat(v); !ok {
			t.Errorf("toFloat(%v) failed", v)
		}
	}
	if _, ok := toFloat(struct{}{}); ok {
		t.Error("struct is not a number")
	}
}

func TestMustParsePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustParse should panic on a bad filter")
		}
	}()
	MustParse(`"`)
}

func TestQuoteRoundTrip(t *testing.T) {
	for _, s := range []string{`and`, `-x`, `@y`, `a:b`, `with space`, `q"uote`, `back\slash`, `(p)`} {
		f := MustParse(quote(s))
		if !f.MatchRecord(rec("pre "+s+" post", nil)) {
			t.Errorf("quoted %q did not match itself (%s)", s, f)
		}
		if !MustParse(f.String()).MatchRecord(rec("pre "+s+" post", nil)) {
			t.Errorf("canonical of %q did not match itself", s)
		}
	}
	f := MustParse(`@k:"a b"`)
	if !f.MatchRecord(rec("", map[string]any{"k": "A B"})) || f.String() != `@k:"a b"` {
		t.Errorf("quoted value: %s", f)
	}
	f = MustParse(`replica:"rep_9"`)
	if !f.MatchRecord(&proto.LogRecord{ReplicaID: "rep_9"}) {
		t.Errorf("quoted replica: %s", f)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`@level:error -healthcheck`, `@httpStatus:>=500 @path:/api`, `"connection refused" OR timeout`,
		`(a OR b) -(c AND d)`, `@x:1..2`, `replica:rep_1`, `@a.b[0]:"x y"`, `"\"`, `-`, `((`, `@`,
	} {
		f.Add(s)
	}
	r := rec("Connection refused at /api", map[string]any{"httpStatus": float64(502), "path": "/api/x", "a": map[string]any{"b": []any{"x y"}}})
	f.Fuzz(func(t *testing.T, s string) {
		flt, err := Parse(s)
		if err != nil {
			if !strings.HasSuffix(err.Error(), ".") {
				t.Fatalf("error %q is not a sentence", err)
			}
			return
		}
		want := flt.MatchRecord(r)
		canon := flt.String()
		g, err := Parse(canon)
		if err != nil {
			t.Fatalf("canonical form %q of %q does not parse: %v", canon, s, err)
		}
		if g.MatchRecord(r) != want {
			t.Fatalf("canonical form %q of %q changes the result", canon, s)
		}
		if g.String() != canon {
			t.Fatalf("canonical form is not stable: %q then %q", canon, g.String())
		}
	})
}
