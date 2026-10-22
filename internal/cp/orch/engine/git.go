// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// GitResolver finds the commit a build checks out. The default asks the
// repository over Git's smart HTTP protocol (the same request `git
// ls-remote` makes), so 5050d needs no git binary, and fills the commit
// message and author from the GitHub API for github.com repositories.
type GitResolver interface {
	// Resolve returns the commit of ref ("" or "latest" = the branch head,
	// a branch name, or a full SHA) in repo (an https clone URL).
	Resolve(ctx context.Context, cloneURL, branch, ref, token string) (api.Commit, error)
}

// httpGit is the default GitResolver.
type httpGit struct {
	client *http.Client
	// apiBase is the GitHub API root (tests point it elsewhere).
	apiBase string
}

func newHTTPGit() *httpGit {
	return &httpGit{client: &http.Client{Timeout: 20 * time.Second}, apiBase: "https://api.github.com"}
}

// cloneURL turns "owner/name", "github.com/owner/name" or a URL into an https clone URL.
func cloneURL(repo string) (string, error) {
	r := strings.TrimSpace(repo)
	if r == "" {
		return "", errorf("This service has no repository. Set one in Settings > Source.")
	}
	if strings.HasPrefix(r, "http://") {
		return "", errorf("The repository URL %s uses http. Use an https URL.", r)
	}
	if !strings.HasPrefix(r, "https://") {
		r = strings.TrimPrefix(r, "github.com/")
		if strings.Count(strings.Trim(r, "/"), "/") != 1 {
			return "", errorf("The repository %q is not in the form owner/name or an https URL.", repo)
		}
		r = "https://github.com/" + strings.Trim(r, "/")
	}
	u, err := url.Parse(r)
	if err != nil || u.Host == "" {
		return "", errorf("The repository URL %q is not valid.", repo)
	}
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path = strings.TrimSuffix(u.Path, "/") + ".git"
	}
	u.User = nil
	return u.String(), nil
}

func isSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (g *httpGit) Resolve(ctx context.Context, clone, branch, ref, token string) (api.Commit, error) {
	ref = strings.TrimSpace(ref)
	if ref == "latest" {
		ref = ""
	}
	var sha, br string
	if isSHA(strings.ToLower(ref)) {
		sha, br = strings.ToLower(ref), branch
	} else {
		if ref != "" {
			branch = ref
		}
		refs, head, err := g.lsRemote(ctx, clone, token)
		if err != nil {
			return api.Commit{}, err
		}
		if branch == "" {
			branch = head
		}
		if branch == "" {
			return api.Commit{}, errorf("The repository %s has no default branch. Set a branch in Settings > Source.", displayRepo(clone))
		}
		sha = refs["refs/heads/"+branch]
		if sha == "" {
			return api.Commit{}, errorf("The branch %s does not exist in %s. Check the branch in Settings > Source.", branch, displayRepo(clone))
		}
		br = branch
	}
	c := api.Commit{SHA: sha, Branch: br}
	g.fillGitHub(ctx, clone, token, &c)
	return c, nil
}

func displayRepo(clone string) string {
	return strings.TrimSuffix(strings.TrimPrefix(clone, "https://"), ".git")
}

// lsRemote reads the advertised refs (pkt-line format) and the default branch.
func (g *httpGit) lsRemote(ctx context.Context, clone, token string) (map[string]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clone+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "git/2.43.0 (5050cloud)")
	if token != "" {
		req.SetBasicAuth("x-access-token", token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, "", errorf("The repository %s could not be reached: %s.", displayRepo(clone), strings.TrimSuffix(err.Error(), "."))
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return nil, "", errorf("The repository %s was not found or is private. Connect the GitHub App or check the URL in Settings > Source.", displayRepo(clone))
	default:
		return nil, "", errorf("The repository %s answered with HTTP %d. Try again in a minute.", displayRepo(clone), resp.StatusCode)
	}
	return parseRefAdvert(io.LimitReader(resp.Body, 32<<20))
}

// parseRefAdvert parses a smart HTTP ref advertisement.
func parseRefAdvert(r io.Reader) (map[string]string, string, error) {
	br := bufio.NewReader(r)
	refs := map[string]string{}
	head := ""
	first := true
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, "", err
		}
		n, err := strconv.ParseUint(string(lenBuf[:]), 16, 16)
		if err != nil {
			return nil, "", errorf("The repository sent a reply that is not a Git ref list.")
		}
		if n == 0 {
			continue // flush packet
		}
		if n < 4 {
			return nil, "", errorf("The repository sent a reply that is not a Git ref list.")
		}
		line := make([]byte, n-4)
		if _, err := io.ReadFull(br, line); err != nil {
			return nil, "", err
		}
		s := strings.TrimRight(string(line), "\n")
		if first && strings.HasPrefix(s, "# service=") {
			first = false
			continue
		}
		first = false
		caps := ""
		if i := strings.IndexByte(s, 0); i >= 0 {
			s, caps = s[:i], s[i+1:]
		}
		parts := strings.SplitN(s, " ", 2)
		if len(parts) != 2 {
			continue
		}
		refs[parts[1]] = parts[0]
		for _, c := range strings.Fields(caps) {
			if strings.HasPrefix(c, "symref=HEAD:refs/heads/") {
				head = strings.TrimPrefix(c, "symref=HEAD:refs/heads/")
			}
		}
	}
	if len(refs) == 0 {
		return nil, "", errorf("The repository has no commits yet. Push a commit, then deploy.")
	}
	if head == "" {
		if sha := refs["HEAD"]; sha != "" {
			for name, v := range refs {
				if v == sha && strings.HasPrefix(name, "refs/heads/") {
					head = strings.TrimPrefix(name, "refs/heads/")
					break
				}
			}
		}
	}
	return refs, head, nil
}

// fillGitHub adds message, author, time and URL for github.com repositories (best effort).
func (g *httpGit) fillGitHub(ctx context.Context, clone, token string, c *api.Commit) {
	u, err := url.Parse(clone)
	if err != nil || u.Host != "github.com" {
		return
	}
	repo := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	c.URL = "https://github.com/" + repo + "/commit/" + c.SHA
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.apiBase+"/repos/"+repo+"/commits/"+c.SHA, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body) != nil {
		return
	}
	c.Message = firstLineOf(body.Commit.Message)
	c.Author = body.Commit.Author.Name
	if body.Author != nil && body.Author.Login != "" {
		c.Author = body.Author.Login
	}
	if !body.Commit.Author.Date.IsZero() {
		c.Time = body.Commit.Author.Date.UnixMilli()
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// gitSource builds the Git part of a build job: the clone URL, a fresh
// installation token for private repos, and the resolved commit.
func (o *Orchestrator) gitSource(ctx context.Context, cfg api.ServiceConfig, commit *api.Commit, ref string) (*proto.GitSource, *api.Commit, error) {
	clone, err := cloneURL(cfg.Source.Repo)
	if err != nil {
		return nil, nil, err
	}
	token, err := o.cfg.GitHubCloneToken(ctx, cfg.Source.Repo)
	if err != nil {
		return nil, nil, errorf("A GitHub token for %s could not be created: %s", cfg.Source.Repo, sentenceOf(err))
	}
	c := commit
	if c == nil || c.SHA == "" || (ref != "" && ref != c.SHA) {
		resolved, err := o.git.Resolve(ctx, clone, cfg.Source.Branch, ref, token)
		if err != nil {
			return nil, nil, err
		}
		if c != nil && resolved.SHA == c.SHA {
			resolved.Message = firstNonEmpty(c.Message, resolved.Message)
			resolved.Author = firstNonEmpty(c.Author, resolved.Author)
		}
		c = &resolved
	}
	branch := c.Branch
	if branch == "" {
		branch = cfg.Source.Branch
	}
	return &proto.GitSource{URL: clone, Token: token, Commit: c.SHA, Branch: branch}, c, nil
}

// sentenceOf ends an error's text with a full stop.
func sentenceOf(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if s == "" {
		return "unknown error."
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
