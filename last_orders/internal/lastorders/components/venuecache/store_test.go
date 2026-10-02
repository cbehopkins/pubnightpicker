package venuecache

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"last_orders/internal/lastorders/basestore"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
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
	store, err := New(base)
	if err != nil {
		t.Fatalf("new venue cache store: %v", err)
	}
	return store
}

func TestStorePutGetReplaceAndDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	projection := VenueProjection{ID: "venue-1", Name: "The Crown", Website: "https://example.test"}
	if err := store.Put(ctx, projection); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.Get(ctx, projection.ID)
	if err != nil || got != projection {
		t.Fatalf("get = %#v, %v; want %#v", got, err, projection)
	}

	projection.Name = "The New Crown"
	if err := store.Put(ctx, projection); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err = store.Get(ctx, projection.ID)
	if err != nil || got != projection {
		t.Fatalf("get after replace = %#v, %v; want %#v", got, err, projection)
	}
	if err := store.Delete(ctx, projection.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, projection.ID); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("get after delete error = %v; want cache miss", err)
	}
}

func TestStoreCreatesIndependentTable(t *testing.T) {
	store := newTestStore(t)
	var name string
	if err := store.base.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'venue_cache'`).Scan(&name); err != nil {
		t.Fatalf("find venue cache table: %v", err)
	}
	if name != "venue_cache" {
		t.Fatalf("table name = %q", name)
	}
}
