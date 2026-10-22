// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Builder choice (docs/SPEC.md 8.2 step 3) and the Nixpacks and Railpack
// hints (9.20.2). Priority for every field, highest first: the
// environments.<current env> block of the config file, the file's base
// fields, the service setting, then the matching variable.

const (
	builderRailpack   = "railpack"
	builderDockerfile = "dockerfile"
)

// buildPlan is the resolved way to build one job.
type buildPlan struct {
	Builder            string // builderRailpack or builderDockerfile
	FromNixpacks       bool   // builder NIXPACKS was asked for
	DetectedDockerfile bool   // auto picked a Dockerfile found in the root directory
	DockerfilePath     string // relative to the context directory, slash separated
	BuildCommand       string
	StartCommand       string
	Railpack           RailpackPin
	NoCache            bool
	// Env is what the build sees: the service variables plus the mapped
	// Nixpacks hints (Railpack builds only).
	Env map[string]string
	// Log lines to print before the build starts, and notices for the
	// deployment Details.
	Log     []string
	Notices []string
}

// normalizeBuilder maps the setting or file value to auto, railpack,
// dockerfile or nixpacks.
func normalizeBuilder(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return "auto", nil
	case "railpack":
		return builderRailpack, nil
	case "dockerfile":
		return builderDockerfile, nil
	case "nixpacks":
		return "nixpacks", nil
	case "heroku", "paketo":
		return "", fail("The %s builder is not supported. Use RAILPACK or DOCKERFILE.", strings.ToUpper(strings.TrimSpace(s)))
	}
	return "", fail("The builder %s is not known. Use RAILPACK or DOCKERFILE.", s)
}

// environmentName is the deployment's environment, from the provided
// variables (9.3).
func environmentName(env map[string]string) string {
	for _, k := range []string{"RAILWAY_ENVIRONMENT_NAME", "FIFTY_ENVIRONMENT_NAME", "RAILWAY_ENVIRONMENT"} {
		if v := env[k]; v != "" {
			return v
		}
	}
	return ""
}

// fileFields picks build and deploy values from the overlay, the current
// environment's block first.
type fileFields struct {
	cfg *manifest.RailwayConfig
	env string
}

func (f fileFields) build(get func(*manifest.RailwayBuild) *string) string {
	if f.cfg == nil {
		return ""
	}
	if e, ok := f.cfg.Environments[f.env]; ok && f.env != "" && e.Build != nil {
		if v := get(e.Build); v != nil && *v != "" {
			return *v
		}
	}
	if f.cfg.Build != nil {
		if v := get(f.cfg.Build); v != nil && *v != "" {
			return *v
		}
	}
	return ""
}

func (f fileFields) startCommand() string {
	if f.cfg == nil {
		return ""
	}
	if e, ok := f.cfg.Environments[f.env]; ok && f.env != "" && e.Deploy != nil && e.Deploy.StartCommand != nil && *e.Deploy.StartCommand != "" {
		return *e.Deploy.StartCommand
	}
	if f.cfg.Deploy != nil && f.cfg.Deploy.StartCommand != nil {
		return *f.cfg.Deploy.StartCommand
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// resolvePlan decides how to build. contextDir is the service root
// directory inside the checkout.
func resolvePlan(job proto.BuildJob, ov *proto.ConfigOverlay, hints *manifest.NixpacksHints, contextDir string) (buildPlan, error) {
	var p buildPlan
	f := fileFields{env: environmentName(job.Env)}
	if ov != nil {
		f.cfg = &ov.Config
	}

	want, err := normalizeBuilder(firstNonEmpty(f.build(func(b *manifest.RailwayBuild) *string { return b.Builder }), job.Builder))
	if err != nil {
		return p, err
	}
	dockerfile := firstNonEmpty(
		f.build(func(b *manifest.RailwayBuild) *string { return b.DockerfilePath }),
		job.DockerfilePath,
		job.Env["RAILWAY_DOCKERFILE_PATH"],
		job.Env["FIFTY_DOCKERFILE_PATH"],
	)
	explicitDockerfile := dockerfile != ""
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	dfRel := cleanRel(dockerfile)
	dfExists := false
	if dfRel != "" && insideRoot(contextDir, dfRel) == nil {
		if st, err := os.Stat(filepath.Join(contextDir, filepath.FromSlash(dfRel))); err == nil && st.Mode().IsRegular() {
			dfExists = true
		}
	}

	switch want {
	case "auto":
		switch {
		case dfExists:
			p.Builder = builderDockerfile
			p.DetectedDockerfile = true
		case explicitDockerfile:
			return p, fail("%s", dockerfileMissingSentence(dockerfile))
		default:
			p.Builder = builderRailpack
		}
	case builderDockerfile:
		if !dfExists {
			return p, fail("%s", dockerfileMissingSentence(dockerfile))
		}
		p.Builder = builderDockerfile
	case "nixpacks":
		p.Builder = builderRailpack
		p.FromNixpacks = true
	default:
		p.Builder = builderRailpack
	}
	if p.Builder == builderDockerfile {
		p.DockerfilePath = dfRel
	}

	version := firstNonEmpty(f.build(func(b *manifest.RailwayBuild) *string { return b.RailpackVersion }), job.RailpackVersion)
	pin, ok := railpackPin(strings.TrimSpace(version))
	if !ok {
		return p, fail("Railpack %s is not available on this install. Available: %s.", version, strings.Join(RailpackVersions(), ", "))
	}
	p.Railpack = pin

	var nixBuild, nixStart string
	if hints != nil {
		nixStart = hints.StartCmd
		nixBuild = strings.Join(hints.BuildCmds, " && ")
	}
	p.BuildCommand = firstNonEmpty(f.build(func(b *manifest.RailwayBuild) *string { return b.BuildCommand }), job.BuildCommand, nixBuild)
	p.StartCommand = firstNonEmpty(f.startCommand(), job.StartCommand, nixStart)
	p.NoCache = job.NoCache || truthy(job.Env["NO_CACHE"]) || truthy(job.Env["FIFTY_NO_CACHE"])

	p.Env = make(map[string]string, len(job.Env)+8)
	for k, v := range job.Env {
		p.Env[k] = v
	}
	if p.Builder == builderRailpack {
		p.applyNixpacks(hints)
	}

	switch {
	case p.FromNixpacks:
		p.Log = append(p.Log, "Nixpacks config detected, building with Railpack.")
	case p.DetectedDockerfile:
		p.Log = append(p.Log, "Using detected Dockerfile")
	}
	if p.Builder == builderDockerfile && p.BuildCommand != "" {
		p.Log = append(p.Log, "The custom build command is not used for Dockerfile builds; put build steps in the Dockerfile.")
	}
	return p, nil
}

// nixpacksVarMap maps the Nixpacks variables Railpack has an equivalent for.
var nixpacksVarMap = map[string]string{
	"NIXPACKS_BUILD_CMD":      "RAILPACK_BUILD_CMD",
	"NIXPACKS_START_CMD":      "RAILPACK_START_CMD",
	"NIXPACKS_INSTALL_CMD":    "RAILPACK_INSTALL_CMD",
	"NIXPACKS_NODE_VERSION":   "RAILPACK_NODE_VERSION",
	"NIXPACKS_PYTHON_VERSION": "RAILPACK_PYTHON_VERSION",
}

// applyNixpacks maps nixpacks.toml hints and NIXPACKS_* variables onto the
// Railpack environment. A value the service sets itself always wins.
func (p *buildPlan) applyNixpacks(h *manifest.NixpacksHints) {
	setIfEmpty := func(k, v string) bool {
		if v == "" || p.Env[k] != "" {
			return false
		}
		p.Env[k] = v
		return true
	}
	if h != nil {
		names := make([]string, 0, len(h.Variables))
		for k := range h.Variables {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			setIfEmpty(k, h.Variables[k])
		}
		if setIfEmpty("RAILPACK_PACKAGES", strings.Join(packageList(h.NixPkgs), " ")) {
			p.Log = append(p.Log, "nixpacks.toml: nixPkgs were passed to Railpack as RAILPACK_PACKAGES.")
		}
		if setIfEmpty("RAILPACK_DEPLOY_APT_PACKAGES", strings.Join(packageList(h.AptPkgs), " ")) {
			p.Log = append(p.Log, "nixpacks.toml: aptPkgs were passed to Railpack as RAILPACK_DEPLOY_APT_PACKAGES.")
		}
	}
	var names []string
	for k := range p.Env {
		if strings.HasPrefix(k, "NIXPACKS_") {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		if to, ok := nixpacksVarMap[k]; ok {
			if setIfEmpty(to, p.Env[k]) {
				p.Log = append(p.Log, fmt.Sprintf("Variable %s was passed to Railpack as %s.", k, to))
			}
			continue
		}
		p.Notices = append(p.Notices, fmt.Sprintf("Variable %s is not supported by Railpack and was ignored.", k))
	}
}

// packageList drops Nixpacks' "..." marker ("extend the defaults"), which
// Railpack does not need, and blank entries.
func packageList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" || p == "..." || p == "@auto" {
			continue
		}
		out = append(out, p)
	}
	return out
}
