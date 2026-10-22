// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// JobOutcome is a job result together with the dispatch error.
type JobOutcome struct {
	Result proto.JobResult
	Err    error
}

func (o JobOutcome) ok() bool {
	return o.Err == nil && o.Result.ExitCode == 0 && !o.Result.TimedOut && o.Result.Error == ""
}

// Job timeouts.
const (
	backupTimeoutSec  = 6 * 3600
	restoreTimeoutSec = 12 * 3600
	shortTimeoutSec   = 60
	wipeTimeoutSec    = 3600
)

// toolDir is where tools and data scripts live inside containers.
const toolDir = "/opt/5050/bin/"

func toolMount(name string) proto.Mount {
	return proto.Mount{Kind: "tool", Source: name, Target: toolDir + name, ReadOnly: true}
}

// volumeName is the Docker volume of a 5050 volume (docs/SPEC.md 9.12).
func volumeName(volID string) string { return "5050-" + volID }

// runJob dispatches a job and logs its lines at debug level.
func (d *Data) runJob(ctx context.Context, hostID string, job proto.Job) JobOutcome {
	if job.JobID == "" {
		job.JobID = newID("job")
	}
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	log := d.log.With("job", job.JobID, "kind", job.Kind)
	res, err := d.run.RunJob(ctx, hostID, job, func(line string) { log.Debug(line) })
	return JobOutcome{Result: res, Err: err}
}

// execInService runs cmd inside the running replica of a service (backups of
// Postgres, Redis and MongoDB, data inspection, credential changes).
func (d *Data) execInService(ctx context.Context, snap cpapi.ServiceSnapshot, kind string, cmd []string, env map[string]string, user string, stdin []byte, timeoutSec int) JobOutcome {
	host, replica, err := d.run.ActiveReplica(ctx, snap.EnvID, snap.ServiceID)
	if err != nil || host == "" || replica == "" {
		return JobOutcome{Err: fmt.Errorf("%s is not running, so the command could not reach it", snap.Config.Name)}
	}
	return d.runJob(ctx, host, proto.Job{
		Kind: "exec", ReplicaID: replica, Cmd: cmd, Env: env, User: user, Stdin: stdin,
		TimeoutSec: timeoutSec, Labels: jobLabels(snap.EnvID, snap.ServiceID, kind),
	})
}

// sharedFiles returns scripts from templates/_shared as job files under
// /opt/5050/bin ("restic-env", "backup-volume", ...).
func (d *Data) sharedFiles(names ...string) ([]proto.FileSpec, error) {
	out := make([]proto.FileSpec, 0, len(names))
	for _, n := range names {
		b, err := d.cat.ReadFile("", "_shared/"+n+".sh")
		if err != nil {
			return nil, err
		}
		mode := uint32(0o755)
		if n == "restic-env" {
			mode = 0o644
		}
		out = append(out, proto.FileSpec{Path: toolDir + n, Content: b, Mode: mode})
	}
	return out, nil
}

// serviceEnv returns the resolved variables of a service: from its active
// deployment's snapshot when it has one, else resolved by core now.
func (d *Data) serviceEnv(ctx context.Context, snap cpapi.ServiceSnapshot) map[string]string {
	if _, _, env, err := d.run.ActiveImage(ctx, snap.EnvID, snap.ServiceID); err == nil && len(env) > 0 {
		return env
	}
	env, _, err := d.cfg.ResolveVariables(ctx, snap.EnvID, snap.ServiceID, map[string]string{
		"FIFTY_PRIVATE_DOMAIN": privateDomain(snap),
	})
	if err != nil {
		return map[string]string{}
	}
	return env
}

// privateDomain is "<private name>.internal".
func privateDomain(snap cpapi.ServiceSnapshot) string {
	name := snap.PrivateName
	if name == "" {
		name = snap.Config.Net.PrivateName
	}
	if name == "" {
		name = strings.ToLower(strings.ReplaceAll(snap.Config.Name, " ", "-"))
	}
	return name + ".internal"
}

// pick returns the first non-empty value of env for the names.
func pick(env map[string]string, names ...string) string {
	for _, n := range names {
		if v := env[n]; v != "" {
			return v
		}
	}
	return ""
}

// copyEnv merges maps into a new one; later maps win.
func copyEnv(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
