// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"regexp"
	"sort"
	"strings"
)

// Dockerfile scanning (docs/SPEC.md 8.2 step 5). Only variables named by an
// ARG line become build arguments, the same rule as Railway. Variables a RUN
// step mounts as a BuildKit secret (RUN --mount=type=secret,id=NAME) are
// passed as secrets, never as build arguments.

// dockerfileInfo is what the builder needs from a Dockerfile.
type dockerfileInfo struct {
	Args    []string // ARG names in file order, deduplicated
	Secrets []string // ids of secret mounts, sorted
}

var (
	argNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	mountRe   = regexp.MustCompile(`--mount=(\S+)`)
)

// parseDockerfile reads ARG names and secret mount ids. It joins line
// continuations, skips comments and heredoc bodies, and accepts several
// names on one ARG line ("ARG A B=1").
func parseDockerfile(src string) dockerfileInfo {
	var info dockerfileInfo
	seenArg := map[string]bool{}
	seenSecret := map[string]bool{}
	escape := byte('\\')
	for i, raw := range logicalLines(src, &escape) {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		instr := strings.ToUpper(fields[0])
		switch instr {
		case "ARG":
			for _, f := range fields[1:] {
				name := f
				if j := strings.IndexByte(f, '='); j >= 0 {
					name = f[:j]
				}
				if argNameRe.MatchString(name) && !seenArg[name] {
					seenArg[name] = true
					info.Args = append(info.Args, name)
				}
			}
		case "RUN":
			for _, m := range mountRe.FindAllStringSubmatch(line, -1) {
				if id := secretMountID(m[1]); id != "" && !seenSecret[id] {
					seenSecret[id] = true
					info.Secrets = append(info.Secrets, id)
				}
			}
		}
	}
	sort.Strings(info.Secrets)
	return info
}

// secretMountID returns the id of a type=secret mount spec, or "".
// Without id=, BuildKit uses the base name of target=.
func secretMountID(spec string) string {
	var typ, id, target string
	for _, kv := range strings.Split(spec, ",") {
		k, v, _ := strings.Cut(kv, "=")
		v = strings.Trim(v, `"'`)
		switch strings.ToLower(k) {
		case "type":
			typ = strings.ToLower(v)
		case "id":
			id = v
		case "target", "dst", "destination":
			target = v
		}
	}
	if typ != "secret" {
		return ""
	}
	if id == "" && target != "" {
		id = target[strings.LastIndex(target, "/")+1:]
	}
	if !argNameRe.MatchString(id) {
		return ""
	}
	return id
}

var escapeDirectiveRe = regexp.MustCompile(`(?i)^#\s*escape\s*=\s*(\S)`)

// logicalLines joins continuation lines, drops comment lines and heredoc
// bodies. The first returned line may be a parser directive.
func logicalLines(src string, escape *byte) []string {
	var out []string
	var cur strings.Builder
	heredoc := ""
	directives := true
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if heredoc != "" {
			if trimmed == heredoc {
				heredoc = ""
			}
			continue
		}
		if directives {
			if m := escapeDirectiveRe.FindStringSubmatch(trimmed); m != nil {
				*escape = m[1][0]
				continue
			}
			if !strings.HasPrefix(trimmed, "#") || trimmed == "" {
				directives = false
			}
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 0 && trimmed[len(trimmed)-1] == *escape {
			cur.WriteString(trimmed[:len(trimmed)-1])
			cur.WriteByte(' ')
			continue
		}
		cur.WriteString(trimmed)
		full := cur.String()
		cur.Reset()
		if m := heredocRe.FindStringSubmatch(full); m != nil {
			heredoc = m[1]
		}
		out = append(out, full)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

var heredocRe = regexp.MustCompile(`<<-?["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)
