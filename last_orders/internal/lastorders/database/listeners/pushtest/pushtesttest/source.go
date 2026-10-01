// Package pushtesttest provides an in-memory pushtest.Source for tests.
package pushtesttest

import (
	"context"
	"maps"
	"sync"

	"last_orders/internal/lastorders/database/listeners/pushtest"
)

// Source replays Requests once per Watch, then idles until cancelled.
type Source struct {
	mu       sync.Mutex
	Requests map[string]any
}

func New() *Source {
	return &Source{Requests: map[string]any{}}
}

func (s *Source) Watch(ctx context.Context) (pushtest.SnapshotStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &stream{ctx: ctx, pending: maps.Clone(s.Requests)}, nil
}

type stream struct {
	ctx     context.Context
	pending map[string]any
	done    bool
}

func (s *stream) Next() (map[string]any, error) {
	if !s.done {
		s.done = true
		return s.pending, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *stream) Stop() {}
