// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// catalog reads templates.FS: template manifests, tools.json and images.json.
// Everything is parsed once and cached; the FS is embedded and never changes
// while 5050d runs.
type catalog struct {
	fsys fs.FS

	mu        sync.Mutex
	templates map[string]*manifest.Template
	images    *imagesFile
	tools     *toolsFile
}

func newCatalog(fsys fs.FS) *catalog {
	return &catalog{fsys: fsys, templates: map[string]*manifest.Template{}}
}

// validTemplateID guards the template id taken from a service config before it
// becomes a path.
func validTemplateID(id string) bool {
	if id == "" || len(id) > 64 || id[0] == '_' || id[0] == '.' {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// Template returns the parsed templates/<id>/template.yaml.
func (c *catalog) Template(id string) (*manifest.Template, error) {
	if !validTemplateID(id) {
		return nil, fmt.Errorf("%q is not a valid template id.", id)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.templates[id]; ok {
		return t, nil
	}
	if c.fsys == nil {
		return nil, errors.New("No templates are available to this control plane.")
	}
	b, err := fs.ReadFile(c.fsys, id+"/template.yaml")
	if err != nil {
		return nil, fmt.Errorf("The template %q does not exist in this version of 5050cloud.", id)
	}
	var t manifest.Template
	if err := yaml.UnmarshalStrict(b, &t); err != nil {
		return nil, fmt.Errorf("The template %q cannot be read: %v.", id, err)
	}
	if t.ID != id {
		return nil, fmt.Errorf("The template in templates/%s says its id is %q.", id, t.ID)
	}
	c.templates[id] = &t
	return &t, nil
}

// TemplateIDs lists every template directory.
func (c *catalog) TemplateIDs() ([]string, error) {
	entries, err := fs.ReadDir(c.fsys, ".")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && validTemplateID(e.Name()) {
			if _, err := fs.Stat(c.fsys, e.Name()+"/template.yaml"); err == nil {
				ids = append(ids, e.Name())
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// serviceFor finds the template service a deployed service was created from.
// ServiceConfig.Template names the template directory that holds the runtime
// rules; when that template has several services with the same template id
// (Plausible and its ClickHouse), the service name decides, then the image
// repository, so a renamed service still matches.
func (c *catalog) serviceFor(templateID, name, image string) (*manifest.Template, *manifest.TemplateService, error) {
	t, err := c.Template(templateID)
	if err != nil {
		return nil, nil, err
	}
	var cands []*manifest.TemplateService
	for i := range t.Services {
		s := &t.Services[i]
		if s.Config.Template == templateID || (s.Config.Template == "" && len(t.Services) == 1) {
			cands = append(cands, s)
		}
	}
	switch len(cands) {
	case 0:
		return nil, nil, fmt.Errorf("The template %q has no service marked with its id.", templateID)
	case 1:
		return t, cands[0], nil
	}
	for _, s := range cands {
		if s.Name == name || s.Config.Name == name {
			return t, s, nil
		}
	}
	repo := imageRepo(image)
	for _, s := range cands {
		if imageRepo(s.Config.Source.Image) == repo {
			return t, s, nil
		}
		for _, img := range s.Images {
			if imageRepo(img) == repo {
				return t, s, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("The service %q does not match any service of the template %q.", name, templateID)
}

// ReadFile reads a file a template refers to. A name without a slash is
// relative to the template's directory; a name with a slash is relative to
// the templates root ("postgres/archive-wal.sh", "_shared/restic-env.sh"),
// so templates can share files.
func (c *catalog) ReadFile(templateID, name string) ([]byte, error) {
	p := name
	if !strings.Contains(name, "/") {
		p = templateID + "/" + name
	}
	p = path.Clean(p)
	if strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || p == ".." {
		return nil, fmt.Errorf("The template file %q is outside the templates directory.", name)
	}
	b, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		return nil, fmt.Errorf("The template file %q is missing.", p)
	}
	return b, nil
}

// imageRepo strips the tag and digest: "ghcr.io/o/r:1.2@sha256:..." -> "ghcr.io/o/r".
func imageRepo(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		ref = ref[:i]
	}
	return ref
}

// ---------------------------------------------------------------- images.json

type imagesFile struct {
	Images map[string]struct {
		Digest    string   `json:"digest"`
		Platforms []string `json:"platforms"`
	} `json:"images"`
	// Releases maps a template id and a major version to the newest image.
	Releases map[string]map[string]string `json:"releases"`
	// Helpers are images the data services run jobs in ("job": busybox).
	Helpers map[string]string `json:"helpers"`
}

func (c *catalog) imagesFile() (*imagesFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.images != nil {
		return c.images, nil
	}
	b, err := fs.ReadFile(c.fsys, "images.json")
	if err != nil {
		return nil, errors.New("templates/images.json is missing from this build.")
	}
	var f imagesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("templates/images.json cannot be read: %v.", err)
	}
	c.images = &f
	return &f, nil
}

// HelperImage returns the pinned helper image for jobs that need only a shell
// (volume backups, wipes, restic retention), pinned by digest.
func (c *catalog) HelperImage() (string, error) {
	f, err := c.imagesFile()
	if err != nil {
		return "", err
	}
	ref := f.Helpers["job"]
	if ref == "" {
		return "", errors.New("templates/images.json pins no helper image for jobs.")
	}
	return c.pinned(f, ref), nil
}

// Pinned returns ref with its digest from images.json appended when known.
func (c *catalog) Pinned(ref string) string {
	f, err := c.imagesFile()
	if err != nil {
		return ref
	}
	return c.pinned(f, ref)
}

func (c *catalog) pinned(f *imagesFile, ref string) string {
	if strings.Contains(ref, "@") {
		return ref
	}
	if img, ok := f.Images[ref]; ok && img.Digest != "" {
		return ref + "@" + img.Digest
	}
	return ref
}

// ---------------------------------------------------------------- tools.json

type toolsFile struct {
	Tools []struct {
		Name    string            `json:"name"`
		Version string            `json:"version"`
		URLs    map[string]string `json:"urls"`
		SHA256  map[string]string `json:"sha256"`
		Path    map[string]string `json:"path"`
	} `json:"tools"`
}

// Tools returns the pinned helper binaries of templates/tools.json for one
// architecture ("amd64" or "arm64") as the agent protocol wants them
// (proto.HostConfig.Tools). WP2a calls it when it builds a host config.
func Tools(fsys fs.FS, arch string) ([]proto.Tool, error) {
	b, err := fs.ReadFile(fsys, "tools.json")
	if err != nil {
		return nil, errors.New("templates/tools.json is missing from this build.")
	}
	var f toolsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("templates/tools.json cannot be read: %v.", err)
	}
	out := make([]proto.Tool, 0, len(f.Tools))
	for _, t := range f.Tools {
		url, sum := t.URLs[arch], t.SHA256[arch]
		if url == "" || sum == "" {
			return nil, fmt.Errorf("templates/tools.json has no %s build of %s.", arch, t.Name)
		}
		out = append(out, proto.Tool{Name: t.Name, Version: t.Version, URL: url, SHA256: sum, Path: t.Path[arch]})
	}
	return out, nil
}

// ImageDigest returns the digest images.json pins for an image reference
// ("postgres:17.10-bookworm"), or "" when the image is not pinned there.
func ImageDigest(fsys fs.FS, ref string) string {
	c := newCatalog(fsys)
	f, err := c.imagesFile()
	if err != nil {
		return ""
	}
	return f.Images[ref].Digest
}
