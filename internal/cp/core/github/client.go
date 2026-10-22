// SPDX-License-Identifier: AGPL-3.0-only

// Package github talks to GitHub for core (docs/SPEC.md 9.14): the GitHub
// App manifest flow, app JWTs and installation tokens, repository and
// branch listing, push webhook signatures and payloads, gitignore-style
// watch paths, public repositories through git ls-remote and the public
// API, and the repository scan behind the shared scan dialog, with a
// shallow clone fallback when the API is rate limited.
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAPI is GitHub's REST API.
const DefaultAPI = "https://api.github.com"

// App holds a GitHub App's credentials as stored (decrypted) in settings.
type App struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Owner         string `json:"owner,omitempty"`
	HTMLURL       string `json:"htmlUrl,omitempty"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret,omitempty"`
	WebhookSecret string `json:"webhookSecret,omitempty"`
	PEM           string `json:"pem,omitempty"`
}

// Client is a GitHub API client.
type Client struct {
	API  string
	HTTP *http.Client
	Now  func() time.Time

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

// NewClient returns a client for base (DefaultAPI when empty).
func NewClient(base string, hc *http.Client) *Client {
	if base == "" {
		base = DefaultAPI
	}
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{API: strings.TrimRight(base, "/"), HTTP: hc, Now: time.Now, tokens: map[int64]cachedToken{}}
}

// StatusError is a non-2xx answer from GitHub.
type StatusError struct {
	Status int
	Msg    string
}

func (e *StatusError) Error() string { return fmt.Sprintf("github answered %d: %s", e.Status, e.Msg) }

// RateLimited reports whether err is a 403 or 429 from GitHub.
func RateLimited(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && (se.Status == http.StatusForbidden || se.Status == http.StatusTooManyRequests)
}

// NotFound reports a 404.
func NotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method, path, auth string, body any, accept string, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = c.API + path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "5050cloud")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &m)
		if m.Message == "" {
			m.Message = http.StatusText(resp.StatusCode)
		}
		return &StatusError{Status: resp.StatusCode, Msg: m.Message}
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

// ---------------------------------------------------------------- app auth

// AppJWT signs the RS256 JWT that authenticates as the app (valid 9 minutes).
func AppJWT(app App, now time.Time) (string, error) {
	key, err := parseKey(app.PEM)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": strconv.FormatInt(app.ID, 10)})
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func parseKey(p string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(p))
	if block == nil {
		return nil, errors.New("the GitHub App private key is not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the GitHub App private key cannot be parsed: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the GitHub App private key is not an RSA key")
	}
	return rk, nil
}

// InstallationToken returns a cached installation access token.
func (c *Client) InstallationToken(ctx context.Context, app App, installationID int64) (string, error) {
	c.mu.Lock()
	if t, ok := c.tokens[installationID]; ok && c.Now().Before(t.expires.Add(-5*time.Minute)) {
		c.mu.Unlock()
		return t.token, nil
	}
	c.mu.Unlock()
	jwt, err := AppJWT(app, c.Now())
	if err != nil {
		return "", err
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installationID), "Bearer "+jwt, map[string]any{}, "", &out); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tokens[installationID] = cachedToken{out.Token, out.ExpiresAt}
	c.mu.Unlock()
	return out.Token, nil
}

// Installation is an app installation.
type Installation struct {
	ID      int64
	Account string
}

// Installations lists the app's installations.
func (c *Client) Installations(ctx context.Context, app App) ([]Installation, error) {
	jwt, err := AppJWT(app, c.Now())
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := c.do(ctx, http.MethodGet, "/app/installations?per_page=100", "Bearer "+jwt, nil, "", &raw); err != nil {
		return nil, err
	}
	out := make([]Installation, 0, len(raw))
	for _, r := range raw {
		out = append(out, Installation{ID: r.ID, Account: r.Account.Login})
	}
	return out, nil
}

// Repo is a repository visible to an installation.
type Repo struct {
	FullName      string
	Private       bool
	DefaultBranch string
	UpdatedAt     int64
	Installation  int64
}

// Repos lists the repositories of one installation (up to 1,000).
func (c *Client) Repos(ctx context.Context, token string, installationID int64) ([]Repo, error) {
	var out []Repo
	for page := 1; page <= 10; page++ {
		var raw struct {
			Repositories []struct {
				FullName      string    `json:"full_name"`
				Private       bool      `json:"private"`
				DefaultBranch string    `json:"default_branch"`
				PushedAt      time.Time `json:"pushed_at"`
			} `json:"repositories"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), "token "+token, nil, "", &raw); err != nil {
			return nil, err
		}
		for _, r := range raw.Repositories {
			out = append(out, Repo{FullName: r.FullName, Private: r.Private, DefaultBranch: r.DefaultBranch, UpdatedAt: r.PushedAt.UnixMilli(), Installation: installationID})
		}
		if len(raw.Repositories) < 100 {
			break
		}
	}
	return out, nil
}

// Branches lists branch names (token may be empty for public repos).
func (c *Client) Branches(ctx context.Context, token, repo string) ([]string, error) {
	var out []string
	for page := 1; page <= 10; page++ {
		var raw []struct {
			Name string `json:"name"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/branches?per_page=100&page=%d", repo, page), authHeader(token), nil, "", &raw); err != nil {
			return nil, err
		}
		for _, b := range raw {
			out = append(out, b.Name)
		}
		if len(raw) < 100 {
			break
		}
	}
	return out, nil
}

// RepoInfo returns the default branch and visibility of a repository.
func (c *Client) RepoInfo(ctx context.Context, token, repo string) (defaultBranch string, private bool, err error) {
	var raw struct {
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repo, authHeader(token), nil, "", &raw); err != nil {
		return "", false, err
	}
	return raw.DefaultBranch, raw.Private, nil
}

// Tree lists every file path at ref.
func (c *Client) Tree(ctx context.Context, token, repo, ref string) ([]string, error) {
	var raw struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", repo, url.PathEscape(ref)), authHeader(token), nil, "", &raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw.Tree))
	for _, e := range raw.Tree {
		if e.Type == "blob" {
			out = append(out, e.Path)
		}
	}
	return out, nil
}

// File returns the raw content of path at ref.
func (c *Client) File(ctx context.Context, token, repo, ref, path string) ([]byte, error) {
	var data []byte
	p := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, escapePath(path), url.QueryEscape(ref))
	err := c.do(ctx, http.MethodGet, p, authHeader(token), nil, "application/vnd.github.raw", &data)
	return data, err
}

// CommitInfo returns commit details for sha or a branch name.
func (c *Client) CommitInfo(ctx context.Context, token, repo, ref string) (Commit, error) {
	var raw struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape(ref)), authHeader(token), nil, "", &raw); err != nil {
		return Commit{}, err
	}
	author := raw.Commit.Author.Name
	if raw.Author != nil && raw.Author.Login != "" {
		author = raw.Author.Login
	}
	return Commit{SHA: raw.SHA, Message: raw.Commit.Message, URL: raw.HTMLURL, Author: author, Time: parseTime(raw.Commit.Author.Date)}, nil
}

// CompareFiles lists files changed between two commits (truncated push payloads).
func (c *Client) CompareFiles(ctx context.Context, token, repo, base, head string) ([]string, error) {
	var raw struct {
		Files []struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/compare/%s...%s", repo, base, head), authHeader(token), nil, "", &raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw.Files))
	for _, f := range raw.Files {
		out = append(out, f.Filename)
	}
	return out, nil
}

// ConvertManifest exchanges the manifest flow code for the new app's credentials.
func (c *Client) ConvertManifest(ctx context.Context, code string) (App, error) {
	var raw struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		Name          string `json:"name"`
		HTMLURL       string `json:"html_url"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		WebhookSecret string `json:"webhook_secret"`
		PEM           string `json:"pem"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := c.do(ctx, http.MethodPost, "/app-manifests/"+url.PathEscape(code)+"/conversions", "", map[string]any{}, "", &raw); err != nil {
		return App{}, err
	}
	return App{ID: raw.ID, Slug: raw.Slug, Name: raw.Name, HTMLURL: raw.HTMLURL, ClientID: raw.ClientID, ClientSecret: raw.ClientSecret, WebhookSecret: raw.WebhookSecret, PEM: raw.PEM, Owner: raw.Owner.Login}, nil
}

// Manifest returns the app manifest for the manifest flow (9.14).
func Manifest(installName, consoleURL, returnTo string) map[string]any {
	name := "5050cloud-" + slugify(installName)
	if len(name) > 34 {
		name = name[:34]
	}
	return map[string]any{
		"name":            name,
		"url":             consoleURL,
		"hook_attributes": map[string]any{"url": consoleURL + "/api/v1/github/webhook", "active": true},
		"redirect_url":    consoleURL + "/api/v1/github/app/callback",
		"setup_url":       consoleURL + "/settings/github?installed=1",
		"setup_on_update": true,
		"public":          false,
		"default_permissions": map[string]string{
			"contents": "read", "metadata": "read", "pull_requests": "read", "checks": "read",
		},
		"default_events": []string{"push"},
	}
}

// FormURL is where the console posts the manifest (with state).
func FormURL(owner, state string) string {
	if owner != "" {
		return "https://github.com/organizations/" + url.PathEscape(owner) + "/settings/apps/new?state=" + url.QueryEscape(state)
	}
	return "https://github.com/settings/apps/new?state=" + url.QueryEscape(state)
}

// InstallURL opens the app's install page.
func InstallURL(slug string) string { return "https://github.com/apps/" + slug + "/installations/new" }

func slugify(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "install"
	}
	return out
}

func authHeader(token string) string {
	if token == "" {
		return ""
	}
	return "token " + token
}

func escapePath(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func parseTime(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// ---------------------------------------------------------------- repository names

// RepoRef is a parsed source repository.
type RepoRef struct {
	FullName string // owner/name
	// URL is set for repositories given as https URLs (public, no app).
	URL string
}

// CloneURL is the https clone URL.
func (r RepoRef) CloneURL() string {
	if r.URL != "" {
		return r.URL
	}
	return "https://github.com/" + r.FullName + ".git"
}

// ParseRepo accepts "owner/name", "https://github.com/owner/name(.git)"
// and "github.com/owner/name".
func ParseRepo(s string) (RepoRef, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return RepoRef{}, false
	}
	isURL := strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "github.com/")
	t := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://"), "www.")
	if isURL {
		if !strings.HasPrefix(t, "github.com/") {
			return RepoRef{}, false
		}
		t = strings.TrimPrefix(t, "github.com/")
	}
	t = strings.TrimSuffix(strings.TrimSuffix(t, "/"), ".git")
	parts := strings.Split(t, "/")
	if len(parts) < 2 || !validPart(parts[0]) || !validPart(parts[1]) {
		return RepoRef{}, false
	}
	if len(parts) > 2 && parts[2] != "tree" {
		return RepoRef{}, false
	}
	ref := RepoRef{FullName: parts[0] + "/" + parts[1]}
	if isURL {
		ref.URL = "https://github.com/" + ref.FullName + ".git"
	}
	return ref, true
}

func validPart(p string) bool {
	if p == "" || len(p) > 100 || p == "." || p == ".." {
		return false
	}
	for _, c := range p {
		if !validChar(c) {
			return false
		}
	}
	return true
}

// SameRepo compares two repository strings in any accepted form.
func SameRepo(a, b string) bool {
	ra, ok1 := ParseRepo(a)
	rb, ok2 := ParseRepo(b)
	return ok1 && ok2 && strings.EqualFold(ra.FullName, rb.FullName)
}

func validChar(c rune) bool {
	return c == '-' || c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
