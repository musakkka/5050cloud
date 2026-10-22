// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/internal/cp/orch/data/fakerunner"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
)

func (e *testEnv) addCron(schedule, overlap string) string {
	e.t.Helper()
	cfg := api.ServiceConfig{Name: "report", Kind: "cron", Source: api.SourceConfig{Type: "image", Image: "busybox:1.37.0"}}
	cfg.Deploy.CronSchedule = schedule
	cfg.Deploy.CronOverlap = overlap
	cfg.Deploy.CronTimeoutSec = 300
	cfg.Deploy.StartCommand = "sh -c 'echo hello'"
	svc := e.cfg.AddService(e.envID, cfg)
	e.run.SetImage(e.envID, svc, fakerunner.Image{Ref: "registry/svc:dep", Config: cfg, Env: map[string]string{"A": "1"}})
	return svc
}

func runsBySlot(t *testing.T, e *testEnv, svc string) map[int64][]api.CronRun {
	t.Helper()
	runs, err := e.d.CronRuns(e.ctx(), e.envID, svc, 500)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64][]api.CronRun{}
	for _, r := range runs {
		out[r.ScheduledAt] = append(out[r.ScheduledAt], r)
	}
	return out
}

func TestCronExactlyOnceAcrossRestart(t *testing.T) {
	e := newEnv(t)
	svc := e.addCron("*/5 * * * *", "")
	ctx := e.ctx()

	// Process A boots at 10:00:30 and runs the 10:05 slot.
	if err := e.d.cron.boot(ctx); err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)
	e.clock.Set(slot.Add(2 * time.Second))
	e.d.cron.tick(ctx, e.clock.Now())
	e.wait()
	if got := len(e.run.JobsOfKind("cron")); got != 1 {
		t.Fatalf("process A ran %d jobs for the slot, want 1", got)
	}

	// Process B starts 8 seconds later on the same state.db, inside the
	// slot's grace minute, and ticks: the slot must not run again.
	e.clock.Set(slot.Add(10 * time.Second))
	b := e.newData(e.db)
	if err := b.cron.boot(ctx); err != nil {
		t.Fatal(err)
	}
	b.cron.tick(ctx, e.clock.Now())
	b.bgWG.Wait()
	if got := len(e.run.JobsOfKind("cron")); got != 1 {
		t.Fatalf("after the restart the slot ran %d times, want exactly once", got)
	}
	runs := runsBySlot(t, e, svc)
	if len(runs[slot.UnixMilli()]) != 1 || runs[slot.UnixMilli()][0].Status != "completed" {
		t.Fatalf("slot rows = %+v, want one completed run", runs[slot.UnixMilli()])
	}

	// The next slot runs normally in process B.
	next := slot.Add(5 * time.Minute)
	e.clock.Set(next.Add(time.Second))
	b.cron.tick(ctx, e.clock.Now())
	b.bgWG.Wait()
	if got := len(e.run.JobsOfKind("cron")); got != 2 {
		t.Fatalf("the next slot did not run: %d jobs", got)
	}
}

func TestCronCrashBeforeDispatchStillRunsOnce(t *testing.T) {
	// Process A dies right after the boundary, before ticking. Process B
	// starts within the grace minute and must run the slot (not drop it).
	e := newEnv(t)
	svc := e.addCron("*/5 * * * *", "")
	ctx := e.ctx()
	if err := e.d.cron.boot(ctx); err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)
	e.clock.Set(slot.Add(20 * time.Second))
	b := e.newData(e.db)
	if err := b.cron.boot(ctx); err != nil {
		t.Fatal(err)
	}
	b.cron.tick(ctx, e.clock.Now())
	b.bgWG.Wait()
	runs := runsBySlot(t, e, svc)
	if len(runs[slot.UnixMilli()]) != 1 || runs[slot.UnixMilli()][0].Status != "completed" {
		t.Fatalf("slot rows = %+v, want one completed run", runs[slot.UnixMilli()])
	}
}

func TestCronMissedSlotsRecordedNotReplayed(t *testing.T) {
	e := newEnv(t)
	svc := e.addCron("@hourly", "")
	ctx := e.ctx()
	if err := e.d.cron.boot(ctx); err != nil { // cron_tick = 10:00:30
		t.Fatal(err)
	}
	// A run was in progress when the control plane went down, and another
	// was recorded but never dispatched.
	if _, err := e.db.Exec(`INSERT INTO o_cron_runs (id, service_id, environment_id, status, scheduled_at, started_at)
		VALUES ('crn_a', ?, ?, 'running', 1, 2), ('crn_b', ?, ?, 'running', 3, 0)`, svc, e.envID, svc, e.envID); err != nil {
		t.Fatal(err)
	}

	// Down for 30 hours: only the last 24 hours of slots are recorded.
	e.clock.Set(time.Date(2026, 10, 22, 16, 30, 0, 0, time.UTC))
	b := e.newData(e.db)
	if err := b.cron.boot(ctx); err != nil {
		t.Fatal(err)
	}
	b.cron.tick(ctx, e.clock.Now())
	b.bgWG.Wait()
	if got := len(e.run.JobsOfKind("cron")); got != 0 {
		t.Fatalf("missed slots were replayed: %d jobs", got)
	}
	runs := runsBySlot(t, e, svc)
	missed := 0
	for at, rs := range runs {
		if at < 10 {
			continue
		}
		for _, r := range rs {
			if r.Status != "missed" || r.Message != msgMissed {
				t.Fatalf("slot %v: %+v, want missed", time.UnixMilli(at).UTC(), r)
			}
			if time.UnixMilli(at).Before(e.clock.Now().Add(-24 * time.Hour)) {
				t.Fatalf("slot %v is older than 24 hours", time.UnixMilli(at).UTC())
			}
			missed++
		}
	}
	if missed != 24 {
		t.Fatalf("recorded %d missed slots, want 24 (one per hour of the last day)", missed)
	}
	if r := runs[1][0]; r.Status != "failed" || r.Message != msgInterrupted {
		t.Fatalf("interrupted run = %+v", r)
	}
	if r := runs[3][0]; r.Status != "missed" {
		t.Fatalf("undispatched run = %+v", r)
	}
}

func TestCronFirstStartRecordsNothing(t *testing.T) {
	e := newEnv(t)
	svc := e.addCron("* * * * *", "")
	if err := e.d.cron.boot(e.ctx()); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, e.db, `SELECT count(*) FROM o_cron_runs WHERE service_id = ?`, svc); n != 0 {
		t.Fatalf("first start wrote %d rows", n)
	}
}

func TestCronOverlapSkipAndQueue(t *testing.T) {
	for _, overlap := range []string{"skip", "queue"} {
		t.Run(overlap, func(t *testing.T) {
			e := newEnv(t)
			svc := e.addCron("* * * * *", overlap)
			release := make(chan struct{})
			var started sync.WaitGroup
			started.Add(1)
			first := true
			var mu sync.Mutex
			e.run.OnJob = func(host string, job proto.Job) (proto.JobResult, error) {
				mu.Lock()
				isFirst := first
				first = false
				mu.Unlock()
				if isFirst {
					started.Done()
					<-release
				}
				return proto.JobResult{ExitCode: 0, Output: "done\n"}, nil
			}
			ctx := e.ctx()
			if err := e.d.cron.boot(ctx); err != nil {
				t.Fatal(err)
			}
			e.clock.Set(time.Date(2026, 10, 21, 10, 1, 1, 0, time.UTC))
			e.d.cron.tick(ctx, e.clock.Now())
			started.Wait()
			e.clock.Set(time.Date(2026, 10, 21, 10, 2, 1, 0, time.UTC))
			e.d.cron.tick(ctx, e.clock.Now())
			close(release)
			e.wait()
			runs, _ := e.d.CronRuns(ctx, e.envID, svc, 10)
			if len(runs) != 2 {
				t.Fatalf("runs = %+v", runs)
			}
			second := runs[0] // newest first
			if overlap == "skip" {
				if second.Status != "skipped" || second.Message != msgSkipped {
					t.Fatalf("second run = %+v, want skipped", second)
				}
				if n := len(e.run.JobsOfKind("cron")); n != 1 {
					t.Fatalf("%d jobs ran, want 1", n)
				}
			} else {
				if second.Status != "completed" {
					t.Fatalf("second run = %+v, want completed after the first", second)
				}
				if n := len(e.run.JobsOfKind("cron")); n != 2 {
					t.Fatalf("%d jobs ran, want 2", n)
				}
			}
		})
	}
}

func TestCronJobShapeAndResults(t *testing.T) {
	e := newEnv(t)
	svc := e.addCron("* * * * *", "")
	ctx := e.ctx()
	cases := []struct {
		res    proto.JobResult
		err    error
		status string
		msg    string
	}{
		{proto.JobResult{ExitCode: 0, Output: "ok\n"}, nil, "completed", ""},
		{proto.JobResult{ExitCode: 3, Output: "boom\n"}, nil, "failed", "Exited with code 3."},
		{proto.JobResult{ExitCode: -1, TimedOut: true}, nil, "timeout", "Stopped after the cron timeout of 300 seconds."},
		{proto.JobResult{}, errors.New("the host is offline"), "failed", "The run could not start: the host is offline."},
	}
	for _, c := range cases {
		c := c
		e.run.OnJob = func(string, proto.Job) (proto.JobResult, error) { return c.res, c.err }
		run, err := e.d.RunCronNow(ctx, e.envID, svc, "usr_1")
		if err != nil {
			t.Fatal(err)
		}
		e.wait()
		runs, _ := e.d.CronRuns(ctx, e.envID, svc, 50)
		var got api.CronRun
		for _, r := range runs {
			if r.ID == run.ID {
				got = r
			}
		}
		if got.Status != c.status || got.Message != c.msg {
			t.Errorf("result %+v -> run %+v, want %s %q", c.res, got, c.status, c.msg)
		}
		e.clock.Add(time.Second)
	}
	jobs := e.run.JobsOfKind("cron")
	j := jobs[0].Job
	if strings.Join(j.Cmd, " ") != "sh -c echo hello" || j.Image != "registry/svc:dep" || j.Env["A"] != "1" || j.Network == "" || j.TimeoutSec != 300 {
		t.Fatalf("job = %+v", j)
	}
	if j.Labels["5050.service"] != svc || j.Labels["5050.cron-run"] == "" {
		t.Fatalf("labels = %v", j.Labels)
	}
}

func TestRunCronNowRefusals(t *testing.T) {
	e := newEnv(t)
	plain := e.cfg.AddService(e.envID, api.ServiceConfig{Name: "web"})
	if _, err := e.d.RunCronNow(e.ctx(), e.envID, plain, ""); err == nil || !strings.Contains(err.Error(), "no cron schedule") {
		t.Fatalf("err = %v", err)
	}
	cfg := api.ServiceConfig{Name: "job"}
	cfg.Deploy.CronSchedule = "@daily"
	undeployed := e.cfg.AddService(e.envID, cfg)
	if _, err := e.d.RunCronNow(e.ctx(), e.envID, undeployed, ""); err == nil || !strings.Contains(err.Error(), "Deploy it first") {
		t.Fatalf("err = %v", err)
	}
}

func TestCronScheduleChangesFollowServiceChanged(t *testing.T) {
	e := newEnv(t)
	svc := e.addCron("@daily", "")
	ctx := e.ctx()
	if err := e.d.cron.boot(ctx); err != nil {
		t.Fatal(err)
	}
	e.cfg.Update(e.envID, svc, func(s *cpapi.ServiceSnapshot) { s.Config.Deploy.CronSchedule = "*/10 * * * *" })
	e.d.ServiceChanged(ctx, e.envID, svc)
	e.clock.Set(time.Date(2026, 10, 21, 10, 10, 5, 0, time.UTC))
	e.d.cron.tick(ctx, e.clock.Now())
	e.wait()
	if n := len(e.run.JobsOfKind("cron")); n != 1 {
		t.Fatalf("%d jobs after the schedule change, want 1", n)
	}
	e.cfg.Delete(e.envID, svc)
	e.d.ServiceChanged(ctx, e.envID, svc)
	e.clock.Set(time.Date(2026, 10, 21, 10, 20, 5, 0, time.UTC))
	e.d.cron.tick(ctx, e.clock.Now())
	e.wait()
	if n := len(e.run.JobsOfKind("cron")); n != 1 {
		t.Fatalf("a deleted service still runs: %d jobs", n)
	}
}

func TestParseCron(t *testing.T) {
	for _, ok := range []string{"@hourly", "@daily", "@weekly", "@monthly", "*/5 * * * *", "0 3 * * 1-5"} {
		if _, _, err := parseCron(ok, "Europe/Berlin"); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"* * * *", "every hour", "61 * * * *"} {
		if _, _, err := parseCron(bad, ""); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if _, _, err := parseCron("@daily", "Mars/Olympus"); err == nil || !strings.Contains(err.Error(), "time zone") {
		t.Errorf("bad zone: %v", err)
	}
	// Schedules are evaluated in their time zone.
	s, loc, _ := parseCron("0 9 * * *", "America/New_York")
	next := s.Next(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC).In(loc))
	if next.UTC().Hour() != 13 {
		t.Errorf("09:00 New York = %v UTC", next.UTC())
	}
}
