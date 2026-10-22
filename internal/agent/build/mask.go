// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"sort"
	"strings"
)

// maskText replaces secret values in log lines.
const maskText = "*****"

// minMaskLen is the shortest value that is masked. Shorter values ("1",
// "on") would turn every log line into stars without protecting anything.
const minMaskLen = 4

// notSecret are common configuration values that appear in build output
// all the time and are never secrets on their own.
var notSecret = map[string]bool{
	"true": true, "false": true, "null": true, "none": true, "yes": true,
	"production": true, "development": true, "staging": true, "test": true,
	"debug": true, "info": true, "warn": true, "error": true,
	"/app": true, "8080": true, "3000": true, "5000": true, "localhost": true,
}

// masker hides every secret value it was given.
type masker struct {
	r *strings.Replacer
}

// newMasker builds a masker for values. Values are matched longest first so
// a value that contains another is hidden whole.
func newMasker(values ...string) *masker {
	seen := map[string]bool{}
	var vals []string
	add := func(v string) {
		if len(v) < minMaskLen || notSecret[strings.ToLower(v)] || seen[v] {
			return
		}
		seen[v] = true
		vals = append(vals, v)
	}
	for _, v := range values {
		add(v)
		// A multi-line value (a PEM key) can be printed line by line.
		if strings.Contains(v, "\n") {
			for _, part := range strings.Split(v, "\n") {
				add(strings.TrimSpace(part))
			}
		}
	}
	sort.Slice(vals, func(i, j int) bool {
		if len(vals[i]) != len(vals[j]) {
			return len(vals[i]) > len(vals[j])
		}
		return vals[i] < vals[j]
	})
	if len(vals) == 0 {
		return &masker{}
	}
	pairs := make([]string, 0, 2*len(vals))
	for _, v := range vals {
		pairs = append(pairs, v, maskText)
	}
	return &masker{r: strings.NewReplacer(pairs...)}
}

// Mask returns s with every secret value replaced.
func (m *masker) Mask(s string) string {
	if m == nil || m.r == nil {
		return s
	}
	return m.r.Replace(s)
}
