package firebaseidempotency

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/truths"

	_ "modernc.org/sqlite"
)

func TestNewCellRequestAssignsUniqueOwners(t *testing.T) {
	envelope, err := truths.NewEnvelope(truths.PollOpenedFanout, truths.PollObservedPayload{PollID: "duplicate-poll"})
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[cellar.CellID]struct{})
	for range 1000 {
		request, err := NewCellRequest("PollOpened", "duplicate-poll", envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := owners[request.ID]; exists {
			t.Fatalf("duplicate observation owner: %s", request.ID)
		}
		owners[request.ID] = struct{}{}
		for _, step := range request.Steps {
			var payload StepPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Owner != string(request.ID) {
				t.Fatalf("step owner = %q, want %q", payload.Owner, request.ID)
			}
		}
	}
}

func TestStorePersistsSequenceOwnerAndLeavesLegacyRowsOwnerless(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE firebase_idempotency_records (
			listener TEXT NOT NULL,
			event_key TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(listener, event_key)
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO firebase_idempotency_records(listener, event_key) VALUES (?, ?)`, "event_venue_observed", "legacy-event"); err != nil {
		t.Fatal(err)
	}
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(base)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertUnlessExistsWork("event_venue_observed", "event-1", "sequence-1")(firebaseStoreTx{tx}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	owner, exists, err := store.LookupOwner(context.Background(), "event_venue_observed", "event-1")
	if err != nil || !exists || owner != "sequence-1" {
		t.Fatalf("LookupOwner() = %q, %t, %v; want sequence-1/present", owner, exists, err)
	}

	failedRetirement := mustBegin(t, db)
	if err := store.RetireEventVenueObservationWork("event-1", "wrong-sequence")(firebaseStoreTx{failedRetirement}); err == nil {
		t.Fatal("retirement accepted a non-matching owner")
	}
	if err := failedRetirement.Rollback(); err != nil {
		t.Fatal(err)
	}
	owner, exists, err = store.LookupOwner(context.Background(), "event_venue_observed", "event-1")
	if err != nil || !exists || owner != "sequence-1" {
		t.Fatalf("failed retirement changed the cache row: %q, %t, %v", owner, exists, err)
	}
	retirement := mustBegin(t, db)
	if err := store.RetireEventVenueObservationWork("event-1", "sequence-1")(firebaseStoreTx{retirement}); err != nil {
		t.Fatal(err)
	}
	if err := retirement.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := store.LookupOwner(context.Background(), "event_venue_observed", "event-1"); err != nil || exists {
		t.Fatalf("retired owner row exists=%t, err=%v; want absent", exists, err)
	}

	owner, exists, err = store.LookupOwner(context.Background(), "event_venue_observed", "legacy-event")
	if err != nil || !exists || owner != "" {
		t.Fatalf("legacy LookupOwner() = %q, %t, %v; want ownerless/present", owner, exists, err)
	}
}

func mustBegin(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

type firebaseStoreTx struct{ tx *sql.Tx }

func (a firebaseStoreTx) Exec(query string, args ...any) error {
	_, err := a.tx.Exec(query, args...)
	return err
}
func (a firebaseStoreTx) ExecContext(ctx context.Context, query string, args ...any) error {
	_, err := a.tx.ExecContext(ctx, query, args...)
	return err
}
func (a firebaseStoreTx) Query(query string, args ...any) (cellar.ApplicationRows, error) {
	return a.tx.Query(query, args...)
}
func (a firebaseStoreTx) QueryContext(ctx context.Context, query string, args ...any) (cellar.ApplicationRows, error) {
	return a.tx.QueryContext(ctx, query, args...)
}
func (a firebaseStoreTx) QueryRow(query string, args ...any) cellar.ApplicationRow {
	return a.tx.QueryRow(query, args...)
}
func (a firebaseStoreTx) QueryRowContext(ctx context.Context, query string, args ...any) cellar.ApplicationRow {
	return a.tx.QueryRowContext(ctx, query, args...)
}
