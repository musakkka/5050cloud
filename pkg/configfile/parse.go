// SPDX-License-Identifier: MIT

// Package configfile reads config-as-code files (docs/SPEC.md 9.20):
// railway.json, railway.toml, 5050.yaml and 5050.json into
// manifest.RailwayConfig with a map from each field to its source line,
// and nixpacks.toml into manifest.NixpacksHints. It rejects unknown fields
// with a suggestion, and merges a file over a service's settings in
// Railway's priority order. The agent's builder (WP4) calls Load; the
// control plane (WP1) calls Merge.
package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/pkg/manifest"
)

// Formats.
const (
	JSON = "json"
	TOML = "toml"
	YAML = "yaml"
)

// Candidates are looked up in this order in the service's root directory.
var Candidates = []string{"5050.yaml", "5050.json", "railway.json", "railway.toml"}

// Error is a problem with a config file, as one sentence that names the
// file and, when known, the line.
type Error struct {
	Path  string // file path as given
	Line  int    // 1-based, 0 when unknown
	Field string // dotted field path, when known
	Msg   string // the sentence without the location
}

func (e *Error) Error() string {
	loc := e.Path
	if e.Line > 0 {
		loc = fmt.Sprintf("%s line %d", e.Path, e.Line)
	}
	return loc + ": " + e.Msg
}

// File is a parsed config file.
type File struct {
	Path   string
	Format string
	Config manifest.RailwayConfig
	// Lines maps field paths ("deploy.startCommand",
	// "environments.production.deploy.numReplicas") to 1-based lines.
	Lines map[string]int
	// ProjectFile is set when a 5050.yaml or 5050.json is a project file
	// written by `5050 config pull` rather than config as code.
	ProjectFile bool
}

// FormatOf returns the format for a file name, or "".
func FormatOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".json":
		return JSON
	case ".toml":
		return TOML
	case ".yaml", ".yml":
		return YAML
	}
	return ""
}

// MaxFileBytes is the largest config file read.
const MaxFileBytes = 1 << 20

// Parse decodes data, the contents of the file at name, strictly.
func Parse(name string, data []byte) (*File, error) {
	format := FormatOf(name)
	if format == "" {
		return nil, &Error{Path: name, Msg: "the file type is not supported; use .json, .toml or .yaml."}
	}
	if len(data) > MaxFileBytes {
		return nil, &Error{Path: name, Msg: "the file is larger than 1 MB."}
	}
	f := &File{Path: name, Format: format}
	var generic any
	switch format {
	case JSON:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&generic); err != nil {
			return nil, &Error{Path: name, Line: jsonErrLine(data, err), Msg: "the JSON is not valid: " + cleanJSONErr(err) + "."}
		}
		if dec.More() {
			return nil, &Error{Path: name, Msg: "the file holds more than one JSON value."}
		}
		f.Lines = jsonLines(data)
	case YAML:
		js, err := yaml.YAMLToJSON(data)
		if err != nil {
			return nil, &Error{Path: name, Msg: "the YAML is not valid: " + strings.TrimPrefix(err.Error(), "error converting YAML to JSON: ") + "."}
		}
		dec := json.NewDecoder(bytes.NewReader(js))
		dec.UseNumber()
		if err := dec.Decode(&generic); err != nil {
			return nil, &Error{Path: name, Msg: "the YAML is not valid: " + cleanJSONErr(err) + "."}
		}
		f.Lines = yamlLines(data)
	case TOML:
		var m map[string]any
		if _, err := toml.Decode(string(data), &m); err != nil {
			line := 0
			var pe toml.ParseError
			if ok := asTOMLParseError(err, &pe); ok {
				line = pe.Position.Line
			}
			return nil, &Error{Path: name, Line: line, Msg: "the TOML is not valid: " + firstLine(err.Error()) + "."}
		}
		generic = normalizeTOML(m)
		f.Lines = tomlLines(data)
	}
	if generic == nil {
		// An empty file sets nothing.
		return f, nil
	}
	obj, ok := generic.(map[string]any)
	if !ok {
		return nil, &Error{Path: name, Msg: "the top level must be an object with build, deploy or environments."}
	}
	base := strings.ToLower(path.Base(name))
	if strings.HasPrefix(base, "5050.") {
		if _, isProject := obj["services"]; isProject {
			f.ProjectFile = true
			return f, nil
		}
		if _, isProject := obj["project"]; isProject {
			f.ProjectFile = true
			return f, nil
		}
	}
	if err := checkKeys(name, f.Lines, "", obj, reflect.TypeOf(manifest.RailwayConfig{})); err != nil {
		return nil, err
	}
	js, err := json.Marshal(obj)
	if err != nil {
		return nil, &Error{Path: name, Msg: "the file cannot be read: " + err.Error() + "."}
	}
	if err := json.Unmarshal(js, &f.Config); err != nil {
		field := ""
		var te *json.UnmarshalTypeError
		if asTypeError(err, &te) {
			field = te.Field
			return nil, &Error{Path: name, Line: f.Lines[field], Field: field, Msg: fmt.Sprintf("%s must be %s, not %s.", field, kindName(te.Type), te.Value)}
		}
		return nil, &Error{Path: name, Msg: "the file cannot be read: " + cleanJSONErr(err) + "."}
	}
	if err := validate(f); err != nil {
		return nil, err
	}
	return f, nil
}

func kindName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Int, reflect.Int64, reflect.Int32:
		return "a whole number"
	case reflect.Bool:
		return "true or false"
	case reflect.Slice:
		return "a list"
	case reflect.Map, reflect.Struct:
		return "an object"
	}
	return t.String()
}

func cleanJSONErr(err error) string {
	return strings.TrimPrefix(err.Error(), "json: ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func jsonErrLine(data []byte, err error) int {
	var se *json.SyntaxError
	if asSyntaxError(err, &se) {
		return lineAt(data, int(se.Offset))
	}
	return 0
}

func lineAt(data []byte, off int) int {
	if off > len(data) {
		off = len(data)
	}
	if off < 0 {
		off = 0
	}
	return bytes.Count(data[:off], []byte("\n")) + 1
}

// normalizeTOML turns TOML values into JSON-compatible ones.
func normalizeTOML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeTOML(val)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeTOML(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeTOML(val)
		}
		return out
	case int64:
		return json.Number(fmt.Sprint(x))
	case float64:
		return json.Number(fmt.Sprint(x))
	}
	return v
}

// ---------------------------------------------------------------- unknown fields

// checkKeys walks obj against the struct type t and reports the first
// unknown key (sorted, so errors are stable) with a suggestion.
func checkKeys(file string, lines map[string]int, prefix string, obj map[string]any, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	fields := jsonFields(t)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := join(prefix, k)
		ft, ok := fields[k]
		if !ok {
			names := make([]string, 0, len(fields))
			for n := range fields {
				names = append(names, n)
			}
			msg := "unknown field " + k
			if s := suggest(k, names); s != "" {
				msg += ". Did you mean " + s + "?"
			} else {
				msg += ". See the config as code reference for the fields 5050cloud reads."
			}
			return &Error{Path: file, Line: lines[p], Field: p, Msg: msg}
		}
		if err := checkValue(file, lines, p, obj[k], ft); err != nil {
			return err
		}
	}
	return nil
}

func checkValue(file string, lines map[string]int, p string, v any, ft reflect.Type) error {
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft == reflect.TypeOf(json.RawMessage{}) {
		return nil // nixpacksPlan: free-form
	}
	switch ft.Kind() {
	case reflect.Struct:
		sub, ok := v.(map[string]any)
		if !ok {
			if v == nil {
				return nil
			}
			return &Error{Path: file, Line: lines[p], Field: p, Msg: fmt.Sprintf("%s must be an object.", p)}
		}
		return checkKeys(file, lines, p, sub, ft)
	case reflect.Map:
		sub, ok := v.(map[string]any)
		if !ok {
			if v == nil {
				return nil
			}
			return &Error{Path: file, Line: lines[p], Field: p, Msg: fmt.Sprintf("%s must be an object.", p)}
		}
		keys := make([]string, 0, len(sub))
		for k := range sub {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := checkValue(file, lines, join(p, k), sub[k], ft.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

// jsonFields maps JSON names to field types, following embedded structs.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if tag == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			for k, v := range jsonFields(f.Type) {
				out[k] = v
			}
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}

// suggest returns the closest known name within edit distance 3.
func suggest(k string, names []string) string {
	sort.Strings(names)
	best, bestD := "", 4
	lk := strings.ToLower(k)
	for _, n := range names {
		if strings.ToLower(n) == lk {
			return n
		}
		if d := levenshtein(lk, strings.ToLower(n)); d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// ---------------------------------------------------------------- values

var builders = map[string]bool{"RAILPACK": true, "DOCKERFILE": true, "NIXPACKS": true}
var restartPolicies = map[string]bool{"ON_FAILURE": true, "ALWAYS": true, "NEVER": true}

func validate(f *File) error {
	if err := validateBlock(f, "", f.Config.Build, f.Config.Deploy); err != nil {
		return err
	}
	names := make([]string, 0, len(f.Config.Environments))
	for n := range f.Config.Environments {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e := f.Config.Environments[n]
		if err := validateBlock(f, "environments."+n+".", e.Build, e.Deploy); err != nil {
			return err
		}
	}
	return nil
}

func validateBlock(f *File, prefix string, b *manifest.RailwayBuild, d *manifest.RailwayDeploy) error {
	bad := func(field, msg string) error {
		p := prefix + field
		return &Error{Path: f.Path, Line: f.Lines[p], Field: p, Msg: msg}
	}
	if b != nil && b.Builder != nil {
		up := strings.ToUpper(*b.Builder)
		switch {
		case up == "HEROKU" || up == "PAKETO":
			return bad("build.builder", fmt.Sprintf("the %s builder is not available in 5050cloud. Use RAILPACK, NIXPACKS (built with Railpack) or DOCKERFILE.", up))
		case !builders[up]:
			return bad("build.builder", fmt.Sprintf("%q is not a builder. Use RAILPACK, NIXPACKS or DOCKERFILE.", *b.Builder))
		}
		*b.Builder = up
	}
	if d == nil {
		return nil
	}
	if d.RestartPolicyType != nil {
		up := strings.ToUpper(*d.RestartPolicyType)
		if !restartPolicies[up] {
			return bad("deploy.restartPolicyType", fmt.Sprintf("%q is not a restart policy. Use ON_FAILURE, ALWAYS or NEVER.", *d.RestartPolicyType))
		}
		*d.RestartPolicyType = up
	}
	if d.HealthcheckPath != nil && *d.HealthcheckPath != "" && !strings.HasPrefix(*d.HealthcheckPath, "/") {
		return bad("deploy.healthcheckPath", "healthcheckPath must start with /.")
	}
	if d.HealthcheckTimeout != nil && (*d.HealthcheckTimeout < 1 || *d.HealthcheckTimeout > 3600) {
		return bad("deploy.healthcheckTimeout", "healthcheckTimeout must be between 1 and 3600 seconds.")
	}
	if d.NumReplicas != nil && (*d.NumReplicas < 1 || *d.NumReplicas > 100) {
		return bad("deploy.numReplicas", "numReplicas must be between 1 and 100.")
	}
	if d.RestartPolicyMaxRetries != nil && *d.RestartPolicyMaxRetries < 0 {
		return bad("deploy.restartPolicyMaxRetries", "restartPolicyMaxRetries cannot be negative.")
	}
	if d.OverlapSeconds != nil && *d.OverlapSeconds < 0 {
		return bad("deploy.overlapSeconds", "overlapSeconds cannot be negative.")
	}
	if d.DrainingSeconds != nil && *d.DrainingSeconds < 0 {
		return bad("deploy.drainingSeconds", "drainingSeconds cannot be negative.")
	}
	if d.Processes != nil && !validProcesses(*d.Processes) {
		return bad("deploy.processes", `processes must be "auto" or a number from 1 to 32.`)
	}
	return nil
}

func validProcesses(s string) bool {
	if s == "auto" {
		return true
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || fmt.Sprint(n) != s {
		return false
	}
	return n >= 1 && n <= 32
}
