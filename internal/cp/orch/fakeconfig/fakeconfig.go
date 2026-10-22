// SPDX-License-Identifier: AGPL-3.0-only

// Package fakeconfig is an in-memory cpapi.Config for orchestrator tests
// (docs/SPEC.md 8.3). It keeps projects, environments, committed service
// instances, variables, domains, TCP proxies, volumes and monitors, resolves
// ${{NAME}}, ${{shared.NAME}} and ${{Service.NAME}} references like core
// does, and records everything the orchestrator reports back (events,
// notices, activity, audit entries, webhook deliveries) so tests can assert
// on it.
package fakeconfig

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

type manifestDeploy = manifest.RailwayDeploy

// Hook is one recorded FireWebhook call.
type Hook struct {
	ProjectID string
	Event     string
	Payload   any
}

// Config implements cpapi.Config in memory. The zero value is not usable; call New.
type Config struct {
	mu sync.Mutex

	install  api.Install
	backups  api.BackupSettings
	projects map[string]api.Project
	envs     map[string]api.Environment
	inst     map[string]*cpapi.ServiceSnapshot // env/svc
	vars     map[string]map[string]string      // env/svc -> user variables
	shared   map[string]map[string]string      // env -> shared variables
	secrets  map[string][]string
	regcreds map[string][3]string
	monitors map[string]api.Monitor
	files    map[string]*proto.ConfigOverlay

	events     []api.Event
	notices    []cpapi.Notice
	activities []api.Activity
	audits     []api.AuditEntry
	hooks      []Hook
	seq        int
	listeners  []chan api.Event
}

// New returns an empty install with sensible defaults.
func New() *Config {
	return &Config{
		install: api.Install{Name: "test", Version: "v0.1.0", ConsoleURL: "https://console.203-0-113-7.sslip.io",
			BaseDomain: "203-0-113-7.sslip.io", PublicIP: "203.0.113.7", RailwayAliases: true, BuildTimeoutMin: 30, RegistryCapPct: 30},
		projects: map[string]api.Project{}, envs: map[string]api.Environment{}, inst: map[string]*cpapi.ServiceSnapshot{},
		vars: map[string]map[string]string{}, shared: map[string]map[string]string{}, secrets: map[string][]string{},
		regcreds: map[string][3]string{}, monitors: map[string]api.Monitor{}, files: map[string]*proto.ConfigOverlay{},
	}
}

func key(env, svc string) string { return env + "/" + svc }

func (c *Config) id(prefix string) string {
	c.seq++
	return fmt.Sprintf("%s_%026d", prefix, c.seq)
}

// ---------------------------------------------------------------- setup helpers

// SetInstall replaces the install settings.
func (c *Config) SetInstall(i api.Install) {
	c.mu.Lock()
	c.install = i
	c.mu.Unlock()
}

// SetBackupSettings replaces the bucket settings.
func (c *Config) SetBackupSettings(b api.BackupSettings) {
	c.mu.Lock()
	c.backups = b
	c.mu.Unlock()
}

// AddProject creates a project with a "production" environment.
func (c *Config) AddProject(name string) (projectID, envID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := api.Project{ID: c.id("prj"), Name: name, CreatedAt: time.Now().UnixMilli()}
	e := api.Environment{ID: c.id("env"), ProjectID: p.ID, Name: "production", Egress: "allowed", CreatedAt: p.CreatedAt}
	p.Environments = []api.Environment{e}
	c.projects[p.ID] = p
	c.envs[e.ID] = e
	return p.ID, e.ID
}

// AddEnvironment adds an environment to a project.
func (c *Config) AddEnvironment(projectID, name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := api.Environment{ID: c.id("env"), ProjectID: projectID, Name: name, Egress: "allowed", CreatedAt: time.Now().UnixMilli()}
	c.envs[e.ID] = e
	p := c.projects[projectID]
	p.Environments = append(p.Environments, e)
	c.projects[projectID] = p
	return e.ID
}

// AddService commits a service instance and returns its id.
func (c *Config) AddService(envID string, cfg api.ServiceConfig) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.envs[envID]
	if !ok {
		panic("fakeconfig: unknown environment " + envID)
	}
	p := c.projects[e.ProjectID]
	id := c.id("svc")
	snap := &cpapi.ServiceSnapshot{ProjectID: p.ID, ProjectName: p.Name, EnvID: envID, EnvName: e.Name, ServiceID: id, Rev: 1,
		Config: cfg, PrivateName: slug(cfg.Name), Egress: e.Egress, EgressAllow: e.EgressAllow}
	if cfg.Net.PrivateName != "" {
		snap.PrivateName = cfg.Net.PrivateName
	}
	c.inst[key(envID, id)] = snap
	return id
}

// Update changes a committed instance and bumps its revision.
func (c *Config) Update(envID, svcID string, fn func(*cpapi.ServiceSnapshot)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.inst[key(envID, svcID)]
	if s == nil {
		panic("fakeconfig: unknown service " + svcID)
	}
	fn(s)
	s.Rev++
}

// SetVariables replaces a service's user variables.
func (c *Config) SetVariables(envID, svcID string, vars map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := map[string]string{}
	for k, v := range vars {
		m[k] = v
	}
	c.vars[key(envID, svcID)] = m
}

// SetShared replaces an environment's shared variables.
func (c *Config) SetShared(envID string, vars map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shared[envID] = vars
}

// SetSecrets sets the values masked in a service's logs.
func (c *Config) SetSecrets(envID, svcID string, values []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.secrets[key(envID, svcID)] = values
}

// AddDomain attaches a domain to a service instance.
func (c *Config) AddDomain(envID, svcID, domain string, port int) api.Domain {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := api.Domain{ID: c.id("dom"), ServiceID: svcID, Domain: domain, Kind: "generated", Port: port, Status: "issuing"}
	s := c.inst[key(envID, svcID)]
	s.Domains = append(s.Domains, d)
	return d
}

// AddTCPProxy attaches a TCP proxy.
func (c *Config) AddTCPProxy(envID, svcID string, target, hostPort int) api.TCPProxy {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := api.TCPProxy{ID: c.id("tcp"), TargetPort: target, HostPort: hostPort}
	s := c.inst[key(envID, svcID)]
	s.TCPProxies = append(s.TCPProxies, t)
	return t
}

// AddVolume attaches a volume.
func (c *Config) AddVolume(envID, svcID, mountPath string) api.Volume {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := api.Volume{ID: c.id("vol"), ServiceID: svcID, Name: "data", MountPath: mountPath, AlertAtPct: 80}
	s := c.inst[key(envID, svcID)]
	s.Volume = &v
	return v
}

// Delete marks a service instance deleted.
func (c *Config) Delete(envID, svcID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.inst[key(envID, svcID)]; s != nil {
		s.Deleted = true
	}
}

// AddMonitor stores a monitor definition and returns it with its id.
func (c *Config) AddMonitor(m api.Monitor) api.Monitor {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m.ID == "" {
		m.ID = c.id("mon")
	}
	if m.State == "" {
		m.State = "ok"
	}
	c.monitors[m.ID] = m
	return m
}

// AddRegistryCredential stores a private registry credential.
func (c *Config) AddRegistryCredential(id, server, user, pass string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.regcreds[id] = [3]string{user, pass, server}
}

// ---------------------------------------------------------------- recorded output

// Events returns every published event.
func (c *Config) Events() []api.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]api.Event(nil), c.events...)
}

// Subscribe returns a channel of future events (buffered; slow readers miss events).
func (c *Config) Subscribe() <-chan api.Event {
	ch := make(chan api.Event, 1024)
	c.mu.Lock()
	c.listeners = append(c.listeners, ch)
	c.mu.Unlock()
	return ch
}

// Notices returns every Notify call.
func (c *Config) Notices() []cpapi.Notice {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]cpapi.Notice(nil), c.notices...)
}

// Activities returns every recorded activity.
func (c *Config) Activities() []api.Activity {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]api.Activity(nil), c.activities...)
}

// Audits returns every audit entry.
func (c *Config) Audits() []api.AuditEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]api.AuditEntry(nil), c.audits...)
}

// Hooks returns every FireWebhook call.
func (c *Config) Hooks() []Hook {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Hook(nil), c.hooks...)
}

// MonitorState returns the recorded state of a monitor.
func (c *Config) MonitorState(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.monitors[id].State
}

// ConfigFile returns the last config file recorded by MergeConfigFile.
func (c *Config) ConfigFile(envID, svcID string) *proto.ConfigOverlay {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files[key(envID, svcID)]
}

// ---------------------------------------------------------------- cpapi.Config

var _ cpapi.Config = (*Config)(nil)

// ErrNotFound is returned for unknown ids.
var ErrNotFound = errors.New("not found")

func (c *Config) Install(context.Context) (api.Install, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.install, nil
}

func (c *Config) BackupSettings(context.Context) (api.BackupSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.backups, nil
}

func (c *Config) Environment(_ context.Context, envID string) (api.Environment, api.Project, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.envs[envID]
	if !ok {
		return api.Environment{}, api.Project{}, ErrNotFound
	}
	return e, c.projects[e.ProjectID], nil
}

func (c *Config) snapshot(s *cpapi.ServiceSnapshot) cpapi.ServiceSnapshot {
	out := *s
	out.Domains = append([]api.Domain(nil), s.Domains...)
	out.TCPProxies = append([]api.TCPProxy(nil), s.TCPProxies...)
	if s.Volume != nil {
		v := *s.Volume
		out.Volume = &v
	}
	if e, ok := c.envs[s.EnvID]; ok {
		out.Egress, out.EgressAllow = e.Egress, e.EgressAllow
	}
	out.References = c.refsLocked(s)
	return out
}

var refRE = regexp.MustCompile(`\$\{\{\s*([^}]+?)\s*\}\}`)

func (c *Config) refsLocked(s *cpapi.ServiceSnapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range c.vars[key(s.EnvID, s.ServiceID)] {
		for _, m := range refRE.FindAllStringSubmatch(v, -1) {
			i := strings.LastIndex(m[1], ".")
			if i <= 0 {
				continue
			}
			name := m[1][:i]
			if name == "shared" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Config) ServiceInstance(_ context.Context, envID, serviceID string) (cpapi.ServiceSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.inst[key(envID, serviceID)]
	if !ok {
		return cpapi.ServiceSnapshot{}, ErrNotFound
	}
	return c.snapshot(s), nil
}

func (c *Config) ServiceInstances(_ context.Context, envID string) ([]cpapi.ServiceSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []cpapi.ServiceSnapshot
	for _, s := range c.inst {
		if s.EnvID == envID && !s.Deleted {
			out = append(out, c.snapshot(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceID < out[j].ServiceID })
	return out, nil
}

func (c *Config) AllInstances(context.Context) ([]cpapi.ServiceSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []cpapi.ServiceSnapshot
	for _, s := range c.inst {
		if !s.Deleted {
			out = append(out, c.snapshot(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i].EnvID, out[i].ServiceID) < key(out[j].EnvID, out[j].ServiceID) })
	return out, nil
}

func (c *Config) byName(envID, name string) *cpapi.ServiceSnapshot {
	for _, s := range c.inst {
		if s.EnvID == envID && s.Config.Name == name && !s.Deleted {
			return s
		}
	}
	return nil
}

func (c *Config) ResolveVariables(_ context.Context, envID, serviceID string, provided map[string]string) (map[string]string, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.inst[key(envID, serviceID)]
	if !ok {
		return nil, nil, ErrNotFound
	}
	env := map[string]string{}
	for k, v := range provided {
		env[k] = v
	}
	var unresolved []string
	own := c.vars[key(envID, serviceID)]
	var resolve func(svc *cpapi.ServiceSnapshot, v string, depth int) string
	lookup := func(svc *cpapi.ServiceSnapshot, name string, depth int) (string, bool) {
		if v, ok := c.vars[key(svc.EnvID, svc.ServiceID)][name]; ok {
			return resolve(svc, v, depth+1), true
		}
		switch name {
		case "RAILWAY_PRIVATE_DOMAIN", "FIFTY_PRIVATE_DOMAIN":
			return svc.PrivateName + ".internal", true
		case "PORT":
			if svc.Config.Net.Port > 0 {
				return fmt.Sprint(svc.Config.Net.Port), true
			}
			return "8080", true
		case "RAILWAY_PUBLIC_DOMAIN", "FIFTY_PUBLIC_DOMAIN":
			if len(svc.Domains) > 0 {
				return svc.Domains[0].Domain, true
			}
			return "", true
		}
		if svc == s {
			if v, ok := provided[name]; ok {
				return v, true
			}
		}
		return "", false
	}
	resolve = func(svc *cpapi.ServiceSnapshot, v string, depth int) string {
		if depth > 10 {
			return ""
		}
		return refRE.ReplaceAllStringFunc(v, func(m string) string {
			ref := strings.TrimSpace(refRE.FindStringSubmatch(m)[1])
			i := strings.LastIndex(ref, ".")
			if i < 0 {
				if out, ok := lookup(svc, ref, depth); ok {
					return out
				}
				unresolved = append(unresolved, ref)
				return ""
			}
			target, name := ref[:i], ref[i+1:]
			if target == "shared" {
				if v, ok := c.shared[svc.EnvID][name]; ok {
					return resolve(svc, v, depth+1)
				}
				unresolved = append(unresolved, ref)
				return ""
			}
			other := c.byName(svc.EnvID, target)
			if other == nil {
				unresolved = append(unresolved, ref)
				return ""
			}
			if out, ok := lookup(other, name, depth); ok {
				return out
			}
			unresolved = append(unresolved, ref)
			return ""
		})
	}
	for k, v := range own {
		env[k] = resolve(s, v, 0)
	}
	sort.Strings(unresolved)
	return env, unresolved, nil
}

func (c *Config) SecretValues(_ context.Context, envID, serviceID string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.secrets[key(envID, serviceID)]...), nil
}

// MergeConfigFile applies the deploy and build fields of an overlay (base
// fields, then the environment block) over the committed config.
func (c *Config) MergeConfigFile(_ context.Context, envID, serviceID, _ string, file *proto.ConfigOverlay) (cpapi.MergedConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.inst[key(envID, serviceID)]
	if !ok {
		return cpapi.MergedConfig{}, ErrNotFound
	}
	out := cpapi.MergedConfig{Config: s.Config, Source: map[string]string{}}
	c.files[key(envID, serviceID)] = file
	if file == nil {
		return out, nil
	}
	apply := func(prefix string, d *manifestDeploy) {
		if d == nil {
			return
		}
		set := func(field string) {
			if line, ok := file.Lines[prefix+field]; ok {
				out.Source[field] = fmt.Sprintf("%s:%d", file.Path, line)
			} else {
				out.Source[field] = file.Path
			}
		}
		if d.StartCommand != nil {
			out.Config.Deploy.StartCommand = *d.StartCommand
			set("deploy.startCommand")
		}
		if d.HealthcheckPath != nil {
			out.Config.Deploy.HealthcheckPath = *d.HealthcheckPath
			set("deploy.healthcheckPath")
		}
		if d.NumReplicas != nil {
			out.Config.Deploy.Replicas = *d.NumReplicas
			set("deploy.numReplicas")
		}
		if d.PreDeployCommand != nil && len(*d.PreDeployCommand) > 0 {
			out.Config.Deploy.PreDeployCommand = strings.Join(*d.PreDeployCommand, " && ")
			set("deploy.preDeployCommand")
		}
	}
	apply("", file.Config.Deploy)
	if e, ok := c.envs[envID]; ok {
		if blk, ok := file.Config.Environments[e.Name]; ok {
			apply("environments."+e.Name+".", blk.Deploy)
		}
	}
	return out, nil
}

func (c *Config) GitHubCloneToken(context.Context, string) (string, error) { return "", nil }

func (c *Config) RegistryCredential(_ context.Context, id string) (string, string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.regcreds[id]
	if !ok {
		return "", "", "", ErrNotFound
	}
	return r[0], r[1], r[2], nil
}

func (c *Config) CloneServiceForRestore(_ context.Context, envID, sourceServiceID, newName, targetEnvID string) (string, error) {
	c.mu.Lock()
	src, ok := c.inst[key(envID, sourceServiceID)]
	c.mu.Unlock()
	if !ok {
		return "", ErrNotFound
	}
	if targetEnvID == "" {
		targetEnvID = envID
	}
	cfg := src.Config
	cfg.Name = newName
	id := c.AddService(targetEnvID, cfg)
	c.mu.Lock()
	c.vars[key(targetEnvID, id)] = c.vars[key(envID, sourceServiceID)]
	c.mu.Unlock()
	return id, nil
}

func (c *Config) SwapServiceNames(_ context.Context, envID, keepNameServiceID, otherServiceID, otherNewName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, b := c.inst[key(envID, keepNameServiceID)], c.inst[key(envID, otherServiceID)]
	if a == nil || b == nil {
		return ErrNotFound
	}
	a.Config.Name, b.Config.Name = b.Config.Name, otherNewName
	a.PrivateName, b.PrivateName = b.PrivateName, slug(otherNewName)
	return nil
}

func (c *Config) CommitVariables(_ context.Context, envID, serviceID string, set map[string]string, _, _ string) (api.CommitResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.vars[key(envID, serviceID)]
	if m == nil {
		m = map[string]string{}
		c.vars[key(envID, serviceID)] = m
	}
	for k, v := range set {
		m[k] = v
	}
	return api.CommitResponse{CommitID: c.id("cmt")}, nil
}

func (c *Config) Monitors(context.Context) ([]api.Monitor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]api.Monitor, 0, len(c.monitors))
	for _, m := range c.monitors {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *Config) SetMonitorState(_ context.Context, monitorID, state string, at int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.monitors[monitorID]
	if !ok {
		return ErrNotFound
	}
	m.State = state
	if state == "firing" {
		m.LastFired = at
	}
	c.monitors[monitorID] = m
	return nil
}

func (c *Config) RecordActivity(_ context.Context, _ string, a api.Activity) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a.ID == "" {
		a.ID = c.id("act")
	}
	c.activities = append(c.activities, a)
	return nil
}

func (c *Config) Audit(_ context.Context, e api.AuditEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.audits = append(c.audits, e)
	return nil
}

func (c *Config) Publish(e api.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	for _, ch := range c.listeners {
		select {
		case ch <- e:
		default:
		}
	}
}

func (c *Config) Notify(_ context.Context, n cpapi.Notice) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notices = append(c.notices, n)
	return nil
}

func (c *Config) FireWebhook(_ context.Context, projectID, event string, payload any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hooks = append(c.hooks, Hook{ProjectID: projectID, Event: event, Payload: payload})
	return nil
}

func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
