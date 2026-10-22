// SPDX-License-Identifier: MIT

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/pkg/api"
)

// App holds what every command shares: streams, global flags and the config.
type App struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer

	// Railway is set when the binary runs as `railway`.
	Railway bool
	// Getenv and Cwd are replaceable in tests.
	Getenv func(string) string
	Cwd    string

	// Global flags.
	Project     string
	Environment string
	Service     string
	JSON        bool
	Yes         bool

	cfg    *Config
	reader *bufio.Reader
}

// Target is a resolved project, environment and (optionally) service.
type Target struct {
	C       *Client
	URL     string
	Project api.Project
	Env     api.Environment
	Service *api.Service
	// Services are the environment's services (from the canvas), when loaded.
	Services []api.Service
	LinkDir  string
}

var errNotLoggedIn = errors.New("you are not logged in. Run `5050 login <url>` first")

func (a *App) config() (*Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	c, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	a.cfg = c
	return c, nil
}

func (a *App) env(k string) string {
	if a.Getenv != nil {
		return a.Getenv(k)
	}
	return os.Getenv(k)
}

func (a *App) cwd() string {
	if a.Cwd != "" {
		return a.Cwd
	}
	d, _ := os.Getwd()
	return d
}

// client returns a client for the instance named by FIFTY_URL, the link of
// the working directory, or the default instance.
func (a *App) client() (*Client, Link, string, error) {
	cfg, err := a.config()
	if err != nil {
		return nil, Link{}, "", err
	}
	link, dir, _ := cfg.FindLink(a.cwd())
	u := normURL(a.env("FIFTY_URL"))
	if u == "" {
		u = link.Instance
	}
	if u == "" {
		u = cfg.Default
	}
	if u == "" && len(cfg.Instances) == 1 {
		for k := range cfg.Instances {
			u = k
		}
	}
	tok := a.env("FIFTY_TOKEN")
	if tok == "" && a.Railway {
		if tok = a.env("RAILWAY_TOKEN"); tok == "" {
			tok = a.env("RAILWAY_API_TOKEN")
		}
	}
	if tok == "" {
		tok = cfg.Instances[u]
	}
	if u == "" || tok == "" {
		return nil, Link{}, "", errNotLoggedIn
	}
	if link.Instance != "" && link.Instance != u {
		link = Link{}
		dir = ""
	}
	return newClient(u, tok), link, dir, nil
}

// resolve finds the project, environment and, when needService is set, the service.
func (a *App) resolve(ctx context.Context, needService bool) (*Target, error) {
	c, link, dir, err := a.client()
	if err != nil {
		return nil, err
	}
	t := &Target{C: c, URL: c.Base, LinkDir: dir}
	var projects []api.Project
	if err := c.Do(ctx, "GET", "/projects", nil, &projects); err != nil {
		return nil, err
	}
	pref := a.Project
	if pref == "" {
		pref = link.Project
	}
	switch {
	case pref != "":
		p, ok := findProject(projects, pref)
		if !ok {
			return nil, fmt.Errorf("no project named %q. Run `5050 list` to see your projects", pref)
		}
		t.Project = p
	case len(projects) == 1:
		t.Project = projects[0]
	default:
		return nil, errors.New("this directory is not linked to a project. Run `5050 link` or pass -p")
	}
	eref := a.Environment
	if eref == "" {
		eref = link.Environment
	}
	e, ok := findEnv(t.Project, eref)
	if !ok {
		return nil, fmt.Errorf("no environment named %q in project %s", eref, t.Project.Name)
	}
	t.Env = e
	sref := a.Service
	if sref == "" && a.Project == "" && a.Environment == "" {
		sref = link.Service
	}
	if !needService && sref == "" {
		return t, nil
	}
	if err := a.loadServices(ctx, t); err != nil {
		return nil, err
	}
	if sref != "" {
		s, ok := findService(t.Services, sref)
		if !ok {
			return nil, fmt.Errorf("no service named %q in %s/%s", sref, t.Project.Name, t.Env.Name)
		}
		t.Service = &s
		return t, nil
	}
	if len(t.Services) == 1 {
		t.Service = &t.Services[0]
		return t, nil
	}
	return nil, errors.New("no service is linked. Run `5050 service` to pick one or pass -s")
}

func (a *App) loadServices(ctx context.Context, t *Target) error {
	var cv api.Canvas
	if err := t.C.Do(ctx, "GET", "/environments/"+t.Env.ID+"/canvas", nil, &cv); err != nil {
		return err
	}
	t.Services = cv.Services
	return nil
}

func findProject(ps []api.Project, ref string) (api.Project, bool) {
	for _, p := range ps {
		if p.ID == ref {
			return p, true
		}
	}
	for _, p := range ps {
		if strings.EqualFold(p.Name, ref) {
			return p, true
		}
	}
	return api.Project{}, false
}

// findEnv matches an id or name; an empty ref picks "production" or the first.
func findEnv(p api.Project, ref string) (api.Environment, bool) {
	if ref == "" {
		for _, e := range p.Environments {
			if e.Name == "production" {
				return e, true
			}
		}
		if len(p.Environments) > 0 {
			return p.Environments[0], true
		}
		return api.Environment{}, false
	}
	for _, e := range p.Environments {
		if e.ID == ref || strings.EqualFold(e.Name, ref) {
			return e, true
		}
	}
	return api.Environment{}, false
}

func findService(ss []api.Service, ref string) (api.Service, bool) {
	for _, s := range ss {
		if s.ID == ref {
			return s, true
		}
	}
	for _, s := range ss {
		if strings.EqualFold(s.Config.Name, ref) {
			return s, true
		}
	}
	return api.Service{}, false
}

func svcPath(t *Target) string {
	return "/environments/" + t.Env.ID + "/services/" + t.Service.ID
}

// ---------------------------------------------------------------- terminal helpers

func isTTY(f any) bool {
	file, ok := f.(*os.File)
	if !ok {
		return false
	}
	st, err := file.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func (a *App) interactive() bool { return isTTY(a.In) && isTTY(a.Out) }

func (a *App) line() (string, error) {
	if a.reader == nil {
		a.reader = bufio.NewReader(a.In)
	}
	s, err := a.reader.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// prompt asks a question and returns the answer (def when empty).
func (a *App) prompt(q, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(a.Err, "%s [%s]: ", q, def)
	} else {
		fmt.Fprintf(a.Err, "%s: ", q)
	}
	s, err := a.line()
	if err != nil {
		return "", err
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}

// pick shows a numbered list and returns the chosen index.
func (a *App) pick(title string, items []string) (int, error) {
	if len(items) == 0 {
		return -1, errors.New("there is nothing to choose from")
	}
	if !a.interactive() {
		return -1, fmt.Errorf("%s: pass it as a flag when not running in a terminal", strings.ToLower(title))
	}
	fmt.Fprintln(a.Err, title)
	for i, it := range items {
		fmt.Fprintf(a.Err, "  %d) %s\n", i+1, it)
	}
	for {
		s, err := a.prompt("Number", "1")
		if err != nil {
			return -1, err
		}
		n, err := strconv.Atoi(s)
		if err == nil && n >= 1 && n <= len(items) {
			return n - 1, nil
		}
		fmt.Fprintf(a.Err, "Type a number from 1 to %d.\n", len(items))
	}
}

// confirm asks yes/no unless -y was given; without a terminal it refuses.
func (a *App) confirm(q string) error {
	if a.Yes {
		return nil
	}
	if !a.interactive() {
		return fmt.Errorf("%s Pass -y to confirm when not running in a terminal", q)
	}
	s, err := a.prompt(q+" (y/N)", "")
	if err != nil {
		return err
	}
	if s != "y" && s != "Y" && !strings.EqualFold(s, "yes") {
		return errors.New("cancelled")
	}
	return nil
}

func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.Out, format, args...) }

// commit commits the environment's staged changes, deploying unless skip is set.
func (a *App) commit(ctx context.Context, t *Target, msg string, deploy bool) (*api.CommitResponse, error) {
	var cr api.CommitResponse
	err := t.C.Do(ctx, "POST", "/environments/"+t.Env.ID+"/changes/commit", api.CommitRequest{Message: msg, Deploy: deploy}, &cr)
	if err != nil {
		return nil, err
	}
	return &cr, nil
}

// serviceURL is the first domain of a service as an https URL, or "".
func serviceURL(s *api.Service) string {
	if s == nil || len(s.Domains) == 0 {
		return ""
	}
	return "https://" + s.Domains[0].Domain
}

// parseKV turns K=V arguments into a map.
func parseKV(args []string) (map[string]string, error) {
	m := map[string]string{}
	for _, kv := range args {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("%q is not KEY=VALUE", kv)
		}
		m[k] = v
	}
	return m, nil
}
