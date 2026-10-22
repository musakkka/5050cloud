// SPDX-License-Identifier: AGPL-3.0-only

// Package events is the in-memory fan-out behind GET /api/v1/events
// (docs/SPEC.md 9.24 and 11.3). Events carry an environment id; install
// events (host, install) reach every subscriber; notification events reach
// one user. Each subscriber has a bounded queue that drops events rather
// than blocking the publisher, and counts what it dropped.
package events

import (
	"sync"
	"sync/atomic"

	"github.com/musakkka/5050cloud/pkg/api"
)

// Broker fans out events.
type Broker struct {
	mu   sync.RWMutex
	subs map[*Sub]struct{}
}

// New returns an empty broker.
func New() *Broker { return &Broker{subs: map[*Sub]struct{}{}} }

// Sub is one subscriber.
type Sub struct {
	EnvID   string
	UserID  string
	C       chan api.Event
	dropped atomic.Int64
	b       *Broker
	once    sync.Once
}

// Dropped returns and resets the number of dropped events.
func (s *Sub) Dropped() int64 { return s.dropped.Swap(0) }

// Close unsubscribes.
func (s *Sub) Close() {
	s.once.Do(func() {
		s.b.mu.Lock()
		delete(s.b.subs, s)
		s.b.mu.Unlock()
	})
}

// Subscribe registers a subscriber for envID ("" = install events only) and user.
func (b *Broker) Subscribe(envID, userID string, buf int) *Sub {
	if buf <= 0 {
		buf = 256
	}
	s := &Sub{EnvID: envID, UserID: userID, C: make(chan api.Event, buf), b: b}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// installWide are event types every subscriber receives.
var installWide = map[string]bool{"host": true, "install": true}

// Publish delivers e to subscribers of its environment, or to everyone
// for install-wide types.
func (b *Broker) Publish(e api.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if installWide[e.Type] || (e.EnvironmentID != "" && s.EnvID == e.EnvironmentID) {
			s.send(e)
		}
	}
}

// PublishUser delivers e (a notification) to every subscriber of userID.
func (b *Broker) PublishUser(userID string, e api.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if s.UserID == userID {
			s.send(e)
		}
	}
}

func (s *Sub) send(e api.Event) {
	select {
	case s.C <- e:
	default:
		s.dropped.Add(1)
	}
}

// Count returns the number of subscribers (tests and metrics).
func (b *Broker) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
