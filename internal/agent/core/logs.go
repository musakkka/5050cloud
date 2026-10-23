// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core/statecache"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// maxLinesPerMessage keeps one log_lines message well under the frame cap.
const maxLinesPerMessage = 500

func (a *Agent) answerLogsQuery(id string, q proto.LogsQuery) {
	res, err := a.logs.Query(q)
	if err != nil {
		a.send(proto.TAck, id, proto.Ack{Error: sentence(err)})
		return
	}
	a.send(proto.TLogsResult, id, res)
}

// subscribeLogs starts a live tail for one socket. Lines are batched every
// 250 ms; a slow consumer loses lines, counted in LogLines.Dropped.
func (a *Agent) subscribeLogs(ctx context.Context, s *session, reqID string, sub proto.LogsSubscribe) {
	if sub.SubID == "" {
		sub.SubID = reqID
	}
	ls, err := a.logs.Subscribe(sub.SubID, sub.Query, 4096)
	if err != nil {
		a.send(proto.TAck, reqID, proto.Ack{Error: sentence(err)})
		return
	}
	sctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if old, ok := s.subs[sub.SubID]; ok {
		old()
	}
	s.subs[sub.SubID] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			a.logs.Unsubscribe(sub.SubID)
		}()
		t := time.NewTicker(a.t.logBatch)
		defer t.Stop()
		var batch []proto.LogRecord
		flush := func() bool {
			dropped := ls.TakeDropped()
			if len(batch) == 0 && dropped == 0 {
				return true
			}
			for len(batch) > maxLinesPerMessage {
				if !a.send(proto.TLogLines, "", proto.LogLines{SubID: sub.SubID, Lines: batch[:maxLinesPerMessage]}) {
					return false
				}
				batch = batch[maxLinesPerMessage:]
			}
			ok := a.send(proto.TLogLines, "", proto.LogLines{SubID: sub.SubID, Lines: batch, Dropped: dropped})
			batch = nil
			return ok
		}
		for {
			select {
			case <-sctx.Done():
				return
			case <-s.done:
				return
			case r, ok := <-ls.C:
				if !ok {
					return
				}
				batch = append(batch, r)
			case <-t.C:
				if !flush() {
					return
				}
			}
		}
	}()
}

func (a *Agent) unsubscribeLogs(s *session, id string) {
	s.mu.Lock()
	cancel := s.subs[id]
	delete(s.subs, id)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.logs.Unsubscribe(id)
}

// tailLoop ships error and deploy lines every 10 s (9.4).
func (a *Agent) tailLoop(ctx context.Context) {
	every(ctx, a.t.tailEvery, func() {
		if a.currentSession() == nil {
			return // the buffer keeps the newest lines until the next connection
		}
		lines := a.logs.Tail().Take()
		if len(lines) > 0 {
			a.send(proto.THostLogsTail, "", proto.HostLogsTail{Lines: lines})
		}
		a.cursors.save()
	})
}

// accessLogLoop stores Caddy access logs under their service (stream http)
// and feeds the HTTP metrics.
func (a *Agent) accessLogLoop(ctx context.Context) {
	for ctx.Err() == nil {
		ch := make(chan proto.LogRecord, 1024)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for r := range ch {
				r.Stream = "http"
				a.http.add(r)
				_ = a.logs.Append(a.envOfService(r.ServiceID), r)
			}
		}()
		err := a.o.Ingress.AccessLogs(ctx, ch)
		close(ch)
		<-done
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.log.Debug("access logs are not available yet", "err", err)
		}
		if !sleepCtx(ctx, 30*time.Second) {
			return
		}
	}
}

// envOfService finds the environment a service runs in on this host.
func (a *Agent) envOfService(svc string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.replicas {
		if r.spec.ServiceID == svc {
			return r.spec.EnvironmentID
		}
	}
	if a.desired != nil {
		for _, r := range a.desired.Replicas {
			if r.ServiceID == svc {
				return r.EnvironmentID
			}
		}
	}
	return "_"
}

// certLoop forwards certificates the ACME primary issued (cert_issued).
func (a *Agent) certLoop(ctx context.Context) {
	for ctx.Err() == nil {
		ch := make(chan proto.CertBundle, 16)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for c := range ch {
				a.sendDurable(proto.TCertIssued, "", proto.CertIssued{CertBundle: c})
			}
		}()
		err := a.o.Ingress.IssuedCerts(ctx, ch)
		close(ch)
		<-done
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.log.Debug("certificate events are not available yet", "err", err)
		}
		if !sleepCtx(ctx, 30*time.Second) {
			return
		}
	}
}

// certStatusLoop reports certificate errors and successes to the control
// plane (cert_status) so the console can show why a certificate is missing.
// Only "error" and "active" are sent: a host that is not the ACME primary
// can only say "issuing", which is no news. A change is sent at once and the
// whole set again every few minutes.
func (a *Agent) certStatusLoop(ctx context.Context) {
	var last map[string]proto.CertDomainState
	var lastSent time.Time
	for {
		if !sleepCtx(ctx, 15*time.Second) {
			return
		}
		st, err := a.o.Ingress.CertStatus(ctx)
		if err != nil {
			continue
		}
		cur := map[string]proto.CertDomainState{}
		for d, s := range st {
			if s.Status == "error" || s.Status == "active" {
				cur[d] = proto.CertDomainState{Status: s.Status, Error: s.Error}
			}
		}
		if len(cur) == 0 {
			last = cur
			continue
		}
		if reflect.DeepEqual(cur, last) && time.Since(lastSent) < 5*time.Minute {
			continue
		}
		a.sendDurable(proto.TCertStatus, "", proto.CertStatus{Domains: cur})
		last, lastSent = cur, time.Now()
	}
}

// cursorFile remembers the newest log timestamp read per container, so an
// agent restart neither loses nor repeats lines.
type cursorFile struct {
	path  string
	mu    sync.Mutex
	m     map[string]time.Time
	dirty bool
}

func loadCursors(path string) *cursorFile {
	c := &cursorFile{path: path, m: map[string]time.Time{}}
	if b, err := os.ReadFile(path); err == nil {
		var raw map[string]int64
		if json.Unmarshal(b, &raw) == nil {
			for k, v := range raw {
				c.m[k] = time.Unix(0, v)
			}
		}
	}
	return c
}

func (c *cursorFile) get(id string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[id]
}

func (c *cursorFile) set(id string, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.m[id]) {
		c.m[id] = t
		c.dirty = true
	}
}

// keep drops cursors of containers that no longer exist.
func (c *cursorFile) keep(ids map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if !ids[k] {
			delete(c.m, k)
			c.dirty = true
		}
	}
}

func (c *cursorFile) save() {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	raw := make(map[string]int64, len(c.m))
	for k, v := range c.m {
		raw[k] = v.UnixNano()
	}
	c.dirty = false
	c.mu.Unlock()
	b, _ := json.Marshal(raw)
	_ = statecache.WriteFileAtomic(c.path, b, 0o600)
}
