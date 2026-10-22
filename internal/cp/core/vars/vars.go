// SPDX-License-Identifier: AGPL-3.0-only

// Package vars implements variables and references (docs/SPEC.md 9.3):
// the ${{...}} syntax, recursive resolution with cycle detection, the
// template functions, provided variable names and their RAILWAY_* aliases,
// the masking list for logs, reference rewrites after a rename, and the
// .env, JSON and Heroku text formats. It is pure: no database, no network.
package vars

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
)

// MaxValueBytes is the largest value accepted (64 KiB).
const MaxValueBytes = 64 << 10

// MaxDepth is the reference resolution depth limit.
const MaxDepth = 10

// Shared is the service part of ${{shared.NAME}}.
const Shared = "shared"

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,255}$`)

// ValidName reports whether name is a valid variable name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// CheckName returns a validation error for an invalid name.
func CheckName(name string) error {
	if !ValidName(name) {
		return fail.Validation("variables."+name, "%q is not a valid variable name. Use letters, digits and underscores, starting with a letter or underscore, at most 256 characters.", name)
	}
	return nil
}

// CheckValue returns a validation error for an oversized or invalid value.
func CheckValue(name, value string) error {
	if len(value) > MaxValueBytes {
		return fail.Validation("variables."+name, "The value of %s is %d KiB; the limit is 64 KiB.", name, len(value)/1024)
	}
	if strings.ContainsRune(value, 0) {
		return fail.Validation("variables."+name, "The value of %s contains a NUL byte, which environment variables cannot hold.", name)
	}
	if _, err := Parse(value); err != nil {
		e := fail.As(err)
		if e != nil {
			e.Field = "variables." + name
		}
		return err
	}
	return nil
}

// Ref is one ${{...}} reference.
type Ref struct {
	// Service is "" for the same service, Shared for shared variables, or a service name.
	Service string
	Name    string
}

// String renders the reference without the braces: "NAME", "shared.NAME", "api.NAME".
func (r Ref) String() string {
	if r.Service == "" {
		return r.Name
	}
	return r.Service + "." + r.Name
}

// Part is a literal or a reference or a function call inside a value.
type Part struct {
	Literal string
	Ref     *Ref
	// Func is a template function call ("secret(32)") or "input.NAME".
	Func string
}

// Parse splits value into literals, references and function calls. An
// unterminated "${{" or an empty or malformed reference is an error.
func Parse(value string) ([]Part, error) {
	var parts []Part
	rest := value
	for {
		i := strings.Index(rest, "${{")
		if i < 0 {
			if rest != "" {
				parts = append(parts, Part{Literal: rest})
			}
			return parts, nil
		}
		if i > 0 {
			parts = append(parts, Part{Literal: rest[:i]})
		}
		j := strings.Index(rest[i+3:], "}}")
		if j < 0 {
			return nil, fail.Validation("", "A reference starts with ${{ but is not closed with }}.")
		}
		inner := strings.TrimSpace(rest[i+3 : i+3+j])
		rest = rest[i+3+j+2:]
		p, err := parseInner(inner)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
}

var funcRe = regexp.MustCompile(`^(secret|randomInt)\s*\((.*)\)$`)

func parseInner(inner string) (Part, error) {
	if inner == "" {
		return Part{}, fail.Validation("", "The reference ${{}} is empty. Write ${{NAME}}, ${{shared.NAME}} or ${{Service.NAME}}.")
	}
	if funcRe.MatchString(inner) {
		return Part{Func: inner}, nil
	}
	if strings.HasPrefix(inner, "input.") {
		return Part{Func: inner}, nil
	}
	dot := strings.LastIndexByte(inner, '.')
	if dot < 0 {
		if !ValidName(inner) {
			return Part{}, fail.Validation("", "${{%s}} is not a valid reference. Variable names use letters, digits and underscores.", inner)
		}
		return Part{Ref: &Ref{Name: inner}}, nil
	}
	svc, name := strings.TrimSpace(inner[:dot]), strings.TrimSpace(inner[dot+1:])
	if svc == "" || !ValidName(name) {
		return Part{}, fail.Validation("", "${{%s}} is not a valid reference. Write ${{Service.NAME}} with an existing service name.", inner)
	}
	return Part{Ref: &Ref{Service: svc, Name: name}}, nil
}

// References lists the references in value (syntax errors yield none).
func References(value string) []Ref {
	parts, err := Parse(value)
	if err != nil {
		return nil
	}
	var out []Ref
	for _, p := range parts {
		if p.Ref != nil {
			out = append(out, *p.Ref)
		}
	}
	return out
}

// HasReference reports whether value contains a reference or function.
func HasReference(value string) bool { return strings.Contains(value, "${{") }

// ReferencedServices returns the other services value references, by name.
func ReferencedServices(value string) []string {
	var out []string
	for _, r := range References(value) {
		if r.Service != "" && r.Service != Shared {
			out = append(out, r.Service)
		}
	}
	return out
}

// ---------------------------------------------------------------- template functions

// EvalFunctions evaluates ${{secret(...)}}, ${{randomInt(...)}} and, with
// inputs, ${{input.NAME}}, leaving references untouched. It runs once,
// when a value is first saved.
func EvalFunctions(value string, inputs map[string]string) (string, error) {
	if !HasReference(value) {
		return value, nil
	}
	parts, err := Parse(value)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.Literal != "":
			b.WriteString(p.Literal)
		case p.Ref != nil:
			b.WriteString("${{" + p.Ref.String() + "}}")
		case p.Func != "":
			v, err := evalFunc(p.Func, inputs)
			if err != nil {
				return "", err
			}
			b.WriteString(v)
		}
	}
	return b.String(), nil
}

// Generate evaluates one function expression ("secret(32)") as used in
// template inputs (TemplateInput.Generate).
func Generate(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}
	if !funcRe.MatchString(expr) {
		return "", fail.Validation("", "%q is not a template function. Use secret(32), secret(16, \"abc\") or randomInt(1000, 9999).", expr)
	}
	return evalFunc(expr, nil)
}

func evalFunc(expr string, inputs map[string]string) (string, error) {
	if strings.HasPrefix(expr, "input.") {
		name := strings.TrimPrefix(expr, "input.")
		v, ok := inputs[name]
		if !ok {
			return "", fail.Validation("inputs."+name, "The template input %s has no value.", name)
		}
		return v, nil
	}
	m := funcRe.FindStringSubmatch(expr)
	args, err := splitArgs(m[2])
	if err != nil {
		return "", err
	}
	switch m[1] {
	case "secret":
		if len(args) < 1 || len(args) > 2 {
			return "", fail.Validation("", "secret() takes a length and an optional alphabet, for example secret(32) or secret(16, \"abc\").")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 || n > 4096 {
			return "", fail.Validation("", "secret() needs a length from 1 to 4096, got %q.", args[0])
		}
		alphabet := ids.Alnum
		if len(args) == 2 {
			alphabet = args[1]
			if alphabet == "" {
				return "", fail.Validation("", "The alphabet given to secret() is empty.")
			}
		}
		return ids.Random(n, alphabet), nil
	case "randomInt":
		if len(args) != 2 {
			return "", fail.Validation("", "randomInt() takes a minimum and a maximum, for example randomInt(1000, 9999).")
		}
		lo, err1 := strconv.ParseInt(args[0], 10, 64)
		hi, err2 := strconv.ParseInt(args[1], 10, 64)
		if err1 != nil || err2 != nil || hi < lo {
			return "", fail.Validation("", "randomInt() needs two whole numbers with the minimum first, got %q.", m[2])
		}
		return strconv.FormatInt(ids.RandomInt(lo, hi), 10), nil
	}
	return "", fail.Validation("", "Unknown template function %s.", m[1])
}

// splitArgs splits "16, \"abc\"" into ["16", "abc"].
func splitArgs(s string) ([]string, error) {
	var out []string
	s = strings.TrimSpace(s)
	for s != "" {
		if s[0] == '"' {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				return nil, fail.Validation("", "A quoted argument is not closed.")
			}
			out = append(out, s[1:end+1])
			s = strings.TrimSpace(s[end+2:])
		} else {
			end := strings.IndexByte(s, ',')
			if end < 0 {
				out = append(out, strings.TrimSpace(s))
				break
			}
			out = append(out, strings.TrimSpace(s[:end]))
			s = s[end:]
		}
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, ",") {
			s = strings.TrimSpace(s[1:])
			if s == "" {
				return nil, fail.Validation("", "A function argument is missing after the comma.")
			}
		} else if s != "" {
			return nil, fail.Validation("", "Function arguments must be separated by commas.")
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- resolution

// Scope is everything one environment's references can see.
type Scope struct {
	// Shared holds the shared variables of the environment.
	Shared map[string]string
	// Services maps service name to its variables (user variables over
	// provided ones, already merged by the caller).
	Services map[string]map[string]string
}

type resolver struct {
	scope      Scope
	unresolved map[string]bool
	stack      map[string]bool
}

// ResolveService resolves every variable of service against scope. It
// returns the final values and the references that resolved to "".
func ResolveService(scope Scope, service string) (map[string]string, []string) {
	r := &resolver{scope: scope, unresolved: map[string]bool{}, stack: map[string]bool{}}
	out := map[string]string{}
	for name, v := range scope.Services[service] {
		out[name] = r.resolve(service, v, 0)
	}
	return out, r.list()
}

// ResolveShared resolves the shared variables (they may reference each other and services).
func ResolveShared(scope Scope) (map[string]string, []string) {
	r := &resolver{scope: scope, unresolved: map[string]bool{}, stack: map[string]bool{}}
	out := map[string]string{}
	for name, v := range scope.Shared {
		out[name] = r.resolve(Shared, v, 0)
	}
	return out, r.list()
}

// ResolveValue resolves one value as if it belonged to service.
func ResolveValue(scope Scope, service, value string) (string, []string) {
	r := &resolver{scope: scope, unresolved: map[string]bool{}, stack: map[string]bool{}}
	return r.resolve(service, value, 0), r.list()
}

// Unresolved returns the references in value that do not resolve (unknown
// service or variable, a cycle, or too deep).
func Unresolved(scope Scope, service, value string) []string {
	_, u := ResolveValue(scope, service, value)
	return u
}

func (r *resolver) list() []string {
	out := make([]string, 0, len(r.unresolved))
	for k := range r.unresolved {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *resolver) resolve(owner, value string, depth int) string {
	if !HasReference(value) {
		return value
	}
	parts, err := Parse(value)
	if err != nil {
		return value
	}
	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.Literal != "":
			b.WriteString(p.Literal)
		case p.Func != "":
			// Functions are evaluated at save time; a leftover one is kept verbatim.
			b.WriteString("${{" + p.Func + "}}")
		case p.Ref != nil:
			b.WriteString(r.lookup(owner, *p.Ref, depth))
		}
	}
	return b.String()
}

func (r *resolver) lookup(owner string, ref Ref, depth int) string {
	target := ref.Service
	if target == "" {
		target = owner
	}
	label := ref.String()
	if ref.Service == "" && owner != "" && owner != Shared {
		label = ref.Name
	}
	if depth >= MaxDepth {
		r.unresolved[label] = true
		return ""
	}
	var vals map[string]string
	if target == Shared {
		vals = r.scope.Shared
	} else {
		vals = r.scope.Services[target]
	}
	v, ok := vals[ref.Name]
	if !ok {
		r.unresolved[label] = true
		return ""
	}
	key := target + "\x00" + ref.Name
	if r.stack[key] {
		r.unresolved[label] = true
		return ""
	}
	r.stack[key] = true
	out := r.resolve(target, v, depth+1)
	delete(r.stack, key)
	return out
}

// ---------------------------------------------------------------- rename

// RewriteService rewrites references to service oldName into newName
// (${{api.X}} becomes ${{backend.X}}). It reports whether value changed.
func RewriteService(value, oldName, newName string) (string, bool) {
	if !HasReference(value) || oldName == newName {
		return value, false
	}
	parts, err := Parse(value)
	if err != nil {
		return value, false
	}
	changed := false
	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.Literal != "":
			b.WriteString(p.Literal)
		case p.Func != "":
			b.WriteString("${{" + p.Func + "}}")
		case p.Ref != nil:
			ref := *p.Ref
			if ref.Service == oldName {
				ref.Service = newName
				changed = true
			}
			b.WriteString("${{" + ref.String() + "}}")
		}
	}
	if !changed {
		return value, false
	}
	return b.String(), true
}

// ---------------------------------------------------------------- provided variables

// ProvidedSuffixes are the provided variable names without their prefix (9.3).
var ProvidedSuffixes = []string{
	"PUBLIC_DOMAIN", "PRIVATE_DOMAIN", "PROJECT_ID", "PROJECT_NAME", "ENVIRONMENT_ID",
	"ENVIRONMENT_NAME", "SERVICE_ID", "SERVICE_NAME", "DEPLOYMENT_ID", "REPLICA_ID", "HOST",
	"PROCESSES", "GIT_COMMIT_SHA", "GIT_BRANCH", "GIT_REPO_OWNER", "GIT_REPO_NAME",
	"GIT_COMMIT_MESSAGE", "GIT_AUTHOR", "VOLUME_NAME", "VOLUME_MOUNT_PATH", "TCP_PROXY_DOMAIN",
	"TCP_PROXY_PORT", "TCP_APPLICATION_PORT", "SNAPSHOT_ID", "REPLICA_REGION",
}

// ProvidedNames lists PORT, the FIFTY_* names and, with aliases, the RAILWAY_* names.
func ProvidedNames(aliases bool) []string {
	out := []string{"PORT"}
	for _, s := range ProvidedSuffixes {
		out = append(out, "FIFTY_"+s)
	}
	if aliases {
		for _, s := range ProvidedSuffixes {
			out = append(out, "RAILWAY_"+s)
		}
		out = append(out, "RAILWAY_ENVIRONMENT")
	}
	return out
}

// WithAliases copies every FIFTY_X to RAILWAY_X and sets RAILWAY_ENVIRONMENT.
func WithAliases(provided map[string]string) map[string]string {
	out := make(map[string]string, len(provided)*2)
	for k, v := range provided {
		out[k] = v
		if strings.HasPrefix(k, "FIFTY_") {
			out["RAILWAY_"+strings.TrimPrefix(k, "FIFTY_")] = v
		}
	}
	if v, ok := provided["FIFTY_ENVIRONMENT_NAME"]; ok {
		out["RAILWAY_ENVIRONMENT"] = v
	}
	return out
}

// Merge returns provided overlaid with user (user variables win).
func Merge(provided, user map[string]string) map[string]string {
	out := make(map[string]string, len(provided)+len(user))
	for k, v := range provided {
		out[k] = v
	}
	for k, v := range user {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------- masking

var secretNameRe = regexp.MustCompile(`(?i)(SECRET|TOKEN|PASSWORD|PASSWD|PASS|KEY|PRIVATE|CREDENTIAL|DSN|AUTH)`)

// IsSecretName reports whether a variable name looks secret (9.15).
func IsSecretName(name string) bool { return secretNameRe.MatchString(name) }

// MinMaskLen is the shortest value that is masked in logs.
const MinMaskLen = 6

// SecretValues returns the values to mask in logs: values of variables
// whose name looks secret or that are sealed, and the password part of any
// URL-valued variable, when at least 6 characters long. The result is
// sorted longest first and deduplicated, so a masker replaces long values
// before their substrings.
func SecretValues(values map[string]string, sealed map[string]bool) []string {
	set := map[string]bool{}
	for name, v := range values {
		if (IsSecretName(name) || sealed[name]) && len(v) >= MinMaskLen {
			set[v] = true
		}
		if pw := urlPassword(v); len(pw) >= MinMaskLen {
			set[pw] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

func urlPassword(v string) string {
	if !strings.Contains(v, "://") || !strings.Contains(v, "@") {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || u.User == nil {
		return ""
	}
	pw, _ := u.User.Password()
	return pw
}

// Mask replaces each secret in s with •••.
func Mask(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "•••")
		}
	}
	return s
}

// MaskURL hides the password of a URL: postgresql://u:•••@h:5432/db.
func MaskURL(v string) string {
	pw := urlPassword(v)
	if pw == "" {
		return v
	}
	return strings.Replace(v, ":"+pw+"@", ":•••@", 1)
}

// ---------------------------------------------------------------- text formats

// ParseRaw parses the Raw Editor text: a JSON object, or .env lines.
func ParseRaw(text string) (map[string]string, error) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "{") {
		return parseJSONObject(t)
	}
	return ParseDotenv(text)
}

// ParseDotenv parses KEY=value lines: comments, "export ", single and
// double quotes (double quotes support \n, \" and multi-line values).
func ParseDotenv(text string) (map[string]string, error) {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fail.Validation("raw", "Line %d has no = sign: %q.", i+1, truncate(line, 40))
		}
		name := strings.TrimSpace(line[:eq])
		if err := CheckName(name); err != nil {
			return nil, fail.Validation("raw", "Line %d: %q is not a valid variable name.", i+1, name)
		}
		val := strings.TrimSpace(line[eq+1:])
		switch {
		case strings.HasPrefix(val, `"`):
			body := val[1:]
			for !closesDouble(body) {
				if i+1 >= len(lines) {
					return nil, fail.Validation("raw", "The value of %s starts with a double quote that is never closed.", name)
				}
				i++
				body += "\n" + lines[i]
			}
			end := lastUnescapedQuote(body)
			val = unescapeDouble(body[:end])
		case strings.HasPrefix(val, `'`):
			body := val[1:]
			for !strings.Contains(body, "'") {
				if i+1 >= len(lines) {
					return nil, fail.Validation("raw", "The value of %s starts with a single quote that is never closed.", name)
				}
				i++
				body += "\n" + lines[i]
			}
			val = body[:strings.LastIndexByte(body, '\'')]
		default:
			if k := strings.Index(val, " #"); k >= 0 {
				val = strings.TrimSpace(val[:k])
			}
		}
		out[name] = val
	}
	return out, nil
}

func closesDouble(s string) bool { return lastUnescapedQuote(s) >= 0 }

func lastUnescapedQuote(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != '"' {
			continue
		}
		bs := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			bs++
		}
		if bs%2 == 0 {
			return i
		}
	}
	return -1
}

func unescapeDouble(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t", `\"`, `"`, `\\`, `\`)
	return r.Replace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ParseHeroku parses `heroku config --json` or `heroku config -s` output
// (KEY=value or KEY='value' lines), and also the aligned "KEY: value"
// output of plain `heroku config`.
func ParseHeroku(text string) (map[string]string, error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil, fail.Validation("body", "The pasted text is empty. Paste the output of heroku config --json or heroku config -s.")
	}
	if strings.HasPrefix(t, "{") {
		return parseJSONObject(t)
	}
	out := map[string]string{}
	for i, line := range strings.Split(strings.ReplaceAll(t, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "===") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		colon := strings.Index(line, ": ")
		var name, val string
		switch {
		case eq > 0 && (colon < 0 || eq < colon):
			name, val = line[:eq], line[eq+1:]
		case colon > 0:
			name, val = line[:colon], strings.TrimSpace(line[colon+2:])
		default:
			return nil, fail.Validation("body", "Line %d is not KEY=value or KEY: value.", i+1)
		}
		name = strings.TrimSpace(name)
		if !ValidName(name) {
			return nil, fail.Validation("body", "Line %d: %q is not a valid variable name.", i+1, name)
		}
		if len(val) >= 2 && (val[0] == '\'' && val[len(val)-1] == '\'' || val[0] == '"' && val[len(val)-1] == '"') {
			if val[0] == '"' {
				val = unescapeDouble(val[1 : len(val)-1])
			} else {
				val = strings.ReplaceAll(val[1:len(val)-1], `'\''`, `'`)
			}
		}
		out[name] = val
	}
	return out, nil
}

func parseJSONObject(t string) (map[string]string, error) {
	var raw map[string]any
	if err := jsonUnmarshal([]byte(t), &raw); err != nil {
		return nil, fail.Validation("raw", "The JSON is not valid: %s.", strings.TrimPrefix(err.Error(), "json: "))
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if !ValidName(k) {
			return nil, fail.Validation("raw", "%q is not a valid variable name.", k)
		}
		switch x := v.(type) {
		case string:
			out[k] = x
		case nil:
			out[k] = ""
		case bool, float64:
			out[k] = fmt.Sprint(x)
		default:
			b, _ := jsonMarshal(x)
			out[k] = string(b)
		}
	}
	return out, nil
}

// FormatDotenv writes values as sorted KEY="value" lines.
func FormatDotenv(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := values[k]
		if strings.ContainsAny(v, " \"'\n#\\") {
			v = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v) + `"`
		}
		b.WriteString(k + "=" + v + "\n")
	}
	return b.String()
}
