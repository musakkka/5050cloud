// SPDX-License-Identifier: MIT

// Package manifest defines the file formats 5050cloud reads and writes:
//
//   - templates/<id>/template.yaml (Template), docs/SPEC.md section 10;
//   - 5050.yaml / 5050.json, the project file written by `5050 config pull`
//     (ProjectFile), docs/SPEC.md section 12;
//   - railway.json / railway.toml and the config-as-code part of 5050.yaml
//     (RailwayConfig), docs/SPEC.md 9.20.
//
// Every YAML file is decoded and encoded with sigs.k8s.io/yaml, which turns
// YAML into JSON and then uses the `json` struct tags. That gives one set of
// camelCase field names (startCommand, networking, dependsOn) for JSON and
// YAML. The types carry no `yaml` tags on purpose: gopkg.in/yaml.v3 would
// silently lower-case untagged fields, so it must never be used on them.
// golden_test.go proves the round trip.
//
// Frozen for v0.1.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/musakkka/5050cloud/pkg/api"
)

// Template is templates/<id>/template.yaml.
//
// Variable values may use ${{secret(32)}}, ${{secret(16, "abc")}},
// ${{randomInt(1000,9999)}}, ${{input.NAME}}, references to other template
// services (${{Postgres.PGPASSWORD}}), and provided variables
// (${{FIFTY_PRIVATE_DOMAIN}}).
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Tagline     string `json:"tagline"` // one line under the name on the detail page
	Description string `json:"description"`
	Docs        string `json:"docs,omitempty"` // https URL of the template's docs
	Icon        string `json:"icon"`           // file in templates/<id>/, served at /icons/<id>.svg
	Category    string `json:"category"`       // "database" | "storage" | "app"
	// MajorVersions lists selectable major versions for database templates
	// ("17", "18"); the first is the default. Changing the major of an
	// existing volume is refused (docs/SPEC.md 7.3).
	MajorVersions []string            `json:"majorVersions,omitempty"`
	Inputs        []api.TemplateInput `json:"inputs,omitempty"`
	Services      []TemplateService   `json:"services"`
}

type TemplateService struct {
	Name string `json:"name"` // "Postgres"
	// DependsOn names services of this template that must be SUCCESS before
	// this one deploys (docs/SPEC.md 9.1).
	DependsOn []string          `json:"dependsOn,omitempty"`
	Config    api.ServiceConfig `json:"config"`
	// Images maps a major version to an image when MajorVersions is set
	// ("17": "postgres:17.6-bookworm"); otherwise Config.Source.Image is used.
	Images    map[string]string `json:"images,omitempty"`
	Variables map[string]string `json:"variables"`
	Volume    *TemplateVolume   `json:"volume,omitempty"`
	// Domain generates a public domain at creation (MinIO console, apps).
	Domain bool `json:"domain,omitempty"`
	// Files are rendered (Go text/template over resolved variables + host
	// facts such as .MemBytes and .MemLimitBytes) and mounted read-only.
	Files []TemplateFile `json:"files,omitempty"`
	// Tools are helper binaries mounted at /opt/5050/bin (e.g. "wal-g", "restic").
	Tools  []string       `json:"tools,omitempty"`
	Backup *BackupRecipe  `json:"backup,omitempty"`
	Probe  *TemplateProbe `json:"probe,omitempty"`
	// Data enables the Data tab, Connect and `5050 connect`:
	// "postgres" | "mysql" | "redis" | "mongodb".
	Data string `json:"data,omitempty"`
	// Monitors are created with the service (default disk monitor at 80%).
	Monitors []TemplateMonitor `json:"monitors,omitempty"`
}

type TemplateVolume struct {
	MountPath string `json:"mountPath"`
}

type TemplateFile struct {
	Path     string `json:"path"`     // mount target in the container
	Template string `json:"template"` // file in templates/<id>/
	Mode     uint32 `json:"mode"`
}

// TemplateProbe holds commands the agent runs through Docker exec.
type TemplateProbe struct {
	// Health must exit 0 for the replica to count as healthy (pg_isready).
	Health []string `json:"health,omitempty"`
	// Metrics prints one JSON object shaped like proto.DBSample (every 60 s).
	Metrics []string `json:"metrics,omitempty"`
	// Check runs in the monthly restore test ("SELECT count(*) FROM pg_class").
	Check []string `json:"check,omitempty"`
}

type TemplateMonitor struct {
	Metric     string  `json:"metric"` // "cpu" | "memory" | "disk" | "http_5xx_rate"
	Threshold  float64 `json:"threshold"`
	ForMinutes int     `json:"forMinutes"`
}

// BackupRecipe tells the control plane how to back up and restore a
// template service. Commands run as Jobs with the service's variables, the
// BACKUP_* variables below, and the tools mounted. Every backup command must
// print a final JSON line {"ok":true,"sizeBytes":N,"id":"<backup id in the store>"}.
//
// Injected: BACKUP_S3_ENDPOINT, BACKUP_S3_BUCKET, BACKUP_S3_REGION,
// BACKUP_S3_PREFIX (= "<install prefix>/<service id>"), AWS_ACCESS_KEY_ID,
// AWS_SECRET_ACCESS_KEY (the write key; retention jobs get the retention
// key), RESTIC_PASSWORD or WALG_LIBSODIUM_KEY, BACKUP_ID,
// RESTORE_TARGET_TIME (RFC 3339, restore only).
type BackupRecipe struct {
	// Kind: "postgres_wal" | "redis_rdb" | "mongo_dump" | "volume". MySQL has no backups in v0.1.
	Kind string `json:"kind"`
	// RunIn "service" execs inside the running replica (needed for wal-g
	// backup-push); "job" starts a separate container with the volume mounted read-only.
	RunIn   string   `json:"runIn"`
	Backup  []string `json:"backup"`
	Restore []string `json:"restore"` // runs in the NEW (restored) service before it starts
	// Retention runs on the control host with the retention key.
	Retention []string `json:"retention,omitempty"`
	// Verify reports the health of continuous archiving.
	Verify     []string `json:"verify,omitempty"`
	Continuous bool     `json:"continuous"` // WAL archiving configured in the service itself
	// Dump and Load move data between majors (Upgrade to a new major version, 9.13).
	Dump []string `json:"dump,omitempty"`
	Load []string `json:"load,omitempty"`
}

// ProjectFile is 5050.yaml as written by `5050 config pull` (and by
// GET /environments/{eid}/export). Secret values are written as "@preserve".
type ProjectFile struct {
	Project      string                     `json:"project"`
	Environments map[string]EnvironmentFile `json:"environments,omitempty"`
	Shared       map[string]string          `json:"shared,omitempty"`
	Services     map[string]ServiceFile     `json:"services"`
}

type EnvironmentFile struct {
	Shared   map[string]string      `json:"shared,omitempty"`
	Services map[string]ServiceFile `json:"services,omitempty"` // per-environment overrides
}

// ServiceFile is one service in a ProjectFile. The embedded ServiceConfig
// fields appear at the same level as variables and domains.
type ServiceFile struct {
	api.ServiceConfig
	Variables map[string]string `json:"variables,omitempty"`
	Domains   []string          `json:"domains,omitempty"`
	Volume    *TemplateVolume   `json:"volume,omitempty"`
}

// ---------------------------------------------------------------- config as code

// RailwayConfig mirrors Railway's config-as-code schema
// (https://railway.com/railway.schema.json) for railway.json, railway.toml,
// 5050.yaml and 5050.json. Pointer fields tell "not set" from a zero value,
// so the UI can mark exactly the fields a file sets. Our own files may also
// set the 5050-only fields marked below. pkg/configfile rejects unknown
// fields with a suggestion.
type RailwayConfig struct {
	Schema       string                        `json:"$schema,omitempty"`
	Build        *RailwayBuild                 `json:"build,omitempty"`
	Deploy       *RailwayDeploy                `json:"deploy,omitempty"`
	Environments map[string]RailwayEnvironment `json:"environments,omitempty"`
}

// RailwayEnvironment overrides the base fields for one environment name.
type RailwayEnvironment struct {
	Build  *RailwayBuild  `json:"build,omitempty"`
	Deploy *RailwayDeploy `json:"deploy,omitempty"`
}

type RailwayBuild struct {
	// Builder is Railway's enum: "RAILPACK" | "DOCKERFILE" | "NIXPACKS"
	// (built with Railpack) | "HEROKU" | "PAKETO" (refused with a sentence).
	Builder            *string  `json:"builder,omitempty"`
	BuildCommand       *string  `json:"buildCommand,omitempty"`
	DockerfilePath     *string  `json:"dockerfilePath,omitempty"`
	WatchPatterns      []string `json:"watchPatterns,omitempty"`
	RailpackVersion    *string  `json:"railpackVersion,omitempty"`
	NixpacksConfigPath *string  `json:"nixpacksConfigPath,omitempty"`
	// NixpacksPlan is accepted and ignored with a notice.
	NixpacksPlan json.RawMessage `json:"nixpacksPlan,omitempty"`
}

type RailwayDeploy struct {
	StartCommand            *string       `json:"startCommand,omitempty"`
	PreDeployCommand        *StringOrList `json:"preDeployCommand,omitempty"`
	HealthcheckPath         *string       `json:"healthcheckPath,omitempty"`
	HealthcheckTimeout      *int          `json:"healthcheckTimeout,omitempty"` // seconds
	RestartPolicyType       *string       `json:"restartPolicyType,omitempty"`  // "ON_FAILURE" | "ALWAYS" | "NEVER"
	RestartPolicyMaxRetries *int          `json:"restartPolicyMaxRetries,omitempty"`
	CronSchedule            *string       `json:"cronSchedule,omitempty"`
	NumReplicas             *int          `json:"numReplicas,omitempty"`
	OverlapSeconds          *int          `json:"overlapSeconds,omitempty"`
	DrainingSeconds         *int          `json:"drainingSeconds,omitempty"`
	// SleepApplication is accepted and ignored with a notice (no serverless in v0.1).
	SleepApplication *bool `json:"sleepApplication,omitempty"`
	// MultiRegionConfig contributes only the sum of its numReplicas.
	MultiRegionConfig map[string]RailwayRegion `json:"multiRegionConfig,omitempty"`
	// Runtime and Region are Railway-only and ignored with a notice.
	Runtime *string `json:"runtime,omitempty"`
	Region  *string `json:"region,omitempty"`

	// 5050-only fields (5050.yaml and 5050.json).
	Processes *string        `json:"processes,omitempty"` // "1".."32" or "auto"
	Placement *api.Placement `json:"placement,omitempty"`
	Singleton *bool          `json:"singleton,omitempty"`
}

type RailwayRegion struct {
	NumReplicas int `json:"numReplicas"`
}

// StringOrList accepts a JSON string or a list of strings; Railway allows
// both for preDeployCommand. A list is joined into one shell command by
// pkg/configfile; Marshal writes a string when there is one item.
type StringOrList []string

// UnmarshalJSON accepts "cmd" and ["cmd1", "cmd2"].
func (s *StringOrList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*s = StringOrList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("expected a string or a list of strings: %w", err)
	}
	*s = many
	return nil
}

// MarshalJSON writes a single command as a string and several as a list.
func (s StringOrList) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}
	return json.Marshal([]string(s))
}

// NixpacksHints is what pkg/configfile keeps from nixpacks.toml (9.20.2).
type NixpacksHints struct {
	StartCmd     string            `json:"startCmd,omitempty"`     // [start] cmd
	BuildCmds    []string          `json:"buildCmds,omitempty"`    // [phases.build] cmds
	Variables    map[string]string `json:"variables,omitempty"`    // [variables], build time
	NixPkgs      []string          `json:"nixPkgs,omitempty"`      // -> RAILPACK_PACKAGES
	AptPkgs      []string          `json:"aptPkgs,omitempty"`      // -> RAILPACK_DEPLOY_APT_PACKAGES
	IgnoredKeys  []string          `json:"ignoredKeys,omitempty"`  // one log line each
	SourceLines  map[string]int    `json:"sourceLines,omitempty"`  // field -> line in nixpacks.toml
	ProviderHint string            `json:"providerHint,omitempty"` // first of [providers], informational
}
