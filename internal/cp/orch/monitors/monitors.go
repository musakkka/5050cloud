// SPDX-License-Identifier: AGPL-3.0-only

// Package monitors evaluates per-service threshold monitors every minute
// from the 30 s metric points (docs/SPEC.md 9.21). Definitions live in core
// (cpapi.Config.Monitors); this package decides the state, records it with
// SetMonitorState and asks core to deliver monitor.triggered once when a
// monitor enters the alarm state and monitor.resolved once when it recovers.
//
// A monitor fires when every point of the last N minutes is at or above its
// threshold and the window is covered by data (at least 60% of the expected
// points, the first one no later than a minute after the window starts). It
// recovers when the points of the last minute are all below the threshold,
// or when there is no data at all for the window.
package monitors

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Point is one 30 s sample in the monitor's unit.
type Point struct {
	TS    int64
	Value float64
}

// Source gives the evaluator its data.
type Source interface {
	// Values returns the monitor's metric over a window, already converted
	// to the monitor's unit (percent, or GB for disk with unit "gb").
	Values(ctx context.Context, m api.Monitor, from, until int64) ([]Point, error)
	// ProjectID returns the project of an environment (for delivery).
	ProjectID(ctx context.Context, envID string) string
}

// Config is the slice of cpapi.Config the evaluator uses.
type Config interface {
	Monitors(ctx context.Context) ([]api.Monitor, error)
	SetMonitorState(ctx context.Context, monitorID, state string, at int64) error
	Notify(ctx context.Context, n cpapi.Notice) error
}

// States.
const (
	StateOK     = "ok"
	StateFiring = "firing"
)

// Evaluator holds the live state of every monitor.
type Evaluator struct {
	Config Config
	Source Source
	Log    *slog.Logger
	Now    func() time.Time

	mu    sync.Mutex
	state map[string]string // monitor id -> state
	svc   map[string]string // monitor id -> env/svc
}

// New builds an evaluator.
func New(c Config, s Source, log *slog.Logger) *Evaluator {
	if log == nil {
		log = slog.Default()
	}
	return &Evaluator{Config: c, Source: s, Log: log.With("component", "monitors"), Now: time.Now,
		state: map[string]string{}, svc: map[string]string{}}
}

// Decision is the result of evaluating one monitor.
type Decision int

const (
	Hold    Decision = iota // no change
	Fire                    // enter the alarm state
	Recover                 // leave it
)

// Evaluate decides a monitor's transition from its points. windowStart is
// now minus ForMinutes.
func Evaluate(m api.Monitor, firing bool, pts []Point, now time.Time) Decision {
	window := time.Duration(clampMinutes(m.ForMinutes)) * time.Minute
	from := now.Add(-window).UnixMilli()
	var in []Point
	for _, p := range pts {
		if p.TS >= from && p.TS <= now.UnixMilli() {
			in = append(in, p)
		}
	}
	if !firing {
		expected := int(window / (30 * time.Second))
		if len(in) == 0 || float64(len(in)) < math.Max(2, 0.6*float64(expected)) {
			return Hold
		}
		if in[0].TS > from+time.Minute.Milliseconds() {
			return Hold
		}
		for _, p := range in {
			if p.Value < m.Threshold {
				return Hold
			}
		}
		return Fire
	}
	if len(in) == 0 {
		return Recover
	}
	lastMin := now.Add(-time.Minute).UnixMilli()
	seen := 0
	for _, p := range in {
		if p.TS < lastMin {
			continue
		}
		seen++
		if p.Value >= m.Threshold {
			return Hold
		}
	}
	if seen == 0 {
		return Hold
	}
	return Recover
}

func clampMinutes(n int) int {
	if n < 1 {
		return 1
	}
	if n > 60 {
		return 60
	}
	return n
}

// Tick evaluates every monitor once.
func (e *Evaluator) Tick(ctx context.Context) {
	ms, err := e.Config.Monitors(ctx)
	if err != nil {
		e.Log.Warn("load monitors", "err", err)
		return
	}
	now := e.Now()
	seen := map[string]bool{}
	for _, m := range ms {
		seen[m.ID] = true
		e.mu.Lock()
		st, ok := e.state[m.ID]
		if !ok {
			st = m.State
			if st != StateFiring {
				st = StateOK
			}
			e.state[m.ID] = st
		}
		e.svc[m.ID] = m.EnvironmentID + "/" + m.ServiceID
		e.mu.Unlock()
		window := time.Duration(clampMinutes(m.ForMinutes)) * time.Minute
		pts, err := e.Source.Values(ctx, m, now.Add(-window-time.Minute).UnixMilli(), now.UnixMilli()+1)
		if err != nil {
			e.Log.Warn("monitor data", "monitor", m.ID, "err", err)
			continue
		}
		switch Evaluate(m, st == StateFiring, pts, now) {
		case Fire:
			e.transition(ctx, m, StateFiring, lastValue(pts), now)
		case Recover:
			e.transition(ctx, m, StateOK, lastValue(pts), now)
		}
	}
	e.mu.Lock()
	for id := range e.state {
		if !seen[id] {
			delete(e.state, id)
			delete(e.svc, id)
		}
	}
	e.mu.Unlock()
}

func lastValue(pts []Point) float64 {
	if len(pts) == 0 {
		return math.NaN()
	}
	return pts[len(pts)-1].Value
}

func (e *Evaluator) transition(ctx context.Context, m api.Monitor, state string, value float64, now time.Time) {
	e.mu.Lock()
	e.state[m.ID] = state
	e.mu.Unlock()
	if err := e.Config.SetMonitorState(ctx, m.ID, state, now.UnixMilli()); err != nil {
		e.Log.Warn("record monitor state", "monitor", m.ID, "err", err)
	}
	event := api.EventMonitorTriggered
	text := FiringText(m, value)
	if state == StateOK {
		event = api.HookMonitorResolved
		text = ResolvedText(m)
	}
	n := cpapi.Notice{
		Event: event, ProjectID: e.Source.ProjectID(ctx, m.EnvironmentID), EnvID: m.EnvironmentID, ServiceID: m.ServiceID,
		Ref: m.ID, Text: text, Users: m.Channels.Users, Webhooks: m.Channels.Webhooks,
		Details: map[string]any{"metric": m.Metric, "threshold": m.Threshold, "unit": unit(m), "forMinutes": clampMinutes(m.ForMinutes), "state": state},
	}
	if !math.IsNaN(value) {
		n.Details["value"] = math.Round(value*10) / 10
	}
	if err := e.Config.Notify(ctx, n); err != nil {
		e.Log.Warn("deliver monitor notice", "monitor", m.ID, "err", err)
	}
}

func unit(m api.Monitor) string {
	if m.Metric == "disk" && strings.EqualFold(m.Unit, "gb") {
		return "gb"
	}
	return "pct"
}

func metricName(m api.Monitor) string {
	switch m.Metric {
	case "cpu":
		return "CPU"
	case "memory":
		return "Memory"
	case "disk":
		return "Disk"
	case "http_5xx_rate":
		return "The 5xx error rate"
	}
	return m.Metric
}

func amount(m api.Monitor, v float64) string {
	if unit(m) == "gb" {
		return fmt.Sprintf("%.1f GB", v)
	}
	return fmt.Sprintf("%.0f%%", v)
}

// FiringText is the one sentence of a triggered monitor.
func FiringText(m api.Monitor, value float64) string {
	mins := clampMinutes(m.ForMinutes)
	plural := "minutes"
	if mins == 1 {
		plural = "minute"
	}
	if math.IsNaN(value) {
		return fmt.Sprintf("%s has been above %s for %d %s.", metricName(m), amount(m, m.Threshold), mins, plural)
	}
	return fmt.Sprintf("%s is at %s, above the %s threshold for %d %s.", metricName(m), amount(m, value), amount(m, m.Threshold), mins, plural)
}

// ResolvedText is the one sentence of a recovered monitor.
func ResolvedText(m api.Monitor) string {
	return fmt.Sprintf("%s is back under the %s threshold.", metricName(m), amount(m, m.Threshold))
}

// State returns the live state of a monitor ("" when unknown).
func (e *Evaluator) State(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state[id]
}

// ServiceStates returns the live state of every monitor of a service instance.
func (e *Evaluator) ServiceStates(envID, serviceID string) map[string]string {
	key := envID + "/" + serviceID
	out := map[string]string{}
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, k := range e.svc {
		if k == key {
			out[id] = e.state[id]
		}
	}
	return out
}

// Firing reports whether any monitor of a service instance is firing.
func (e *Evaluator) Firing(envID, serviceID string) bool {
	for _, s := range e.ServiceStates(envID, serviceID) {
		if s == StateFiring {
			return true
		}
	}
	return false
}

// Run evaluates every minute until ctx is done.
func (e *Evaluator) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Tick(ctx)
		}
	}
}
