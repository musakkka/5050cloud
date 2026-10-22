// SPDX-License-Identifier: AGPL-3.0-only

package github

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/configfile"
)

// RepoFS is a read-only view of a repository at one commit.
type RepoFS interface {
	// Files lists every file path relative to the repository root.
	Files(ctx context.Context) ([]string, error)
	Read(ctx context.Context, path string) ([]byte, error)
}

// APIRepo reads a repository through the contents API.
type APIRepo struct {
	Client *Client
	Token  string
	Repo   string
	Ref    string
	files  []string
}

func (r *APIRepo) Files(ctx context.Context) ([]string, error) {
	if r.files != nil {
		return r.files, nil
	}
	f, err := r.Client.Tree(ctx, r.Token, r.Repo, r.Ref)
	if err != nil {
		return nil, err
	}
	r.files = f
	return f, nil
}

func (r *APIRepo) Read(ctx context.Context, p string) ([]byte, error) {
	return r.Client.File(ctx, r.Token, r.Repo, r.Ref, p)
}

// DirRepo reads a checked-out directory (the shallow clone fallback).
type DirRepo struct{ Root string }

func (d DirRepo) Files(context.Context) ([]string, error) {
	var out []string
	err := filepath.WalkDir(d.Root, func(p string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fs.SkipDir // unreadable entries are left out of the scan
		}
		if e.IsDir() && (e.Name() == ".git" || e.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !e.IsDir() {
			rel, _ := filepath.Rel(d.Root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func (d DirRepo) Read(_ context.Context, p string) ([]byte, error) {
	clean := path.Clean("/" + p)
	return os.ReadFile(filepath.Join(d.Root, filepath.FromSlash(clean)))
}

// maxScanFile caps files read during a scan.
const maxScanFile = 512 << 10

// envSecretFiles suggest names only; example files also suggest values (9.3).
var envSecretFiles = map[string]bool{".env": true, ".env.local": true, ".env.production": true}
var envExampleFiles = map[string]bool{".env.example": true, ".env.sample": true, ".env.template": true}

// IsEnvFile reports whether base is one of the .env* files of 9.3.
func IsEnvFile(base string) bool {
	return envSecretFiles[base] || envExampleFiles[base] || strings.HasPrefix(base, ".env.")
}

var exposeRe = regexp.MustCompile(`(?im)^\s*EXPOSE\s+(\d+)`)

// Scan inspects a repository for the scan dialog (9.14).
func Scan(ctx context.Context, repo RepoFS, rootDir string) (api.ScanResult, error) {
	files, err := repo.Files(ctx)
	if err != nil {
		return api.ScanResult{}, err
	}
	root := strings.Trim(path.Clean("/"+rootDir), "/")
	in := func(p string) string {
		if root == "" {
			return p
		}
		return root + "/" + p
	}
	has := map[string]bool{}
	for _, f := range files {
		has[f] = true
	}
	read := func(p string) []byte {
		if !has[p] {
			return nil
		}
		b, err := repo.Read(ctx, p)
		if err != nil || len(b) > maxScanFile {
			return nil
		}
		return b
	}
	res := api.ScanResult{Builder: "railpack", EnvNames: []string{}}
	if df := read(in("Dockerfile")); df != nil {
		res.Builder, res.Dockerfile, res.Provider = "dockerfile", true, "dockerfile"
		if m := exposeRe.FindSubmatch(df); m != nil {
			res.SuggestedPort, _ = strconv.Atoi(string(m[1]))
		}
	}
	// Provider guess from manifest files.
	if res.Provider == "" {
		for _, p := range []struct{ file, provider string }{
			{"package.json", "node"}, {"deno.json", "deno"}, {"go.mod", "go"}, {"requirements.txt", "python"},
			{"pyproject.toml", "python"}, {"Pipfile", "python"}, {"Cargo.toml", "rust"}, {"Gemfile", "ruby"},
			{"composer.json", "php"}, {"pom.xml", "java"}, {"build.gradle", "java"}, {"build.gradle.kts", "java"},
			{"mix.exs", "elixir"}, {"index.html", "staticfile"},
		} {
			if has[in(p.file)] {
				res.Provider = p.provider
				break
			}
		}
	}
	var pkg *packageJSON
	if b := read(in("package.json")); b != nil {
		pkg = parsePackageJSON(b)
	}
	if pkg != nil {
		res.StartCommand = startCommand(pkg, files, in(""))
	}
	if res.SuggestedPort == 0 {
		res.SuggestedPort = defaultPort(res.Provider)
	}
	// Variables from .env* files in the repo root and the root directory.
	names := map[string]bool{}
	examples := map[string]string{}
	for _, f := range files {
		dir, base := path.Split(f)
		dir = strings.TrimSuffix(dir, "/")
		if !IsEnvFile(base) || (dir != "" && dir != root) {
			continue
		}
		b := read(f)
		if b == nil {
			continue
		}
		values, err := vars.ParseDotenv(string(b))
		if err != nil {
			continue
		}
		for k, v := range values {
			names[k] = true
			if envExampleFiles[base] {
				examples[k] = v
			}
		}
	}
	for k := range names {
		res.EnvNames = append(res.EnvNames, k)
	}
	sort.Strings(res.EnvNames)
	if len(examples) > 0 {
		res.EnvExamples = examples
		if p, err := strconv.Atoi(examples["PORT"]); err == nil && p > 0 && p < 65536 {
			res.SuggestedPort = p
		}
	}
	// Config files with what they set.
	for _, name := range append(append([]string{}, configfile.Candidates...), "nixpacks.toml") {
		p := in(name)
		b := read(p)
		if b == nil {
			continue
		}
		sc := api.ScanConfigFile{Path: p, Sets: []string{}}
		if name == "nixpacks.toml" {
			if h, err := configfile.ParseNixpacks(p, b); err == nil {
				if h.StartCmd != "" {
					sc.Sets = append(sc.Sets, "deploy.startCommand")
				}
				if len(h.BuildCmds) > 0 {
					sc.Sets = append(sc.Sets, "build.buildCommand")
				}
			}
		} else if f, err := configfile.Parse(p, b); err == nil {
			sc.Sets = configfile.Sets(f, "production")
			if f.Config.Build != nil && f.Config.Build.Builder != nil && *f.Config.Build.Builder == "DOCKERFILE" {
				res.Builder = "dockerfile"
			}
		}
		res.ConfigFiles = append(res.ConfigFiles, sc)
	}
	// JS monorepo packages.
	if pkg != nil || has[in("pnpm-workspace.yaml")] {
		globs := []string{}
		if pkg != nil {
			globs = append(globs, pkg.Workspaces...)
		}
		if b := read(in("pnpm-workspace.yaml")); b != nil {
			var pw struct {
				Packages []string `json:"packages"`
			}
			if yaml.Unmarshal(b, &pw) == nil {
				globs = append(globs, pw.Packages...)
			}
		}
		res.Packages = workspacePackages(ctx, globs, files, root, read)
	}
	return res, nil
}

type packageJSON struct {
	Name           string
	Scripts        map[string]string
	Workspaces     []string
	PackageManager string
	Main           string
}

func parsePackageJSON(b []byte) *packageJSON {
	var raw struct {
		Name           string            `json:"name"`
		Scripts        map[string]string `json:"scripts"`
		Workspaces     json.RawMessage   `json:"workspaces"`
		PackageManager string            `json:"packageManager"`
		Main           string            `json:"main"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	p := &packageJSON{Name: raw.Name, Scripts: raw.Scripts, PackageManager: raw.PackageManager, Main: raw.Main}
	if len(raw.Workspaces) > 0 {
		var list []string
		if json.Unmarshal(raw.Workspaces, &list) != nil {
			var obj struct {
				Packages []string `json:"packages"`
			}
			if json.Unmarshal(raw.Workspaces, &obj) == nil {
				list = obj.Packages
			}
		}
		p.Workspaces = list
	}
	return p
}

func packageManager(p *packageJSON, files []string, dir string) string {
	switch {
	case strings.HasPrefix(p.PackageManager, "pnpm"):
		return "pnpm"
	case strings.HasPrefix(p.PackageManager, "yarn"):
		return "yarn"
	case strings.HasPrefix(p.PackageManager, "bun"):
		return "bun"
	}
	for _, f := range files {
		switch f {
		case dir + "pnpm-lock.yaml":
			return "pnpm"
		case dir + "yarn.lock":
			return "yarn"
		case dir + "bun.lockb", dir + "bun.lock":
			return "bun"
		}
	}
	return "npm"
}

func startCommand(p *packageJSON, files []string, dir string) string {
	if dir != "" && !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	if _, ok := p.Scripts["start"]; ok {
		pm := packageManager(p, files, dir)
		if pm == "npm" || pm == "yarn" || pm == "pnpm" {
			return pm + " start"
		}
		return pm + " run start"
	}
	if p.Main != "" {
		return "node " + p.Main
	}
	return ""
}

func defaultPort(provider string) int {
	switch provider {
	case "node", "ruby", "deno":
		return 3000
	case "python":
		return 8000
	case "go", "java", "rust", "php", "elixir":
		return 8080
	case "staticfile":
		return 80
	}
	return 0
}

var skipPackage = regexp.MustCompile(`(?i)(eslint|prettier|tsconfig|lint|config|test|types|jest|vitest|storybook)`)

func workspacePackages(ctx context.Context, globs, files []string, root string, read func(string) []byte) []api.ScanPackage {
	if len(globs) == 0 {
		return nil
	}
	var out []api.ScanPackage
	seen := map[string]bool{}
	for _, f := range files {
		if path.Base(f) != "package.json" {
			continue
		}
		dir := path.Dir(f)
		rel := dir
		if root != "" {
			if !strings.HasPrefix(dir, root+"/") {
				continue
			}
			rel = strings.TrimPrefix(dir, root+"/")
		}
		if dir == "." || rel == "" || seen[dir] || strings.Contains(dir, "node_modules") {
			continue
		}
		matched := false
		for _, g := range globs {
			if strings.HasPrefix(g, "!") {
				continue
			}
			if globMatch(strings.TrimSuffix(strings.TrimPrefix(g, "./"), "/"), rel) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		b := read(f)
		if b == nil {
			continue
		}
		p := parsePackageJSON(b)
		if p == nil {
			continue
		}
		name := p.Name
		if name == "" {
			name = path.Base(dir)
		}
		if skipPackage.MatchString(name) || skipPackage.MatchString(path.Base(dir)) {
			continue
		}
		_, hasStart := p.Scripts["start"]
		_, hasDev := p.Scripts["dev"]
		if !hasStart && !hasDev && p.Main == "" {
			continue // libraries
		}
		seen[dir] = true
		sp := api.ScanPackage{Name: name, RootDir: "/" + dir, WatchPaths: []string{"/" + dir + "/**"}}
		if hasStart {
			sp.StartCommand = "npm start"
		}
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RootDir < out[j].RootDir })
	_ = ctx
	return out
}

// ScanRemote scans a repository through the API and falls back to a
// shallow clone when the API is rate limited (403 or 429) or unreachable
// for a public repository.
func ScanRemote(ctx context.Context, c *Client, g Git, ref RepoRef, token, branch, rootDir string) (api.ScanResult, error) {
	r := branch
	if r == "" {
		r = "HEAD"
	}
	res, err := Scan(ctx, &APIRepo{Client: c, Token: token, Repo: ref.FullName, Ref: r}, rootDir)
	if err == nil {
		return res, nil
	}
	if !RateLimited(err) && token != "" {
		return api.ScanResult{}, err
	}
	if NotFound(err) && token != "" {
		return api.ScanResult{}, err
	}
	dir, cerr := g.ShallowClone(ctx, ref.CloneURL(), branch)
	if cerr != nil {
		return api.ScanResult{}, cerr
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(dir)) }()
	return Scan(ctx, DirRepo{Root: dir}, rootDir)
}
