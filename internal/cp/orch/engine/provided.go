// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"io/fs"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/routes"
	"github.com/musakkka/5050cloud/pkg/api"
)

// providedVars are the variables 5050cloud injects for a deployment
// (docs/SPEC.md 9.3), passed to Config.ResolveVariables; user variables win.
// Per-replica ones are added by perReplicaVars.
func providedVars(s cpapi.ServiceSnapshot, depID string, commit *api.Commit, inst api.Install) map[string]string {
	cfg := s.Config
	m := map[string]string{
		"PORT":                   strconv.Itoa(targetPort(cfg, nil)),
		"FIFTY_PRIVATE_DOMAIN":   routes.PrivateDomain(s),
		"FIFTY_PROJECT_ID":       s.ProjectID,
		"FIFTY_PROJECT_NAME":     s.ProjectName,
		"FIFTY_ENVIRONMENT_ID":   s.EnvID,
		"FIFTY_ENVIRONMENT_NAME": s.EnvName,
		"FIFTY_SERVICE_ID":       s.ServiceID,
		"FIFTY_SERVICE_NAME":     cfg.Name,
		"FIFTY_DEPLOYMENT_ID":    depID,
		"FIFTY_SNAPSHOT_ID":      depID,
	}
	if d := publicDomain(s); d != "" {
		m["FIFTY_PUBLIC_DOMAIN"] = d
	}
	if commit != nil {
		m["FIFTY_GIT_COMMIT_SHA"] = commit.SHA
		m["FIFTY_GIT_BRANCH"] = commit.Branch
		m["FIFTY_GIT_COMMIT_MESSAGE"] = commit.Message
		m["FIFTY_GIT_AUTHOR"] = commit.Author
	}
	if cfg.Source.Type == "github" {
		owner, name := repoParts(cfg.Source.Repo)
		m["FIFTY_GIT_REPO_OWNER"] = owner
		m["FIFTY_GIT_REPO_NAME"] = name
		if _, ok := m["FIFTY_GIT_BRANCH"]; !ok && cfg.Source.Branch != "" {
			m["FIFTY_GIT_BRANCH"] = cfg.Source.Branch
		}
	}
	if s.Volume != nil {
		m["FIFTY_VOLUME_NAME"] = s.Volume.Name
		m["FIFTY_VOLUME_MOUNT_PATH"] = s.Volume.MountPath
	}
	if len(s.TCPProxies) > 0 {
		t := s.TCPProxies[0]
		dom := inst.BaseDomain
		if dom == "" {
			dom = inst.PublicIP
		}
		m["FIFTY_TCP_PROXY_DOMAIN"] = dom
		m["FIFTY_TCP_PROXY_PORT"] = strconv.Itoa(t.HostPort)
		m["FIFTY_TCP_APPLICATION_PORT"] = strconv.Itoa(t.TargetPort)
	}
	if inst.RailwayAliases {
		for k, v := range m {
			if strings.HasPrefix(k, "FIFTY_") {
				m["RAILWAY_"+strings.TrimPrefix(k, "FIFTY_")] = v
			}
		}
		m["RAILWAY_ENVIRONMENT"] = s.EnvName
	}
	return m
}

// perReplicaVars differ per replica (9.1).
func perReplicaVars(replicaID, hostName string, processes int, aliases bool) map[string]string {
	m := map[string]string{
		"FIFTY_REPLICA_ID":     replicaID,
		"FIFTY_HOST":           hostName,
		"FIFTY_REPLICA_REGION": hostName,
		"FIFTY_PROCESSES":      strconv.Itoa(processes),
	}
	if aliases {
		m["RAILWAY_REPLICA_ID"] = replicaID
		m["RAILWAY_REPLICA_REGION"] = hostName
	}
	return m
}

func publicDomain(s cpapi.ServiceSnapshot) string {
	for _, d := range s.Domains {
		if d.Kind == "custom" && d.Domain != "" {
			return d.Domain
		}
	}
	for _, d := range s.Domains {
		if d.Domain != "" {
			return d.Domain
		}
	}
	return ""
}

// repoParts splits "owner/name" or a GitHub URL.
func repoParts(repo string) (string, string) {
	r := strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	for _, p := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		r = strings.TrimPrefix(r, p)
	}
	parts := strings.Split(strings.Trim(r, "/"), "/")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", r
}

// applyVarOverrides fills empty settings from Railway's and our config
// variables (9.3): RAILWAY_* or FIFTY_* healthcheck timeout, overlap and
// draining seconds, run uid, shm size, Dockerfile path and NO_CACHE.
func applyVarOverrides(cfg *api.ServiceConfig, env map[string]string) {
	get := func(name string) string {
		if v := env["FIFTY_"+name]; v != "" {
			return v
		}
		return env["RAILWAY_"+name]
	}
	atoi := func(s string) int {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	if cfg.Deploy.HealthcheckTimeoutSec == 0 {
		cfg.Deploy.HealthcheckTimeoutSec = atoi(get("HEALTHCHECK_TIMEOUT_SEC"))
	}
	if cfg.Deploy.OverlapSec == 0 {
		cfg.Deploy.OverlapSec = atoi(get("DEPLOYMENT_OVERLAP_SECONDS"))
	}
	if cfg.Deploy.DrainingSec == 0 {
		cfg.Deploy.DrainingSec = atoi(get("DEPLOYMENT_DRAINING_SECONDS"))
	}
	if cfg.Deploy.RunAsUser == "" {
		cfg.Deploy.RunAsUser = get("RUN_UID")
	}
	if cfg.Deploy.ShmSizeMB == 0 {
		if b := atoi(get("SHM_SIZE_BYTES")); b > 0 {
			cfg.Deploy.ShmSizeMB = (b + (1 << 20) - 1) >> 20
		}
	}
	if cfg.Build.DockerfilePath == "" {
		cfg.Build.DockerfilePath = get("DOCKERFILE_PATH")
	}
	if !cfg.Build.NoCache {
		v := strings.ToLower(strings.TrimSpace(env["NO_CACHE"]))
		cfg.Build.NoCache = v == "1" || v == "true" || v == "yes"
	}
}

// shellMeta are characters that need a shell; a start command with any of
// them runs as ["sh", "-c", cmd] inside the container (docs/SPEC.md 15).
const shellMeta = "|&;<>()$`\\*?[]#~{}!\n"

// startArgv turns a start command into an argv array: a plain command is
// split on spaces with single and double quotes; anything with shell
// syntax runs through sh -c.
func startArgv(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	// "=" only matters to a shell in a leading assignment (FOO=1 app);
	// inside an argument (postgres -c config_file=/x) it is plain text.
	if strings.ContainsAny(cmd, shellMeta) || leadingAssign(cmd) {
		return []string{"sh", "-c", cmd}
	}
	var out []string
	var cur strings.Builder
	quote := rune(0)
	inWord := false
	for _, r := range cmd {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return []string{"sh", "-c", cmd}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

func readFS(f fs.FS, name string) ([]byte, error) { return fs.ReadFile(f, name) }

// leadingAssign reports whether a command starts with NAME=value.
func leadingAssign(cmd string) bool {
	first, _, _ := strings.Cut(cmd, " ")
	name, _, ok := strings.Cut(first, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}
