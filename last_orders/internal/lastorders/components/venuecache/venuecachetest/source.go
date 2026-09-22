// Package venuecachetest provides an in-memory stand-in for venuecache.Source. It
// exists only to support tests; production code must bind to a Firestore source.
package venuecachetest

import (
	"context"
	"sync"

	"last_orders/internal/lastorders/components/venuecache"
)

// Source is a controllable stand-in. Watch replays Changes and then idles until the
// context is cancelled, mirroring a live Firestore snapshot stream.
type Source struct {
	mu        sync.Mutex
	Changes   []venuecache.Change
	Documents map[string]venuecache.Document
	EventList []venuecache.Document
}

func New() *Source {
	return &Source{Documents: map[string]venuecache.Document{}}
}

func (s *Source) Get(_ context.Context, venueID string) (venuecache.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.Documents[venueID]
	if !ok {
		return venuecache.Document{}, venuecache.ErrNotFound
	}
	return doc, nil
}

func (s *Source) ListEventVenues(context.Context) ([]venuecache.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.EventList, nil
}

func (s *Source) Watch(ctx context.Context) (venuecache.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := make([]venuecache.Change, len(s.Changes))
	copy(pending, s.Changes)
	return &stream{ctx: ctx, pending: pending}, nil
}

type stream struct {
	ctx     context.Context
	pending []venuecache.Change
	done    bool
}

func (s *stream) Next() ([]venuecache.Change, error) {
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
