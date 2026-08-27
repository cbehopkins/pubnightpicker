package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cellar/pkg/cellar"
)

func TestStoreAddPreservesID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()
	request := cellar.CellRequest{
		ID:    "background-worker",
		Steps: []cellar.CellStep{{HandlerName: "worker", Payload: []byte("payload")}},
	}

	if _, err := store.Add([]cellar.CellRequest{request}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got, err := store.Get(request.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != request.ID {
		t.Fatalf("Get().ID = %q, want %q", got.ID, request.ID)
	}
	if got.Steps[0].HandlerName != request.Steps[0].HandlerName {
		t.Fatalf("Get().Steps[0].HandlerName = %q, want %q", got.Steps[0].HandlerName, request.Steps[0].HandlerName)
	}
}

func TestStoreAddRejectsExistingID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()
	request := cellar.CellRequest{ID: "background-worker", Steps: []cellar.CellStep{{HandlerName: "worker"}}}

	if _, err := store.Add([]cellar.CellRequest{request}); err != nil {
		t.Fatalf("first Add() error = %v", err)
	}
	if _, err := store.Add([]cellar.CellRequest{request}); !errors.Is(err, cellar.ErrCellAlreadyExists) {
		t.Fatalf("second Add() error = %v, want ErrCellAlreadyExists", err)
	}
}

func TestStoreAddDuplicateIDsIsAtomic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()
	requests := []cellar.CellRequest{
		{ID: "worker", Steps: []cellar.CellStep{{HandlerName: "first"}}},
		{ID: "worker", Steps: []cellar.CellStep{{HandlerName: "second"}}},
	}

	if _, err := store.Add(requests); !errors.Is(err, cellar.ErrCellAlreadyExists) {
		t.Fatalf("Add() error = %v, want ErrCellAlreadyExists", err)
	}
	if _, err := store.Get("worker"); !errors.Is(err, cellar.ErrCellNotFound) {
		t.Fatalf("Get() error = %v, want ErrCellNotFound", err)
	}
}

func TestStoreDBAccessorReturnsUnderlyingConnection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	if store.DB() == nil {
		t.Fatal("Store.DB() = nil, want underlying database connection")
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")

	store := mustOpenStore(t, dbPath)
	ids, err := store.Add([]cellar.CellRequest{{
		Steps: []cellar.CellStep{{HandlerName: "send-email", Payload: []byte("hello")}},
	}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened := mustOpenStore(t, dbPath)
	defer func() { _ = reopened.Close() }()

	got, err := reopened.Get(ids[0])
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if got.ID != ids[0] {
		t.Fatalf("Get().ID = %q, want %q", got.ID, ids[0])
	}
	if got.State != cellar.CellStateReady {
		t.Fatalf("Get().State = %q, want %q", got.State, cellar.CellStateReady)
	}
}

func TestStoreCompleteWithChildrenIsAtomic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	err = store.Complete(parent.ID, []cellar.CellRequest{
		{Steps: []cellar.CellStep{{HandlerName: "child-a", Payload: []byte("a")}}},
		{Steps: []cellar.CellStep{{HandlerName: "child-b", Payload: []byte("b")}}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if _, err := store.Get(parent.ID); err != cellar.ErrCellNotFound {
		t.Fatalf("Get(parent) err = %v, want ErrCellNotFound", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("len(ListActive()) = %d, want 2", len(active))
	}
	for _, cell := range active {
		if cell.State != cellar.CellStateReady {
			t.Fatalf("child state = %q, want %q", cell.State, cellar.CellStateReady)
		}
	}
}

func TestStoreCompleteIdentifiedChildCollisionLeavesStateUnchanged(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := store.Add([]cellar.CellRequest{{ID: "stable-child", Steps: []cellar.CellStep{{HandlerName: "existing"}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed parent", parent, ok, err)
	}

	err = store.Complete(parent.ID, []cellar.CellRequest{{ID: "stable-child", Steps: []cellar.CellStep{{HandlerName: "replacement"}}}})
	if !errors.Is(err, cellar.ErrCellAlreadyExists) {
		t.Fatalf("Complete() error = %v, want ErrCellAlreadyExists", err)
	}

	persisted, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if persisted.State != cellar.CellStateClaimed {
		t.Fatalf("parent state = %q, want %q", persisted.State, cellar.CellStateClaimed)
	}
}

func TestStoreConcurrentClaimNextNoDoubleClaim(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "one"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	const workers = 32
	results := make(chan cellar.CellID, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cell, ok, claimErr := store.ClaimNext(time.Now())
			if claimErr != nil {
				t.Errorf("ClaimNext() error = %v", claimErr)
				return
			}
			if ok {
				results <- cell.ID
			}
		}()
	}
	wg.Wait()
	close(results)

	count := 0
	for range results {
		count++
	}
	if count != 1 {
		t.Fatalf("claimed count = %d, want 1", count)
	}
}

func TestStoreCompleteAppliesApplicationWorkAtomically(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	_, err = store.db.Exec(`CREATE TABLE app_state (id TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("CREATE TABLE app_state error = %v", err)
	}

	err = store.Complete(
		parent.ID,
		[]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "child"}}}},
		cellar.ApplicationWork(func(tx cellar.ApplicationTx) error {
			return tx.Exec(`INSERT INTO app_state (id, value) VALUES (?, ?)`, "one", "ok")
		}),
		cellar.ApplicationWork(func(tx cellar.ApplicationTx) error {
			return tx.Exec(`INSERT INTO app_state (id, value) VALUES (?, ?)`, "two", "ok")
		}),
	)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if _, err := store.Get(parent.ID); err != cellar.ErrCellNotFound {
		t.Fatalf("Get(parent) err = %v, want ErrCellNotFound", err)
	}

	var count int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM app_state`).Scan(&count)
	if err != nil {
		t.Fatalf("Query app_state count error = %v", err)
	}
	if count != 2 {
		t.Fatalf("app_state rows = %d, want 2", count)
	}
}

func TestStoreRecoverMovesClaimedCellsToReady(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	ids, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "ready"}}}, {Steps: []cellar.CellStep{{HandlerName: "claimed"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	claimed, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", claimed, ok, err)
	}
	if claimed.ID != ids[0] {
		t.Fatalf("ClaimNext().ID = %q, want %q", claimed.ID, ids[0])
	}

	if err := store.Recover(); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("len(ListActive()) = %d, want 2", len(active))
	}
	for _, cell := range active {
		if cell.State != cellar.CellStateReady {
			t.Fatalf("state after Recover() = %q, want %q", cell.State, cellar.CellStateReady)
		}
	}
}

func TestStoreCompleteSupportsContextAwareApplicationWork(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	_, err = store.db.Exec(`CREATE TABLE app_state (id TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("CREATE TABLE app_state error = %v", err)
	}

	err = store.Complete(
		parent.ID,
		[]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "child"}}}},
		cellar.ApplicationWork(func(tx cellar.ApplicationTx) error {
			return tx.ExecContext(context.Background(), `INSERT INTO app_state (id, value) VALUES (?, ?)`, "one", "ok")
		}),
	)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	var count int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM app_state`).Scan(&count)
	if err != nil {
		t.Fatalf("Query app_state count error = %v", err)
	}
	if count != 1 {
		t.Fatalf("app_state rows = %d, want 1", count)
	}
}

func TestStoreCompleteRollsBackOnApplicationWorkError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	_, err = store.db.Exec(`CREATE TABLE app_state (id TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("CREATE TABLE app_state error = %v", err)
	}

	err = store.Complete(parent.ID, nil, cellar.ApplicationWork(func(tx cellar.ApplicationTx) error {
		execErr := tx.Exec(`INSERT INTO app_state (id, value) VALUES (?, ?)`, "one", "ok")
		if execErr != nil {
			return execErr
		}
		return errors.New("boom")
	}))
	if err == nil {
		t.Fatal("Complete() error = nil, want application work failure")
	}

	got, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if got.State != cellar.CellStateClaimed {
		t.Fatalf("Get(parent).State = %q, want %q", got.State, cellar.CellStateClaimed)
	}

	var count int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM app_state`).Scan(&count)
	if err != nil {
		t.Fatalf("Query app_state count error = %v", err)
	}
	if count != 0 {
		t.Fatalf("app_state rows = %d, want 0", count)
	}
}

func TestInspectorIntegrationWithSQLiteStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	payload, err := json.Marshal(map[string]any{"user": "alice", "attempt": float64(1)})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = store.Add([]cellar.CellRequest{{
		Steps: []cellar.CellStep{{HandlerName: "send-email", Payload: payload}},
	}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	inspector := cellar.NewCellInspector()
	inspector.RegisterDecoder("send-email", cellar.JSONAnyDecoder())

	inspections, err := inspector.InspectAll(store)
	if err != nil {
		t.Fatalf("InspectAll() error = %v", err)
	}
	if len(inspections) != 1 {
		t.Fatalf("len(InspectAll()) = %d, want 1", len(inspections))
	}
	if inspections[0].DecodeError != nil {
		t.Fatalf("DecodeError = %v, want nil", inspections[0].DecodeError)
	}
	if inspections[0].PayloadFormat != "json" {
		t.Fatalf("PayloadFormat = %q, want json", inspections[0].PayloadFormat)
	}
}

func mustOpenStore(t *testing.T, dbPath string) *Store {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout("+defaultBusyTimeout+")&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store, err := NewStore(db, cellar.NewSequentialAllocator("test-", 1))
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}
