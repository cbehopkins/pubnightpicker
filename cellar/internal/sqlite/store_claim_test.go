package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"cellar/pkg/cellar"
)

func TestStoreClaimNextMarksCellClaimedAndPreventsReclaim(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	store, err := NewStore(db, cellar.NewSequentialAllocator("test-", 1))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ids, err := store.Add([]cellar.CellRequest{{HandlerName: "demo"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("len(Add()) = %d, want 1", len(ids))
	}

	first, ok, err := store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("ClaimNext() first error = %v", err)
	}
	if !ok {
		t.Fatal("ClaimNext() first ok = false, want true")
	}
	if first.State != cellar.CellStateClaimed {
		t.Fatalf("ClaimNext() first state = %q, want %q", first.State, cellar.CellStateClaimed)
	}
	if first.ID != ids[0] {
		t.Fatalf("ClaimNext() first ID = %q, want %q", first.ID, ids[0])
	}

	second, ok, err := store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("ClaimNext() second error = %v", err)
	}
	if ok {
		t.Fatalf("ClaimNext() second ok = true, want false; returned %q", second.ID)
	}
}
