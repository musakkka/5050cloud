// SPDX-License-Identifier: AGPL-3.0-only

package logstore

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// MaxLineBytes caps one stored line (docs/SPEC.md 8.1).
const MaxLineBytes = 8 << 10

// MaxLinesPerSecond is the per-replica rate above which lines are dropped.
const MaxLinesPerSecond = 500

// Mask replaces secret values in log lines (docs/SPEC.md 9.15).
const Mask = "•••"

// Normalized levels.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

var (
	ansiRe        = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
	levelKVRe     = regexp.MustCompile(`(?i)(?:^|[\s,{])(?:level|lvl|severity)=["']?([a-z]+|[0-9]+)`)
	leadingTokRe  = regexp.MustCompile(`(?i)^\s*(?:\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\s+)?[\[(<]?(trace|debug|dbug|info|notice|warn|warning|error|err|eror|fatal|critical|crit|panic|alert|emerg)[\])>]?(?::|\s|$)`)
	errorWordsRe  = regexp.MustCompile(`(?i)\b(error|exception|panic|fatal|traceback|uncaught|segfault)\b`)
	secretNameRe  = regexp.MustCompile(`(?i)(SECRET|TOKEN|PASSWORD|PASSWD|PASS|KEY|PRIVATE|CREDENTIAL|DSN|AUTH)`)
	minSecretSize = 6
)

// StripANSI removes terminal escape sequences.
func StripANSI(b []byte) []byte {
	if bytes.IndexByte(b, 0x1b) < 0 {
		return b
	}
	return ansiRe.ReplaceAll(b, nil)
}

// CapLine cuts a line to MaxLineBytes on a UTF-8 boundary.
func CapLine(s string) string {
	if len(s) <= MaxLineBytes {
		return s
	}
	cut := MaxLineBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// NormalizeLevel maps the many spellings of a level to debug, info, warn or
// error. It returns "" for something it does not know.
func NormalizeLevel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "trace", "debug", "dbug", "verbose", "silly":
		return LevelDebug
	case "info", "information", "notice", "log", "inf":
		return LevelInfo
	case "warn", "warning", "wrn":
		return LevelWarn
	case "error", "err", "eror", "fatal", "critical", "crit", "panic", "alert", "emerg", "emergency", "severe":
		return LevelError
	}
	if n, err := strconv.Atoi(s); err == nil {
		return numericLevel(float64(n))
	}
	return ""
}

// numericLevel maps pino/bunyan numbers (10 trace ... 60 fatal).
func numericLevel(n float64) string {
	switch {
	case n >= 50:
		return LevelError
	case n >= 40:
		return LevelWarn
	case n >= 30:
		return LevelInfo
	case n >= 10:
		return LevelDebug
	}
	return ""
}

// DetectLevel finds a line's level: an explicit JSON level or severity,
// then level=, then a leading [ERROR]-style token, then error words.
// stderr alone means nothing, so the default is info.
func DetectLevel(line string, attrs map[string]any) string {
	if attrs != nil {
		for _, k := range []string{"level", "severity", "lvl", "log.level"} {
			if v, ok := attrs[k]; ok {
				switch x := v.(type) {
				case string:
					if l := NormalizeLevel(x); l != "" {
						return l
					}
				case float64:
					if l := numericLevel(x); l != "" {
						return l
					}
				}
			}
		}
	}
	if m := levelKVRe.FindStringSubmatch(line); m != nil {
		if l := NormalizeLevel(m[1]); l != "" {
			return l
		}
	}
	if m := leadingTokRe.FindStringSubmatch(line); m != nil {
		if l := NormalizeLevel(m[1]); l != "" {
			return l
		}
	}
	if errorWordsRe.MatchString(line) {
		return LevelError
	}
	return LevelInfo
}

// ParseJSON returns the attributes of a JSON object line and its message
// (the msg or message field, else the whole line).
func ParseJSON(line string) (map[string]any, string, bool) {
	t := strings.TrimSpace(line)
	if len(t) < 2 || t[0] != '{' || t[len(t)-1] != '}' {
		return nil, line, false
	}
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, line, false
	}
	normalizeNumbers(m)
	msg := line
	for _, k := range []string{"msg", "message", "MESSAGE"} {
		if s, ok := m[k].(string); ok {
			msg = s
			break
		}
	}
	return m, msg, true
}

// normalizeNumbers turns json.Number into float64 (or keeps big integers as strings).
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeNumbers(e)
		}
		return x
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	}
	return v
}

// Masker replaces secret values with Mask.
type Masker struct {
	r *strings.Replacer
}

// IsSecretName reports whether a variable name looks secret.
func IsSecretName(name string) bool { return secretNameRe.MatchString(name) }

// NewMasker builds a masker from a replica's resolved environment: values
// of variables whose name looks secret, and the password part of any
// URL-valued variable, when they are at least 6 characters long. extra adds
// values that are secret regardless of their name (sealed variables).
func NewMasker(env map[string]string, extra ...string) *Masker {
	set := map[string]bool{}
	for k, v := range env {
		if len(v) >= minSecretSize && IsSecretName(k) {
			set[v] = true
		}
		if strings.Contains(v, "://") {
			if u, err := url.Parse(v); err == nil && u.User != nil {
				if p, ok := u.User.Password(); ok && len(p) >= minSecretSize {
					set[p] = true
					if esc := url.QueryEscape(p); esc != p {
						set[esc] = true
					}
				}
			}
		}
	}
	for _, v := range extra {
		if len(v) >= minSecretSize {
			set[v] = true
		}
	}
	if len(set) == 0 {
		return &Masker{}
	}
	vals := make([]string, 0, len(set))
	for v := range set {
		vals = append(vals, v)
	}
	// Longest first, so a secret that contains another is masked whole.
	sort.Slice(vals, func(i, j int) bool {
		if len(vals[i]) != len(vals[j]) {
			return len(vals[i]) > len(vals[j])
		}
		return vals[i] < vals[j]
	})
	pairs := make([]string, 0, 2*len(vals))
	for _, v := range vals {
		pairs = append(pairs, v, Mask)
	}
	return &Masker{r: strings.NewReplacer(pairs...)}
}

// Apply masks s.
func (m *Masker) Apply(s string) string {
	if m == nil || m.r == nil {
		return s
	}
	return m.r.Replace(s)
}

// Source identifies where lines come from.
type Source struct {
	EnvironmentID string
	ServiceID     string
	DeploymentID  string
	ReplicaID     string
}

// Processor turns raw container output into LogRecords for one replica:
// it strips ANSI, caps lines, masks secrets, parses JSON, detects the level
// and drops lines above MaxLinesPerSecond with one warning per second.
type Processor struct {
	src    Source
	mask   *Masker
	now    func() time.Time
	mu     sync.Mutex
	sec    int64
	count  int
	drop   int
	lastSE string // last stderr line, for the "exits right after start" diagnosis
}

// NewProcessor returns a processor for one replica.
func NewProcessor(src Source, mask *Masker) *Processor {
	return &Processor{src: src, mask: mask, now: time.Now}
}

// Line processes one raw line. It returns the records to store: none when
// the line is dropped, and a warning record first when lines were dropped
// in the previous second.
func (p *Processor) Line(stream string, ts time.Time, raw []byte) []proto.LogRecord {
	if ts.IsZero() {
		ts = p.now()
	}
	p.mu.Lock()
	var out []proto.LogRecord
	sec := ts.Unix()
	if sec != p.sec {
		if p.drop > 0 {
			out = append(out, p.dropRecord(p.drop, time.Unix(p.sec, 999e6)))
		}
		p.sec, p.count, p.drop = sec, 0, 0
	}
	p.count++
	if p.count > MaxLinesPerSecond {
		p.drop++
		p.mu.Unlock()
		return out
	}
	p.mu.Unlock()

	text := CapLine(string(StripANSI(raw)))
	text = strings.TrimRight(text, "\r")
	text = p.mask.Apply(text)
	attrs, msg, _ := ParseJSON(text)
	rec := proto.LogRecord{
		TS: ts.UnixMilli(), ServiceID: p.src.ServiceID, DeploymentID: p.src.DeploymentID, ReplicaID: p.src.ReplicaID,
		Stream: stream, Level: DetectLevel(text, attrs), Msg: msg, Attrs: attrs,
	}
	if stream == "stderr" && strings.TrimSpace(msg) != "" {
		p.mu.Lock()
		p.lastSE = msg
		p.mu.Unlock()
	}
	return append(out, rec)
}

// Flush returns the pending drop warning, if any, for a second that ended.
func (p *Processor) Flush(now time.Time) []proto.LogRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.drop == 0 || now.Unix() == p.sec {
		return nil
	}
	r := p.dropRecord(p.drop, time.Unix(p.sec, 999e6))
	p.drop, p.count, p.sec = 0, 0, now.Unix()
	return []proto.LogRecord{r}
}

// LastStderr returns the last non-empty stderr line.
func (p *Processor) LastStderr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSE
}

// System returns a record written by the agent itself about this replica
// (stream "system"), such as "Pulling image ...".
func (p *Processor) System(level, msg string) proto.LogRecord {
	return proto.LogRecord{TS: p.now().UnixMilli(), ServiceID: p.src.ServiceID, DeploymentID: p.src.DeploymentID,
		ReplicaID: p.src.ReplicaID, Stream: "system", Level: level, Msg: msg}
}

func (p *Processor) dropRecord(n int, at time.Time) proto.LogRecord {
	return proto.LogRecord{TS: at.UnixMilli(), ServiceID: p.src.ServiceID, DeploymentID: p.src.DeploymentID, ReplicaID: p.src.ReplicaID,
		Stream: "system", Level: LevelWarn, Msg: strconv.Itoa(n) + " log lines dropped: more than 500 lines per second"}
}
