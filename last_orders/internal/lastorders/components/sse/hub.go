// Package sse provides the instance-local lifecycle and delivery primitives for
// full-view server-sent-event streams.
package sse

import (
	"sync"
)

// Hub routes coalescible view-change wake-ups and authorization invalidations
// to active subscriptions. It carries no view data or credentials.
type Hub struct {
	mu      sync.Mutex
	nextID  uint64
	closed  bool
	done    chan struct{}
	streams map[uint64]*Subscription
}

// Subscription receives signals for one view, authorization scope and UID.
// Its channels are intentionally never closed; Hub owns registration and
// signals them while holding its lock.
type Subscription struct {
	id      uint64
	hub     *Hub
	viewKey string
	scope   string
	uid     string
	changed chan struct{}
	reset   chan struct{}
	removed bool
}

func NewHub() *Hub {
	return &Hub{done: make(chan struct{}), streams: make(map[uint64]*Subscription)}
}

// Register installs a subscription before the caller performs authorization.
// A hub that is already shut down returns an unregistered subscription whose
// Done signal is already ready.
func (h *Hub) Register(viewKey, scopeKey, uid string) *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	s := &Subscription{
		id: h.nextID, hub: h, viewKey: viewKey, scope: scopeKey, uid: uid,
		changed: make(chan struct{}, 1), reset: make(chan struct{}, 1),
	}
	if h.closed {
		s.removed = true
		return s
	}
	h.streams[s.id] = s
	return s
}

func (s *Subscription) Changed() <-chan struct{}     { return s.changed }
func (s *Subscription) Invalidated() <-chan struct{} { return s.reset }
func (s *Subscription) Done() <-chan struct{}        { return s.hub.done }

// Unregister is safe to call more than once and concurrently with hub signals.
func (s *Subscription) Unregister() {
	h := s.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.removed {
		return
	}
	s.removed = true
	delete(h.streams, s.id)
}

func (h *Hub) PublishChanged(viewKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.streams {
		if s.viewKey == viewKey {
			signal(s.changed)
		}
	}
}

func (h *Hub) InvalidateUID(uid string) {
	if uid == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.streams {
		if s.uid == uid {
			signal(s.reset)
		}
	}
}

func (h *Hub) InvalidateAllAuthenticated() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.streams {
		if s.uid != "" {
			signal(s.reset)
		}
	}
}

func (h *Hub) InvalidateScope(scopeKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.streams {
		if s.scope == scopeKey {
			signal(s.reset)
		}
	}
}

// Shutdown ends all subscriptions. Closing the shared done channel is safe;
// per-subscription channels remain open.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.done)
	for _, s := range h.streams {
		s.removed = true
	}
	clear(h.streams)
}

// SubscriberCount is intended for diagnostics and lifecycle tests.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.streams)
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
