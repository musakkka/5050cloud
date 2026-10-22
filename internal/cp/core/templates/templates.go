// SPDX-License-Identifier: AGPL-3.0-only

// Package templates loads the built-in template manifests from templates.FS
// (docs/SPEC.md 10). Core uses them at creation; files, tools and backup
// recipes are rendered by the orchestrator's data services.
package templates

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
)

// Catalog is the parsed set of templates.
type Catalog struct {
	FS   fs.FS
	list []manifest.Template
	byID map[string]manifest.Template
}

// Load reads every <id>/template.yaml in fsys.
func Load(fsys fs.FS) (*Catalog, error) {
	c := &Catalog{FS: fsys, byID: map[string]manifest.Template{}}
	if fsys == nil {
		return c, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(e.Name(), "template.yaml"))
		if err != nil {
			continue
		}
		var t manifest.Template
		if err := yaml.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("template %s: %w", e.Name(), err)
		}
		if t.ID == "" {
			t.ID = e.Name()
		}
		c.list = append(c.list, t)
		c.byID[t.ID] = t
	}
	sort.Slice(c.list, func(i, j int) bool {
		if c.list[i].Category != c.list[j].Category {
			return c.list[i].Category < c.list[j].Category
		}
		return c.list[i].Name < c.list[j].Name
	})
	return c, nil
}

// Get returns one template.
func (c *Catalog) Get(id string) (manifest.Template, bool) {
	t, ok := c.byID[id]
	return t, ok
}

// Summary is the API form of a template.
func Summary(t manifest.Template) api.TemplateSummary {
	s := api.TemplateSummary{ID: t.ID, Name: t.Name, Tagline: t.Tagline, Description: t.Description, Docs: t.Docs,
		Icon: "/api/v1/icons/" + t.ID + ".svg", Category: t.Category, Services: []string{}, Inputs: t.Inputs}
	if s.Inputs == nil {
		s.Inputs = []api.TemplateInput{}
	}
	for _, sv := range t.Services {
		s.Services = append(s.Services, sv.Name)
	}
	return s
}

// Detail is the API detail form.
func Detail(t manifest.Template) api.TemplateDetail {
	d := api.TemplateDetail{TemplateSummary: Summary(t), MajorVersions: t.MajorVersions, ServiceInfo: []api.TemplateServiceInfo{}}
	for _, sv := range t.Services {
		info := api.TemplateServiceInfo{Name: sv.Name, Image: sv.Config.Source.Image, Domain: sv.Domain, DependsOn: sv.DependsOn}
		if sv.Volume != nil {
			info.Volume = sv.Volume.MountPath
		}
		d.ServiceInfo = append(d.ServiceInfo, info)
	}
	return d
}

// List returns templates matching q (name, tagline or id).
func (c *Catalog) List(q string) []api.TemplateSummary {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []api.TemplateSummary{}
	for _, t := range c.list {
		if q == "" || strings.Contains(strings.ToLower(t.Name+" "+t.ID+" "+t.Tagline), q) {
			out = append(out, Summary(t))
		}
	}
	return out
}

// Icon returns the SVG icon of a template, or of any template service icon key.
func (c *Catalog) Icon(name string) ([]byte, bool) {
	id := strings.TrimSuffix(name, ".svg")
	if strings.ContainsAny(id, "/\\") || id == "" {
		return nil, false
	}
	if t, ok := c.byID[id]; ok && t.Icon != "" {
		if b, err := fs.ReadFile(c.FS, path.Join(t.ID, t.Icon)); err == nil {
			return b, true
		}
	}
	for _, t := range c.list {
		if b, err := fs.ReadFile(c.FS, path.Join(t.ID, id+".svg")); err == nil {
			return b, true
		}
	}
	return nil, false
}

// Ordered returns the services in dependency order.
func Ordered(t manifest.Template) []manifest.TemplateService {
	done := map[string]bool{}
	var out []manifest.TemplateService
	for len(out) < len(t.Services) {
		progress := false
		for _, s := range t.Services {
			if done[s.Name] {
				continue
			}
			ready := true
			for _, d := range s.DependsOn {
				if !done[d] {
					ready = false
				}
			}
			if ready {
				done[s.Name] = true
				out = append(out, s)
				progress = true
			}
		}
		if !progress {
			for _, s := range t.Services {
				if !done[s.Name] {
					done[s.Name] = true
					out = append(out, s)
				}
			}
		}
	}
	return out
}
