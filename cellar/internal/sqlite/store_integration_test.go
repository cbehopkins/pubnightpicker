package sqlite

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cellar/pkg/cellar"
)

func TestStorePersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")

	store := mustOpenStore(t, dbPath)
	ids, err := store.Add([]cellar.CellRequest{{
		HandlerName: "send-email",
		Payload:     []byte("hello"),
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

	_, err := store.Add([]cellar.CellRequest{{HandlerName: "parent"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	err = store.Complete(parent.ID, []cellar.CellRequest{
		{HandlerName: "child-a", Payload: []byte("a")},
		{HandlerName: "child-b", Payload: []byte("b")},
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

func TestStoreConcurrentClaimNextNoDoubleClaim(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	_, err := store.Add([]cellar.CellRequest{{HandlerName: "one"}})
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

func TestInspectorIntegrationWithSQLiteStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	store := mustOpenStore(t, dbPath)
	defer func() { _ = store.Close() }()

	payload, err := json.Marshal(map[string]any{"user": "alice", "attempt": float64(1)})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = store.Add([]cellar.CellRequest{{
		HandlerName: "send-email",
		Payload:     payload,
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

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout("+defaultBusyTimeout+")")
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
