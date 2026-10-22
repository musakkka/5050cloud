// SPDX-License-Identifier: MIT

package configfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// jsonLines maps each object key path to the line of the key, using the
// streaming decoder's input offsets.
func jsonLines(data []byte) map[string]int {
	lines := map[string]int{}
	dec := json.NewDecoder(bytes.NewReader(data))
	type frame struct {
		obj    bool
		key    string
		expect bool // next string token is a key
	}
	var stack []frame
	path := func(k string) string {
		var parts []string
		for _, f := range stack {
			if f.obj && f.key != "" {
				parts = append(parts, f.key)
			}
		}
		// the last frame's key is the one being replaced
		if len(parts) > 0 && stack[len(stack)-1].obj && stack[len(stack)-1].key != "" {
			parts = parts[:len(parts)-1]
		}
		return strings.Join(append(parts, k), ".")
	}
	afterValue := func() {
		if n := len(stack); n > 0 && stack[n-1].obj {
			stack[n-1].expect = true
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return lines
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, frame{obj: true, expect: true})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				afterValue()
			}
		default:
			n := len(stack)
			if n > 0 && stack[n-1].obj && stack[n-1].expect {
				k, _ := v.(string)
				p := path(k)
				lines[p] = lineAt(data, int(dec.InputOffset()))
				stack[n-1].key = k
				stack[n-1].expect = false
				continue
			}
			afterValue()
		}
	}
}

// yamlLines maps key paths of block-style YAML to their lines by
// indentation. Flow-style mappings are not mapped (their fields get no line).
func yamlLines(data []byte) map[string]int {
	lines := map[string]int{}
	type level struct {
		indent int
		key    string
	}
	var stack []level
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), MaxFileBytes)
	n := 0
	for sc.Scan() {
		n++
		raw := sc.Text()
		trimmed := strings.TrimLeft(raw, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			continue
		}
		indent := len(raw) - len(trimmed)
		key, ok := yamlKey(trimmed)
		if !ok {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		parts := make([]string, 0, len(stack)+1)
		for _, l := range stack {
			parts = append(parts, l.key)
		}
		parts = append(parts, key)
		lines[strings.Join(parts, ".")] = n
		stack = append(stack, level{indent, key})
	}
	return lines
}

func yamlKey(s string) (string, bool) {
	if s[0] == '"' || s[0] == '\'' {
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return "", false
		}
		key := s[1 : end+1]
		rest := strings.TrimLeft(s[end+2:], " ")
		return key, strings.HasPrefix(rest, ":")
	}
	i := strings.Index(s, ":")
	if i <= 0 {
		return "", false
	}
	if i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '\t' {
		return "", false // "http://x" style scalars
	}
	key := strings.TrimSpace(s[:i])
	if strings.ContainsAny(key, "{}[],") {
		return "", false
	}
	return key, true
}

// tomlLines maps keys to lines for tables ([deploy],
// [environments.production.deploy]) and dotted keys.
func tomlLines(data []byte) map[string]int {
	lines := map[string]int{}
	table := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), MaxFileBytes)
	n := 0
	inMulti := ""
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if inMulti != "" {
			if strings.Contains(line, inMulti) {
				inMulti = ""
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.LastIndex(line, "]")
			if end < 0 {
				continue
			}
			name := strings.Trim(line[:end+1], "[]")
			table = tomlPath(name)
			if table != "" {
				lines[table] = n
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := tomlPath(strings.TrimSpace(line[:eq]))
		if key == "" {
			continue
		}
		full := key
		if table != "" {
			full = table + "." + key
		}
		lines[full] = n
		val := strings.TrimSpace(line[eq+1:])
		for _, q := range []string{`"""`, `'''`} {
			if strings.HasPrefix(val, q) && strings.Count(val, q) == 1 {
				inMulti = q
			}
		}
	}
	return lines
}

// tomlPath normalises a.b."c d" into a.b.c d.
func tomlPath(s string) string {
	var parts []string
	for _, p := range splitDotted(s) {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p == "" {
			return ""
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ".")
}

func splitDotted(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == '.':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, cur.String())
}
