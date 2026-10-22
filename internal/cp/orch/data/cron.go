// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Cron run messages (docs/SPEC.md 9.9).
const (
	msgMissed      = "Missed while the control plane was down"
	msgInterrupted = "The control plane restarted while this run was in progress, so its result is unknown"
	msgSkipped     = "Skipped because the previous run was still running"
	msgQueued      = "Waiting for the previous run to finish"
	msgNoImage     = "The service has no successful deployment yet, so there is nothing to run"
)

// missedWindow is how far back a restart records missed slots; a slot later
// than lateGrace ago still runs instead of being recorded as missed.
const (
	missedWindow = 24 * time.Hour
	lateGrace    = time.Minute
	outputLimit  = 64 << 10
)

type cronEntry struct {
	envID, svcID string
	spec, tz     string
	sched        cron.Schedule
	loc          *time.Location
	overlap      string
	last         time.Time // the newest slot already handled
}

// cronScheduler fires cron services. The control plane schedules, the agent
// runs (docs/SPEC.md 9.9). A slot runs at most once: its o_cron_runs row is
// inserted under UNIQUE(service, environment, scheduled_at) before the job is
// dispatched.
type cronScheduler struct {
	d *Data

	mu      sync.Mutex
	entries map[string]*cronEntry
	locks   map[string]chan struct{}
	wake    chan struct{}
	booted  bool
}

func newCronScheduler(d *Data) *cronScheduler {
	return &cronScheduler{d: d, entries: map[string]*cronEntry{}, locks: map[string]chan struct{}{}, wake: make(chan struct{}, 1)}
}

func cronKey(envID, svcID string) string { return envID + "/" + svcID }

// parseCron parses a service's schedule in its time zone.
func parseCron(spec, tz string) (cron.Schedule, *time.Location, error) {
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, nil, fmt.Errorf("%q is not a time zone name such as UTC or America/New_York.", tz)
	}
	s, err := cronParser.Parse(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("%q is not a valid cron schedule: use five fields such as */5 * * * * or @hourly.", spec)
	}
	return s, loc, nil
}

// boot settles runs a previous process left behind, records slots missed
// while the control plane was down (without replaying them) and loads the
// schedules.
func (c *cronScheduler) boot(ctx context.Context) error {
	d := c.d
	now := d.now()
	if _, err := d.st.exec(ctx, `UPDATE o_cron_runs SET status = 'failed', message = ?, finished_at = ?
		WHERE status = 'running' AND started_at > 0`, msgInterrupted, now.UnixMilli()); err != nil {
		return err
	}
	if _, err := d.st.exec(ctx, `UPDATE o_cron_runs SET status = 'missed', message = ?, finished_at = ?
		WHERE status = 'running' AND started_at = 0`, msgMissed, now.UnixMilli()); err != nil {
		return err
	}
	var lastTick int64
	if v, _ := d.st.stateGet(ctx, "cron_tick"); v != "" {
		lastTick, _ = strconv.ParseInt(v, 10, 64)
	}
	snaps, err := d.cfg.AllInstances(ctx)
	if err != nil {
		return err
	}
	for _, snap := range snaps {
		c.update(ctx, snap)
	}
	c.mu.Lock()
	entries := make([]*cronEntry, 0, len(c.entries))
	for _, e := range c.entries {
		entries = append(entries, e)
	}
	c.booted = true
	c.mu.Unlock()

	horizon := now.Add(-lateGrace)
	for _, e := range entries {
		if lastTick == 0 {
			continue // first start: nothing was scheduled before
		}
		from := time.UnixMilli(lastTick)
		if w := now.Add(-missedWindow); from.Before(w) {
			from = w
		}
		if since := d.cronSince(ctx, e.envID, e.svcID); since > 0 && time.UnixMilli(since).After(from) {
			from = time.UnixMilli(since)
		}
		n := 0
		for slot := e.sched.Next(from.In(e.loc)); !slot.After(horizon) && n < 2000; slot = e.sched.Next(slot) {
			n++
			c.insertRun(ctx, e.envID, e.svcID, slot.UnixMilli(), "schedule", "missed", msgMissed)
		}
		// Slots after the horizon still run on the next tick; their UNIQUE
		// row keeps a slot the previous process already started from running
		// twice. Slots before lastTick were handled by the previous process.
		start := horizon
		if from.After(start) {
			start = from
		}
		c.mu.Lock()
		e.last = start
		c.mu.Unlock()
	}
	return d.st.stateSet(ctx, "cron_tick", strconv.FormatInt(now.UnixMilli(), 10))
}

func (d *Data) cronSince(ctx context.Context, envID, svcID string) int64 {
	var v int64
	_ = d.st.db.QueryRowContext(ctx, `SELECT cron_since FROM o_data_services WHERE service_id = ? AND environment_id = ?`, svcID, envID).Scan(&v)
	return v
}

// update follows a service's committed schedule.
func (c *cronScheduler) update(ctx context.Context, snap cpapi.ServiceSnapshot) {
	spec := snap.Config.Deploy.CronSchedule
	k := cronKey(snap.EnvID, snap.ServiceID)
	if snap.Deleted || spec == "" {
		c.remove(snap.EnvID, snap.ServiceID)
		return
	}
	sched, loc, err := parseCron(spec, snap.Config.Deploy.CronTimezone)
	if err != nil {
		c.d.log.Warn("cron schedule ignored", "service", snap.ServiceID, "err", err)
		c.remove(snap.EnvID, snap.ServiceID)
		return
	}
	now := c.d.now()
	_ = c.d.st.ensureServiceRow(ctx, snap.EnvID, snap.ServiceID, now.UnixMilli())
	_, _ = c.d.st.exec(ctx, `UPDATE o_data_services SET cron_since = ? WHERE service_id = ? AND environment_id = ? AND cron_since = 0`,
		now.UnixMilli(), snap.ServiceID, snap.EnvID)
	overlap := snap.Config.Deploy.CronOverlap
	if overlap != "queue" {
		overlap = "skip"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if ok && e.spec == spec && e.tz == snap.Config.Deploy.CronTimezone {
		e.overlap = overlap
		return
	}
	c.entries[k] = &cronEntry{envID: snap.EnvID, svcID: snap.ServiceID, spec: spec, tz: snap.Config.Deploy.CronTimezone,
		sched: sched, loc: loc, overlap: overlap, last: now}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *cronScheduler) remove(envID, svcID string) {
	c.mu.Lock()
	delete(c.entries, cronKey(envID, svcID))
	c.mu.Unlock()
}

// loop checks for due slots every few seconds.
func (c *cronScheduler) loop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	lastSaved := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.wake:
		}
		now := c.d.now()
		c.tick(ctx, now)
		if now.Sub(lastSaved) >= 30*time.Second {
			lastSaved = now
			_ = c.d.st.stateSet(ctx, "cron_tick", strconv.FormatInt(now.UnixMilli(), 10))
		}
	}
}

// tick fires every slot that came due since the last tick.
func (c *cronScheduler) tick(ctx context.Context, now time.Time) {
	type due struct {
		e    *cronEntry
		slot time.Time
	}
	var fire []due
	c.mu.Lock()
	for _, e := range c.entries {
		n := 0
		for slot := e.sched.Next(e.last.In(e.loc)); !slot.After(now) && n < 2000; slot = e.sched.Next(slot) {
			n++
			fire = append(fire, due{e, slot})
			e.last = slot
		}
	}
	c.mu.Unlock()
	for _, f := range fire {
		if now.Sub(f.slot) > lateGrace {
			// The scheduler itself was stalled (a suspended host): record it.
			c.insertRun(ctx, f.e.envID, f.e.svcID, f.slot.UnixMilli(), "schedule", "missed", msgMissed)
			continue
		}
		c.fire(ctx, f.e.envID, f.e.svcID, f.slot.UnixMilli(), f.e.overlap)
	}
}

// insertRun records one slot; it reports false when the slot already has a row.
func (c *cronScheduler) insertRun(ctx context.Context, envID, svcID string, scheduledAt int64, trigger, status, msg string) (string, bool) {
	id := newID("crn")
	finished := int64(0)
	if status != "running" {
		finished = c.d.now().UnixMilli()
	}
	_, err := c.d.st.exec(ctx, `INSERT INTO o_cron_runs (id, service_id, environment_id, status, trigger, message, scheduled_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, svcID, envID, status, trigger, msg, scheduledAt, finished)
	if err != nil {
		if !isUniqueViolation(err) {
			c.d.log.Warn("record cron run", "service", svcID, "err", err)
		}
		return "", false
	}
	return id, true
}

func (c *cronScheduler) hasRunning(ctx context.Context, envID, svcID string) bool {
	var n int
	_ = c.d.st.db.QueryRowContext(ctx, `SELECT count(*) FROM o_cron_runs WHERE environment_id = ? AND service_id = ? AND status = 'running'`,
		envID, svcID).Scan(&n)
	return n > 0
}

// fire records a slot and starts its run, honouring the overlap setting.
func (c *cronScheduler) fire(ctx context.Context, envID, svcID string, scheduledAt int64, overlap string) {
	if overlap != "queue" && c.hasRunning(ctx, envID, svcID) {
		c.insertRun(ctx, envID, svcID, scheduledAt, "schedule", "skipped", msgSkipped)
		return
	}
	id, ok := c.insertRun(ctx, envID, svcID, scheduledAt, "schedule", "running", "")
	if !ok {
		return // this slot already ran (or was recorded): never twice
	}
	c.d.background("cron "+id, func(bg context.Context) {
		lock := c.lock(envID, svcID)
		select {
		case lock <- struct{}{}:
		default:
			_, _ = c.d.st.exec(bg, `UPDATE o_cron_runs SET message = ? WHERE id = ?`, msgQueued, id)
			select {
			case lock <- struct{}{}:
			case <-bg.Done():
				return
			}
		}
		defer func() { <-lock }()
		c.execute(bg, envID, svcID, id)
	})
}

func (c *cronScheduler) lock(envID, svcID string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := cronKey(envID, svcID)
	l, ok := c.locks[k]
	if !ok {
		l = make(chan struct{}, 1)
		c.locks[k] = l
	}
	return l
}

// execute runs one recorded run as a job and stores its result.
func (c *cronScheduler) execute(ctx context.Context, envID, svcID, runID string) {
	d := c.d
	start := d.now().UnixMilli()
	jobID := newID("job")
	_, _ = d.st.exec(ctx, `UPDATE o_cron_runs SET started_at = ?, job_id = ?, message = '' WHERE id = ?`, start, jobID, runID)
	finish := func(status string, exit *int, msg, output string) {
		if len(output) > outputLimit {
			output = output[len(output)-outputLimit:]
		}
		var code any
		if exit != nil {
			code = *exit
		}
		_, _ = d.st.exec(context.WithoutCancel(ctx), `UPDATE o_cron_runs SET status = ?, exit_code = ?, message = ?, output = ?, finished_at = ? WHERE id = ?`,
			status, code, msg, output, d.now().UnixMilli(), runID)
	}
	snap, err := d.instance(ctx, envID, svcID)
	if err != nil {
		finish("failed", nil, sentence(err.Error()), "")
		return
	}
	image, cfg, env, err := d.run.ActiveImage(ctx, envID, svcID)
	if err != nil || image == "" {
		finish("failed", nil, msgNoImage, "")
		return
	}
	host := ""
	if snap.Volume != nil {
		host, _ = d.run.VolumeHost(ctx, envID, svcID)
	}
	if host == "" {
		host, err = d.run.PlaceJob(ctx, envID, cfg.Deploy.Placement.Roles)
		if err != nil {
			finish("failed", nil, sentence("No host can run this job: "+err.Error()), "")
			return
		}
	}
	job := proto.Job{
		JobID: jobID, Kind: "cron", Image: image, Cmd: commandArgv(cfg.Deploy.StartCommand), Env: env,
		TimeoutSec: cfg.Deploy.CronTimeoutSec, MemLimitMB: cfg.Deploy.MemoryLimitMB, User: cfg.Deploy.RunAsUser,
		Labels: jobLabels(envID, svcID, "cron"),
	}
	job.Labels["5050.cron-run"] = runID
	if snap.Volume != nil {
		job.Mounts = append(job.Mounts, proto.Mount{Kind: "volume", Source: volumeName(snap.Volume.ID), Target: snap.Volume.MountPath})
	}
	network, ip, release, err := d.run.JobNetwork(ctx, envID, host)
	if err != nil {
		finish("failed", nil, sentence("The job could not join the environment's private network: "+err.Error()), "")
		return
	}
	defer release()
	job.Network, job.IP = network, ip
	out := d.runJob(ctx, host, job)
	switch {
	case out.Err != nil:
		if ctx.Err() != nil {
			return // shutting down: the next start marks the run as interrupted
		}
		finish("failed", nil, sentence("The run could not start: "+out.Err.Error()), "")
	case out.Result.TimedOut:
		code := out.Result.ExitCode
		finish("timeout", &code, fmt.Sprintf("Stopped after the cron timeout of %d seconds.", cfg.Deploy.CronTimeoutSec), out.Result.Output)
	case out.Result.Error != "":
		finish("failed", nil, sentence(out.Result.Error), out.Result.Output)
	case out.Result.ExitCode == 0:
		code := 0
		finish("completed", &code, "", out.Result.Output)
	default:
		code := out.Result.ExitCode
		finish("failed", &code, fmt.Sprintf("Exited with code %d.", code), out.Result.Output)
	}
}

// CronRuns lists the newest runs of a cron service.
func (d *Data) CronRuns(ctx context.Context, envID, serviceID string, limit int) ([]api.CronRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := d.st.db.QueryContext(ctx, `SELECT id, service_id, status, trigger, exit_code, message, scheduled_at, started_at, finished_at
		FROM o_cron_runs WHERE environment_id = ? AND service_id = ? ORDER BY scheduled_at DESC LIMIT ?`, envID, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.CronRun{}
	for rows.Next() {
		var r api.CronRun
		var code sql.NullInt64
		if err := rows.Scan(&r.ID, &r.ServiceID, &r.Status, &r.Trigger, &code, &r.Message, &r.ScheduledAt, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		if code.Valid {
			c := int(code.Int64)
			r.ExitCode = &c
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunCronNow starts a run at once, whatever the overlap setting.
func (d *Data) RunCronNow(ctx context.Context, envID, serviceID, actor string) (api.CronRun, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return api.CronRun{}, err
	}
	if snap.Config.Deploy.CronSchedule == "" {
		return api.CronRun{}, errors.New("This service has no cron schedule. Set one in Settings > Deploy to make it a cron service.")
	}
	if _, _, _, err := d.run.ActiveImage(ctx, envID, serviceID); err != nil {
		return api.CronRun{}, errors.New(msgNoImage + ". Deploy it first.")
	}
	at := d.now().UnixMilli()
	var id string
	for i := 0; i < 5; i++ {
		var ok bool
		if id, ok = d.cron.insertRun(ctx, envID, serviceID, at+int64(i), "manual", "running", ""); ok {
			at += int64(i)
			break
		}
	}
	if id == "" {
		return api.CronRun{}, errors.New("The run could not be recorded. Try again.")
	}
	d.background("cron "+id, func(bg context.Context) { d.cron.execute(bg, envID, serviceID, id) })
	return api.CronRun{ID: id, ServiceID: serviceID, Status: "running", Trigger: "manual", ScheduledAt: at}, nil
}
