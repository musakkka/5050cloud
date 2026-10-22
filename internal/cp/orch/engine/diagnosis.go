// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/pkg/api"
)

// The control plane assembles the one-sentence diagnosis of docs/SPEC.md
// 9.18 from what agents and builders detected. Agents send a diagnosis with
// crashed or unhealthy replicas; the engine adds the port rules, which need
// the target port only the control plane knows.

// Diagnosis inputs for one failed replica.
type replicaFacts struct {
	State        string
	ExitCode     *int
	OOM          bool
	MemLimitMB   int
	Diagnosis    string // what the agent detected
	Listening    []int
	LoopbackOnly []int
	TargetPort   int
	HealthPath   string
	HasPort      bool // the service is expected to listen (web)
}

// diagnose returns the sentence for a failed replica.
func diagnose(f replicaFacts) string {
	if f.OOM {
		if f.MemLimitMB > 0 {
			return fmt.Sprintf("Out of memory at %d MB (limit). Raise the memory limit or lower Processes.", f.MemLimitMB)
		}
		if f.Diagnosis != "" {
			return f.Diagnosis
		}
		return "The app ran out of memory on its host. Set a memory limit or lower Processes."
	}
	if f.HasPort && f.TargetPort > 0 && f.State != "crashed" && f.State != "exited" {
		if containsInt(f.LoopbackOnly, f.TargetPort) {
			return fmt.Sprintf("Your app listens on 127.0.0.1:%d, which is unreachable from outside the container. Listen on 0.0.0.0.", f.TargetPort)
		}
		if len(f.Listening) > 0 && !containsInt(f.Listening, f.TargetPort) {
			others := append([]int(nil), f.Listening...)
			sort.Ints(others)
			return fmt.Sprintf("Your app listens on %s but the target port is %d. Set PORT=%d in your app or change the target port to %s.",
				joinInts(others), f.TargetPort, f.TargetPort, strconv.Itoa(others[0]))
		}
		if len(f.Listening) == 0 && len(f.LoopbackOnly) > 0 {
			lo := append([]int(nil), f.LoopbackOnly...)
			sort.Ints(lo)
			return fmt.Sprintf("Your app listens on 127.0.0.1:%d, which is unreachable from outside the container. Listen on 0.0.0.0.", lo[0])
		}
	}
	if f.Diagnosis != "" {
		return f.Diagnosis
	}
	switch f.State {
	case "crashed", "exited":
		code := 1
		if f.ExitCode != nil {
			code = *f.ExitCode
		}
		return fmt.Sprintf("The app exited with code %d right after starting. Check the deploy logs for the error.", code)
	case "unhealthy":
		if f.HealthPath != "" {
			return fmt.Sprintf("Health check %s did not pass. Check the path in Settings > Deploy.", f.HealthPath)
		}
		return healthTimeout(f.TargetPort, 300)
	}
	return "The new version did not become healthy. Check the deploy logs for the error."
}

// healthTimeout is the sentence when nothing answered before the deadline.
func healthTimeout(port, deadlineSec int) string {
	return fmt.Sprintf("Nothing answered on port %d within %d s. Make sure the app starts and listens on PORT.", port, deadlineSec)
}

// preDeployFailed is the sentence of a pre-deploy command with a non-zero exit.
func preDeployFailed(code int, last string) string {
	last = strings.TrimSpace(last)
	if last == "" {
		return fmt.Sprintf("Pre-deploy command exited with code %d.", code)
	}
	last = strings.TrimRight(last, ".")
	if len(last) > 300 {
		last = last[:300]
	}
	return fmt.Sprintf("Pre-deploy command exited with code %d: %s.", code, last)
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// replicaDiagnosis assembles the sentence for a replica of d.
func (o *Orchestrator) replicaDiagnosis(ctx context.Context, d depRow, r repRow) string {
	env, _ := o.openEnv(d.ID, d.EnvEnc)
	snapDomains := false
	if s, err := o.cfg.ServiceInstance(ctx, d.EnvID, d.ServiceID); err == nil {
		snapDomains = len(s.Domains) > 0
	}
	hasPort := d.Config.Net.Port > 0 || snapDomains || d.Config.Kind == api.KindWeb || d.Config.Kind == api.KindDatabase
	return diagnose(replicaFacts{
		State: r.State, ExitCode: r.ExitCode, OOM: r.OOM, MemLimitMB: d.Config.Deploy.MemoryLimitMB, Diagnosis: r.Diagnosis,
		Listening: r.Listening, LoopbackOnly: r.LoopbackOnly, TargetPort: targetPort(d.Config, env),
		HealthPath: d.Config.Deploy.HealthcheckPath, HasPort: hasPort,
	})
}
