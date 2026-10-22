// SPDX-License-Identifier: MIT

package configfile

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/manifest"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Result is the effective config of one deployment.
type Result struct {
	Config api.ServiceConfig
	// Source maps ServiceConfig field paths ("deploy.startCommand") to
	// "file:line" ("railway.json:12"), or "file" when the line is unknown.
	Source map[string]string
	// WatchPaths from the file apply to the next push.
	WatchPaths []string
	// Notices are one line each for fields that were read but ignored.
	Notices []string
}

// Merge applies a config file over the service settings in Railway's
// priority order: the environments.<envName> block of the file, then the
// file's base fields, then the settings in base. Hints from nixpacks.toml
// fill the start and build commands only when nothing else sets them,
// unless build.nixpacksCompat is false. ov may be nil.
func Merge(base api.ServiceConfig, ov *proto.ConfigOverlay, envName string) Result {
	r := Result{Config: base, Source: map[string]string{}}
	if ov == nil {
		return r
	}
	file := path.Base(ov.Path)
	if ov.Path == "" {
		file = "config file"
	}
	loc := func(field string) string {
		if l := ov.Lines[field]; l > 0 {
			return fmt.Sprintf("%s:%d", file, l)
		}
		return file
	}
	apply := func(prefix string, b *manifest.RailwayBuild, d *manifest.RailwayDeploy) {
		applyBuild(&r, prefix, b, loc)
		applyDeploy(&r, prefix, d, loc, file)
	}
	apply("", ov.Config.Build, ov.Config.Deploy)
	if e, ok := ov.Config.Environments[envName]; ok {
		apply("environments."+envName+".", e.Build, e.Deploy)
	}
	compat := base.Build.NixpacksCompat == nil || *base.Build.NixpacksCompat
	if ov.Nixpacks != nil && compat {
		n := ov.Nixpacks
		nloc := func(field string) string {
			if l := n.SourceLines[field]; l > 0 {
				return fmt.Sprintf("nixpacks.toml:%d", l)
			}
			return "nixpacks.toml"
		}
		if r.Config.Deploy.StartCommand == "" && n.StartCmd != "" {
			r.Config.Deploy.StartCommand = n.StartCmd
			r.Source["deploy.startCommand"] = nloc("startCmd")
		}
		if r.Config.Build.BuildCommand == "" && len(n.BuildCmds) > 0 {
			r.Config.Build.BuildCommand = strings.Join(n.BuildCmds, " && ")
			r.Source["build.buildCommand"] = nloc("buildCmds")
		}
		for _, k := range n.IgnoredKeys {
			r.Notices = append(r.Notices, fmt.Sprintf("nixpacks.toml: %s is not supported by Railpack and was ignored.", k))
		}
	}
	if r.Config.Build.Builder == "nixpacks" {
		r.Notices = append([]string{"Nixpacks config detected, building with Railpack."}, r.Notices...)
	}
	r.Notices = dedupe(r.Notices)
	return r
}

func applyBuild(r *Result, prefix string, b *manifest.RailwayBuild, loc func(string) string) {
	if b == nil {
		return
	}
	c := &r.Config.Build
	if b.Builder != nil {
		c.Builder = strings.ToLower(*b.Builder)
		r.Source["build.builder"] = loc(prefix + "build.builder")
	}
	if b.BuildCommand != nil {
		c.BuildCommand = *b.BuildCommand
		r.Source["build.buildCommand"] = loc(prefix + "build.buildCommand")
	}
	if b.DockerfilePath != nil {
		c.DockerfilePath = *b.DockerfilePath
		r.Source["build.dockerfilePath"] = loc(prefix + "build.dockerfilePath")
	}
	if b.WatchPatterns != nil {
		c.WatchPaths = append([]string(nil), b.WatchPatterns...)
		r.WatchPaths = c.WatchPaths
		r.Source["build.watchPaths"] = loc(prefix + "build.watchPatterns")
	}
	if b.RailpackVersion != nil {
		c.RailpackVersion = *b.RailpackVersion
		r.Source["build.railpackVersion"] = loc(prefix + "build.railpackVersion")
	}
	if len(b.NixpacksPlan) > 0 && string(b.NixpacksPlan) != "null" {
		r.Notices = append(r.Notices, fileOf(loc(""))+": nixpacksPlan is not supported in 5050cloud v0.1 and was ignored.")
	}
}

func fileOf(loc string) string {
	if i := strings.LastIndexByte(loc, ':'); i > 0 {
		return loc[:i]
	}
	return loc
}

func applyDeploy(r *Result, prefix string, d *manifest.RailwayDeploy, loc func(string) string, file string) {
	if d == nil {
		return
	}
	c := &r.Config.Deploy
	set := func(field, src string) { r.Source[field] = loc(prefix + src) }
	if d.StartCommand != nil {
		c.StartCommand = *d.StartCommand
		set("deploy.startCommand", "deploy.startCommand")
	}
	if d.PreDeployCommand != nil {
		c.PreDeployCommand = strings.Join(*d.PreDeployCommand, " && ")
		set("deploy.preDeployCommand", "deploy.preDeployCommand")
	}
	if d.HealthcheckPath != nil {
		c.HealthcheckPath = *d.HealthcheckPath
		set("deploy.healthcheckPath", "deploy.healthcheckPath")
	}
	if d.HealthcheckTimeout != nil {
		c.HealthcheckTimeoutSec = *d.HealthcheckTimeout
		set("deploy.healthcheckTimeoutSec", "deploy.healthcheckTimeout")
	}
	if d.RestartPolicyType != nil {
		c.RestartPolicy = strings.ToLower(*d.RestartPolicyType)
		set("deploy.restartPolicy", "deploy.restartPolicyType")
	}
	if d.RestartPolicyMaxRetries != nil {
		c.RestartMaxRetries = *d.RestartPolicyMaxRetries
		set("deploy.restartMaxRetries", "deploy.restartPolicyMaxRetries")
	}
	if d.CronSchedule != nil {
		c.CronSchedule = *d.CronSchedule
		set("deploy.cronSchedule", "deploy.cronSchedule")
	}
	if len(d.MultiRegionConfig) > 0 {
		sum := 0
		regions := make([]string, 0, len(d.MultiRegionConfig))
		for name, reg := range d.MultiRegionConfig {
			sum += reg.NumReplicas
			regions = append(regions, name)
		}
		sort.Strings(regions)
		if sum > 0 {
			c.Replicas = sum
			set("deploy.replicas", "deploy.multiRegionConfig")
		}
		r.Notices = append(r.Notices, fmt.Sprintf("%s: multiRegionConfig contributes only its replica count (%d); regions are your hosts in 5050cloud.", file, sum))
	} else if d.NumReplicas != nil {
		c.Replicas = *d.NumReplicas
		set("deploy.replicas", "deploy.numReplicas")
	}
	if d.OverlapSeconds != nil {
		c.OverlapSec = *d.OverlapSeconds
		set("deploy.overlapSec", "deploy.overlapSeconds")
	}
	if d.DrainingSeconds != nil {
		c.DrainingSec = *d.DrainingSeconds
		set("deploy.drainingSec", "deploy.drainingSeconds")
	}
	if d.Processes != nil {
		c.Processes = *d.Processes
		set("deploy.processes", "deploy.processes")
	}
	if d.Placement != nil {
		c.Placement = *d.Placement
		set("deploy.placement", "deploy.placement")
	}
	if d.Singleton != nil {
		v := *d.Singleton
		c.Singleton = &v
		set("deploy.singleton", "deploy.singleton")
	}
	if d.SleepApplication != nil && *d.SleepApplication {
		r.Notices = append(r.Notices, file+": sleepApplication is not supported in 5050cloud v0.1 and was ignored.")
	}
	if d.Runtime != nil {
		r.Notices = append(r.Notices, file+": runtime is a Railway setting and was ignored.")
	}
	if d.Region != nil {
		r.Notices = append(r.Notices, file+": region is a Railway setting and was ignored; placement uses your hosts.")
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Overlay turns a parsed file (and optional nixpacks hints) into the wire form.
func Overlay(f *File, nix *manifest.NixpacksHints) *proto.ConfigOverlay {
	if f == nil && nix == nil {
		return nil
	}
	ov := &proto.ConfigOverlay{Nixpacks: nix, Lines: map[string]int{}}
	if f != nil && !f.ProjectFile {
		ov.Path, ov.Format, ov.Config, ov.Lines = f.Path, f.Format, f.Config, f.Lines
	}
	return ov
}

// Sets lists the ServiceConfig field paths a file sets (the scan dialog).
func Sets(f *File, envName string) []string {
	if f == nil || f.ProjectFile {
		return nil
	}
	r := Merge(api.ServiceConfig{}, Overlay(f, nil), envName)
	out := make([]string, 0, len(r.Source))
	for k := range r.Source {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
