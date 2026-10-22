// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/agent/assets"
	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Default values for fields the control plane may leave at zero.
const (
	defaultStopGraceSec = 10
	defaultMaxRetries   = 10
	maxFileBytes        = 256 << 10
)

// ContainerName is the Docker name of a replica's container.
func ContainerName(replicaID string) string { return "5050-" + replicaID }

// JobContainerName is the Docker name of a job's container.
func JobContainerName(jobID string) string { return "5050-job-" + jobID }

// SpecHash fingerprints the parts of a ReplicaSpec that shape the container.
// A change under the same replica id means the container is recreated.
// Health settings, the desired run state, the probe and the stop grace do
// not change the container and are left out.
func SpecHash(s proto.ReplicaSpec) string {
	c := s
	c.Health = proto.HealthCheck{}
	c.Desired = ""
	c.Probe = nil
	c.StopGraceSec = 0
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// RestartPolicy maps the service restart policy to Docker's (9.8).
func RestartPolicy(p proto.RestartPolicy) engine.Restart {
	switch p.Policy {
	case "always":
		return engine.Restart{Name: "unless-stopped"}
	case "never":
		return engine.Restart{Name: "no"}
	default:
		n := p.MaxRetries
		if n <= 0 {
			n = defaultMaxRetries
		}
		return engine.Restart{Name: "on-failure", MaxRetries: n}
	}
}

// WrapCluster rewrites `node [flags] <file> [args]` to
// `node [flags] /opt/5050/cluster.cjs <n> <file> [args]` (8.1). ok is false
// when argv is not a plain node command, so it cannot be wrapped.
func WrapCluster(argv []string, n int) ([]string, bool) {
	if len(argv) < 2 || n < 1 {
		return nil, false
	}
	bin := path.Base(argv[0])
	if bin != "node" && bin != "nodejs" {
		return nil, false
	}
	i := 1
	for i < len(argv) && strings.HasPrefix(argv[i], "-") {
		flag := argv[i]
		// Flags that run code instead of a file cannot be wrapped.
		switch flag {
		case "-e", "--eval", "-p", "--print", "-i", "--interactive", "-", "--test", "-c", "--check":
			return nil, false
		}
		// Flags whose value is a separate argument.
		if (flag == "-r" || flag == "--require" || flag == "--import" || flag == "--loader" || flag == "--experimental-loader" ||
			flag == "--env-file" || flag == "--inspect-port" || flag == "--title" || flag == "--conditions" || flag == "-C") && i+1 < len(argv) {
			i += 2
			continue
		}
		i++
	}
	if i >= len(argv) {
		return nil, false
	}
	out := make([]string, 0, len(argv)+2)
	out = append(out, argv[:i]...)
	out = append(out, assets.ClusterTarget, strconv.Itoa(n))
	out = append(out, argv[i:]...)
	return out, true
}

// specInput is what buildSpec needs besides the ReplicaSpec.
type specInput struct {
	Epoch      string
	DataDir    string
	DNS        string            // the environment resolver, "" when unknown
	ToolPaths  map[string]string // tool name -> host path
	AssetPaths map[string]string // asset name -> host path
	FilePaths  map[string]string // container path -> host path of the written file
	ImageArgv  []string          // image ENTRYPOINT + CMD, for the cluster wrap
}

// buildSpec turns a ReplicaSpec into a container spec. It is a pure
// function so the mapping is unit tested.
func buildSpec(s proto.ReplicaSpec, in specInput) (engine.Spec, []string, error) {
	var notes []string
	env := make(map[string]string, len(s.Env)+3)
	for k, v := range s.Env {
		env[k] = v
	}
	n := s.Processes
	if n < 1 {
		n = 1
	}
	env["FIFTY_PROCESSES"] = strconv.Itoa(n)
	if _, set := s.Env["WEB_CONCURRENCY"]; !set {
		env["WEB_CONCURRENCY"] = strconv.Itoa(n)
	}
	user := s.User
	if user == "" {
		user = s.Env["RAILWAY_RUN_UID"]
	}
	labels := map[string]string{}
	for k, v := range s.Labels {
		labels[k] = v
	}
	grace := s.StopGraceSec
	if grace <= 0 {
		grace = defaultStopGraceSec
	}
	labels[engine.LabelReplica] = s.ID
	labels[engine.LabelDeployment] = s.DeploymentID
	labels[engine.LabelService] = s.ServiceID
	labels[engine.LabelEnvironment] = s.EnvironmentID
	labels[engine.LabelProject] = s.ProjectID
	labels[engine.LabelEpoch] = in.Epoch
	labels[engine.LabelSpec] = SpecHash(s)
	labels[engine.LabelStopGrace] = strconv.Itoa(grace)

	sp := engine.Spec{
		Name:              ContainerName(s.ID),
		Image:             s.Image,
		Labels:            labels,
		User:              user,
		Network:           s.Network,
		IP:                s.IP,
		Aliases:           s.Aliases,
		MemoryReservation: s.MemoryReservation,
		CgroupParent:      s.CgroupParent,
		Restart:           RestartPolicy(s.Restart),
		StopTimeoutSec:    grace,
		LogMaxSize:        "20m",
		LogMaxFile:        3,
		NoNewPrivileges:   true,
	}
	if in.DNS != "" {
		sp.DNS = []string{in.DNS}
	}
	if s.MemLimitMB > 0 {
		sp.MemoryBytes = int64(s.MemLimitMB) << 20
		sp.MemorySwap = s.MemorySwap
		if sp.MemorySwap == 0 || sp.MemorySwap < sp.MemoryBytes {
			// Swap equal to the limit: an out-of-memory app is killed, not swapped (8.1).
			sp.MemorySwap = sp.MemoryBytes
		}
	}
	if s.CPULimit > 0 {
		sp.NanoCPUs = int64(s.CPULimit * 1e9)
	}
	if s.ShmSizeMB > 0 {
		sp.ShmSize = int64(s.ShmSizeMB) << 20
	}

	// Start command semantics follow Railway: an entrypoint replaces both
	// ENTRYPOINT and CMD of the image; a Cmd alone keeps the image ENTRYPOINT.
	if len(s.Entrypoint) > 0 {
		sp.Entrypoint = s.Entrypoint
		sp.Cmd = s.Cmd
		if sp.Cmd == nil {
			sp.Cmd = []string{}
		}
	} else if len(s.Cmd) > 0 {
		sp.Cmd = s.Cmd
	}
	if s.ClusterWrap && n > 1 {
		argv := append(append([]string{}, s.Entrypoint...), s.Cmd...)
		if len(s.Entrypoint) == 0 && len(s.Cmd) == 0 {
			argv = in.ImageArgv
		} else if len(s.Entrypoint) == 0 {
			// Cmd alone runs under the image entrypoint; only a bare node command can be wrapped.
			argv = s.Cmd
		}
		if wrapped, ok := WrapCluster(argv, n); ok {
			sp.Entrypoint = wrapped
			sp.Cmd = []string{}
			p := in.AssetPaths[assets.ClusterName]
			if p == "" {
				p = filepath.Join(in.DataDir, "assets", assets.ClusterName)
			}
			sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: p, Target: assets.ClusterTarget, ReadOnly: true})
		} else {
			notes = append(notes, fmt.Sprintf("The start command %q is not a plain node command, so it runs as one process with WEB_CONCURRENCY=%d.", strings.Join(argv, " "), n))
		}
	}

	envList := make([]string, 0, len(env))
	for k, v := range env {
		envList = append(envList, k+"="+v)
	}
	sort.Strings(envList)
	sp.Env = envList

	for _, m := range s.Mounts {
		switch m.Kind {
		case "volume":
			sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountVolume, Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
		case "tool":
			p := in.ToolPaths[m.Source]
			if p == "" {
				return engine.Spec{}, notes, fmt.Errorf("the tool %s is not available on this host", m.Source)
			}
			sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: p, Target: m.Target, ReadOnly: true})
		case "asset":
			p := in.AssetPaths[m.Source]
			if p == "" {
				return engine.Spec{}, notes, fmt.Errorf("the agent has no asset named %s", m.Source)
			}
			sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: p, Target: m.Target, ReadOnly: true})
		case "tmpfs":
			sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountTmpfs, Target: m.Target})
		default:
			return engine.Spec{}, notes, fmt.Errorf("the mount kind %q is not supported", m.Kind)
		}
	}
	for _, f := range s.Files {
		p := in.FilePaths[f.Path]
		if p == "" {
			return engine.Spec{}, notes, fmt.Errorf("the file %s was not written", f.Path)
		}
		sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: p, Target: f.Path, ReadOnly: true})
	}
	return sp, notes, nil
}

// fileName maps a container path to a stable file name on the host.
func fileName(containerPath string) string {
	sum := sha256.Sum256([]byte(containerPath))
	base := path.Base(containerPath)
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, base)
	if clean == "" || clean == "." || clean == ".." {
		clean = "file"
	}
	return hex.EncodeToString(sum[:6]) + "-" + clean
}

// validateFile checks a FileSpec before it is written.
func validateFile(f proto.FileSpec) error {
	if !path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || f.Path == "/" {
		return fmt.Errorf("the file path %q must be an absolute, clean path", f.Path)
	}
	if len(f.Content) > maxFileBytes {
		return fmt.Errorf("the file %s is larger than 256 KiB", f.Path)
	}
	return nil
}
