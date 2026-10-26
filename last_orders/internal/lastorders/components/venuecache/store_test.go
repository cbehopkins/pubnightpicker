package venuecache

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"

	"github.com/google/uuid"
	"modernc.org/sqlite"
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
	projection := VenueProjection{
		ID: "venue-1", Name: "The Crown", Website: "https://example.test",
		VisibleGroupIDs: []string{"group-1"}, PreviousVenueIDs: []string{"venue-old"},
	}
	if err := store.Put(ctx, projection); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.Get(ctx, projection.ID)
	if err != nil || !got.Equal(projection) {
		t.Fatalf("get = %#v, %v; want %#v", got, err, projection)
	}

	projection.Name = "The New Crown"
	if err := store.Put(ctx, projection); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err = store.Get(ctx, projection.ID)
	if err != nil || !got.Equal(projection) {
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

// A complete baseline changes only differing venues, retains deletion markers,
// and allocates no new versions when replayed, including an empty baseline.
func TestStoreBaselineAppliesOnlyMixedDifferences(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	kept := VenueProjection{ID: "kept", Name: "Kept venue"}
	changed := VenueProjection{ID: "changed", Name: "Old name"}
	removed := VenueProjection{ID: "removed", Name: "Removed venue"}
	recreated := VenueProjection{ID: "recreated", Name: "Recreated venue"}
	for _, projection := range []VenueProjection{kept, changed, removed, recreated} {
		if err := store.Put(ctx, projection); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Delete(ctx, recreated.ID); err != nil {
		t.Fatal(err)
	}
	changed.Name = "New name"
	added := VenueProjection{ID: "added", Name: "Added venue"}
	baseline := []Mutation{
		{ID: kept.ID, Projection: &kept}, {ID: changed.ID, Projection: &changed},
		{ID: recreated.ID, Projection: &recreated}, {ID: added.ID, Projection: &added},
	}
	if err := store.ApplyBatch(ctx, baseline, true); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		projection VenueProjection
		version    int64
		deleted    bool
	}{
		{kept, 1, false}, {removed, 6, true}, {changed, 7, false},
		{recreated, 8, false}, {added, 9, false},
	} {
		if version, deleted := venueVersion(t, store, test.projection.ID); version != test.version || deleted != test.deleted {
			t.Fatalf("%s version=%d, deleted=%v; want %d, %v", test.projection.ID, version, deleted, test.version, test.deleted)
		}
		got, err := store.Get(ctx, test.projection.ID)
		if test.deleted {
			if !errors.Is(err, ErrCacheMiss) {
				t.Fatalf("removed venue read = %v", err)
			}
		} else if err != nil || !got.Equal(test.projection) {
			t.Fatalf("baseline venue = %+v, %v; want %+v", got, err, test.projection)
		}
	}
	before := venueSyncState(t, store.base)
	if before.Sequence != 9 {
		t.Fatalf("mixed baseline sequence = %d; want 9", before.Sequence)
	}
	if err := store.ApplyBatch(ctx, baseline, true); err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, store.base); got != before {
		t.Fatalf("identical baseline changed metadata: %+v; want %+v", got, before)
	}
	if err := store.ApplyBatch(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, store.base).Sequence; got != 13 {
		t.Fatalf("empty baseline sequence = %d; want 13", got)
	}
	for _, projection := range []VenueProjection{kept, changed, recreated, added} {
		if _, err := store.Get(ctx, projection.ID); !errors.Is(err, ErrCacheMiss) {
			t.Fatalf("empty baseline retained %s: %v", projection.ID, err)
		}
	}
	if version, deleted := venueVersion(t, store, removed.ID); version != 6 || !deleted {
		t.Fatalf("empty baseline changed old tombstone: version=%d, deleted=%v", version, deleted)
	}
	if err := store.ApplyBatch(ctx, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := venueSyncState(t, store.base).Sequence; got != 13 {
		t.Fatalf("replayed empty baseline advanced sequence to %d", got)
	}
}

// Invalid input is rejected before writing; a database failure rolls back earlier
// writes and allocations. Either failure leaves rows, versions and tombstones intact.
func TestStoreFailedBaselinePreservesProjectionAndMetadata(t *testing.T) {
	for _, failure := range []string{"persistence", "invalid ID", "mismatched ID", "invalid recurrence", "duplicate ID"} {
		t.Run(failure, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			kept := VenueProjection{ID: "kept", Name: "Original venue"}
			removed := VenueProjection{ID: "removed", Name: "Still present"}
			oldTombstone := VenueProjection{ID: "old-tombstone", Name: "Already deleted"}
			for _, projection := range []VenueProjection{kept, removed, oldTombstone} {
				if err := store.Put(ctx, projection); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Delete(ctx, oldTombstone.ID); err != nil {
				t.Fatal(err)
			}
			changed := kept
			changed.Name = "Changed venue"
			rejected := VenueProjection{ID: "rejected", Name: "Rejected venue"}
			baseline := []Mutation{{ID: changed.ID, Projection: &changed}, {ID: rejected.ID, Projection: &rejected}}
			switch failure {
			case "persistence":
				if _, err := store.base.DB().Exec(`
					CREATE TRIGGER reject_baseline BEFORE INSERT ON venue_cache
					WHEN NEW.venue_id = 'rejected' BEGIN SELECT RAISE(ABORT, 'injected baseline failure'); END;
				`); err != nil {
					t.Fatal(err)
				}
			case "invalid ID":
				baseline[1].ID = ""
			case "mismatched ID":
				baseline[1].ID = "different"
			case "invalid recurrence":
				rejected.RecurrenceJSON = `{`
			case "duplicate ID":
				baseline = append(baseline, baseline[0])
			}
			before := venueSyncState(t, store.base)
			if err := store.ApplyBatch(ctx, baseline, true); err == nil {
				t.Fatal("invalid baseline succeeded")
			}
			if got := venueSyncState(t, store.base); got != before {
				t.Fatalf("failed baseline metadata = %+v; want %+v", got, before)
			}
			for index, projection := range []VenueProjection{kept, removed} {
				if got, err := store.Get(ctx, projection.ID); err != nil || !got.Equal(projection) {
					t.Fatalf("failed baseline changed %s: %+v, %v", projection.ID, got, err)
				}
				if version, deleted := venueVersion(t, store, projection.ID); version != int64(index+1) || deleted {
					t.Fatalf("failed baseline changed %s metadata: version=%d, deleted=%v", projection.ID, version, deleted)
				}
			}
			if version, deleted := venueVersion(t, store, oldTombstone.ID); version != 4 || !deleted {
				t.Fatalf("failed baseline changed tombstone: version=%d, deleted=%v", version, deleted)
			}
			if _, err := store.Get(ctx, rejected.ID); !errors.Is(err, ErrCacheMiss) {
				t.Fatalf("failed baseline inserted rejected row: %v", err)
			}
		})
	}
}

type venueSnapshotRow struct {
	ID      string
	Name    string
	Version int64
	Deleted bool
}

func readVenueSnapshot(t *testing.T, tx *sql.Tx) []venueSnapshotRow {
	t.Helper()
	rows, err := tx.Query(`SELECT venue_id, name, change_version, deleted FROM venue_cache ORDER BY venue_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var snapshot []venueSnapshotRow
	for rows.Next() {
		var row venueSnapshotRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Version, &row.Deleted); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestStoreBaselineVisibilityAndWriterOrdering(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%v", fail), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			paused := make(chan struct{})
			release := make(chan struct{})
			releaseWriter := sync.OnceFunc(func() { close(release) })
			defer releaseWriter()
			function := "pause_baseline_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if err := sqlite.RegisterScalarFunction(function, 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
				close(paused)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				if fail {
					return nil, errors.New("injected baseline rollback")
				}
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "baseline.db")
			db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
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
			removed := VenueProjection{ID: "removed", Name: "Removed venue"}
			kept := VenueProjection{ID: "kept", Name: "Old name"}
			for _, projection := range []VenueProjection{removed, kept} {
				if err := store.Put(ctx, projection); err != nil {
					t.Fatal(err)
				}
			}
			before := venueSyncState(t, base)
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`
				CREATE TRIGGER pause_baseline AFTER INSERT ON venue_cache
				WHEN NEW.venue_id = 'added' BEGIN SELECT %s(); END;
			`, function)); err != nil {
				t.Fatal(err)
			}
			reader, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			baselineDone := make(chan error, 1)
			kept.Name = "New name"
			added := VenueProjection{ID: "added", Name: "Added venue"}
			go func() {
				baselineDone <- store.ApplyBatch(ctx, []Mutation{{ID: kept.ID, Projection: &kept}, {ID: added.ID, Projection: &added}}, true)
			}()
			select {
			case <-paused:
			case err := <-baselineDone:
				t.Fatalf("baseline completed before pause: %v", err)
			case <-ctx.Done():
				t.Fatal("baseline did not reach the transaction pause")
			}
			snapshot, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Rollback()
			oldRows := []venueSnapshotRow{{"kept", "Old name", 2, false}, {"removed", "Removed venue", 1, false}}
			if rows := readVenueSnapshot(t, snapshot); !slices.Equal(rows, oldRows) {
				t.Fatalf("reader saw uncommitted baseline: %+v", rows)
			}
			if state, err := base.ReadSyncState(ctx, snapshot); err != nil || state != before {
				t.Fatalf("reader metadata during baseline = %+v, %v; want %+v", state, err, before)
			}
			contenderStarted := make(chan struct{})
			contenderDone := make(chan error, 1)
			go func() {
				close(contenderStarted)
				contenderDone <- store.Put(ctx, VenueProjection{ID: "later", Name: "Later venue"})
			}()
			<-contenderStarted
			select {
			case err := <-contenderDone:
				t.Fatalf("writer completed inside another write transaction: %v", err)
			default:
			}
			releaseWriter()
			select {
			case err := <-baselineDone:
				if (err != nil) != fail {
					t.Fatalf("baseline error = %v; rollback=%v", err, fail)
				}
			case <-ctx.Done():
				t.Fatal("baseline did not finish")
			}
			select {
			case err := <-contenderDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("competing writer did not finish")
			}
			if rows := readVenueSnapshot(t, snapshot); !slices.Equal(rows, oldRows) {
				t.Fatalf("existing read snapshot changed after commit: %+v", rows)
			}
			if state, err := base.ReadSyncState(ctx, snapshot); err != nil || state != before {
				t.Fatalf("existing metadata snapshot changed: %+v, %v", state, err)
			}
			fresh, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Rollback()
			wantRows := []venueSnapshotRow{
				{"added", "Added venue", 5, false}, {"kept", "New name", 4, false},
				{"later", "Later venue", 6, false}, {"removed", "Removed venue", 3, true},
			}
			wantSequence := int64(6)
			if fail {
				wantRows = []venueSnapshotRow{{"kept", "Old name", 2, false}, {"later", "Later venue", 3, false}, {"removed", "Removed venue", 1, false}}
				wantSequence = 3
			}
			if rows := readVenueSnapshot(t, fresh); !slices.Equal(rows, wantRows) {
				t.Fatalf("new read snapshot = %+v; want %+v", rows, wantRows)
			}
			state, err := base.ReadSyncState(ctx, fresh)
			if err != nil || state.Generation != before.Generation || state.Sequence != wantSequence {
				t.Fatalf("new metadata snapshot = %+v, %v; want sequence %d", state, err, wantSequence)
			}
		})
	}
}
