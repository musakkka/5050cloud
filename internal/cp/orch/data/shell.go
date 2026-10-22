// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// commandArgv turns a start command into argv the way the agent runs
// replicas (proto.ReplicaSpec.Cmd): a command with shell operators or
// expansions runs as ["sh", "-c", cmd]; a plain one is split on spaces with
// single and double quotes honoured. An empty command returns nil (the
// image's own command).
func commandArgv(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	if strings.ContainsAny(cmd, "|&;<>()$`*?~\n\\#") {
		return []string{"sh", "-c", cmd}
	}
	var (
		out   []string
		cur   strings.Builder
		quote rune
		have  bool
	)
	for _, r := range cmd {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, have = r, true
		case r == ' ' || r == '\t':
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if quote != 0 {
		return []string{"sh", "-c", cmd}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// shellQuote quotes one argument for POSIX sh.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`|&;<>()*?[]{}~#!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// shellJoin quotes argv into one shell command line.
func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

// lastJSONLine finds the last line of output that parses as a JSON object.
// Backup commands print their summary that way (manifest.BackupRecipe).
func lastJSONLine(output string, v any) error {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "{") && json.Unmarshal([]byte(l), v) == nil {
			return nil
		}
	}
	return errors.New("the command printed no JSON summary line")
}

// tailLines returns the last n non-empty lines of output, for messages.
func tailLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "{") {
			keep = append([]string{l}, keep...)
		}
	}
	return strings.Join(keep, " ")
}

// sentence makes sure a message ends like a sentence.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

// humanBytes formats a size for messages ("1.4 GB", "350 MB").
func humanBytes(n int64) string {
	const (
		mb = 1 << 20
		gb = 1 << 30
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%d MB", n/mb)
	default:
		return fmt.Sprintf("%d KB", n/1024)
	}
}
