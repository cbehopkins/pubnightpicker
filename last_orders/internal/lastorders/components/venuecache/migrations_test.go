package venuecache

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"last_orders/internal/lastorders/basestore"
)

func venueSyncState(t *testing.T, base *basestore.Store) basestore.SyncState {
	t.Helper()
	tx, err := base.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state, err := base.ReadSyncState(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestStoreMigratesLegacyCacheOnceAndPreservesOtherData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE venue_cache(venue_id TEXT PRIMARY KEY, name TEXT NOT NULL);
		INSERT INTO venue_cache VALUES('legacy', 'Old venue');
		CREATE TABLE unrelated(value TEXT NOT NULL);
		INSERT INTO unrelated VALUES('durable work');
	`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := base.NextSequence(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	before := venueSyncState(t, base)
	store, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	after := venueSyncState(t, base)
	if after.Generation == before.Generation || after.Sequence != before.Sequence {
		t.Fatalf("migration metadata = %+v; previous = %+v", after, before)
	}
	if _, err := store.Get(context.Background(), "legacy"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("legacy row error = %v; want cache miss", err)
	}
	if err := store.Put(context.Background(), VenueProjection{ID: "new", Name: "New venue"}); err != nil {
		t.Fatal(err)
	}
	after = venueSyncState(t, base)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	base, err = basestore.New(reopened)
	if err != nil {
		t.Fatal(err)
	}
	store, err = New(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, base); got != after {
		t.Fatalf("reopened metadata = %+v; want %+v", got, after)
	}
	if got, err := store.Get(context.Background(), "new"); err != nil || got.Name != "New venue" {
		t.Fatalf("reopened row = %+v, %v", got, err)
	}
	var value string
	if err := reopened.QueryRow(`SELECT value FROM unrelated`).Scan(&value); err != nil || value != "durable work" {
		t.Fatalf("unrelated data = %q, %v", value, err)
	}
}

func TestStoreMigrationRollsBackCacheAndGeneration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE venue_cache(venue_id TEXT PRIMARY KEY, name TEXT NOT NULL);
		INSERT INTO venue_cache VALUES('legacy', 'Old venue');
		CREATE TABLE venue_cache_schema(singleton INTEGER PRIMARY KEY, version INTEGER NOT NULL);
		CREATE TRIGGER reject_schema_version BEFORE UPDATE OF version ON venue_cache_schema
		BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	before := venueSyncState(t, base)
	if _, err := New(base); err == nil {
		t.Fatal("migration succeeded despite injected failure")
	}
	if got := venueSyncState(t, base); got != before {
		t.Fatalf("metadata after failure = %+v; want %+v", got, before)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM venue_cache WHERE venue_id = 'legacy'`).Scan(&name); err != nil || name != "Old venue" {
		t.Fatalf("legacy row after failure = %q, %v", name, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM venue_cache_schema`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("committed migration markers = %d, %v; want 0", count, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_schema_version`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(base); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
}

func TestStoreFreshSchemaPreservesGeneration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	before := venueSyncState(t, base)
	for range 2 {
		if _, err := New(base); err != nil {
			t.Fatal(err)
		}
		if got := venueSyncState(t, base); got != before {
			t.Fatalf("initialisation changed metadata: %+v; want %+v", got, before)
		}
	}
}

func TestStoreRejectsUnknownSchemaWithoutChangingData(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, VenueProjection{ID: "current", Name: "Keep this venue"}); err != nil {
		t.Fatal(err)
	}
	before := venueSyncState(t, store.base)
	if _, err := store.base.DB().Exec(`UPDATE venue_cache_schema SET version = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(store.base); err == nil {
		t.Fatal("unknown schema version was accepted")
	}
	if got := venueSyncState(t, store.base); got != before {
		t.Fatalf("failed startup changed metadata: %+v; want %+v", got, before)
	}
	if got, err := store.Get(ctx, "current"); err != nil || got.Name != "Keep this venue" {
		t.Fatalf("row after rejected migration = %+v, %v", got, err)
	}
}
