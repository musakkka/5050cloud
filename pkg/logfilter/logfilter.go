// SPDX-License-Identifier: MIT

// Package logfilter parses and evaluates the log filter syntax of
// docs/SPEC.md 9.4, which follows Railway's:
//
//	expr    := term { [AND|OR] term }          (adjacent terms mean AND)
//	term    := [-] ( word | "phrase" | @attr:value | @attr:op number | @attr:a..b | replica:<id> | ( expr ) )
//	op      := > | >= | < | <=
//
// Matching is case-insensitive. AND binds tighter than OR, so
// `a b OR c` means `(a AND b) OR c`. A word or phrase matches when the
// message contains it. An attribute term looks up the record's fields
// (level, service, replica, deployment, stream, msg) and then any parsed
// JSON attribute by dotted path, with [i] for array elements. HTTP access
// log attributes (method, path, host, httpStatus, responseTime or its alias
// durationMs, srcIp, requestId, userAgent, bytes) are ordinary attributes.
//
// The package is used by the agent (to filter stored logs) and by the
// control plane (to filter the lines it keeps for offline hosts), so it has
// no dependencies besides pkg/proto.
package logfilter

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Filter is a parsed filter expression. The zero value and a nil *Filter
// match everything.
type Filter struct {
	root node
}

// Fields is what a filter evaluates against.
type Fields interface {
	// Message is the log line text matched by words and phrases.
	Message() string
	// Attr returns an attribute by name or dotted path. ok is false when it
	// does not exist.
	Attr(name string) (value any, ok bool)
}

// Parse parses a filter. An empty or blank string gives a filter that
// matches everything. Errors are full sentences naming the position (1-based
// character) where the problem is.
func Parse(s string) (*Filter, error) {
	toks, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if len(toks) == 0 {
		return &Filter{}, nil
	}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.toks) {
		t := p.toks[p.pos]
		if t.kind == tRParen {
			return nil, errorf("The filter has a closing parenthesis without an opening one at position %d.", t.at)
		}
		return nil, errorf("The filter has an unexpected %q at position %d.", t.text, t.at)
	}
	return &Filter{root: n}, nil
}

// MustParse is Parse for filters known to be valid (tests, constants). It
// panics on an error.
func MustParse(s string) *Filter {
	f, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return f
}

// Match reports whether the fields match the filter.
func (f *Filter) Match(r Fields) bool {
	if f == nil || f.root == nil {
		return true
	}
	return f.root.eval(r)
}

// MatchRecord reports whether a stored log record matches the filter.
func (f *Filter) MatchRecord(r *proto.LogRecord) bool {
	if f == nil || f.root == nil {
		return true
	}
	return f.root.eval(recordFields{r})
}

// Empty reports whether the filter matches everything.
func (f *Filter) Empty() bool { return f == nil || f.root == nil }

// String returns the filter in canonical form: operators upper case,
// explicit AND, every group parenthesised. Parse(f.String()) gives an
// equivalent filter.
func (f *Filter) String() string {
	if f == nil || f.root == nil {
		return ""
	}
	return f.root.String()
}

// ---------------------------------------------------------------- records

// recordFields adapts a proto.LogRecord to Fields.
type recordFields struct{ r *proto.LogRecord }

func (f recordFields) Message() string { return f.r.Msg }

func (f recordFields) Attr(name string) (any, bool) {
	switch strings.ToLower(name) {
	case "level":
		return f.r.Level, true
	case "service", "svc":
		return f.r.ServiceID, true
	case "replica", "rep":
		return f.r.ReplicaID, true
	case "deployment", "dep":
		return f.r.DeploymentID, true
	case "stream":
		return f.r.Stream, true
	case "msg", "message":
		return f.r.Msg, true
	}
	if v, ok := Lookup(f.r.Attrs, name); ok {
		return v, true
	}
	// responseTime and durationMs are the same thing (9.4).
	switch strings.ToLower(name) {
	case "responsetime":
		return Lookup(f.r.Attrs, "durationMs")
	case "durationms":
		return Lookup(f.r.Attrs, "responseTime")
	}
	return nil, false
}

// Lookup resolves a dotted path with optional [i] array indexes ("a.b[2].c")
// in parsed JSON attributes. A key is matched exactly first and then
// ignoring case.
func Lookup(attrs map[string]any, p string) (any, bool) {
	if attrs == nil || p == "" {
		return nil, false
	}
	// A key that itself contains dots ("http.status" written flat by some
	// loggers) wins over the nested interpretation.
	if v, ok := getKey(attrs, p); ok {
		return v, true
	}
	var cur any = attrs
	for _, seg := range strings.Split(p, ".") {
		name, idx, err := splitIndexes(seg)
		if err != nil {
			return nil, false
		}
		if name != "" {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			v, ok := getKey(m, name)
			if !ok {
				return nil, false
			}
			cur = v
		}
		for _, i := range idx {
			arr, ok := cur.([]any)
			if !ok || i < 0 || i >= len(arr) {
				return nil, false
			}
			cur = arr[i]
		}
	}
	return cur, true
}

func getKey(m map[string]any, k string) (any, bool) {
	if v, ok := m[k]; ok {
		return v, true
	}
	for mk, v := range m {
		if strings.EqualFold(mk, k) {
			return v, true
		}
	}
	return nil, false
}

// splitIndexes splits "items[2][0]" into "items" and [2 0].
func splitIndexes(seg string) (string, []int, error) {
	open := strings.IndexByte(seg, '[')
	if open < 0 {
		return seg, nil, nil
	}
	name := seg[:open]
	rest := seg[open:]
	var idx []int
	for rest != "" {
		if rest[0] != '[' {
			return "", nil, errors.New("bad index")
		}
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return "", nil, errors.New("bad index")
		}
		n, err := strconv.Atoi(rest[1:end])
		if err != nil {
			return "", nil, err
		}
		idx = append(idx, n)
		rest = rest[end+1:]
	}
	return name, idx, nil
}

// ---------------------------------------------------------------- AST

type node interface {
	eval(Fields) bool
	String() string
}

type andNode struct{ l, r node }
type orNode struct{ l, r node }
type notNode struct{ n node }

func (n andNode) eval(f Fields) bool { return n.l.eval(f) && n.r.eval(f) }
func (n orNode) eval(f Fields) bool  { return n.l.eval(f) || n.r.eval(f) }
func (n notNode) eval(f Fields) bool { return !n.n.eval(f) }
func (n andNode) String() string     { return "(" + n.l.String() + " AND " + n.r.String() + ")" }
func (n orNode) String() string      { return "(" + n.l.String() + " OR " + n.r.String() + ")" }
func (n notNode) String() string {
	if _, ok := n.n.(notNode); ok {
		return "-(" + n.n.String() + ")"
	}
	return "-" + n.n.String()
}

// textNode matches a word or phrase as a case-insensitive substring of the message.
type textNode struct {
	text   string // lower case
	phrase bool
}

func (n textNode) eval(f Fields) bool {
	return strings.Contains(strings.ToLower(f.Message()), n.text)
}

func (n textNode) String() string {
	if n.phrase || needsQuote(n.text) {
		return quote(n.text)
	}
	return n.text
}

type cmpOp int

const (
	opEq cmpOp = iota
	opGT
	opGE
	opLT
	opLE
	opRange
)

// attrNode matches one attribute.
type attrNode struct {
	name   string
	op     cmpOp
	value  string // lower case, for opEq
	quoted bool
	num    float64 // for numeric comparisons and opEq with a numeric value
	isNum  bool
	lo, hi float64 // opRange
	bare   bool    // written as replica:<id> without @
}

func (n attrNode) eval(f Fields) bool {
	v, ok := f.Attr(n.name)
	if !ok {
		return false
	}
	switch n.op {
	case opEq:
		return eqValue(v, n)
	case opRange:
		x, ok := toFloat(v)
		return ok && x >= n.lo && x <= n.hi
	default:
		x, ok := toFloat(v)
		if !ok {
			return false
		}
		switch n.op {
		case opGT:
			return x > n.num
		case opGE:
			return x >= n.num
		case opLT:
			return x < n.num
		case opLE:
			return x <= n.num
		}
	}
	return false
}

func eqValue(v any, n attrNode) bool {
	if arr, ok := v.([]any); ok {
		// An array attribute matches when any element matches.
		for _, e := range arr {
			if eqValue(e, n) {
				return true
			}
		}
		return false
	}
	if n.isNum {
		if x, ok := toFloat(v); ok {
			return x == n.num
		}
	}
	s := strings.ToLower(stringify(v))
	if strings.ContainsAny(n.value, "*?") {
		ok, err := path.Match(n.value, s)
		return err == nil && ok
	}
	if s == n.value {
		return true
	}
	// "@path:/api" matches /api and everything under it.
	if strings.EqualFold(n.name, "path") && strings.HasPrefix(n.value, "/") {
		return strings.HasPrefix(s, strings.TrimSuffix(n.value, "/")+"/") || s == strings.TrimSuffix(n.value, "/")
	}
	return false
}

func (n attrNode) String() string {
	prefix := "@" + n.name + ":"
	if n.bare {
		prefix = n.name + ":"
	}
	switch n.op {
	case opGT:
		return prefix + ">" + fmtNum(n.num)
	case opGE:
		return prefix + ">=" + fmtNum(n.num)
	case opLT:
		return prefix + "<" + fmtNum(n.num)
	case opLE:
		return prefix + "<=" + fmtNum(n.num)
	case opRange:
		return prefix + fmtNum(n.lo) + ".." + fmtNum(n.hi)
	}
	if n.quoted || needsQuote(n.value) {
		return prefix + quote(n.value)
	}
	return prefix + n.value
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	if isOperatorWord(s) || s[0] == '-' || s[0] == '@' {
		return true
	}
	return strings.ContainsAny(s, " \t\r\n\"()\\") || strings.Contains(s, ":")
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return fmtNum(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return fmt.Sprint(x)
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x)
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil && !math.IsNaN(f)
	}
	return 0, false
}

// ---------------------------------------------------------------- lexer

type tokKind int

const (
	tTerm   tokKind = iota // a word, @attr:value or replica:<id>
	tPhrase                // "quoted phrase"
	tLParen
	tRParen
	tAnd
	tOr
	tNeg // a leading '-' attached to the next term or group
)

type token struct {
	kind tokKind
	text string
	at   int // 1-based character position
	// quotedValue is set for @attr:"value" terms.
	quotedValue bool
}

func isOperatorWord(s string) bool {
	return strings.EqualFold(s, "AND") || strings.EqualFold(s, "OR")
}

func lex(s string) ([]token, error) {
	var toks []token
	rs := []rune(s)
	i := 0
	for i < len(rs) {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{kind: tLParen, text: "(", at: i + 1})
			i++
		case c == ')':
			toks = append(toks, token{kind: tRParen, text: ")", at: i + 1})
			i++
		case c == '-' && i+1 < len(rs) && (rs[i+1] == '(' || rs[i+1] == '"' || isWordStart(rs[i+1])):
			toks = append(toks, token{kind: tNeg, text: "-", at: i + 1})
			i++
		case c == '"':
			text, n, err := readQuoted(rs, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tPhrase, text: text, at: i + 1})
			i = n
		default:
			start := i
			var b strings.Builder
			quotedValue := false
			for i < len(rs) {
				c := rs[i]
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' || c == ')' {
					break
				}
				if c == '"' {
					// Only @attr:"value" may contain a quote, right after the colon.
					w := b.String()
					if strings.HasSuffix(w, ":") && strings.Count(w, ":") == 1 && (strings.HasPrefix(w, "@") || strings.EqualFold(w, "replica:")) {
						text, n, err := readQuoted(rs, i)
						if err != nil {
							return nil, err
						}
						b.WriteString(text)
						i = n
						quotedValue = true
						break
					}
					return nil, errorf("The filter has a quote inside a word at position %d. Put the whole phrase in quotes.", i+1)
				}
				b.WriteRune(c)
				i++
			}
			w := b.String()
			kind := tTerm
			if !quotedValue && isOperatorWord(w) {
				if strings.EqualFold(w, "AND") {
					kind = tAnd
				} else {
					kind = tOr
				}
			}
			toks = append(toks, token{kind: kind, text: w, at: start + 1, quotedValue: quotedValue})
		}
	}
	return toks, nil
}

func isWordStart(r rune) bool {
	return r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != ')' && r != '-'
}

// readQuoted reads a quoted string starting at rs[i] == '"'. Backslash
// escapes the next character. It returns the text and the index after the
// closing quote.
func readQuoted(rs []rune, i int) (string, int, error) {
	start := i
	i++
	var b strings.Builder
	for i < len(rs) {
		c := rs[i]
		if c == '\\' && i+1 < len(rs) {
			b.WriteRune(rs[i+1])
			i += 2
			continue
		}
		if c == '"' {
			return b.String(), i + 1, nil
		}
		b.WriteRune(c)
		i++
	}
	return "", 0, errorf("The filter has a quote at position %d that is never closed.", start+1)
}

// ---------------------------------------------------------------- parser

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() *token {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *parser) parseOr() (node, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != tOr {
			return l, nil
		}
		p.pos++
		if nt := p.peek(); nt == nil || nt.kind == tRParen || nt.kind == tOr || nt.kind == tAnd {
			return nil, errorf("The filter has OR at position %d with nothing after it.", t.at)
		}
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = orNode{l, r}
	}
}

func (p *parser) parseAnd() (node, error) {
	t := p.peek()
	if t != nil && (t.kind == tAnd || t.kind == tOr) {
		return nil, errorf("The filter starts a group with %s at position %d; put a term before it.", strings.ToUpper(t.text), t.at)
	}
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind == tOr || t.kind == tRParen {
			return l, nil
		}
		if t.kind == tAnd {
			p.pos++
			if nt := p.peek(); nt == nil || nt.kind == tRParen || nt.kind == tOr || nt.kind == tAnd {
				return nil, errorf("The filter has AND at position %d with nothing after it.", t.at)
			}
		}
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = andNode{l, r}
	}
}

func (p *parser) parseUnary() (node, error) {
	t := p.peek()
	if t == nil {
		return nil, errorf("The filter ends where a term was expected.")
	}
	if t.kind == tNeg {
		p.pos++
		n, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return notNode{n}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	t := p.peek()
	if t == nil {
		return nil, errorf("The filter ends where a term was expected.")
	}
	switch t.kind {
	case tLParen:
		p.pos++
		if nt := p.peek(); nt != nil && nt.kind == tRParen {
			return nil, errorf("The filter has empty parentheses at position %d.", t.at)
		}
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		ct := p.peek()
		if ct == nil || ct.kind != tRParen {
			return nil, errorf("The filter has an opening parenthesis at position %d that is never closed.", t.at)
		}
		p.pos++
		return n, nil
	case tRParen:
		return nil, errorf("The filter has a closing parenthesis without an opening one at position %d.", t.at)
	case tPhrase:
		p.pos++
		return textNode{text: strings.ToLower(t.text), phrase: true}, nil
	case tTerm:
		p.pos++
		return parseTerm(*t)
	case tNeg:
		return nil, errorf("The filter has a double negation at position %d.", t.at)
	default:
		return nil, errorf("The filter has %s at position %d where a term was expected.", strings.ToUpper(t.text), t.at)
	}
}

func parseTerm(t token) (node, error) {
	w := t.text
	bare := false
	switch {
	case strings.HasPrefix(w, "@"):
		w = w[1:]
	case len(w) > len("replica:") && strings.EqualFold(w[:len("replica:")], "replica:"):
		bare = true
	case strings.EqualFold(w, "replica:") && t.quotedValue:
		bare = true
	default:
		return textNode{text: strings.ToLower(w)}, nil
	}
	colon := strings.IndexByte(w, ':')
	if colon <= 0 {
		return nil, errorf("The filter term %q at position %d needs the form @name:value.", t.text, t.at)
	}
	name := w[:colon]
	val := w[colon+1:]
	if bare {
		name = "replica"
	}
	if err := checkAttrName(name); err != nil {
		return nil, errorf("The filter term %q at position %d has %v", t.text, t.at, err)
	}
	n := attrNode{name: name, bare: bare, quoted: t.quotedValue}
	if t.quotedValue {
		n.op = opEq
		n.value = strings.ToLower(val)
		return n, nil
	}
	if val == "" {
		return nil, errorf("The filter term %q at position %d has no value after the colon.", t.text, t.at)
	}
	for _, op := range []struct {
		s  string
		op cmpOp
	}{{">=", opGE}, {"<=", opLE}, {">", opGT}, {"<", opLT}} {
		if strings.HasPrefix(val, op.s) {
			num, err := strconv.ParseFloat(val[len(op.s):], 64)
			if err != nil || math.IsNaN(num) || math.IsInf(num, 0) {
				return nil, errorf("The filter term %q at position %d compares with %q, which is not a number.", t.text, t.at, val[len(op.s):])
			}
			n.op = op.op
			n.num = num
			return n, nil
		}
	}
	if i := strings.Index(val, ".."); i > 0 {
		lo, err1 := strconv.ParseFloat(val[:i], 64)
		hi, err2 := strconv.ParseFloat(val[i+2:], 64)
		if err1 == nil && err2 == nil && !math.IsNaN(lo) && !math.IsNaN(hi) && !math.IsInf(lo, 0) && !math.IsInf(hi, 0) {
			if lo > hi {
				return nil, errorf("The filter range %q at position %d starts above where it ends.", val, t.at)
			}
			n.op = opRange
			n.lo, n.hi = lo, hi
			return n, nil
		}
	}
	n.op = opEq
	n.value = strings.ToLower(val)
	if f, err := strconv.ParseFloat(val, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		n.isNum = true
		n.num = f
	}
	return n, nil
}

func checkAttrName(name string) error {
	if _, _, err := splitIndexes(lastSeg(name)); err != nil {
		return errorf("an array index that is not a number in brackets.")
	}
	for _, seg := range strings.Split(name, ".") {
		if seg == "" {
			return errorf("an empty part in the attribute name.")
		}
		if _, _, err := splitIndexes(seg); err != nil {
			return errorf("an array index that is not a number in brackets.")
		}
	}
	return nil
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// errorf builds an error whose text is a full sentence for people to read
// (docs/SPEC.md 0: real error messages are full sentences).
func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
