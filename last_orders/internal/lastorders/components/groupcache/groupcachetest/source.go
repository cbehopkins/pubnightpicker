// Package groupcachetest provides an in-memory stand-in for groupcache.Source.
package groupcachetest

import (
	"context"

	"last_orders/internal/lastorders/components/groupcache"
)

type Source struct {
	Documents []groupcache.Document
}

func New() *Source {
	return NewWithDocuments(groupcache.Document{
		ID:   "test-default-group",
		Data: map[string]any{"name": "ampubnight", "visibility": "public"},
	})
}

func NewWithDocuments(documents ...groupcache.Document) *Source {
	return &Source{Documents: append([]groupcache.Document(nil), documents...)}
}

func (s *Source) Watch(ctx context.Context) (groupcache.ChangeStream, error) {
	changes := make([]groupcache.Change, 0, len(s.Documents))
	for _, document := range s.Documents {
		changes = append(changes, groupcache.Change{Kind: groupcache.ChangeAdded, Doc: document})
	}
	return &stream{ctx: ctx, changes: changes}, nil
}

type stream struct {
	ctx     context.Context
	changes []groupcache.Change
	done    bool
}

func (s *stream) Next() ([]groupcache.Change, error) {
	if !s.done {
		s.done = true
		return s.changes, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (*stream) Stop() {}
