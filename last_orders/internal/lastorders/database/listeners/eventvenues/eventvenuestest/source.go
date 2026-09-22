// Package eventvenuestest provides a stand-in for the eventvenues Source. It exists
// only to support tests; production code must bind to a Firestore source.
package eventvenuestest

import (
	"context"
	"sync"

	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/database/listeners/eventvenues"
)

// Source is a controllable stand-in. Watch replays Changes and then idles until the
// context is cancelled.
type Source struct {
	mu      sync.Mutex
	Changes []eventvenues.Change
	Venues  []recurrence.EventVenue
}

func New() *Source {
	return &Source{}
}

func (s *Source) ListEventVenues(context.Context) ([]recurrence.EventVenue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Venues, nil
}

func (s *Source) Watch(ctx context.Context) (eventvenues.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := make([]eventvenues.Change, len(s.Changes))
	copy(pending, s.Changes)
	return &stream{ctx: ctx, pending: pending}, nil
}

type stream struct {
	ctx     context.Context
	pending []eventvenues.Change
	done    bool
}

func (s *stream) Next() ([]eventvenues.Change, error) {
	if !s.done {
		s.done = true
		if len(s.pending) > 0 {
			return s.pending, nil
		}
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *stream) Stop() {}
