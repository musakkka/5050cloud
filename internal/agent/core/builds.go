// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// startBuild runs a build in the background. It keeps going when the
// socket drops; logs and the result are buffered and sent after the next
// hello (7.5).
func (a *Agent) startBuild(ctx context.Context, job proto.BuildJob) {
	if job.BuildID == "" {
		return
	}
	a.mu.Lock()
	if _, running := a.builds[job.BuildID]; running {
		a.mu.Unlock()
		return
	}
	var host proto.HostConfig
	if a.desired != nil {
		host = a.desired.Host
	}
	timeout := time.Duration(job.TimeoutSec) * time.Second
	if timeout <= 0 {
		mins := host.BuildTimeoutMin
		if mins <= 0 {
			mins = 30
		}
		timeout = time.Duration(mins) * time.Minute
	}
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	a.builds[job.BuildID] = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.builds, job.BuildID)
			a.mu.Unlock()
			cancel()
		}()
		res := a.runBuild(bctx, job, host, timeout)
		a.sendDurable(proto.TBuildResult, "", res)
	}()
}

func (a *Agent) runBuild(ctx context.Context, job proto.BuildJob, host proto.HostConfig, timeout time.Duration) proto.BuildResult {
	start := time.Now()
	if host.RegistryPush == nil {
		return proto.BuildResult{BuildID: job.BuildID, Diagnosis: "This host has no registry push credential, so it cannot build. Give it the build role or build on another host."}
	}
	a.disk.beforeBuild(ctx)
	mask := logstore.NewMasker(job.Env)
	batch := newLineBatcher(a.t.logBatch, func(lines []string) {
		a.sendDurable(proto.TBuildLog, "", proto.BuildLog{BuildID: job.BuildID, Lines: lines})
	})
	res := a.o.Builder.Build(ctx, job, *host.RegistryPush, func(line string) {
		batch.add(mask.Apply(line))
	})
	batch.close()
	res.BuildID = job.BuildID
	if res.DurationMs == 0 {
		res.DurationMs = time.Since(start).Milliseconds()
	}
	if !res.OK {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			res.Diagnosis = fmt.Sprintf("The build took longer than %d minutes and was stopped. Raise the build timeout in Settings > Build.", int(timeout/time.Minute))
		case errors.Is(ctx.Err(), context.Canceled) && res.Diagnosis == "":
			res.Diagnosis = "The build was cancelled."
		case res.Diagnosis == "":
			res.Diagnosis = "The build failed. See the build logs for the error."
		}
	}
	return res
}

func (a *Agent) cancelBuild(id string) {
	a.mu.Lock()
	cancel := a.builds[id]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
