// SPDX-License-Identifier: MIT

// Package compose maps a docker-compose.yml to 5050cloud services
// (docs/SPEC.md 9.22): images or builds, commands, environment, ports,
// named volumes, depends_on, deploy.replicas, health checks and restart
// policies. Well-known database images become templates. Everything it
// cannot carry over is listed with a reason, so the import report can say
// what was skipped and why. The control plane stages the result.
package compose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/musakkka/5050cloud/pkg/api"
)

// Error is a problem with the file as a whole.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Item is one line of the import report.
type Item = api.ImportItem

// Service is one compose service mapped to a 5050cloud service.
type Service struct {
	// Name is the compose name, made valid for 5050cloud.
	Name string
	// Config is the service config to stage. For template services only
	// Name, Template and TemplateVersion matter.
	Config api.ServiceConfig
	// Template is set for well-known database images ("postgres", "mysql", "redis", "mongodb").
	Template        string
	TemplateVersion string
	Variables       map[string]string
	Volume          *Volume
	DependsOn       []string
	// Domain suggests a generated domain (the first published port looks like HTTP).
	Domain bool
	// TCPProxyPort suggests a TCP proxy on this target port (published non-HTTP port).
	TCPProxyPort int
	// Notes are one-line notes shown in the report for this service.
	Notes []string
}

// Volume is a named volume mount.
type Volume struct {
	Name      string
	MountPath string
}

// Project is the mapped file.
type Project struct {
	Name     string
	Services []Service // in dependency order
	Skipped  []Item
}

// Options carries files referenced by env_file, keyed by the path written in the compose file.
type Options struct {
	EnvFiles map[string]string
}

// MaxFileBytes is the largest compose file read.
const MaxFileBytes = 1 << 20

// Parse maps a compose file.
func Parse(data []byte, o Options) (*Project, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &Error{"The compose file is empty."}
	}
	if len(data) > MaxFileBytes {
		return nil, &Error{"The compose file is larger than 1 MB."}
	}
	js, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, &Error{"The compose file is not valid YAML: " + strings.TrimPrefix(err.Error(), "error converting YAML to JSON: ") + "."}
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, &Error{"The compose file must be a mapping with a services key."}
	}
	rawServices, ok := doc["services"].(map[string]any)
	if !ok || len(rawServices) == 0 {
		return nil, &Error{"The compose file has no services."}
	}
	p := &Project{}
	if n, ok := doc["name"].(string); ok {
		p.Name = n
	}
	for _, k := range sortedKeys(doc) {
		switch k {
		case "services", "volumes", "version", "name":
		default:
			p.Skipped = append(p.Skipped, Item{Kind: "setting", Name: k, Detail: "Top-level " + k + " is not used by 5050cloud: services of one environment already share a private network."})
		}
	}
	namedVolumes := map[string]bool{}
	if vols, ok := doc["volumes"].(map[string]any); ok {
		for k := range vols {
			namedVolumes[k] = true
		}
	}
	names := map[string]string{} // compose name -> 5050 name
	used := map[string]bool{}
	for _, k := range sortedKeys(rawServices) {
		n := cleanName(k)
		base := n
		for i := 2; used[n]; i++ {
			n = trimTo(base, 29) + "-" + strconv.Itoa(i)
		}
		used[n] = true
		names[k] = n
	}
	var svcs []Service
	for _, k := range sortedKeys(rawServices) {
		raw, ok := rawServices[k].(map[string]any)
		if !ok {
			p.Skipped = append(p.Skipped, Item{Kind: "service", Name: k, Detail: "The service definition is not a mapping."})
			continue
		}
		s, skipped := mapService(k, names[k], raw, namedVolumes, names, o)
		p.Skipped = append(p.Skipped, skipped...)
		svcs = append(svcs, s)
	}
	p.Services = order(svcs)
	return p, nil
}

var dbImages = []struct {
	match    *regexp.Regexp
	template string
}{
	{regexp.MustCompile(`(^|/)postgis/postgis$`), "postgis"},
	{regexp.MustCompile(`(^|/)pgvector/pgvector$`), "pgvector"},
	{regexp.MustCompile(`(^|/)(postgres|postgresql)$`), "postgres"},
	{regexp.MustCompile(`(^|/)(mysql|mariadb)$`), "mysql"},
	{regexp.MustCompile(`(^|/)(redis|valkey/valkey|redis/redis-stack-server)$`), "redis"},
	{regexp.MustCompile(`(^|/)(mongo|mongodb/mongodb-community-server)$`), "mongodb"},
}

// DatabaseTemplate returns the template id for a well-known database image, or "".
func DatabaseTemplate(image string) (template, major string) {
	repo, tag := splitImage(image)
	repo = strings.TrimPrefix(repo, "docker.io/")
	repo = strings.TrimPrefix(repo, "library/")
	for _, d := range dbImages {
		if d.match.MatchString(repo) {
			major = tag
			if i := strings.IndexAny(major, ".-"); i >= 0 {
				major = major[:i]
			}
			if _, err := strconv.Atoi(major); err != nil {
				major = ""
			}
			return d.template, major
		}
	}
	return "", ""
}

func splitImage(image string) (repo, tag string) {
	image = strings.TrimSpace(image)
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndexByte(image, '/')
	if colon := strings.LastIndexByte(image, ':'); colon > slash {
		return image[:colon], image[colon+1:]
	}
	return image, ""
}

var handledKeys = map[string]bool{
	"image": true, "build": true, "command": true, "entrypoint": true, "environment": true,
	"env_file": true, "ports": true, "expose": true, "volumes": true, "depends_on": true,
	"deploy": true, "healthcheck": true, "restart": true, "user": true, "shm_size": true,
	"stop_grace_period": true,
}

var skipReasons = map[string]string{
	"container_name": "5050cloud names containers itself.",
	"networks":       "Services of one environment share a private network; reach them at <name>.internal.",
	"network_mode":   "Containers always use their environment's private network.",
	"privileged":     "Privileged containers are not allowed.",
	"cap_add":        "Extra capabilities are not allowed.",
	"devices":        "Host devices are not available to services.",
	"links":          "Services reach each other at <name>.internal.",
	"extra_hosts":    "Custom host entries are not supported.",
	"labels":         "Container labels are managed by 5050cloud.",
	"logging":        "Logs are collected by 5050cloud.",
	"profiles":       "Every service is imported.",
	"secrets":        "Use sealed variables instead.",
	"configs":        "Put configuration in variables or the image.",
	"working_dir":    "Set the working directory in the image.",
	"hostname":       "Hostnames come from the service name.",
	"platform":       "The platform follows the host architecture.",
}

func mapService(composeName, name string, raw map[string]any, namedVolumes map[string]bool, names map[string]string, o Options) (Service, []Item) {
	s := Service{Name: name, Variables: map[string]string{}}
	var skipped []Item
	skip := func(key, why string) {
		skipped = append(skipped, Item{Kind: "setting", Name: composeName + "." + key, Detail: why})
	}
	if name != composeName {
		s.Notes = append(s.Notes, fmt.Sprintf("Renamed from %q to %q to fit service name rules.", composeName, name))
	}
	cfg := api.ServiceConfig{Name: name}
	image, _ := raw["image"].(string)
	if tpl, major := DatabaseTemplate(image); tpl != "" {
		s.Template, s.TemplateVersion = tpl, major
		cfg.Template = tpl
		s.Notes = append(s.Notes, fmt.Sprintf("Image %s becomes the %s template with generated credentials.", image, tpl))
	}
	switch b := raw["build"].(type) {
	case string:
		cfg.Source = api.SourceConfig{Type: "empty", RootDir: cleanDir(b)}
		s.Notes = append(s.Notes, fmt.Sprintf("Builds from %s: connect a repository in Settings > Source.", b))
	case map[string]any:
		ctx, _ := b["context"].(string)
		df, _ := b["dockerfile"].(string)
		cfg.Source = api.SourceConfig{Type: "empty", RootDir: cleanDir(ctx)}
		cfg.Build.Builder = "dockerfile"
		if df != "" {
			cfg.Build.DockerfilePath = df
		}
		s.Notes = append(s.Notes, fmt.Sprintf("Builds from %s: connect a repository in Settings > Source.", orDot(ctx)))
		for _, k := range sortedKeys(b) {
			if k != "context" && k != "dockerfile" {
				skip("build."+k, "Only build context and dockerfile are imported.")
			}
		}
	default:
		if image == "" {
			cfg.Source = api.SourceConfig{Type: "empty"}
			s.Notes = append(s.Notes, "The service has neither image nor build: set a source in Settings > Source.")
		} else if s.Template == "" {
			cfg.Source = api.SourceConfig{Type: "image", Image: image}
		}
	}
	if cfg.Source.Type == "" && s.Template == "" {
		cfg.Source = api.SourceConfig{Type: "image", Image: image}
	}
	// command and entrypoint
	cmd := shellJoin(raw["entrypoint"])
	if c := shellJoin(raw["command"]); c != "" {
		cmd = strings.TrimSpace(cmd + " " + c)
	}
	if cmd != "" && s.Template == "" {
		cfg.Deploy.StartCommand = cmd
	}
	// environment
	if files := toList(raw["env_file"]); len(files) > 0 {
		for _, f := range files {
			text, ok := o.EnvFiles[f]
			if !ok {
				skip("env_file", fmt.Sprintf("The file %s was not included; paste its variables into the Raw Editor.", f))
				continue
			}
			for k, v := range parseEnvFile(text) {
				s.Variables[k] = v
			}
		}
	}
	for k, v := range envMap(raw["environment"]) {
		s.Variables[k] = v
	}
	for k, v := range s.Variables {
		nv, unset := interpolate(v)
		s.Variables[k] = nv
		for _, u := range unset {
			s.Notes = append(s.Notes, fmt.Sprintf("%s uses ${%s} from your shell: set a value.", k, u))
		}
	}
	if s.Template != "" {
		for k := range s.Variables {
			if isTemplateCredential(k) {
				delete(s.Variables, k)
				s.Notes = append(s.Notes, fmt.Sprintf("%s is replaced by the template's generated value.", k))
			}
		}
	}
	// ports
	ports := toList2(raw["ports"])
	for i, pr := range ports {
		target, published, ok := parsePort(pr)
		if !ok {
			skip("ports", fmt.Sprintf("The port %v cannot be read.", pr))
			continue
		}
		if i == 0 {
			cfg.Net.Port = target
			if published > 0 && s.Template == "" {
				if httpPort(target) {
					s.Domain = true
				} else {
					s.TCPProxyPort = target
				}
			}
		} else {
			skip("ports", fmt.Sprintf("Only the first port becomes the target port; port %d was not imported.", target))
		}
	}
	if cfg.Net.Port == 0 {
		if ex := toList2(raw["expose"]); len(ex) > 0 {
			if t, _, ok := parsePort(ex[0]); ok {
				cfg.Net.Port = t
			}
		}
	}
	// volumes
	for _, v := range toList2(raw["volumes"]) {
		src, target, kind := parseVolume(v)
		switch {
		case target == "":
			skip("volumes", fmt.Sprintf("The volume %v cannot be read.", v))
		case kind == "bind":
			skip("volumes", fmt.Sprintf("The bind mount %s:%s reads files from the host, which services cannot do; use a volume and copy the data in.", src, target))
		case s.Template != "":
			skip("volumes", fmt.Sprintf("The %s template brings its own volume; %s was not imported.", s.Template, target))
		case s.Volume != nil:
			skip("volumes", fmt.Sprintf("A service has one volume; %s was not imported.", target))
		default:
			vn := src
			if vn == "" {
				vn = name + "-data"
			} else if !namedVolumes[vn] && kind == "volume" {
				s.Notes = append(s.Notes, fmt.Sprintf("Volume %s is not declared under top-level volumes; it was imported anyway.", vn))
			}
			s.Volume = &Volume{Name: cleanName(vn), MountPath: target}
		}
	}
	// depends_on
	for _, d := range dependsOn(raw["depends_on"]) {
		if n, ok := names[d]; ok {
			s.DependsOn = append(s.DependsOn, n)
		} else {
			skip("depends_on", fmt.Sprintf("%s depends on %s, which is not in the file.", composeName, d))
		}
	}
	// deploy
	if dep, ok := raw["deploy"].(map[string]any); ok {
		for _, k := range sortedKeys(dep) {
			switch k {
			case "replicas":
				if n := toInt(dep[k]); n > 0 {
					cfg.Deploy.Replicas = n
				}
			case "resources":
				res, _ := dep[k].(map[string]any)
				lim, _ := res["limits"].(map[string]any)
				if c, err := strconv.ParseFloat(fmt.Sprint(lim["cpus"]), 64); err == nil && c > 0 {
					cfg.Deploy.CPULimit = c
				}
				if mb := memMB(fmt.Sprint(lim["memory"])); mb > 0 {
					cfg.Deploy.MemoryLimitMB = mb
				}
			default:
				skip("deploy."+k, "Only deploy.replicas and deploy.resources.limits are imported.")
			}
		}
	}
	// healthcheck
	if hc, ok := raw["healthcheck"].(map[string]any); ok {
		if p := healthPath(hc["test"]); p != "" {
			cfg.Deploy.HealthcheckPath = p
		} else if d, _ := hc["disable"].(bool); !d {
			skip("healthcheck", "Only HTTP health checks that call a path are imported; 5050cloud checks the port otherwise.")
		}
	}
	// restart
	if r, ok := raw["restart"].(string); ok {
		pol, retries := restartPolicy(r)
		if pol == "" {
			skip("restart", fmt.Sprintf("Restart policy %q is not known.", r))
		} else {
			cfg.Deploy.RestartPolicy = pol
			cfg.Deploy.RestartMaxRetries = retries
		}
	}
	if u, ok := raw["user"]; ok {
		cfg.Deploy.RunAsUser = fmt.Sprint(u)
	}
	if sh, ok := raw["shm_size"]; ok {
		if mb := memMB(fmt.Sprint(sh)); mb > 0 {
			cfg.Deploy.ShmSizeMB = mb
		}
	}
	if sg, ok := raw["stop_grace_period"].(string); ok {
		if sec := durationSec(sg); sec > 0 {
			cfg.Deploy.DrainingSec = sec
		}
	}
	for _, k := range sortedKeys(raw) {
		if handledKeys[k] {
			continue
		}
		why, ok := skipReasons[k]
		if !ok {
			why = "This compose key is not supported."
		}
		skip(k, why)
	}
	s.Config = cfg
	return s, skipped
}

func isTemplateCredential(k string) bool {
	switch k {
	case "POSTGRES_PASSWORD", "POSTGRES_USER", "MYSQL_ROOT_PASSWORD", "MYSQL_PASSWORD", "MYSQL_USER",
		"MONGO_INITDB_ROOT_USERNAME", "MONGO_INITDB_ROOT_PASSWORD", "REDIS_PASSWORD":
		return true
	}
	return false
}

// order sorts services so dependencies come first (stable by name; cycles keep name order).
func order(svcs []Service) []Service {
	byName := map[string]*Service{}
	for i := range svcs {
		byName[svcs[i].Name] = &svcs[i]
	}
	var out []Service
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(n string)
	visit = func(n string) {
		if state[n] != 0 {
			return
		}
		state[n] = 1
		s := byName[n]
		deps := append([]string(nil), s.DependsOn...)
		sort.Strings(deps)
		for _, d := range deps {
			if _, ok := byName[d]; ok && state[d] == 0 {
				visit(d)
			}
		}
		state[n] = 2
		out = append(out, *s)
	}
	names := make([]string, 0, len(svcs))
	for _, s := range svcs {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		visit(n)
	}
	return out
}

// ---------------------------------------------------------------- value helpers

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var nameChars = regexp.MustCompile(`[^a-zA-Z0-9_ -]+`)

func cleanName(s string) string {
	s = nameChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "- ")
	if s == "" {
		s = "service"
	}
	return trimTo(s, 32)
}

func trimTo(s string, n int) string {
	if len(s) > n {
		return strings.TrimRight(s[:n], "- ")
	}
	return s
}

func cleanDir(d string) string {
	d = strings.TrimSpace(d)
	if d == "" || d == "." || d == "./" {
		return "/"
	}
	d = strings.TrimPrefix(d, "./")
	return "/" + strings.Trim(d, "/")
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}

func toList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			switch y := e.(type) {
			case string:
				out = append(out, y)
			case map[string]any:
				if p, ok := y["path"].(string); ok {
					out = append(out, p)
				}
			}
		}
		return out
	}
	return nil
}

func toList2(v any) []any {
	if x, ok := v.([]any); ok {
		return x
	}
	return nil
}

func toInt(v any) int {
	n, _ := strconv.Atoi(fmt.Sprint(v))
	return n
}

// shellJoin renders a compose command (string or list) as one command line.
func shellJoin(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, shellQuote(fmt.Sprint(e)))
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\$`;&|<>()*?[]#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func envMap(v any) map[string]string {
	out := map[string]string{}
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if val == nil {
				out[k] = ""
			} else {
				out[k] = fmt.Sprint(val)
			}
		}
	case []any:
		for _, e := range x {
			s := fmt.Sprint(e)
			if i := strings.IndexByte(s, '='); i >= 0 {
				out[s[:i]] = s[i+1:]
			} else {
				out[s] = ""
			}
		}
	}
	return out
}

func parseEnvFile(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		if i := strings.IndexByte(line, '='); i > 0 {
			v := strings.TrimSpace(line[i+1:])
			if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
			out[strings.TrimSpace(line[:i])] = v
		}
	}
	return out
}

var interp = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?])([^}]*))?\}`)

// interpolate applies ${VAR:-default}; ${VAR} without a default stays as text and is reported.
func interpolate(v string) (string, []string) {
	var unset []string
	out := interp.ReplaceAllStringFunc(v, func(m string) string {
		sub := interp.FindStringSubmatch(m)
		if sub[2] == ":-" || sub[2] == "-" {
			return sub[3]
		}
		unset = append(unset, sub[1])
		return m
	})
	out = strings.ReplaceAll(out, "$$", "$")
	return out, unset
}

func parsePort(v any) (target, published int, ok bool) {
	switch x := v.(type) {
	case map[string]any:
		target = toInt(x["target"])
		published = toInt(x["published"])
		return target, published, target > 0
	default:
		s := fmt.Sprint(x)
		s = strings.Split(s, "/")[0]
		parts := strings.Split(s, ":")
		last := parts[len(parts)-1]
		if i := strings.IndexByte(last, '-'); i >= 0 {
			last = last[:i]
		}
		t, err := strconv.Atoi(last)
		if err != nil || t < 1 || t > 65535 {
			return 0, 0, false
		}
		if len(parts) >= 2 {
			pub := parts[len(parts)-2]
			if i := strings.IndexByte(pub, '-'); i >= 0 {
				pub = pub[:i]
			}
			published, _ = strconv.Atoi(pub)
		}
		return t, published, true
	}
}

func httpPort(p int) bool {
	switch p {
	case 5432, 3306, 6379, 27017, 22, 25, 53, 1883, 5672, 9042, 11211:
		return false
	}
	return true
}

func parseVolume(v any) (src, target, kind string) {
	switch x := v.(type) {
	case map[string]any:
		src, _ = x["source"].(string)
		target, _ = x["target"].(string)
		kind, _ = x["type"].(string)
		if kind == "" {
			kind = "volume"
		}
		if kind == "tmpfs" {
			return src, target, "bind"
		}
		return src, target, kind
	default:
		s := fmt.Sprint(x)
		parts := strings.Split(s, ":")
		switch len(parts) {
		case 1:
			return "", parts[0], "volume"
		default:
			src, target = parts[0], parts[1]
			if strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") || strings.HasPrefix(src, "~") {
				return src, target, "bind"
			}
			return src, target, "volume"
		}
	}
}

func dependsOn(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case map[string]any:
		return sortedKeys(x)
	}
	return nil
}

var urlInText = regexp.MustCompile(`https?://[^\s'"]+`)

func healthPath(v any) string {
	var text string
	switch x := v.(type) {
	case string:
		text = x
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, fmt.Sprint(e))
		}
		text = strings.Join(parts, " ")
	}
	m := urlInText.FindString(text)
	if m == "" {
		return ""
	}
	u, err := url.Parse(m)
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}

func restartPolicy(r string) (string, int) {
	switch {
	case r == "always" || r == "unless-stopped":
		return "always", 0
	case r == "no" || r == "never":
		return "never", 0
	case r == "on-failure":
		return "on_failure", 0
	case strings.HasPrefix(r, "on-failure:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(r, "on-failure:"))
		return "on_failure", n
	}
	return "", 0
}

var memRe = regexp.MustCompile(`(?i)^\s*([0-9.]+)\s*([kmgt]?)i?b?\s*$`)

func memMB(s string) int {
	m := memRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "":
		f /= 1 << 20
	case "k":
		f /= 1 << 10
	case "g":
		f *= 1 << 10
	case "t":
		f *= 1 << 20
	}
	return int(f)
}

var durRe = regexp.MustCompile(`^(\d+)(ms|s|m|h)?$`)

func durationSec(s string) int {
	m := durRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "ms":
		return n / 1000
	case "m":
		return n * 60
	case "h":
		return n * 3600
	}
	return n
}
