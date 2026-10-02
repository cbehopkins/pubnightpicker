// Package notificationmirrortest provides an in-memory notification mirror source.
package notificationmirrortest

import (
	"context"
	"maps"
	"reflect"
	"sync"

	"last_orders/internal/lastorders/database/listeners/notificationmirror"
)

type Source struct {
	mu       sync.Mutex
	Requests []notificationmirror.Document
	Acks     map[string]map[string]any
}

func New() *Source {
	return &Source{Acks: map[string]map[string]any{}}
}

func (s *Source) Watch(ctx context.Context) (notificationmirror.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := make([]notificationmirror.Document, len(s.Requests))
	copy(requests, s.Requests)
	return &stream{ctx: ctx, pending: requests}, nil
}

func (s *Source) Mirror(_ context.Context, request notificationmirror.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ack := s.Acks[request.ID]
	if ack == nil {
		ack = map[string]any{}
		s.Acks[request.ID] = ack
	}
	for key, value := range request.Data {
		if existing, ok := ack[key]; !ok || !reflect.DeepEqual(existing, value) {
			ack[key] = value
		}
	}
	return nil
}

func (s *Source) Ack(documentID string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.Acks[documentID])
}

type stream struct {
	ctx     context.Context
	pending []notificationmirror.Document
	done    bool
}

func (s *stream) Next() ([]notificationmirror.Document, error) {
	if !s.done {
		s.done = true
		return s.pending, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *stream) Stop() {}
