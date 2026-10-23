package venuecache

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
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

func TestStoreApplyBatchReplacesBaselineAtomically(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, VenueProjection{ID: "old", Name: "Old venue"}); err != nil {
		t.Fatalf("seed old venue: %v", err)
	}

	baseline := []Mutation{{ID: "new", Projection: &VenueProjection{ID: "new", Name: "New venue"}}}
	if err := store.ApplyBatch(ctx, baseline, true); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	if _, err := store.Get(ctx, "old"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("old venue error = %v; want cache miss", err)
	}
	if got, err := store.Get(ctx, "new"); err != nil || got.Name != "New venue" {
		t.Fatalf("new venue = %+v, %v; want new projection", got, err)
	}

	if err := store.ApplyBatch(ctx, []Mutation{{ID: ""}}, true); err == nil {
		t.Fatal("invalid baseline mutation succeeded")
	}
	if got, err := store.Get(ctx, "new"); err != nil || got.Name != "New venue" {
		t.Fatalf("venue after failed baseline = %+v, %v; want prior projection intact", got, err)
	}
}

func TestStoreRoundTripsOptionalValuesAndHidesTombstones(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	projection, err := ProjectionFromDocument(Document{ID: "venue-1", Data: map[string]any{
		"name": "The Crown", "notes": "", "parking": false, "food": true,
		"dog_friend": true, "beer_gerden": false, "out_of_town": true, "banned": false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, projection); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(ctx, projection.ID); err != nil || !got.Equal(projection) {
		t.Fatalf("round trip = %+v, %v; want %+v", got, err, projection)
	}
	projection = VenueProjection{ID: "venue-1", Name: "The Crown"}
	if err := store.Put(ctx, projection); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(ctx, projection.ID); err != nil || !got.Equal(projection) {
		t.Fatalf("cleared optional fields = %+v, %v; want %+v", got, err, projection)
	}
	if _, err := store.base.DB().Exec(`UPDATE venue_cache SET deleted = 1, change_version = 1 WHERE venue_id = ?`, projection.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, projection.ID); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("tombstone get error = %v; want cache miss", err)
	}
	var count int
	if err := store.base.DB().QueryRow(`SELECT COUNT(*) FROM venue_cache WHERE deleted = 1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retained tombstones = %d, %v; want 1", count, err)
	}
}

func venueVersion(t *testing.T, store *Store, venueID string) (int64, bool) {
	t.Helper()
	var version int64
	var deleted bool
	if err := store.base.DB().QueryRow(`SELECT change_version, deleted FROM venue_cache WHERE venue_id = ?`, venueID).Scan(&version, &deleted); err != nil {
		t.Fatal(err)
	}
	return version, deleted
}

func TestStoreVersionsChangesAndRetainsDeletion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	projection, err := ProjectionFromDocument(Document{ID: "venue-1", Data: map[string]any{
		"name": "The Crown", "parking": false, "notes": "",
		"recurrence": map[string]any{"frequency": "weekly", "interval": int64(1), "weekdays": []int{1, 4}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, projection); err != nil {
		t.Fatal(err)
	}
	if version, deleted := venueVersion(t, store, projection.ID); version != 1 || deleted {
		t.Fatalf("insert version = %d, deleted=%v", version, deleted)
	}
	replay, err := ProjectionFromDocument(Document{ID: "venue-1", Data: map[string]any{
		"name": "The Crown", "parking": false, "notes": "",
		"recurrence": map[string]any{"frequency": "weekly", "interval": float64(1), "weekdays": []int{4, 1, 4}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	replay.RecurrenceJSON = ` { "weekdays": [4,1,4], "interval": 1, "frequency": "weekly" } `
	if err := store.Put(ctx, replay); err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, store.base).Sequence; got != 1 {
		t.Fatalf("identical put advanced sequence to %d", got)
	}
	projection.Parking = nil
	if err := store.Put(ctx, projection); err != nil {
		t.Fatal(err)
	}
	if version, deleted := venueVersion(t, store, projection.ID); version != 2 || deleted {
		t.Fatalf("optional field change version = %d, deleted=%v", version, deleted)
	}
	if err := store.Delete(ctx, projection.ID); err != nil {
		t.Fatal(err)
	}
	if version, deleted := venueVersion(t, store, projection.ID); version != 3 || !deleted {
		t.Fatalf("delete version = %d, deleted=%v", version, deleted)
	}
	if _, err := store.Get(ctx, projection.ID); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("deleted resource read = %v", err)
	}
	for _, venueID := range []string{projection.ID, "never-existed"} {
		if err := store.Delete(ctx, venueID); err != nil {
			t.Fatal(err)
		}
	}
	if got := venueSyncState(t, store.base).Sequence; got != 3 {
		t.Fatalf("absent deletes advanced sequence to %d", got)
	}
	if err := store.Put(ctx, projection); err != nil {
		t.Fatal(err)
	}
	if version, deleted := venueVersion(t, store, projection.ID); version != 4 || deleted {
		t.Fatalf("recreation version = %d, deleted=%v", version, deleted)
	}
}

func TestStoreBatchVersionsAndRollback(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	first := VenueProjection{ID: "first", Name: "First venue"}
	second := VenueProjection{ID: "second", Name: "Second venue"}
	if err := store.ApplyBatch(ctx, []Mutation{{ID: first.ID, Projection: &first}, {ID: second.ID, Projection: &second}}, false); err != nil {
		t.Fatal(err)
	}
	if version, _ := venueVersion(t, store, first.ID); version != 1 {
		t.Fatalf("first version = %d", version)
	}
	if version, _ := venueVersion(t, store, second.ID); version != 2 {
		t.Fatalf("second version = %d", version)
	}
	if _, err := store.base.DB().Exec(`
		CREATE TRIGGER reject_venue_insert BEFORE INSERT ON venue_cache
		WHEN NEW.venue_id = 'rejected' BEGIN SELECT RAISE(ABORT, 'injected persistence failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	changed := second
	changed.Name = "Changed venue"
	rejected := VenueProjection{ID: "rejected", Name: "Rejected venue"}
	before := venueSyncState(t, store.base)
	if err := store.ApplyBatch(ctx, []Mutation{
		{ID: first.ID}, {ID: changed.ID, Projection: &changed}, {ID: rejected.ID, Projection: &rejected},
	}, false); err == nil {
		t.Fatal("batch succeeded despite injected persistence failure")
	}
	if got := venueSyncState(t, store.base); got != before {
		t.Fatalf("failed batch changed metadata: %+v; want %+v", got, before)
	}
	if version, deleted := venueVersion(t, store, first.ID); version != 1 || deleted {
		t.Fatalf("failed batch changed first row: version=%d, deleted=%v", version, deleted)
	}
	if got, err := store.Get(ctx, second.ID); err != nil || !got.Equal(second) {
		t.Fatalf("failed batch changed second row: %+v, %v", got, err)
	}
	if version, deleted := venueVersion(t, store, second.ID); version != 2 || deleted {
		t.Fatalf("failed batch changed second version: %d, deleted=%v", version, deleted)
	}
	if _, err := store.Get(ctx, rejected.ID); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("failed batch inserted rejected row: %v", err)
	}
	if _, err := store.base.DB().Exec(`DROP TRIGGER reject_venue_insert`); err != nil {
		t.Fatal(err)
	}
	mutations := []Mutation{{ID: first.ID}, {ID: changed.ID, Projection: &changed}}
	if err := store.ApplyBatch(ctx, mutations, false); err != nil {
		t.Fatal(err)
	}
	if version, deleted := venueVersion(t, store, first.ID); version != 3 || !deleted {
		t.Fatalf("committed deletion: version=%d, deleted=%v", version, deleted)
	}
	if version, deleted := venueVersion(t, store, second.ID); version != 4 || deleted {
		t.Fatalf("committed change: version=%d, deleted=%v", version, deleted)
	}
	if err := store.ApplyBatch(ctx, mutations, false); err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, store.base).Sequence; got != 4 {
		t.Fatalf("replayed delta advanced sequence to %d", got)
	}
}

func TestStoreBaselineRetainsVersionsAndTombstones(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	kept := VenueProjection{ID: "kept", Name: "Kept venue"}
	removed := VenueProjection{ID: "removed", Name: "Removed venue"}
	if err := store.ApplyBatch(ctx, []Mutation{{ID: kept.ID, Projection: &kept}, {ID: removed.ID, Projection: &removed}}, false); err != nil {
		t.Fatal(err)
	}
	baseline := []Mutation{{ID: kept.ID, Projection: &kept}}
	if err := store.ApplyBatch(ctx, baseline, true); err != nil {
		t.Fatal(err)
	}
	if version, deleted := venueVersion(t, store, kept.ID); version != 1 || deleted {
		t.Fatalf("baseline changed identical row: version=%d, deleted=%v", version, deleted)
	}
	if version, deleted := venueVersion(t, store, removed.ID); version != 3 || !deleted {
		t.Fatalf("baseline removal: version=%d, deleted=%v", version, deleted)
	}
	if err := store.ApplyBatch(ctx, baseline, true); err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, store.base).Sequence; got != 3 {
		t.Fatalf("identical baseline advanced sequence to %d", got)
	}
	if version, deleted := venueVersion(t, store, removed.ID); version != 3 || !deleted {
		t.Fatalf("baseline lost tombstone: version=%d, deleted=%v", version, deleted)
	}
}

func TestStoreRejectsInvalidWritesWithoutAllocating(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, projection := range []VenueProjection{
		{ID: "", Name: "Missing ID"}, {ID: "missing-name"},
		{ID: "bad-recurrence", Name: "Bad recurrence", RecurrenceJSON: `{`},
	} {
		if err := store.Put(ctx, projection); err == nil {
			t.Fatalf("invalid put succeeded: %+v", projection)
		}
	}
	if err := store.Delete(ctx, ""); err == nil {
		t.Fatal("empty delete ID accepted")
	}
	if got := venueSyncState(t, store.base).Sequence; got != 0 {
		t.Fatalf("invalid writes advanced sequence to %d", got)
	}
}

func TestStoreConcurrentReplayAllocatesOnce(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "venues.db")+
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	projection := VenueProjection{ID: "shared", Name: "Shared venue"}
	const workers = 8
	for _, remove := range []bool{false, true} {
		start := make(chan struct{})
		failures := make(chan error, workers)
		var group sync.WaitGroup
		for range workers {
			group.Go(func() {
				<-start
				var err error
				if remove {
					err = store.Delete(ctx, projection.ID)
				} else {
					err = store.Put(ctx, projection)
				}
				if err != nil {
					failures <- err
				}
			})
		}
		close(start)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		want := int64(1)
		if remove {
			want = 2
		}
		if version, deleted := venueVersion(t, store, projection.ID); version != want || deleted != remove {
			t.Fatalf("concurrent replay: version=%d, deleted=%v; want %d, %v", version, deleted, want, remove)
		}
		if got := venueSyncState(t, base).Sequence; got != want {
			t.Fatalf("concurrent replay sequence = %d; want %d", got, want)
		}
	}
}

func TestStoreSchemaRejectsUnversionedRows(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.base.DB().Exec(`INSERT INTO venue_cache(venue_id, name) VALUES('invalid', 'Unversioned venue')`); err == nil {
		t.Fatal("schema accepted a row without a version")
	}
	if _, err := store.base.DB().Exec(`INSERT INTO venue_cache(venue_id, name, change_version) VALUES('invalid', 'Unversioned venue', 0)`); err == nil {
		t.Fatal("schema accepted a zero version")
	}
}
