// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// specCommands reads the command table of docs/SPEC.md section 12 and
// returns each command name with its description.
func specCommands(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile("../../docs/SPEC.md")
	if err != nil {
		t.Skip("docs/SPEC.md not found")
	}
	s := string(b)
	start := strings.Index(s, "## 12. CLI")
	end := strings.Index(s[start:], "\n## 13.")
	sec := s[start : start+end]
	tick := regexp.MustCompile("`([^`]+)`")
	out := map[string]string{}
	for _, line := range strings.Split(sec, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cols := strings.Split(line, " | ")
		if len(cols) < 2 {
			continue
		}
		desc := strings.TrimSpace(strings.ReplaceAll(cols[1], `\|`, "|"))
		ms := tick.FindAllStringSubmatch(cols[0], -1)
		if len(ms) > 1 || strings.Trim(desc, "| ") == "" {
			desc = "" // shared or empty description: only the name is checked
		}
		for _, m := range ms {
			name := strings.Fields(m[1])[0]
			if strings.HasPrefix(name, "-") {
				continue
			}
			out[name] = desc
		}
	}
	return out
}

func TestHelpMatchesSpec(t *testing.T) {
	cmds := specCommands(t)
	if len(cmds) < 30 {
		t.Fatalf("read only %d commands from section 12", len(cmds))
	}
	root := (&App{}).Root()
	for name, desc := range cmds {
		c, _, err := root.Find([]string{name})
		if err != nil || c == root {
			t.Errorf("section 12 lists %q but the CLI has no such command", name)
			continue
		}
		if desc == "" || strings.Contains(desc, "`") {
			continue
		}
		if c.Short != desc {
			t.Errorf("%s: help says %q, section 12 says %q", name, c.Short, desc)
		}
	}
}

func TestPublicTextHasNoForbiddenWords(t *testing.T) {
	root := (&App{}).Root()
	for _, c := range root.Commands() {
		for _, s := range []string{c.Short, c.Long} {
			if strings.Contains(strings.ToLower(s), "b"+"ets") || strings.ContainsRune(s, 0x2014) {
				t.Errorf("%s: forbidden text in %q", c.Name(), s)
			}
		}
	}
}
