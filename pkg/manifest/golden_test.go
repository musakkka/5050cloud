// SPDX-License-Identifier: MIT

package manifest

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden.yaml")

// fixtures pairs each testdata file with the type it decodes into.
var fixtures = []struct {
	file string
	new  func() any
}{
	{"template.yaml", func() any { return new(Template) }},
	{"project.yaml", func() any { return new(ProjectFile) }},
	{"railway.yaml", func() any { return new(RailwayConfig) }},
	{"nixpacks-hints.yaml", func() any { return new(NixpacksHints) }},
}

// TestGoldenRoundTrip decodes each fixture strictly with sigs.k8s.io/yaml,
// encodes it again, compares the result with the golden file and checks that
// decoding and encoding the output is stable. (Empty lists in a fixture are
// dropped by omitempty, so the first decode and the second may differ in
// nil versus empty, which is why stability is checked on the bytes.)
func TestGoldenRoundTrip(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.file, func(t *testing.T) {
			in, err := os.ReadFile(filepath.Join("testdata", f.file))
			if err != nil {
				t.Fatal(err)
			}
			v := f.new()
			if err := yaml.UnmarshalStrict(in, v); err != nil {
				t.Fatalf("decode %s: %v", f.file, err)
			}
			out, err := yaml.Marshal(v)
			if err != nil {
				t.Fatalf("encode %s: %v", f.file, err)
			}
			golden := filepath.Join("testdata", strings.TrimSuffix(f.file, ".yaml")+".golden.yaml")
			if *update {
				if err := os.WriteFile(golden, out, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run go test ./pkg/manifest -update to create it): %v", err)
			}
			if !bytes.Equal(out, want) {
				t.Errorf("%s: encoded output differs from %s; run go test ./pkg/manifest -update and review the diff.\n--- got\n%s", f.file, golden, out)
			}
			again := f.new()
			if err := yaml.UnmarshalStrict(out, again); err != nil {
				t.Fatalf("decode the encoded output: %v", err)
			}
			out2, err := yaml.Marshal(again)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, out2) {
				t.Errorf("%s: encoding is not stable across a second round trip.\n--- first\n%s\n--- second\n%s", f.file, out, out2)
			}
		})
	}
}

// TestEveryFieldCovered walks the manifest types and fails when a JSON field
// name is set by no fixture, so a new field cannot skip the golden test.
func TestEveryFieldCovered(t *testing.T) {
	keys := map[string]bool{}
	for _, f := range fixtures {
		in, err := os.ReadFile(filepath.Join("testdata", f.file))
		if err != nil {
			t.Fatal(err)
		}
		j, err := yaml.YAMLToJSON(in)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(j, &doc); err != nil {
			t.Fatal(err)
		}
		collectKeys(doc, keys)
	}
	want := map[string]bool{}
	seen := map[reflect.Type]bool{}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(Template{}), reflect.TypeOf(ProjectFile{}),
		reflect.TypeOf(RailwayConfig{}), reflect.TypeOf(NixpacksHints{}),
	} {
		jsonNames(typ, want, seen)
	}
	var missing []string
	for name := range want {
		if !keys[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no fixture in testdata sets these fields: %s", strings.Join(missing, ", "))
	}
}

// TestCamelCaseOutput proves the reason for sigs.k8s.io/yaml: encoding
// writes the json tag names (camelCase), where gopkg.in/yaml.v3 would write
// lower-cased Go field names. Decoding through encoding/json matches keys
// case-insensitively, so pkg/configfile checks exact key spelling itself
// before it suggests a fix for an unknown field.
func TestCamelCaseOutput(t *testing.T) {
	cmd := "node server.js"
	n := 2
	out, err := yaml.Marshal(RailwayConfig{Deploy: &RailwayDeploy{StartCommand: &cmd, NumReplicas: &n}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"startCommand: node server.js", "numReplicas: 2"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("encoded YAML lacks %q:\n%s", want, out)
		}
	}
	for _, wrong := range []string{"startcommand", "numreplicas", "StartCommand"} {
		if strings.Contains(string(out), wrong) {
			t.Errorf("encoded YAML contains %q:\n%s", wrong, out)
		}
	}
	tpl, err := os.ReadFile(filepath.Join("testdata", "template.golden.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mountPath:", "networking:", "majorVersions:", "memoryReservationMb:", "healthcheckTimeoutSec:"} {
		if !bytes.Contains(tpl, []byte(want)) {
			t.Errorf("template.golden.yaml lacks %q", want)
		}
	}
}

func TestStrictRejectsUnknownFields(t *testing.T) {
	var d RailwayDeploy
	err := yaml.UnmarshalStrict([]byte("startComand: node server.js\n"), &d)
	if err == nil {
		t.Fatal("a misspelled field was accepted; strict decoding must reject unknown fields")
	}
}

func TestStringOrList(t *testing.T) {
	cases := []struct {
		in   string
		want StringOrList
		out  string
	}{
		{`"npm run migrate"`, StringOrList{"npm run migrate"}, `"npm run migrate"`},
		{`["a","b"]`, StringOrList{"a", "b"}, `["a","b"]`},
	}
	for _, c := range cases {
		var s StringOrList
		if err := json.Unmarshal([]byte(c.in), &s); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !reflect.DeepEqual(s, c.want) {
			t.Fatalf("%s: got %#v", c.in, s)
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.out {
			t.Fatalf("%s: marshalled to %s", c.in, b)
		}
	}
	var s StringOrList
	if err := json.Unmarshal([]byte(`42`), &s); err == nil {
		t.Fatal("a number was accepted as a command")
	}
}

func collectKeys(v any, keys map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			keys[k] = true
			collectKeys(e, keys)
		}
	case []any:
		for _, e := range x {
			collectKeys(e, keys)
		}
	}
}

var rawMessage = reflect.TypeOf(json.RawMessage{})

func jsonNames(t reflect.Type, out map[string]bool, seen map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		if t == rawMessage {
			return
		}
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			jsonNames(f.Type, out, seen)
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = true
		jsonNames(f.Type, out, seen)
	}
}
