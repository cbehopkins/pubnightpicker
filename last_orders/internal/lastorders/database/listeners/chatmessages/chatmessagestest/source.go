// Package chatmessagestest provides an in-memory chatmessages.Source for tests.
package chatmessagestest

import (
	"context"
	"slices"
	"sync"

	"last_orders/internal/lastorders/database/listeners/chatmessages"
)

// Source replays Documents once per Watch, then idles until cancelled.
type Source struct {
	mu        sync.Mutex
	Documents []chatmessages.Document
}

func New() *Source {
	return &Source{}
}

func (s *Source) Watch(ctx context.Context) (chatmessages.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &stream{ctx: ctx, pending: slices.Clone(s.Documents)}, nil
}

type stream struct {
	ctx     context.Context
	pending []chatmessages.Document
	done    bool
}

func (s *stream) Next() ([]chatmessages.Document, error) {
	if !s.done {
		s.done = true
		return s.pending, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *stream) Stop() {}
