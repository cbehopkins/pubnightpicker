// Package newpollstest provides a stand-in for the newpolls Source. It exists only
// to support tests; production code must bind to a Firestore source.
package newpollstest

import (
	"context"
	"sync"

	"last_orders/internal/lastorders/database/listeners/newpolls"
)

// Source is a controllable stand-in. Watch replays Changes and then idles until the
// context is cancelled.
type Source struct {
	mu      sync.Mutex
	Changes []newpolls.Change
}

func New() *Source {
	return &Source{}
}

// AddPoll queues an added-poll change for the next Watch.
func (s *Source) AddPoll(pollID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Changes = append(s.Changes, newpolls.Change{
		Kind: newpolls.ChangeAdded,
		Doc:  newpolls.Document{ID: pollID, Data: map[string]any{}},
	})
}

func (s *Source) Watch(ctx context.Context) (newpolls.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := make([]newpolls.Change, len(s.Changes))
	copy(pending, s.Changes)
	return &stream{ctx: ctx, pending: pending}, nil
}

type stream struct {
	ctx     context.Context
	pending []newpolls.Change
	done    bool
}

func (s *stream) Next() ([]newpolls.Change, error) {
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
