// SPDX-License-Identifier: MIT

package cli

import (
	"bufio"
	"os"
	"path"
	"regexp"
	"strings"
)

// ignoreRule is one line of a .gitignore or .5050ignore file.
type ignoreRule struct {
	base    string // directory of the ignore file, relative to the archive root ("" = root)
	neg     bool
	dirOnly bool
	full    bool // the pattern has a slash, so it matches the whole relative path
	re      *regexp.Regexp
}

// ignoreSet holds the rules collected while walking; later rules win.
type ignoreSet struct{ rules []ignoreRule }

// load reads an ignore file in directory base (relative, slash separated).
func (s *ignoreSet) load(file, base string) error {
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s.add(sc.Text(), base)
	}
	return sc.Err()
}

func (s *ignoreSet) add(line, base string) {
	line = strings.TrimRight(line, "\r")
	// Trailing spaces are ignored unless escaped.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\\ ") {
		line = line[:len(line)-1]
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	r := ignoreRule{base: base}
	if strings.HasPrefix(line, "!") {
		r.neg = true
		line = line[1:]
	} else if strings.HasPrefix(line, "\\!") || strings.HasPrefix(line, "\\#") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		r.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return
	}
	if strings.Contains(line, "/") {
		r.full = true
		line = strings.TrimPrefix(line, "/")
	}
	re, err := regexp.Compile("^" + globToRegexp(line) + "$")
	if err != nil {
		return
	}
	r.re = re
	s.rules = append(s.rules, r)
}

// globToRegexp converts gitignore glob syntax to a regular expression.
func globToRegexp(g string) string {
	var b strings.Builder
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch {
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(g[i:], "/**") && i+3 == len(g):
			b.WriteString("(?:/.*)?")
			i += 2
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			j := strings.IndexByte(g[i+1:], ']')
			if j < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := g[i+1 : i+1+j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += j + 1
		case c == '\\' && i+1 < len(g):
			i++
			b.WriteString(regexp.QuoteMeta(string(g[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// ignored reports whether rel (slash separated, relative to the archive root) is ignored.
func (s *ignoreSet) ignored(rel string, isDir bool) bool {
	out := false
	for _, r := range s.rules {
		sub := rel
		if r.base != "" {
			if !strings.HasPrefix(rel, r.base+"/") {
				continue
			}
			sub = rel[len(r.base)+1:]
		}
		if r.dirOnly && !isDir {
			continue
		}
		target := sub
		if !r.full {
			target = path.Base(sub)
		}
		if r.re.MatchString(target) {
			out = !r.neg
		}
	}
	return out
}
