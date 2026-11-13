package firebaseidempotency

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
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

	listener := "test-listener"
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertUnlessExistsWork(listener, "event-1", "sequence-1")(firebaseStoreTx{tx}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	owner, exists, err := store.LookupOwner(context.Background(), listener, "event-1")
	if err != nil || !exists || owner != "sequence-1" {
		t.Fatalf("LookupOwner() = %q, %t, %v; want sequence-1/present", owner, exists, err)
	}

	failedRetirement := mustBegin(t, db)
	if err := store.RetireCacheEntryWork(listener, "event-1", "wrong-sequence")(firebaseStoreTx{failedRetirement}); err == nil {
		t.Fatal("retirement accepted a non-matching owner")
	}
	if err := failedRetirement.Rollback(); err != nil {
		t.Fatal(err)
	}
	owner, exists, err = store.LookupOwner(context.Background(), listener, "event-1")
	if err != nil || !exists || owner != "sequence-1" {
		t.Fatalf("failed retirement changed the cache row: %q, %t, %v", owner, exists, err)
	}
	wrongListenerRetirement := mustBegin(t, db)
	if err := store.RetireCacheEntryWork("other-listener", "event-1", "sequence-1")(firebaseStoreTx{wrongListenerRetirement}); err == nil {
		t.Fatal("retirement accepted a non-matching listener")
	}
	if err := wrongListenerRetirement.Rollback(); err != nil {
		t.Fatal(err)
	}
	retirement := mustBegin(t, db)
	if err := store.RetireCacheEntryWork(listener, "event-1", "sequence-1")(firebaseStoreTx{retirement}); err != nil {
		t.Fatal(err)
	}
	if err := retirement.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := store.LookupOwner(context.Background(), listener, "event-1"); err != nil || exists {
		t.Fatalf("retired owner row exists=%t, err=%v; want absent", exists, err)
	}

	owner, exists, err = store.LookupOwner(context.Background(), "event_venue_observed", "legacy-event")
	if err != nil || !exists || owner != "" {
		t.Fatalf("legacy LookupOwner() = %q, %t, %v; want ownerless/present", owner, exists, err)
	}
}

func TestPopulateRemoteGateRefusalRelinquishesByConfirmedOwner(t *testing.T) {
	for _, test := range []struct {
		name          string
		remoteOwner   string
		remoteExists  bool
		lookupOwner   string
		lookupExists  bool
		lookupErr     error
		wantError     bool
		wantCleanup   bool
		wantRemoteKey bool
	}{
		{name: "absent claim"},
		{name: "owned claim", remoteOwner: "sequence-owner", remoteExists: true, lookupOwner: "sequence-owner", lookupExists: true, wantCleanup: true},
		{name: "competing owner", remoteOwner: "competitor", remoteExists: true, lookupOwner: "competitor", lookupExists: true, wantRemoteKey: true},
		{name: "ownerless claim", remoteOwner: "seeded", remoteExists: true, lookupExists: true, wantError: true, wantRemoteKey: true},
		{name: "failed lookup", lookupErr: errors.New("lookup unavailable"), wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, store := newHandlerTestStore(t)
			const listener = "test-listener"
			const eventKey = "test-event"
			const owner = "sequence-owner"
			insertCacheOwner(t, db, store, listener, eventKey, owner)

			memoryRemote := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
			if test.remoteExists {
				remoteOwner := test.remoteOwner
				if remoteOwner != "" {
					if _, _, err := memoryRemote.CreateKey(context.Background(), listener, eventKey, remoteOwner); err != nil {
						t.Fatal(err)
					}
				}
			}
			remote := ownerLookupRemote{
				Remote: memoryRemote,
				owner:  test.lookupOwner,
				exists: test.lookupExists,
				err:    test.lookupErr,
			}
			gate := &ClaimGate{}
			gate.Close()
			result := (PopulateRemoteHandler{Remote: remote, Gate: gate, Store: store}).Handle(context.Background(), StepPayload{
				Listener: listener,
				EventKey: eventKey,
				Owner:    owner,
			})

			if test.wantError {
				if _, ok := result.(cellar.ErrorResult); !ok {
					t.Fatalf("gate refusal result = %T, want ErrorResult", result)
				}
				if _, exists, err := store.LookupOwner(context.Background(), listener, eventKey); err != nil || !exists {
					t.Fatalf("unresolved claim was retired: exists=%t, err=%v", exists, err)
				}
				return
			}

			kill, ok := result.(cellar.Kill)
			if !ok {
				t.Fatalf("gate refusal result = %T, want Kill", result)
			}
			if len(kill.NewCells) != 0 && !test.wantCleanup {
				t.Fatalf("gate refusal created %d release cells for a non-owned claim", len(kill.NewCells))
			}
			if test.wantCleanup {
				if len(kill.NewCells) != 1 || kill.NewCells[0].Steps[0].HandlerName != HandlerReleaseRemote {
					t.Fatalf("owned claim cleanup cells = %+v, want one remote release step", kill.NewCells)
				}
				var releasePayload ReleasePayload
				if err := json.Unmarshal(kill.NewCells[0].Steps[0].Payload, &releasePayload); err != nil {
					t.Fatal(err)
				}
				if releasePayload.Owner != owner || releasePayload.Listener != listener || releasePayload.EventKey != eventKey {
					t.Fatalf("release payload = %+v, want exact original identity and owner", releasePayload)
				}
			}
			if len(kill.ApplicationWork) != 1 {
				t.Fatalf("local retirement work = %d, want 1", len(kill.ApplicationWork))
			}
			retirement := mustBegin(t, db)
			if err := kill.ApplicationWork[0](firebaseStoreTx{retirement}); err != nil {
				t.Fatal(err)
			}
			if err := retirement.Commit(); err != nil {
				t.Fatal(err)
			}
			if _, exists, err := store.LookupOwner(context.Background(), listener, eventKey); err != nil || exists {
				t.Fatalf("retired cache row exists=%t, err=%v", exists, err)
			}
			if test.wantCleanup {
				var releasePayload ReleasePayload
				if err := json.Unmarshal(kill.NewCells[0].Steps[0].Payload, &releasePayload); err != nil {
					t.Fatal(err)
				}
				result := (ReleaseRemoteHandler{Remote: memoryRemote}).Handle(context.Background(), releasePayload)
				if _, ok := result.(cellar.Complete); !ok {
					t.Fatalf("remote cleanup result = %#v, want Complete", result)
				}
			}

			remoteOwner, exists, err := memoryRemote.LookupOwner(context.Background(), listener, eventKey)
			if err != nil || exists != test.wantRemoteKey {
				t.Fatalf("remote claim exists=%t, err=%v; want exists=%t", exists, err, test.wantRemoteKey)
			}
			if exists && remoteOwner != test.remoteOwner {
				t.Fatalf("remote owner = %q, want %q", remoteOwner, test.remoteOwner)
			}
		})
	}
}

func TestReleaseRemoteHandlerPreservesReplacementAndSurfacesFailure(t *testing.T) {
	const listener = "test-listener"
	const eventKey = "test-event"
	const owner = "sequence-owner"
	payload := ReleasePayload{Listener: listener, EventKey: eventKey, Owner: owner}
	remote := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
	if _, _, err := remote.CreateKey(context.Background(), listener, eventKey, owner); err != nil {
		t.Fatal(err)
	}
	remote.SeedExisting(listener, eventKey, true)
	releaseResult := (ReleaseRemoteHandler{Remote: remote}).Handle(context.Background(), payload)
	if _, ok := releaseResult.(cellar.ErrorResult); !ok {
		t.Fatalf("replacement-owner release result = %#v, want ErrorResult", releaseResult)
	}
	if got, exists, err := remote.LookupOwner(context.Background(), listener, eventKey); err != nil || !exists || got != "seeded" {
		t.Fatalf("replacement claim = %q, %t, %v; want seeded owner preserved", got, exists, err)
	}

	releaseErr := errors.New("release response unavailable")
	result := (ReleaseRemoteHandler{Remote: releaseErrorRemote{Remote: remote, err: releaseErr}}).Handle(context.Background(), payload)
	failed, ok := result.(cellar.ErrorResult)
	if !ok || !errors.Is(failed.Err, releaseErr) {
		t.Fatalf("inconclusive release result = %#v, want ErrorResult wrapping release error", result)
	}
}

type ownerLookupRemote struct {
	Remote
	owner  string
	exists bool
	err    error
}

func (r ownerLookupRemote) LookupOwner(context.Context, string, string) (string, bool, error) {
	return r.owner, r.exists, r.err
}

type releaseErrorRemote struct {
	Remote
	err error
}

func (r releaseErrorRemote) ReleaseKey(context.Context, string, string, string) (bool, error) {
	return false, r.err
}

func newHandlerTestStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	return db, store
}

func insertCacheOwner(t *testing.T, db *sql.DB, store *Store, listener, eventKey, owner string) {
	t.Helper()
	tx := mustBegin(t, db)
	if err := store.InsertUnlessExistsWork(listener, eventKey, owner)(firebaseStoreTx{tx}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
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
