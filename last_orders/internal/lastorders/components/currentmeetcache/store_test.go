package currentmeetcache

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/venuecache"

	_ "modernc.org/sqlite"
)

func TestNewStoreDiscardsPreviousProcessViewsWithoutResettingSequence(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	validUntil := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	if err := store.Admit(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", validUntil); err != nil {
		t.Fatal(err)
	}
	_, changed, err := store.Apply(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", []meetcache.Projection{
		{ID: "meet-a", GroupID: "group-a", Date: "2026-11-20", VotingComplete: true},
	}, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if err != nil || !changed {
		t.Fatalf("apply current-Meets baseline changed=%t err=%v", changed, err)
	}
	stateBefore := readCurrentSyncState(t, base)

	restartedStore, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedStore.Snapshot(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", venues); !errors.Is(err, ErrRetired) {
		t.Fatalf("snapshot after restart error=%v, want ErrRetired", err)
	}
	stateAfter := readCurrentSyncState(t, base)
	if stateAfter.Generation != stateBefore.Generation || stateAfter.Sequence != stateBefore.Sequence {
		t.Fatalf("global sync state changed across current-view restart: before=%+v after=%+v", stateBefore, stateAfter)
	}
}

func TestSnapshotDoesNotTreatUnobservedVenueAsConfirmedMissing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Admit(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Apply(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", []meetcache.Projection{{
		ID: "meet-a", GroupID: "group-a", Date: "2026-11-20", VotingComplete: true, SelectedVenueID: "venue-a",
	}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", venues)
	if !errors.Is(err, venuecache.ErrCacheMiss) || snapshot.Meets != nil {
		t.Fatalf("snapshot=%+v err=%v, want an unconfirmed Venue cache miss", snapshot, err)
	}
}

func TestSnapshotTreatsObservedVenueTombstoneAsConfirmedMissing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(base)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := venuecache.New(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Admit(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Apply(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", []meetcache.Projection{{
		ID: "meet-a", GroupID: "group-a", Date: "2026-11-20", VotingComplete: true, SelectedVenueID: "venue-a",
	}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := venues.Put(ctx, venuecache.VenueProjection{ID: "venue-a", Name: "Former Pub"}); err != nil {
		t.Fatal(err)
	}
	if err := venues.Delete(ctx, "venue-a"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx, CurrentView, "group-a", "2026-11-20", "owner-a", venues)
	if err != nil || len(snapshot.Meets) != 1 || snapshot.Meets[0].MissingVenueID != "venue-a" || snapshot.Meets[0].Venue != nil {
		t.Fatalf("snapshot=%+v err=%v, want the tombstoned Venue represented as missing", snapshot, err)
	}
}

func readCurrentSyncState(t *testing.T, base *basestore.Store) basestore.SyncState {
	t.Helper()
	tx, err := base.DB().BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
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
