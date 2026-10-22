// SPDX-License-Identifier: AGPL-3.0-only

package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"
)

// VerifySignature checks X-Hub-Signature-256 ("sha256=<hex>") over body.
func VerifySignature(secret string, body []byte, header string) bool {
	if secret == "" || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hmac.Equal(m.Sum(nil), want)
}

// Sign returns the X-Hub-Signature-256 value for body (tests and the dev server).
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Push is the part of a push event core needs.
type Push struct {
	Ref           string // refs/heads/main
	Branch        string // main (empty for tag pushes)
	Before, After string
	Created       bool
	Deleted       bool
	Forced        bool
	Repo          string // owner/name
	DefaultBranch string
	Private       bool
	Compare       string
	// Files are the changed paths of every commit; Truncated is set when
	// the payload carried 20 or more commits (GitHub truncates the list).
	Files     []string
	Truncated bool
	Head      Commit
}

// Commit is a commit as the deploy pipeline shows it.
type Commit struct {
	SHA     string
	Author  string
	Message string
	URL     string
	Time    int64
}

const zeroSHA = "0000000000000000000000000000000000000000"

// FirstPush reports whether this push creates the branch (deploy everything).
func (p Push) FirstPush() bool { return p.Created || p.Before == zeroSHA || p.Before == "" }

// ParsePush decodes a push event body.
func ParsePush(body []byte) (Push, error) {
	var raw struct {
		Ref     string `json:"ref"`
		Before  string `json:"before"`
		After   string `json:"after"`
		Created bool   `json:"created"`
		Deleted bool   `json:"deleted"`
		Forced  bool   `json:"forced"`
		Compare string `json:"compare"`
		Repo    struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			Private       bool   `json:"private"`
		} `json:"repository"`
		Commits []struct {
			Added    []string `json:"added"`
			Removed  []string `json:"removed"`
			Modified []string `json:"modified"`
		} `json:"commits"`
		Head *struct {
			ID        string `json:"id"`
			Message   string `json:"message"`
			URL       string `json:"url"`
			Timestamp string `json:"timestamp"`
			Author    struct {
				Name     string `json:"name"`
				Username string `json:"username"`
			} `json:"author"`
		} `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Push{}, err
	}
	p := Push{
		Ref: raw.Ref, Before: raw.Before, After: raw.After, Created: raw.Created, Deleted: raw.Deleted,
		Forced: raw.Forced, Compare: raw.Compare, Repo: raw.Repo.FullName, DefaultBranch: raw.Repo.DefaultBranch,
		Private: raw.Repo.Private, Truncated: len(raw.Commits) >= 20,
	}
	if strings.HasPrefix(raw.Ref, "refs/heads/") {
		p.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
	}
	seen := map[string]bool{}
	for _, c := range raw.Commits {
		for _, list := range [][]string{c.Added, c.Removed, c.Modified} {
			for _, f := range list {
				if !seen[f] {
					seen[f] = true
					p.Files = append(p.Files, f)
				}
			}
		}
	}
	if raw.Head != nil {
		author := raw.Head.Author.Username
		if author == "" {
			author = raw.Head.Author.Name
		}
		p.Head = Commit{SHA: raw.Head.ID, Message: raw.Head.Message, URL: raw.Head.URL, Author: author, Time: parseTime(raw.Head.Timestamp)}
	}
	return p, nil
}

// MatchWatch reports whether any changed file matches the watch patterns
// (gitignore style: "**", "*", "?", "!" negation, later patterns win; a
// pattern without a slash matches at any depth; a directory pattern
// matches everything below it). No patterns match everything.
func MatchWatch(patterns, files []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, f := range files {
		if watched(patterns, strings.TrimPrefix(f, "/")) {
			return true
		}
	}
	return false
}

func watched(patterns []string, file string) bool {
	match := false
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		neg := strings.HasPrefix(p, "!")
		if neg {
			p = p[1:]
		}
		if matchPattern(p, file) {
			match = !neg
		}
	}
	return match
}

func matchPattern(p, file string) bool {
	anchored := strings.HasPrefix(p, "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return true
	}
	if !anchored && !strings.Contains(p, "/") {
		p = "**/" + p
	}
	// The file itself or any parent directory may match.
	parts := strings.Split(file, "/")
	for i := len(parts); i >= 1; i-- {
		if globMatch(p, strings.Join(parts[:i], "/")) {
			return true
		}
	}
	return false
}

// globMatch matches pattern segments with ** spanning any number of directories.
func globMatch(pattern, name string) bool {
	return segMatch(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func segMatch(ps, ns []string) bool {
	for len(ps) > 0 {
		if ps[0] == "**" {
			rest := ps[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(ns); i++ {
				if segMatch(rest, ns[i:]) {
					return true
				}
			}
			return false
		}
		if len(ns) == 0 {
			return false
		}
		ok, err := path.Match(ps[0], ns[0])
		if err != nil || !ok {
			return false
		}
		ps, ns = ps[1:], ns[1:]
	}
	return len(ns) == 0
}
