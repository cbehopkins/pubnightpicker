// Package completedpollstest provides a stand-in for the completedpolls Source. It
// exists only to support tests; production code must bind to a Firestore source.
package completedpollstest

import (
	"context"
	"sync"

	"last_orders/internal/lastorders/database/listeners/completedpolls"
)

// Source is a controllable stand-in. Watch replays Changes and then idles until the
// context is cancelled.
type Source struct {
	mu      sync.Mutex
	Changes []completedpolls.Change
}

func New() *Source {
	return &Source{}
}

// CompletePoll queues a completed-poll change for the next Watch.
func (s *Source) CompletePoll(pollID, selectedVenueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Changes = append(s.Changes, completedpolls.Change{
		Kind: completedpolls.ChangeModified,
		Doc: completedpolls.Document{ID: pollID, Data: map[string]any{
			"completed": true,
			"selected":  selectedVenueID,
		}},
	})
}

func (s *Source) Watch(ctx context.Context) (completedpolls.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := make([]completedpolls.Change, len(s.Changes))
	copy(pending, s.Changes)
	return &stream{ctx: ctx, pending: pending}, nil
}

type stream struct {
	ctx     context.Context
	pending []completedpolls.Change
	done    bool
}

func (s *stream) Next() ([]completedpolls.Change, error) {
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
