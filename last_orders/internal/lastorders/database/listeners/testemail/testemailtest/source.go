// Package testemailtest provides an in-memory testemail.Source for tests.
package testemailtest

import (
	"context"
	"slices"
	"sync"

	"last_orders/internal/lastorders/database/listeners/testemail"
)

// Source replays Documents once per Watch, then idles until cancelled, and
// records acknowledgements.
type Source struct {
	mu        sync.Mutex
	Documents []testemail.Document
	acks      map[string]string
}

func New() *Source {
	return &Source{acks: map[string]string{}}
}

func (s *Source) Watch(ctx context.Context) (testemail.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &stream{ctx: ctx, pending: slices.Clone(s.Documents)}, nil
}

func (s *Source) AckTestEmail(_ context.Context, userID, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acks == nil {
		s.acks = map[string]string{}
	}
	s.acks[userID] = requestID
	return nil
}

// Ack returns the acknowledged request ID for a user.
func (s *Source) Ack(userID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requestID, ok := s.acks[userID]
	return requestID, ok
}

type stream struct {
	ctx     context.Context
	pending []testemail.Document
	done    bool
}

func (s *stream) Next() ([]testemail.Document, error) {
	if !s.done {
		s.done = true
		return s.pending, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *stream) Stop() {}
