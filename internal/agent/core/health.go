// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/logstore"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Health check defaults (7.4).
const (
	defaultIntervalSec = 3
	defaultTimeoutSec  = 10
	defaultDeadlineSec = 300
	defaultGraceSec    = 5
	steadyIntervalSec  = 30
	steadyFailures     = 3
	workerHealthySec   = 10
)

// probeResult is one health probe.
type probeResult struct {
	ok     bool
	status int    // HTTP status; 0 when nothing answered
	kind   string // "http" | "tcp" | "exec"
	err    error
}

func (a *Agent) units(n, def int) time.Duration {
	if n <= 0 {
		n = def
	}
	return time.Duration(n) * a.t.unit
}

// probeAddr is where the agent probes a replica from the host.
func probeAddr(s proto.ReplicaSpec, c engine.Container) string {
	ip := s.IP
	if ip == "" {
		ip = c.IPs[s.Network]
	}
	if ip == "" {
		for _, v := range c.IPs {
			if v != "" {
				ip = v
				break
			}
		}
	}
	return net.JoinHostPort(ip, strconv.Itoa(s.Port))
}

// probe runs one health probe: an HTTP GET with the path (2xx and 3xx
// pass, redirects are not followed), else a TCP connect, plus the exec
// command when there is one.
func (a *Agent) probe(ctx context.Context, s proto.ReplicaSpec, c engine.Container) probeResult {
	timeout := a.units(s.Health.TimeoutSec, defaultTimeoutSec)
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res := probeResult{ok: true}
	if s.Port > 0 {
		addr := probeAddr(s, c)
		if s.Health.Path != "" {
			res = httpProbe(pctx, addr, s)
		} else {
			res.kind = "tcp"
			d := net.Dialer{}
			conn, err := d.DialContext(pctx, "tcp", addr)
			if err != nil {
				return probeResult{kind: "tcp", err: err}
			}
			conn.Close()
		}
	}
	if res.ok && len(s.Health.Exec) > 0 {
		code, _, err := a.execRun(pctx, c.ID, s.Health.Exec, nil)
		if err != nil || code != 0 {
			if err == nil {
				err = fmt.Errorf("exit code %d", code)
			}
			return probeResult{kind: "exec", err: err}
		}
	}
	return res
}

func httpProbe(ctx context.Context, addr string, s proto.ReplicaSpec) probeResult {
	p := s.Health.Path
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+p, nil)
	if err != nil {
		return probeResult{kind: "http", err: err}
	}
	host := s.Health.Host
	if host == "" {
		host = s.ServiceName + ".internal"
	}
	req.Host = host
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("User-Agent", "5050cloud-healthcheck")
	client := &http.Client{
		Transport:     &http.Transport{DisableKeepAlives: true, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return probeResult{kind: "http", err: err}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return probeResult{kind: "http", status: resp.StatusCode, ok: resp.StatusCode >= 200 && resp.StatusCode < 400}
}

// HealthDiagnosis is the one sentence shown when a replica fails its
// health check (9.18).
func HealthDiagnosis(s proto.ReplicaSpec, res probeResult, listening, loopback []int, deadlineSec int) string {
	port := s.Port
	if port > 0 && (len(listening) > 0 || len(loopback) > 0) && !slices.Contains(listening, port) {
		if slices.Contains(loopback, port) {
			return fmt.Sprintf("Your app listens on 127.0.0.1:%d, which is unreachable from outside the container. Listen on 0.0.0.0.", port)
		}
		if len(listening) > 0 {
			other := listening[0]
			return fmt.Sprintf("Your app listens on %d but the target port is %d. Set PORT=%d in your app or change the target port to %d.", other, port, port, other)
		}
		return fmt.Sprintf("Your app listens on 127.0.0.1:%d, which is unreachable from outside the container. Listen on 0.0.0.0.", loopback[0])
	}
	if res.status != 0 {
		return fmt.Sprintf("Health check %s returned %d. Check the path in Settings > Deploy.", s.Health.Path, res.status)
	}
	if res.kind == "exec" {
		return fmt.Sprintf("The health command %s did not succeed within %d s.", strings.Join(s.Health.Exec, " "), deadlineSec)
	}
	return fmt.Sprintf("Nothing answered on port %d within %d s. Make sure the app starts and listens on PORT.", port, deadlineSec)
}

// ports reads the listening ports of a container's process.
func (a *Agent) ports(ctx context.Context, cid string) (listening, loopback []int) {
	c, err := a.eng.Inspect(ctx, cid)
	if err != nil || c.Pid <= 0 {
		return nil, nil
	}
	l, lb, err := a.roots.Ports(c.Pid)
	if err != nil {
		return nil, nil
	}
	return l, lb
}

// checkHealth runs the health checker of one container run (7.4): probe
// every interval; after the first pass wait the grace and probe once more,
// then report healthy; past the deadline report unhealthy with a
// diagnosis. Then probe every 30 s: three failures in a row report
// unhealthy, the next pass healthy again. Workers without a port are
// healthy once they have run 10 s.
func (a *Agent) checkHealth(ctx context.Context, r *replica, c engine.Container) {
	a.mu.Lock()
	s := r.spec
	a.mu.Unlock()
	started := c.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	report := func(healthy bool, diag string, listening, loopback []int) {
		a.mu.Lock()
		changed := r.localUnhealthy == healthy
		r.localUnhealthy = !healthy
		cached := a.cached
		a.mu.Unlock()
		a.setStatus(r, func(st *proto.ReplicaStatus) {
			if healthy {
				st.State = "healthy"
				st.Diagnosis = ""
			} else {
				st.State = "unhealthy"
				st.Diagnosis = diag
			}
			st.ListeningPorts = listening
			st.LoopbackOnly = loopback
		})
		if changed && cached {
			a.trigger() // recompute upstreams locally while the control plane is away
		}
	}
	if s.Port == 0 && len(s.Health.Exec) == 0 {
		wait := time.Until(started.Add(workerHealthySec * a.t.unit))
		if wait > 0 && !sleepCtx(ctx, wait) {
			return
		}
		if cur, err := a.eng.Inspect(ctx, c.ID); err == nil && cur.Running() && cur.StartedAt.Equal(c.StartedAt) {
			report(true, "", nil, nil)
		}
		return
	}
	interval := a.units(s.Health.IntervalSec, defaultIntervalSec)
	deadlineSec := s.Health.DeadlineSec
	if deadlineSec <= 0 {
		deadlineSec = defaultDeadlineSec
	}
	deadline := started.Add(time.Duration(deadlineSec) * a.t.unit)
	grace := a.units(s.Health.GraceSec, defaultGraceSec)
	healthy := false
	for {
		res := a.probe(ctx, s, c)
		if ctx.Err() != nil {
			return
		}
		if res.ok {
			if !sleepCtx(ctx, grace) {
				return
			}
			res = a.probe(ctx, s, c)
			if res.ok {
				l, lb := a.ports(ctx, c.ID)
				report(true, "", l, lb)
				healthy = true
				break
			}
		}
		if time.Now().After(deadline) {
			l, lb := a.ports(ctx, c.ID)
			diag := HealthDiagnosis(s, res, l, lb, deadlineSec)
			a.systemLine(r, logstore.LevelError, diag)
			report(false, diag, l, lb)
			break
		}
		if !sleepCtx(ctx, interval) {
			return
		}
	}
	fails := 0
	steady := steadyIntervalSec * a.t.unit
	for sleepCtx(ctx, steady) {
		res := a.probe(ctx, s, c)
		if ctx.Err() != nil {
			return
		}
		if res.ok {
			fails = 0
			if !healthy {
				healthy = true
				l, lb := a.ports(ctx, c.ID)
				report(true, "", l, lb)
			}
			continue
		}
		fails++
		if healthy && fails >= steadyFailures {
			healthy = false
			l, lb := a.ports(ctx, c.ID)
			diag := HealthDiagnosis(s, res, l, lb, deadlineSec)
			a.systemLine(r, logstore.LevelError, diag)
			report(false, diag, l, lb)
		}
	}
}

// startFollowers attaches the log follower and the health checker to a
// container run; it does nothing when they already follow this run.
func (a *Agent) startFollowers(r *replica, c engine.Container) {
	a.mu.Lock()
	if r.logCancel != nil && c.StartedAt.Equal(r.followStarted) {
		a.mu.Unlock()
		return
	}
	a.stopFollowersLocked(r)
	r.followStarted = c.StartedAt
	r.startedAt = c.StartedAt
	r.cid = c.ID
	lctx, lcancel := context.WithCancel(a.ctx)
	hctx, hcancel := context.WithCancel(a.ctx)
	r.logCancel, r.healthCancel = lcancel, hcancel
	a.mu.Unlock()
	a.setStatus(r, func(st *proto.ReplicaStatus) {
		switch st.State {
		case "", "pulling", "stopped", "removed":
			st.State = "starting"
		}
	})
	go a.followLogs(lctx, r, c.ID)
	go a.checkHealth(hctx, r, c)
}

func (a *Agent) stopHealth(r *replica) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.healthCancel != nil {
		r.healthCancel()
		r.healthCancel = nil
	}
}

func (a *Agent) stopFollowers(r *replica) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopFollowersLocked(r)
}

func (a *Agent) stopFollowersLocked(r *replica) {
	if r.healthCancel != nil {
		r.healthCancel()
		r.healthCancel = nil
	}
	if r.logCancel != nil {
		r.logCancel()
		r.logCancel = nil
	}
	r.followStarted = time.Time{}
}

func (a *Agent) stopAllFollowers() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.replicas {
		a.stopFollowersLocked(r)
	}
}

// followLogs stores a container's output until it stops.
func (a *Agent) followLogs(ctx context.Context, r *replica, cid string) {
	a.mu.Lock()
	proc := r.proc
	env := r.spec.EnvironmentID
	a.mu.Unlock()
	since := a.cursors.get(cid)
	err := a.eng.Logs(ctx, cid, since, true, func(l engine.LogLine) {
		if !since.IsZero() && !l.TS.After(since) {
			return
		}
		if recs := proc.Line(l.Stream, l.TS, l.Text); len(recs) > 0 {
			_ = a.logs.Append(env, recs...)
		}
		if !l.TS.IsZero() {
			a.cursors.set(cid, l.TS)
		}
	})
	if recs := proc.Flush(time.Now().Add(time.Second)); len(recs) > 0 {
		_ = a.logs.Append(env, recs...)
	}
	if err != nil && ctx.Err() == nil && !errors.Is(err, engine.ErrNotFound) {
		a.log.Debug("log stream ended", "replica", r.spec.ID, "err", err)
	}
}
