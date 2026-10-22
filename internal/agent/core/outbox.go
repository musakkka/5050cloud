// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"sync"
	"time"
)

// maxOutbox caps buffered messages while the control plane is unreachable.
const maxOutbox = 20000

// outbox keeps build and job messages produced while the socket was down
// and sends them after the next hello (7.5: results are kept for an hour).
// When it is full, log lines go first; results are kept.
type outbox struct {
	mu    sync.Mutex
	ttl   time.Duration
	items []outItem
}

type outItem struct {
	b      []byte
	result bool
	at     time.Time
}

func newOutbox(ttl time.Duration) *outbox { return &outbox{ttl: ttl} }

func (o *outbox) add(b []byte, result bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.items = append(o.items, outItem{b: b, result: result, at: time.Now()})
	if len(o.items) <= maxOutbox {
		return
	}
	// Drop the oldest log line, or the oldest item when only results remain.
	for i, it := range o.items {
		if !it.result {
			o.items = append(o.items[:i], o.items[i+1:]...)
			return
		}
	}
	o.items = o.items[1:]
}

// drain returns the items younger than the TTL in order and empties the box.
func (o *outbox) drain() [][]byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out [][]byte
	for _, it := range o.items {
		if time.Since(it.at) <= o.ttl {
			out = append(out, it.b)
		}
	}
	o.items = nil
	return out
}

func (o *outbox) len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.items)
}
