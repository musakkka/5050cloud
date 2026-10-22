// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxJobOutput is how much combined output a job result carries.
const maxJobOutput = 64 << 10

// maxStdin caps Job.Stdin.
const maxStdin = 1 << 20

// lineBatcher sends lines in batches: every 250 ms or 100 lines.
type lineBatcher struct {
	mu    sync.Mutex
	lines []string
	flush func([]string)
	stop  chan struct{}
	done  chan struct{}
}

func newLineBatcher(every time.Duration, flush func([]string)) *lineBatcher {
	b := &lineBatcher{flush: flush, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(b.done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				b.send()
			case <-b.stop:
				b.send()
				return
			}
		}
	}()
	return b
}

func (b *lineBatcher) add(line string) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	full := len(b.lines) >= 100
	b.mu.Unlock()
	if full {
		b.send()
	}
}

func (b *lineBatcher) send() {
	b.mu.Lock()
	lines := b.lines
	b.lines = nil
	b.mu.Unlock()
	if len(lines) > 0 {
		b.flush(lines)
	}
}

func (b *lineBatcher) close() {
	close(b.stop)
	<-b.done
}

// tailBuffer keeps the last n bytes written.
type tailBuffer struct {
	n   int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.n {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.n:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }

func (a *Agent) startJob(ctx context.Context, j proto.Job) {
	if j.JobID == "" {
		return
	}
	a.mu.Lock()
	if _, running := a.jobs[j.JobID]; running {
		a.mu.Unlock()
		return // a duplicate after a reconnect; the running job will report
	}
	jctx, cancel := context.WithCancel(ctx)
	a.jobs[j.JobID] = cancel
	a.mu.Unlock()
	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.jobs, j.JobID)
			a.mu.Unlock()
			cancel()
		}()
		res := a.runJob(jctx, j)
		a.sendDurable(proto.TJobResult, "", res)
	}()
}

func (a *Agent) cancelJob(id string) {
	a.mu.Lock()
	cancel := a.jobs[id]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func jobEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// runJob runs a one-off container, or a command inside a running replica
// for kind "exec" (8.1), streams its output as job_log and reports the
// exit code. A job past TimeoutSec is killed.
func (a *Agent) runJob(ctx context.Context, j proto.Job) proto.JobResult {
	start := time.Now()
	res := proto.JobResult{JobID: j.JobID}
	if len(j.Stdin) > maxStdin {
		res.ExitCode = -1
		res.Error = "The job input is larger than 1 MiB."
		return res
	}
	timeout := time.Duration(j.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Hour
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	mask := logstore.NewMasker(j.Env)
	batch := newLineBatcher(a.t.logBatch, func(lines []string) {
		a.sendDurable(proto.TJobLog, "", proto.JobLog{JobID: j.JobID, Lines: lines})
	})
	out := &tailBuffer{n: maxJobOutput}
	emit := func(line string) {
		line = mask.Apply(logstore.CapLine(string(logstore.StripANSI([]byte(line)))))
		batch.add(line)
		out.Write([]byte(line + "\n"))
	}
	var code int
	var err error
	if j.Kind == "maintenance" && len(j.Cmd) > 0 && j.Cmd[0] == RegistryReadOnlyCmd {
		code, err = a.registryReadOnlyJob(j, emit)
	} else if j.Kind == "exec" && j.ReplicaID != "" {
		code, err = a.runExecJob(tctx, j, emit)
	} else {
		code, err = a.runContainerJob(tctx, j, emit)
	}
	batch.close()
	res.ExitCode = code
	res.DurationMs = time.Since(start).Milliseconds()
	res.Output = out.String()
	switch {
	case errors.Is(tctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		res.TimedOut = true
		res.ExitCode = -1
		res.Error = fmt.Sprintf("The job timed out after %d seconds and was stopped.", int(timeout/time.Second))
	case ctx.Err() != nil:
		res.ExitCode = -1
		res.Error = "The job was cancelled."
	case err != nil:
		res.Error = sentence(err)
		if res.ExitCode == 0 {
			res.ExitCode = -1
		}
	}
	return res
}

// execRun runs a command inside a container and returns its exit code and
// combined output (at most 64 KiB).
func (a *Agent) execRun(ctx context.Context, cid string, cmd []string, stdin []byte) (int, []byte, error) {
	ex, err := a.eng.Exec(ctx, cid, engine.ExecSpec{Cmd: cmd, Stdin: stdin != nil})
	if err != nil {
		return -1, nil, err
	}
	defer ex.Close()
	if stdin != nil {
		go func() {
			_, _ = ex.Input().Write(stdin)
			_ = ex.CloseInput()
		}()
	}
	out := &tailBuffer{n: maxJobOutput}
	_, _ = io.Copy(out, ex.Output())
	code, err := ex.Wait(ctx)
	return code, out.buf, err
}

func (a *Agent) runExecJob(ctx context.Context, j proto.Job, emit func(string)) (int, error) {
	name := ContainerName(j.ReplicaID)
	ex, err := a.eng.Exec(ctx, name, engine.ExecSpec{Cmd: j.Cmd, Env: jobEnv(j.Env), User: j.User, Stdin: len(j.Stdin) > 0})
	if err != nil {
		return -1, fmt.Errorf("could not run the command in replica %s: %w", j.ReplicaID, err)
	}
	defer ex.Close()
	if len(j.Stdin) > 0 {
		go func() {
			_, _ = ex.Input().Write(j.Stdin)
			_ = ex.CloseInput()
		}()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(ex.Output())
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			emit(sc.Text())
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		ex.Close()
		<-done
		return -1, ctx.Err()
	}
	return ex.Wait(ctx)
}

func (a *Agent) runContainerJob(ctx context.Context, j proto.Job, emit func(string)) (int, error) {
	name := JobContainerName(j.JobID)
	a.mu.Lock()
	var host proto.HostConfig
	if a.desired != nil {
		host = a.desired.Host
	}
	a.mu.Unlock()

	// A job container left from before an agent restart is followed, not run twice.
	id := ""
	if c, err := a.eng.Inspect(ctx, name); err == nil && c.Labels[engine.LabelJob] == j.JobID {
		id = c.ID
		if !c.Running() {
			code := c.ExitCode
			_ = a.eng.Logs(context.WithoutCancel(ctx), id, time.Time{}, false, func(l engine.LogLine) { emit(string(l.Text)) })
			_ = a.eng.Remove(context.WithoutCancel(ctx), id, true)
			return code, nil
		}
	}
	if id == "" {
		have, err := a.eng.ImageExists(ctx, j.Image)
		if err != nil {
			return -1, err
		}
		if !have {
			emit("Pulling image " + displayImage(j.Image))
			a.disk.beforePull(ctx)
			if err := a.eng.Pull(ctx, j.Image, a.pullAuth(host, j.Image)); err != nil {
				return -1, errors.New(PullDiagnosis(j.Image, err))
			}
		}
		for _, v := range j.Volumes {
			if _, err := a.eng.VolumeCreate(ctx, v, map[string]string{engine.LabelVolume: v}); err != nil {
				return -1, err
			}
		}
		filesDir := filepath.Join(a.o.DataDir, "jobs", j.JobID, "files")
		defer os.RemoveAll(filepath.Join(a.o.DataDir, "jobs", j.JobID))
		fp, err := a.writeFiles(filesDir, j.Files)
		if err != nil {
			return -1, err
		}
		labels := map[string]string{}
		for k, v := range j.Labels {
			labels[k] = v
		}
		labels[engine.LabelJob] = j.JobID
		sp := engine.Spec{
			Name: name, Image: j.Image, Cmd: j.Cmd, Env: jobEnv(j.Env), Labels: labels, User: j.User,
			Network: j.Network, IP: j.IP, Restart: engine.Restart{Name: "no"}, OpenStdin: len(j.Stdin) > 0,
			LogMaxSize: "20m", LogMaxFile: 3, NoNewPrivileges: true,
		}
		if j.MemLimitMB > 0 {
			sp.MemoryBytes = int64(j.MemLimitMB) << 20
			sp.MemorySwap = sp.MemoryBytes
		}
		if j.Network != "" {
			if dns := a.o.Network.DNSServer(envOfNetwork(a, j.Network)); dns != "" {
				sp.DNS = []string{dns}
			}
		}
		for _, m := range j.Mounts {
			switch m.Kind {
			case "volume":
				sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountVolume, Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
			case "tool":
				p, err := a.tools.Path(ctx, m.Source)
				if err != nil {
					return -1, err
				}
				sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: p, Target: m.Target, ReadOnly: true})
			case "asset":
				p := a.assetPaths[m.Source]
				if p == "" {
					return -1, fmt.Errorf("the agent has no asset named %s", m.Source)
				}
				sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: p, Target: m.Target, ReadOnly: true})
			case "tmpfs":
				sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountTmpfs, Target: m.Target})
			default:
				return -1, fmt.Errorf("the mount kind %q is not supported", m.Kind)
			}
		}
		for _, f := range j.Files {
			sp.Mounts = append(sp.Mounts, engine.Mount{Type: engine.MountBind, Source: fp[f.Path], Target: f.Path, ReadOnly: true})
		}
		id, err = a.eng.Create(ctx, sp)
		if err != nil {
			return -1, err
		}
		if len(j.Stdin) > 0 {
			w, err := a.eng.AttachStdin(ctx, id)
			if err != nil {
				_ = a.eng.Remove(context.WithoutCancel(ctx), id, true)
				return -1, err
			}
			defer w.Close()
			if err := a.eng.Start(ctx, id); err != nil {
				_ = a.eng.Remove(context.WithoutCancel(ctx), id, true)
				return -1, err
			}
			go func() {
				_, _ = io.Copy(w, bytes.NewReader(j.Stdin))
				_ = w.Close()
			}()
		} else if err := a.eng.Start(ctx, id); err != nil {
			_ = a.eng.Remove(context.WithoutCancel(ctx), id, true)
			return -1, err
		}
	}
	defer func() { _ = a.eng.Remove(context.WithoutCancel(ctx), id, true) }()

	logsDone := make(chan struct{})
	lctx, lcancel := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer close(logsDone)
		_ = a.eng.Logs(lctx, id, time.Time{}, true, func(l engine.LogLine) { emit(string(l.Text)) })
	}()
	code, err := a.eng.Wait(ctx, id)
	if ctx.Err() != nil {
		_ = a.eng.Kill(context.WithoutCancel(ctx), id, "KILL")
	}
	// Let the log stream drain after the exit, briefly.
	select {
	case <-logsDone:
	case <-time.After(2 * time.Second):
	}
	lcancel()
	<-logsDone
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	return code, err
}

// envOfNetwork finds the environment of a docker network name.
func envOfNetwork(a *Agent, name string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.desired != nil {
		for _, n := range a.desired.Networks {
			if n.Name == name {
				return n.EnvironmentID
			}
		}
	}
	return strings.TrimPrefix(name, "5050-")
}

// registryReadOnlyJob switches the registry's read-only mode (9.6):
// ["registry-readonly", "on"|"off"].
func (a *Agent) registryReadOnlyJob(j proto.Job, emit func(string)) (int, error) {
	if len(j.Cmd) != 2 || (j.Cmd[1] != "on" && j.Cmd[1] != "off") {
		return 2, errors.New("registry-readonly takes one argument, on or off")
	}
	on := j.Cmd[1] == "on"
	a.regFront.SetReadOnly(on)
	if on {
		emit("The registry is read-only: pulls keep working, pushes wait.")
	} else {
		emit("The registry accepts pushes again.")
	}
	return 0, nil
}
