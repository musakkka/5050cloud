// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// earlyExit is how soon after start an exit counts as "right after starting".
const earlyExit = 10 * time.Second

// eventLoop folds Docker container events into replica statuses and kicks
// a reconcile pass for each (7.2: reconcile is level-triggered).
func (a *Agent) eventLoop(ctx context.Context) {
	for ctx.Err() == nil {
		evs, errc := a.eng.Events(ctx)
	inner:
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-evs:
				if !ok {
					break inner
				}
				a.handleEvent(ctx, ev)
			case err := <-errc:
				if ctx.Err() == nil {
					a.log.Warn("the Docker event stream broke; reconnecting", "err", err)
				}
				break inner
			}
		}
		if !sleepCtx(ctx, time.Second) {
			return
		}
		a.trigger() // catch up with anything missed
	}
}

func (a *Agent) handleEvent(ctx context.Context, ev engine.Event) {
	rid := ev.Labels[engine.LabelReplica]
	if rid == "" {
		return
	}
	a.mu.Lock()
	r := a.replicas[rid]
	a.mu.Unlock()
	if r == nil {
		a.trigger()
		return
	}
	action := ev.Action
	if i := strings.IndexByte(action, ':'); i > 0 {
		action = action[:i]
	}
	switch action {
	case "start":
		c, err := a.eng.Inspect(ctx, ev.ContainerID)
		if err != nil {
			break
		}
		a.mu.Lock()
		prev := r.status.State
		mine := r.cid == c.ID
		a.mu.Unlock()
		if mine && (prev == "exited" || prev == "crashed") {
			a.send(proto.TEvent, "", proto.Event{ReplicaID: rid, Kind: "restart", Text: fmt.Sprintf("Restarted (%d restarts so far).", c.RestartCount), At: time.Now().UnixMilli()})
			a.systemLine(r, logstore.LevelWarn, fmt.Sprintf("Container restarted (%d restarts so far)", c.RestartCount))
			a.setStatus(r, func(st *proto.ReplicaStatus) {
				st.State = "starting"
				st.Restarts = c.RestartCount
				st.ExitCode = nil
			})
		}
		if mine {
			a.startFollowers(r, c)
		}
	case "die":
		c, err := a.eng.Inspect(ctx, ev.ContainerID)
		if err != nil {
			if errors.Is(err, engine.ErrNotFound) {
				break
			}
			break
		}
		a.mu.Lock()
		mine := r.cid == c.ID || r.cid == ""
		a.mu.Unlock()
		if mine {
			a.recordExit(r, c, true)
		}
	case "oom":
		a.mu.Lock()
		spec := r.spec
		a.mu.Unlock()
		diag := OOMDiagnosis(spec.MemLimitMB)
		a.setStatus(r, func(st *proto.ReplicaStatus) { st.OOMKilled = true })
		a.send(proto.TEvent, "", proto.Event{ReplicaID: rid, Kind: "oom", Text: diag, At: time.Now().UnixMilli()})
	}
	a.trigger()
}

// recordExit records why a container stopped (9.8): crashed when the
// restart policy gave up (or is "never" and the code was not 0), exited
// otherwise, stopped when the agent stopped it.
func (a *Agent) recordExit(r *replica, c engine.Container, fromEvent bool) {
	if c.Running() || c.State == "created" {
		return
	}
	a.stopHealth(r)
	a.mu.Lock()
	spec := r.spec
	byAgent := r.stoppedByAgent || r.removing
	proc := r.proc
	prevState := r.status.State
	a.mu.Unlock()
	code := c.ExitCode
	state := "exited"
	switch {
	case byAgent:
		state = "stopped"
	case spec.Restart.Policy == "never":
		if code != 0 {
			state = "crashed"
		}
	case spec.Restart.Policy == "always":
		state = "exited"
	default:
		max := spec.Restart.MaxRetries
		if max <= 0 {
			max = defaultMaxRetries
		}
		if code != 0 && c.RestartCount >= max {
			state = "crashed"
		}
	}
	diag := ""
	if !byAgent {
		switch {
		case c.OOMKilled:
			limit := spec.MemLimitMB
			if limit == 0 && c.MemLimit > 0 {
				limit = int(c.MemLimit >> 20)
			}
			diag = OOMDiagnosis(limit)
		case code != 0 && !c.StartedAt.IsZero() && !c.FinishedAt.IsZero() && c.FinishedAt.Sub(c.StartedAt) < earlyExit:
			last := ""
			if proc != nil {
				last = proc.LastStderr()
			}
			diag = EarlyExitDiagnosis(code, last)
		case state == "crashed":
			diag = fmt.Sprintf("The app exited with code %d and was restarted %d times; it is no longer restarted.", code, c.RestartCount)
		}
	}
	a.setStatus(r, func(st *proto.ReplicaStatus) {
		st.State = state
		st.ExitCode = &code
		st.OOMKilled = c.OOMKilled
		st.Restarts = c.RestartCount
		st.Diagnosis = diag
		if c.ID != "" {
			st.ContainerID = c.ID
		}
	})
	if fromEvent && !byAgent && prevState != state {
		a.send(proto.TEvent, "", proto.Event{ReplicaID: spec.ID, Kind: "die", Text: fmt.Sprintf("Exited with code %d.", code), At: time.Now().UnixMilli()})
		if diag != "" {
			a.systemLine(r, logstore.LevelError, diag)
		}
	}
}

// OOMDiagnosis is the sentence for an out-of-memory kill (9.18).
func OOMDiagnosis(limitMB int) string {
	if limitMB <= 0 {
		return "Out of memory: the kernel stopped the app because the host ran out of memory. Set a memory limit or lower Processes."
	}
	return fmt.Sprintf("Out of memory at %d MB (limit). Raise the memory limit or lower Processes.", limitMB)
}

// EarlyExitDiagnosis is the sentence for an app that exits right away (9.18).
func EarlyExitDiagnosis(code int, lastStderr string) string {
	lastStderr = strings.TrimSpace(lastStderr)
	if lastStderr == "" {
		return fmt.Sprintf("The app exited with code %d right after starting.", code)
	}
	if len(lastStderr) > 300 {
		return fmt.Sprintf("The app exited with code %d right after starting: %s....", code, strings.ToValidUTF8(lastStderr[:300], ""))
	}
	lastStderr = strings.TrimRight(lastStderr, ".")
	return fmt.Sprintf("The app exited with code %d right after starting: %s.", code, lastStderr)
}

// PullDiagnosis is the sentence for a failed image pull (9.18).
func PullDiagnosis(image string, err error) string {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		return fmt.Sprintf("Image %s was not found.", displayImage(image))
	case errors.Is(err, engine.ErrUnauthorized):
		return fmt.Sprintf("The registry refused access to %s. Add registry credentials in Settings > Source.", repoOf(image))
	case strings.Contains(strings.ToLower(err.Error()), "no space left"):
		return "The host ran out of disk while pulling " + displayImage(image) + ". Free space or add a data disk."
	}
	return fmt.Sprintf("Image %s could not be pulled: %s", displayImage(image), sentence(err))
}

// displayImage drops a digest when a tag is present ("app:v1@sha256:..." -> "app:v1").
func displayImage(ref string) string {
	if at := strings.Index(ref, "@"); at > 0 {
		base := ref[:at]
		if strings.LastIndex(base, ":") > strings.LastIndex(base, "/") {
			return base
		}
	}
	return ref
}

// repoOf is the repository of a reference, without tag or digest.
func repoOf(ref string) string {
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	if c := strings.LastIndex(ref, ":"); c > strings.LastIndex(ref, "/") {
		ref = ref[:c]
	}
	return ref
}
