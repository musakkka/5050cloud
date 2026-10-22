// SPDX-License-Identifier: AGPL-3.0-only

package changes

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/vars"
	"github.com/musakkka/5050cloud/pkg/api"
)

var serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9_ -]+$`)

// CheckServiceName validates a service name (1 to 32 characters).
func CheckServiceName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fail.Validation("name", "A service needs a name.")
	case len(name) > MaxServiceNameLen:
		return fail.Validation("name", "The service name %q is %d characters long; the limit is 32.", name, len(name))
	case !serviceNameRe.MatchString(name):
		return fail.Validation("name", "The service name %q may contain only letters, digits, spaces, dashes and underscores.", name)
	case name != strings.TrimSpace(name):
		return fail.Validation("name", "The service name cannot start or end with a space.")
	}
	return nil
}

var privateNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Slug turns a service name into a DNS label: "My API" -> "my-api".
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	if s == "" {
		s = "service"
	}
	return s
}

// PrivateName is the private DNS name of a service: networking.privateName or the slug of its name.
func PrivateName(c api.ServiceConfig) string {
	if c.Net.PrivateName != "" {
		return c.Net.PrivateName
	}
	return Slug(c.Name)
}

// CronParser accepts 5-field crontab lines and the @hourly family.
var CronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// CheckCron validates a schedule and its time zone.
func CheckCron(schedule, tz string) error {
	if schedule == "" {
		return nil
	}
	if strings.HasPrefix(schedule, "@every") {
		return fail.Validation("deploy.cronSchedule", "Use a 5-field crontab line or @hourly, @daily, @weekly or @monthly; @every is not supported.")
	}
	if _, err := CronParser.Parse(schedule); err != nil {
		return fail.Validation("deploy.cronSchedule", "%q is not a valid cron schedule: %s.", schedule, err.Error())
	}
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fail.Validation("deploy.cronTimezone", "%q is not a known time zone. Use an IANA name such as Europe/Berlin.", tz)
		}
	}
	return nil
}

// Validate checks a config as committed (9.5). hasVolume is the state
// after the commit. The error names the field.
func Validate(c api.ServiceConfig, hasVolume bool) error {
	if err := CheckServiceName(c.Name); err != nil {
		return err
	}
	if c.Net.PrivateName != "" && !privateNameRe.MatchString(c.Net.PrivateName) {
		return fail.Validation("networking.privateName", "The private name %q must be a DNS label: lower-case letters, digits and dashes.", c.Net.PrivateName)
	}
	switch c.Source.Type {
	case "github", "image", "upload", "empty", "":
	default:
		return fail.Validation("source.type", "%q is not a source type. Use github, image, upload or empty.", c.Source.Type)
	}
	if c.Source.Type == "image" && c.Template == "" && strings.TrimSpace(c.Source.Image) == "" {
		return fail.Validation("source.image", "An image service needs an image, for example nginx:1.27.")
	}
	if c.Source.Type == "github" && strings.TrimSpace(c.Source.Repo) == "" {
		return fail.Validation("source.repo", "A GitHub service needs a repository, as owner/name or a public https URL.")
	}
	if c.Source.WaitForCI {
		return fail.Validation("source.waitForCi", "Wait for CI is not in 5050cloud v0.1. Deploys start when the push arrives.")
	}
	if c.Source.RootDir != "" && !strings.HasPrefix(c.Source.RootDir, "/") {
		return fail.Validation("source.rootDir", "The root directory must start with /.")
	}
	switch c.Build.Builder {
	case "", "auto", "railpack", "dockerfile", "nixpacks":
	default:
		return fail.Validation("build.builder", "%q is not a builder. Use auto, railpack, dockerfile or nixpacks.", c.Build.Builder)
	}
	if c.Build.TimeoutMin < 0 || c.Build.TimeoutMin > 720 {
		return fail.Validation("build.timeoutMin", "The build timeout must be between 1 and 720 minutes.")
	}
	d := c.Deploy
	if d.Replicas < 0 {
		return fail.Validation("deploy.replicas", "Replicas must be at least 1.")
	}
	if d.Replicas > 100 {
		return fail.Validation("deploy.replicas", "Replicas must be at most 100.")
	}
	if hasVolume && d.Replicas > 1 {
		return fail.Validation("deploy.replicas", "A service with a volume runs exactly 1 replica, because two containers cannot share one volume.")
	}
	if err := CheckCron(d.CronSchedule, d.CronTimezone); err != nil {
		return err
	}
	switch d.CronOverlap {
	case "", "skip", "queue":
	default:
		return fail.Validation("deploy.cronOverlap", "Cron overlap must be skip or queue.")
	}
	if d.HealthcheckPath != "" && !strings.HasPrefix(d.HealthcheckPath, "/") {
		return fail.Validation("deploy.healthcheckPath", "The health check path must start with /.")
	}
	if d.HealthcheckTimeoutSec < 0 || d.HealthcheckTimeoutSec > 3600 {
		return fail.Validation("deploy.healthcheckTimeoutSec", "The health check timeout must be between 1 and 3600 seconds.")
	}
	if d.PreDeployTimeoutSec < 0 || d.PreDeployTimeoutSec > 3600 {
		return fail.Validation("deploy.preDeployTimeoutSec", "The pre-deploy timeout must be between 1 and 3600 seconds.")
	}
	switch d.RestartPolicy {
	case "", "on_failure", "always", "never":
	default:
		return fail.Validation("deploy.restartPolicy", "The restart policy must be on_failure, always or never.")
	}
	if d.RestartMaxRetries < 0 || d.OverlapSec < 0 || d.DrainingSec < 0 || d.CronTimeoutSec < 0 {
		return fail.Validation("deploy", "Retries, overlap, draining and timeouts cannot be negative.")
	}
	if d.MaxSurge != "" && !validSurge(d.MaxSurge) {
		return fail.Validation("deploy.maxSurge", "Max surge must be a number such as 1 or a percentage such as 25%%.")
	}
	if d.Processes != "" && d.Processes != "auto" {
		n, err := strconv.Atoi(d.Processes)
		if err != nil || n < 1 || n > 32 {
			return fail.Validation("deploy.processes", "Processes must be auto or a number from 1 to 32.")
		}
	}
	if d.CPULimit < 0 || d.CPULimit > 256 {
		return fail.Validation("deploy.cpuLimit", "The vCPU limit must be between 0 (unlimited) and 256.")
	}
	if d.MemoryLimitMB != 0 && d.MemoryLimitMB < 64 {
		return fail.Validation("deploy.memoryLimitMb", "The memory limit must be at least 64 MB.")
	}
	if d.MemoryReservationMB != 0 && d.MemoryReservationMB < 64 {
		return fail.Validation("deploy.memoryReservationMb", "The memory reservation must be at least 64 MB.")
	}
	if d.MemoryLimitMB > 0 && d.MemoryReservationMB > d.MemoryLimitMB {
		return fail.Validation("deploy.memoryReservationMb", "The memory reservation cannot be larger than the memory limit.")
	}
	if d.ShmSizeMB < 0 {
		return fail.Validation("deploy.shmSizeMb", "The shared memory size cannot be negative.")
	}
	for _, r := range d.Placement.Roles {
		switch r {
		case "app", "data", "build", "ingress", "control":
		default:
			return fail.Validation("deploy.placement.roles", "%q is not a host role. Use app, data, build, ingress or control.", r)
		}
	}
	if c.Net.Port < 0 || c.Net.Port > 65535 {
		return fail.Validation("networking.port", "The port must be between 1 and 65535.")
	}
	return nil
}

func validSurge(s string) bool {
	if strings.HasSuffix(s, "%") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
		return err == nil && n >= 1 && n <= 100
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1
}

// CheckMountPath validates a volume mount path.
func CheckMountPath(p string) error {
	switch {
	case p == "":
		return fail.Validation("mountPath", "A volume needs a mount path, for example /data.")
	case !strings.HasPrefix(p, "/"):
		return fail.Validation("mountPath", "The mount path must be absolute, for example /data.")
	case p == "/" || strings.Trim(p, "/") == "":
		return fail.Validation("mountPath", "The mount path cannot be /.")
	case strings.Contains(p, ".."):
		return fail.Validation("mountPath", "The mount path cannot contain ..")
	}
	return nil
}

// CheckVariables validates names, sizes and reference syntax.
func CheckVariables(values map[string]string) error {
	for _, k := range sortedKeys(values) {
		if err := vars.CheckName(k); err != nil {
			return err
		}
		if err := vars.CheckValue(k, values[k]); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
