// SPDX-License-Identifier: AGPL-3.0-only

package vars

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	for _, ok := range []string{"A", "_x", "DATABASE_URL", "a1", strings.Repeat("A", 256)} {
		if !ValidName(ok) || CheckName(ok) != nil {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "1A", "A-B", "A B", strings.Repeat("A", 257), "é"} {
		if ValidName(bad) || CheckName(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if err := CheckValue("A", strings.Repeat("x", MaxValueBytes+1)); err == nil {
		t.Error("oversized value accepted")
	}
	if err := CheckValue("A", "a\x00b"); err == nil {
		t.Error("NUL accepted")
	}
	if err := CheckValue("A", "${{oops"); err == nil {
		t.Error("unterminated reference accepted")
	}
	if err := CheckValue("A", "line1\nline2 ${{shared.X}}"); err != nil {
		t.Error(err)
	}
}

func TestParse(t *testing.T) {
	parts, err := Parse("postgres://${{PGUSER}}:${{ Postgres.PGPASSWORD }}@${{My Service.HOST}}/${{shared.DB}}?x=${{secret(8)}}")
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	var funcs []string
	for _, p := range parts {
		if p.Ref != nil {
			refs = append(refs, p.Ref.String())
		}
		if p.Func != "" {
			funcs = append(funcs, p.Func)
		}
	}
	want := []string{"PGUSER", "Postgres.PGPASSWORD", "My Service.HOST", "shared.DB"}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %v", refs)
	}
	if !reflect.DeepEqual(funcs, []string{"secret(8)"}) {
		t.Fatalf("funcs = %v", funcs)
	}
	for _, bad := range []string{"${{}}", "${{a-b}}", "${{.X}}", "${{svc.}}", "${{x"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	if got := ReferencedServices("${{api.X}} ${{shared.Y}} ${{Z}} ${{db.U}}"); !reflect.DeepEqual(got, []string{"api", "db"}) {
		t.Fatalf("ReferencedServices = %v", got)
	}
	if References("${{bad") != nil {
		t.Fatal("References of a bad value must be empty")
	}
	if HasReference("plain") || !HasReference("${{X}}") {
		t.Fatal("HasReference")
	}
}

func TestEvalFunctions(t *testing.T) {
	v, err := EvalFunctions("pw=${{secret(32)}} n=${{randomInt(1000, 1000)}} k=${{secret(4, \"ab\")}} ${{api.X}} ${{input.DB}}", map[string]string{"DB": "railway"})
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^pw=[A-Za-z0-9]{32} n=1000 k=[ab]{4} \$\{\{api\.X\}\} railway$`)
	if !re.MatchString(v) {
		t.Fatalf("got %q", v)
	}
	plain, err := EvalFunctions("no refs", nil)
	if err != nil || plain != "no refs" {
		t.Fatal("plain values pass through")
	}
	for _, bad := range []string{
		"${{secret()}}", "${{secret(0)}}", "${{secret(x)}}", "${{secret(1, \"\")}}", "${{secret(1,2,3)}}",
		"${{randomInt(1)}}", "${{randomInt(5, 1)}}", "${{randomInt(a, b)}}", "${{input.MISSING}}",
		"${{secret(1, \"abc)}}", "${{secret(1,)}}", "${{secret(1 2)}}",
	} {
		if _, err := EvalFunctions(bad, map[string]string{}); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	if _, err := EvalFunctions("${{x", nil); err == nil {
		t.Error("syntax error must fail")
	}
	g, err := Generate("secret(16)")
	if err != nil || len(g) != 16 {
		t.Fatalf("Generate = %q, %v", g, err)
	}
	if g, err := Generate(""); err != nil || g != "" {
		t.Fatal("empty Generate")
	}
	if _, err := Generate("api.X"); err == nil {
		t.Fatal("Generate accepts only functions")
	}
	n, err := Generate("randomInt(1,3)")
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := strconv.Atoi(n); i < 1 || i > 3 {
		t.Fatalf("randomInt = %s", n)
	}
}

func scope() Scope {
	return Scope{
		Shared: map[string]string{"DB": "railway", "LOOP": "${{shared.LOOP}}", "URL": "${{Postgres.DATABASE_URL}}"},
		Services: map[string]map[string]string{
			"Postgres": {
				"PGUSER":       "postgres",
				"PGPASSWORD":   "s3cret-pass",
				"DATABASE_URL": "postgresql://${{PGUSER}}:${{PGPASSWORD}}@${{FIFTY_PRIVATE_DOMAIN}}:5432/${{shared.DB}}",
				// provided variable merged in by the caller
				"FIFTY_PRIVATE_DOMAIN": "postgres.internal",
			},
			"api": {
				"DATABASE_URL": "${{Postgres.DATABASE_URL}}",
				"A":            "${{B}}",
				"B":            "${{A}}",
				"MISSING":      "x${{nope.X}}y${{Postgres.NOPE}}",
				"PLAIN":        "hello",
				"FN":           "${{secret(4)}}",
			},
			"deep": {"V0": "${{V1}}", "V1": "${{V2}}", "V2": "${{V3}}", "V3": "${{V4}}", "V4": "${{V5}}", "V5": "${{V6}}", "V6": "${{V7}}", "V7": "${{V8}}", "V8": "${{V9}}", "V9": "${{V10}}", "V10": "${{V11}}", "V11": "end"},
		},
	}
}

func TestResolve(t *testing.T) {
	s := scope()
	got, unresolved := ResolveService(s, "api")
	if got["DATABASE_URL"] != "postgresql://postgres:s3cret-pass@postgres.internal:5432/railway" {
		t.Fatalf("DATABASE_URL = %q", got["DATABASE_URL"])
	}
	if got["A"] != "" || got["B"] != "" {
		t.Fatalf("cycle should resolve to empty: %q %q", got["A"], got["B"])
	}
	if got["MISSING"] != "xy" || got["PLAIN"] != "hello" {
		t.Fatalf("MISSING=%q PLAIN=%q", got["MISSING"], got["PLAIN"])
	}
	if got["FN"] != "${{secret(4)}}" {
		t.Fatalf("leftover function = %q", got["FN"])
	}
	want := []string{"A", "B", "Postgres.NOPE", "nope.X"}
	if !reflect.DeepEqual(unresolved, want) {
		t.Fatalf("unresolved = %v", unresolved)
	}
	shared, u := ResolveShared(s)
	if shared["URL"] == "" || shared["DB"] != "railway" || shared["LOOP"] != "" {
		t.Fatalf("shared = %v", shared)
	}
	if !reflect.DeepEqual(u, []string{"shared.LOOP"}) {
		t.Fatalf("shared unresolved = %v", u)
	}
	deep, u := ResolveService(s, "deep")
	if deep["V11"] != "end" || deep["V0"] != "" || len(u) == 0 {
		t.Fatalf("depth limit not applied: %q %v", deep["V0"], u)
	}
	if deep["V2"] != "end" {
		t.Fatalf("V2 = %q (within the limit)", deep["V2"])
	}
	v, u := ResolveValue(s, "api", "${{PLAIN}} ${{shared.DB}}")
	if v != "hello railway" || len(u) != 0 {
		t.Fatalf("ResolveValue = %q %v", v, u)
	}
	if got := Unresolved(s, "api", "${{zzz.Q}}"); !reflect.DeepEqual(got, []string{"zzz.Q"}) {
		t.Fatalf("Unresolved = %v", got)
	}
	if v, _ := ResolveValue(s, "api", "${{bad"); v != "${{bad" {
		t.Fatal("syntax errors are kept verbatim")
	}
}

func TestRewriteService(t *testing.T) {
	v, ok := RewriteService("http://${{api.HOST}}:${{api.PORT}}/${{apiv2.X}} ${{shared.Y}} ${{secret(3)}}", "api", "backend")
	if !ok || v != "http://${{backend.HOST}}:${{backend.PORT}}/${{apiv2.X}} ${{shared.Y}} ${{secret(3)}}" {
		t.Fatalf("got %q %v", v, ok)
	}
	if _, ok := RewriteService("${{other.X}}", "api", "backend"); ok {
		t.Fatal("unrelated value changed")
	}
	if _, ok := RewriteService("plain", "api", "backend"); ok {
		t.Fatal("plain value changed")
	}
	if _, ok := RewriteService("${{bad", "api", "backend"); ok {
		t.Fatal("bad value changed")
	}
	if v, ok := RewriteService("${{My API.X}}", "My API", "Core"); !ok || v != "${{Core.X}}" {
		t.Fatalf("names with spaces: %q", v)
	}
}

func TestProvided(t *testing.T) {
	names := ProvidedNames(true)
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	for _, n := range []string{"PORT", "FIFTY_PUBLIC_DOMAIN", "RAILWAY_PUBLIC_DOMAIN", "RAILWAY_ENVIRONMENT", "RAILWAY_REPLICA_REGION"} {
		if !has[n] {
			t.Errorf("missing %s", n)
		}
	}
	if len(ProvidedNames(false)) != len(ProvidedSuffixes)+1 {
		t.Fatal("without aliases only PORT and FIFTY_*")
	}
	a := WithAliases(map[string]string{"PORT": "8080", "FIFTY_ENVIRONMENT_NAME": "production", "FIFTY_HOST": "h1"})
	if a["RAILWAY_HOST"] != "h1" || a["RAILWAY_ENVIRONMENT"] != "production" || a["PORT"] != "8080" {
		t.Fatalf("aliases = %v", a)
	}
	m := Merge(map[string]string{"PORT": "8080", "X": "p"}, map[string]string{"PORT": "3000"})
	if m["PORT"] != "3000" || m["X"] != "p" {
		t.Fatal("user variables must win")
	}
}

func TestMasking(t *testing.T) {
	vals := map[string]string{
		"API_KEY":      "abcdef123",
		"SHORT_TOKEN":  "abc",
		"DATABASE_URL": "postgresql://postgres:hunter22@db:5432/x",
		"PLAIN":        "visible-value",
		"SEALED_ONE":   "sealed-value",
		"REDIS_URL":    "redis://default:pw@redis:6379",
	}
	got := SecretValues(vals, map[string]bool{"SEALED_ONE": true})
	want := []string{"sealed-value", "abcdef123", "hunter22"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretValues = %v", got)
	}
	if Mask("key abcdef123 and hunter22", got) != "key ••• and •••" {
		t.Fatal("Mask")
	}
	if MaskURL(vals["DATABASE_URL"]) != "postgresql://postgres:•••@db:5432/x" {
		t.Fatalf("MaskURL = %q", MaskURL(vals["DATABASE_URL"]))
	}
	if MaskURL("not a url") != "not a url" || MaskURL("http://%zz@x") != "http://%zz@x" {
		t.Fatal("MaskURL must leave non-URLs alone")
	}
	if !IsSecretName("my_password") || IsSecretName("HOSTNAME") {
		t.Fatal("IsSecretName")
	}
}

func TestDotenv(t *testing.T) {
	text := "# comment\nexport A=1\nB = \"two words\" \nC='single # kept'\nD=plain # trailing comment\nE=\"multi\nline\"\nF=\"esc\\n\\\"q\\\"\"\n\nG=\nH='multi\nsingle'\n"
	got, err := ParseRaw(text)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two words", "C": "single # kept", "D": "plain", "E": "multi\nline", "F": "esc\n\"q\"", "G": "", "H": "multi\nsingle"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	for _, bad := range []string{"NOEQUALS", "1A=x", "A=\"open", "A='open"} {
		if _, err := ParseDotenv(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	j, err := ParseRaw(`{"A": "x", "N": 3, "B": true, "Z": null, "O": {"k": 1}}`)
	if err != nil {
		t.Fatal(err)
	}
	if j["A"] != "x" || j["N"] != "3" || j["B"] != "true" || j["Z"] != "" || j["O"] != `{"k":1}` {
		t.Fatalf("json = %v", j)
	}
	if _, err := ParseRaw(`{"bad-name": "x"}`); err == nil {
		t.Fatal("bad JSON name accepted")
	}
	if _, err := ParseRaw(`{"A": `); err == nil {
		t.Fatal("broken JSON accepted")
	}
	round, err := ParseDotenv(FormatDotenv(want))
	if err != nil || !reflect.DeepEqual(round, want) {
		t.Fatalf("round trip: %v %v", round, err)
	}
	if truncate("abcdef", 3) != "abc..." || truncate("ab", 3) != "ab" {
		t.Fatal("truncate")
	}
}

func TestHeroku(t *testing.T) {
	js, err := ParseHeroku(`{"DATABASE_URL": "postgres://x", "WEB_CONCURRENCY": "2"}`)
	if err != nil || js["WEB_CONCURRENCY"] != "2" {
		t.Fatalf("json: %v %v", js, err)
	}
	s, err := ParseHeroku("=== myapp Config Vars\nDATABASE_URL:   postgres://u:p@h/d\nA=1\nB='it'\\''s'\nC=\"q\\\"x\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if s["DATABASE_URL"] != "postgres://u:p@h/d" || s["A"] != "1" || s["B"] != "it's" || s["C"] != `q"x` {
		t.Fatalf("shell: %#v", s)
	}
	for _, bad := range []string{"", "nonsense", "1A=x"} {
		if _, err := ParseHeroku(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
