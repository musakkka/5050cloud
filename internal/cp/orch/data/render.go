// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxFileBytes is the limit of one rendered file (proto.FileSpec).
const maxFileBytes = 256 << 10

// Sizing is what template files see about memory (docs/SPEC.md 10.1).
type Sizing struct {
	// MemBytes is the host's memory; MemLimitBytes the service's limit (0 = none).
	MemBytes      int64
	MemLimitBytes int64
	CPUs          int
	// EffectiveBytes is the limit, or the host's memory when there is none.
	EffectiveBytes int64

	Postgres struct {
		MaxConnections       int
		SharedBuffersMB      int64
		EffectiveCacheSizeMB int64
		WorkMemMB            int64
		MaintenanceWorkMemMB int64
		MaxWALSizeMB         int64
	}
	MySQL struct{ BufferPoolMB int64 }
	Redis struct{ MaxMemoryMB int64 }
	Mongo struct{ CacheSizeGB string }
}

const mib = 1 << 20

func clamp(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// computeSizing derives the memory settings from the host and the limit.
func computeSizing(host proto.Facts, limitMB int, cpus int) Sizing {
	var s Sizing
	s.MemBytes = host.MemBytes
	if s.MemBytes <= 0 {
		s.MemBytes = 1 << 30
	}
	s.MemLimitBytes = int64(limitMB) * mib
	s.CPUs = cpus
	if s.CPUs <= 0 {
		s.CPUs = host.CPUs
	}
	// Postgres: shared_buffers is 25% of the limit, or of host RAM / 4 when
	// unlimited; for an unlimited service that is the same quarter of the host.
	base := s.MemLimitBytes
	if base == 0 {
		base = s.MemBytes
	}
	s.EffectiveBytes = base
	baseMB := base / mib
	pg := &s.Postgres
	pg.MaxConnections = 100
	pg.SharedBuffersMB = clamp(baseMB/4, 32, 64<<10)
	pg.EffectiveCacheSizeMB = clamp(baseMB*3/4, 64, 192<<10)
	// work_mem scales with memory per connection, 4 MB to 64 MB.
	pg.WorkMemMB = clamp(baseMB/int64(pg.MaxConnections*4), 4, 64)
	pg.MaintenanceWorkMemMB = clamp(baseMB/16, 64, 2048)
	pg.MaxWALSizeMB = clamp(baseMB, 1024, 4096)

	// MySQL: half of the limit, or a quarter of the host, at least 128 MB.
	if s.MemLimitBytes > 0 {
		s.MySQL.BufferPoolMB = clamp(baseMB/2, 128, 1<<20)
	} else {
		s.MySQL.BufferPoolMB = clamp(baseMB/4, 128, 1<<20)
	}
	// Redis: 80% of the limit; without a limit, half of the host. noeviction
	// then makes writes fail with an error instead of the kernel killing Redis.
	if s.MemLimitBytes > 0 {
		s.Redis.MaxMemoryMB = clamp(baseMB*8/10, 32, 1<<20)
	} else {
		s.Redis.MaxMemoryMB = clamp(baseMB/2, 64, 1<<20)
	}
	// MongoDB: WiredTiger's own rule, 50% of (memory - 1 GB), at least 0.25 GB.
	gb := float64(base) / (1 << 30)
	cache := (gb - 1) / 2
	if cache < 0.25 {
		cache = 0.25
	}
	s.Mongo.CacheSizeGB = strconv.FormatFloat(float64(int(cache*100))/100, 'f', 2, 64)
	return s
}

// renderData is the dot of a template file.
type renderData struct {
	Sizing
	Service     string
	Environment string
	Major       string
	// BackupsConfigured is true when a bucket is set.
	BackupsConfigured bool
}

// Render returns the files, mounts, tools and env of a template service for
// one deploy on a host. Services without a template return a zero Rendered.
func (d *Data) Render(ctx context.Context, snap cpapi.ServiceSnapshot, host proto.Facts) (Rendered, error) {
	if snap.Config.Template == "" {
		return Rendered{}, nil
	}
	t, ts, err := d.cat.serviceFor(snap.Config.Template, snap.Config.Name, snap.Config.Source.Image)
	if err != nil {
		return Rendered{}, err
	}
	b, configured, err := d.bucket(ctx)
	if err != nil {
		configured = false
	}
	cpus := int(snap.Config.Deploy.CPULimit)
	out, err := d.renderService(ctx, snap, t, ts, host, cpus, configured)
	if err != nil {
		return Rendered{}, err
	}

	// Postgres-like services: PGDATA inside the volume (Postgres 18 images
	// moved the default out of the mount path) and continuous archiving.
	if ts.Data == "postgres" {
		out.Env["PGDATA"] = pgData(ts)
		archive := "on"
		if !configured {
			archive = "off"
		} else if d.isUnpromotedCopy(ctx, snap.EnvID, snap.ServiceID) {
			archive = "off"
		} else if sch, err := d.schedule(ctx, snap, recipeInfo{recipe: ts.Backup}); err == nil && !sch.ContinuousWAL {
			archive = "off"
		}
		out.Env["FIFTY_ARCHIVE"] = archive
		if configured && ts.Backup != nil {
			keys, err := d.keys.get(ctx)
			if err != nil {
				return Rendered{}, err
			}
			for k, v := range walOnlyEnv(backupEnv(b, keys, snap.ServiceID, false)) {
				out.Env[k] = v
			}
		}
	}
	return out, nil
}

// renderService renders files and runtime settings without bucket material.
func (d *Data) renderService(ctx context.Context, snap cpapi.ServiceSnapshot, t *manifest.Template, ts *manifest.TemplateService, host proto.Facts, cpus int, configured bool) (Rendered, error) {
	sizing := computeSizing(host, snap.Config.Deploy.MemoryLimitMB, cpus)
	data := renderData{
		Sizing: sizing, Service: snap.Config.Name, Environment: snap.EnvName,
		Major: snap.Config.TemplateVersion, BackupsConfigured: configured,
	}
	var resolved map[string]string
	envFn := func(name string) (string, error) {
		if resolved == nil {
			env, _, err := d.cfg.ResolveVariables(ctx, snap.EnvID, snap.ServiceID, map[string]string{
				"FIFTY_PRIVATE_DOMAIN": privateDomain(snap),
			})
			if err != nil {
				return "", fmt.Errorf("resolve the variables of %s: %w", snap.Config.Name, err)
			}
			resolved = env
		}
		v := resolved[name]
		if v == "" {
			return "", fmt.Errorf("the variable %s of %s is empty", name, snap.Config.Name)
		}
		return v, nil
	}

	out := Rendered{Env: map[string]string{}}
	for _, f := range ts.Files {
		body, err := d.cat.ReadFile(t.ID, f.Template)
		if err != nil {
			return Rendered{}, err
		}
		if strings.HasSuffix(f.Template, ".tmpl") {
			body, err = execTemplate(f.Template, body, data, envFn)
			if err != nil {
				return Rendered{}, fmt.Errorf("The template file %s of %s cannot be rendered: %v.", f.Template, t.ID, strings.TrimSuffix(err.Error(), "."))
			}
		}
		if len(body) > maxFileBytes {
			return Rendered{}, fmt.Errorf("The template file %s is %d bytes; files are limited to 256 KiB.", f.Template, len(body))
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		out.Files = append(out.Files, proto.FileSpec{Path: f.Path, Content: body, Mode: mode})
	}
	for _, tool := range ts.Tools {
		out.Tools = append(out.Tools, tool)
		out.Mounts = append(out.Mounts, toolMount(tool))
	}
	if ts.Probe != nil {
		out.Health = append([]string(nil), ts.Probe.Health...)
		out.Probe = append([]string(nil), ts.Probe.Metrics...)
	}
	switch {
	case ts.Data == "postgres":
		out.StopGraceSec = 120
		out.ShmSizeMB = max(256, snap.Config.Deploy.ShmSizeMB)
	case ts.Data != "" || t.Category == "database":
		out.StopGraceSec = 60
	}
	if t.Category == "database" || ts.Data != "" {
		out.MemoryReservationMB = 512
		if snap.Config.Deploy.MemoryReservationMB > 0 {
			out.MemoryReservationMB = snap.Config.Deploy.MemoryReservationMB
		}
	}
	return out, nil
}

func execTemplate(name string, body []byte, data any, envFn func(string) (string, error)) ([]byte, error) {
	tpl, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
		"env": envFn,
		// quote wraps a value in double quotes for config files (Redis).
		"quote": func(s string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
		},
	}).Parse(string(body))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pgData is the data directory inside the volume.
func pgData(ts *manifest.TemplateService) string {
	if v := ts.Variables["PGDATA"]; v != "" && !strings.Contains(v, "${{") {
		return v
	}
	mount := "/var/lib/postgresql/data"
	if ts.Volume != nil && ts.Volume.MountPath != "" {
		mount = ts.Volume.MountPath
	}
	return strings.TrimRight(mount, "/") + "/pgdata"
}

// renderArgs renders "{{.RetainDays}}" style placeholders in recipe argv.
func renderArgs(argv []string, data any) ([]string, error) {
	out := make([]string, len(argv))
	for i, a := range argv {
		if !strings.Contains(a, "{{") {
			out[i] = a
			continue
		}
		b, err := execTemplate("arg", []byte(a), data, func(string) (string, error) { return "", nil })
		if err != nil {
			return nil, fmt.Errorf("The recipe argument %q cannot be rendered: %w.", a, err)
		}
		out[i] = string(b)
	}
	return out, nil
}
