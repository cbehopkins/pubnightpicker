package venuecache

import (
	"context"
	"errors"
	"testing"
)

type fakeSource struct {
	doc      Document
	err      error
	getCalls int
	watch    ChangeStream
}

func (s *fakeSource) Get(context.Context, string) (Document, error) {
	s.getCalls++
	return s.doc, s.err
}

func (s *fakeSource) Watch(context.Context) (ChangeStream, error) {
	return s.watch, nil
}

func TestServiceUsesAuthoritativeSourceOnCacheMiss(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{doc: Document{ID: "venue-1", Data: map[string]any{"name": "The Crown"}}}
	service, err := NewService(store, source, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	got, err := service.Get(context.Background(), "venue-1")
	if err != nil || got.Name != "The Crown" {
		t.Fatalf("get = %#v, %v", got, err)
	}
	if source.getCalls != 1 {
		t.Fatalf("source calls = %d; want 1", source.getCalls)
	}
	if _, err := store.Get(context.Background(), "venue-1"); err != nil {
		t.Fatalf("expected read-through population: %v", err)
	}
	if _, err := service.Get(context.Background(), "venue-1"); err != nil || source.getCalls != 1 {
		t.Fatalf("cache hit = %v, source calls = %d", err, source.getCalls)
	}
}

func TestServiceReturnsAuthoritativeNotFound(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{err: ErrNotFound}
	service, err := NewService(store, source, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v; want not found", err)
	}
}
