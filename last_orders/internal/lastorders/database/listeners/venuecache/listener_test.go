package venuecache

import (
	"context"
	"database/sql"
	"testing"

	"last_orders/internal/lastorders/basestore"
	component "last_orders/internal/lastorders/components/venuecache"

	_ "modernc.org/sqlite"
)

type fakeSource struct{}

func (fakeSource) Get(context.Context, string) (component.Document, error) {
	return component.Document{}, component.ErrNotFound
}

func (fakeSource) ListEventVenues(context.Context) ([]component.Document, error) {
	return nil, nil
}

func (fakeSource) Watch(context.Context) (component.ChangeStream, error) {
	return nil, nil
}

func TestApplyAddedModifiedAndRemovedChanges(t *testing.T) {
	store := newTestStore(t)
	service, err := component.NewService(store, &fakeSource{}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	listener, err := New(service, store, nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	change := component.Change{Kind: component.ChangeAdded, Doc: component.Document{ID: "venue-1", Data: map[string]any{"name": "The Crown"}}}
	if err := listener.apply(context.Background(), change); err != nil {
		t.Fatalf("apply added: %v", err)
	}
	change.Kind = component.ChangeModified
	change.Doc.Data["name"] = "The New Crown"
	if err := listener.apply(context.Background(), change); err != nil {
		t.Fatalf("apply modified: %v", err)
	}
	got, err := store.Get(context.Background(), "venue-1")
	if err != nil || got.Name != "The New Crown" {
		t.Fatalf("cached projection = %#v, %v", got, err)
	}
	change.Kind = component.ChangeRemoved
	if err := listener.apply(context.Background(), change); err != nil {
		t.Fatalf("apply removed: %v", err)
	}
	if _, err := store.Get(context.Background(), "venue-1"); err != component.ErrCacheMiss {
		t.Fatalf("after removal error = %v; want cache miss", err)
	}
}

func newTestStore(t *testing.T) *component.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	store, err := component.New(base)
	if err != nil {
		t.Fatalf("new venue cache store: %v", err)
	}
	return store
}
