package cellar

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingApplier struct {
	name  string
	calls *[]string
	err   error
}

func (a *recordingApplier) ApplyResult(ctx context.Context, cell Cell, result Result) error {
	if a.calls != nil {
		*a.calls = append(*a.calls, a.name)
	}
	return a.err
}

func TestMultiResultApplierSkipsNilAndStopsOnFirstError(t *testing.T) {
	calls := make([]string, 0, 3)
	first := &recordingApplier{name: "first", calls: &calls}
	second := &recordingApplier{name: "second", calls: &calls, err: errors.New("boom")}
	third := &recordingApplier{name: "third", calls: &calls}

	applier := MultiResultApplier{nil, first, second, third}
	err := applier.ApplyResult(context.Background(), Cell{ID: "cell-1"}, Complete{})
	if err == nil {
		t.Fatal("ApplyResult() error = nil, want error")
	}
	if err.Error() != "boom" {
		t.Fatalf("ApplyResult() error = %q, want %q", err.Error(), "boom")
	}
	if len(calls) != 2 {
		t.Fatalf("applier calls = %d, want 2", len(calls))
	}
	if calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("applier calls = %v, want [first second]", calls)
	}
}

func TestStoreResultApplierAppliesCompleteResults(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	applier := NewStoreResultApplier(store)
	err = applier.ApplyResult(context.Background(), parent, Complete{NewCells: []CellRequest{{Steps: []CellStep{{HandlerName: "child"}}}}})
	if err != nil {
		t.Fatalf("ApplyResult() error = %v", err)
	}

	if _, err := store.Get(parent.ID); err != ErrCellNotFound {
		t.Fatalf("Get(parent) err = %v, want ErrCellNotFound", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("len(ListActive()) = %d, want 1", len(active))
	}
	if active[0].Steps[0].HandlerName != "child" {
		t.Fatalf("child handler = %q, want %q", active[0].Steps[0].HandlerName, "child")
	}
}

func TestStoreResultApplierPreservesNewCellIDs(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	applier := NewStoreResultApplier(store)
	err = applier.ApplyResult(context.Background(), parent, Complete{
		NewCells: []CellRequest{{ID: "stable-child", Steps: []CellStep{{HandlerName: "child"}}}},
	})
	if err != nil {
		t.Fatalf("ApplyResult() error = %v", err)
	}

	child, err := store.Get("stable-child")
	if err != nil {
		t.Fatalf("Get(child) error = %v", err)
	}
	if child.Steps[0].HandlerName != "child" {
		t.Fatalf("child handler = %q, want %q", child.Steps[0].HandlerName, "child")
	}
}

func TestStoreResultApplierAppliesRetryResults(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	nextTime := time.Now().Add(2 * time.Minute)
	applier := NewStoreResultApplier(store)
	err = applier.ApplyResult(context.Background(), parent, Retry{NotBefore: &nextTime})
	if err != nil {
		t.Fatalf("ApplyResult() error = %v", err)
	}

	cell, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if cell.State != CellStateReady {
		t.Fatalf("cell state = %q, want %q", cell.State, CellStateReady)
	}
	if cell.NotBefore == nil || !cell.NotBefore.Equal(nextTime) {
		t.Fatalf("cell not_before = %v, want %v", cell.NotBefore, nextTime)
	}
}

func TestStoreResultApplierRetryAppliesChildrenBeforeRequeue(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	if _, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	err = NewStoreResultApplier(store).ApplyResult(context.Background(), parent, Retry{
		NewCells: []CellRequest{{Steps: []CellStep{{HandlerName: "audit"}}}},
	})
	if err != nil {
		t.Fatalf("ApplyResult() error = %v", err)
	}
	persisted, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if persisted.State != CellStateReady || persisted.CurrentStep != parent.CurrentStep {
		t.Fatalf("parent after retry = state %q, step %d", persisted.State, persisted.CurrentStep)
	}
	if _, err := store.Get("test-2"); err != nil {
		t.Fatalf("Get(child) error = %v", err)
	}
}

func TestStoreResultApplierRetrySequenceAppliesChildrenBeforeReset(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	if _, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "first"}, {HandlerName: "second"}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}
	if err := store.ApplyResult(parent, Complete{}); err != nil {
		t.Fatalf("ApplyResult(Complete) error = %v", err)
	}
	claimed, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext(second) = (%v, %v, %v), want claimed cell", claimed, ok, err)
	}

	err = store.ApplyResult(claimed, RetrySequence{
		NewCells: []CellRequest{{Steps: []CellStep{{HandlerName: "audit"}}}},
	})
	if err != nil {
		t.Fatalf("ApplyResult(RetrySequence) error = %v", err)
	}
	persisted, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if persisted.CurrentStep != 0 || persisted.State != CellStateReady {
		t.Fatalf("parent after sequence retry = state %q, step %d", persisted.State, persisted.CurrentStep)
	}
	if _, err := store.Get("test-2"); err != nil {
		t.Fatalf("Get(child) error = %v", err)
	}
}

func TestStoreResultApplierKillAppliesChildrenBeforeDeletingCell(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	if _, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	applier := NewStoreResultApplier(store)
	err = applier.ApplyResult(context.Background(), parent, Kill{
		NewCells: []CellRequest{{Steps: []CellStep{{HandlerName: "cleanup"}}}},
	})
	if err != nil {
		t.Fatalf("ApplyResult() error = %v", err)
	}
	if _, err := store.Get(parent.ID); !errors.Is(err, ErrCellNotFound) {
		t.Fatalf("Get(parent) error = %v, want ErrCellNotFound", err)
	}
	if _, err := store.Get("test-2"); err != nil {
		t.Fatalf("Get(child) error = %v", err)
	}
}

func TestStoreResultApplierKillRollsBackChildrenOnApplicationFailure(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	if _, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", parent, ok, err)
	}

	wantErr := errors.New("application failure")
	err = NewStoreResultApplier(store).ApplyResult(context.Background(), parent, Kill{
		NewCells: []CellRequest{{Steps: []CellStep{{HandlerName: "cleanup"}}}},
		ApplicationWork: []ApplicationWork{func(ApplicationTx) error {
			return wantErr
		}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyResult() error = %v, want %v", err, wantErr)
	}
	persisted, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if persisted.State != CellStateClaimed {
		t.Fatalf("parent state = %q, want %q", persisted.State, CellStateClaimed)
	}
	if _, err := store.Get("test-2"); !errors.Is(err, ErrCellNotFound) {
		t.Fatalf("Get(child) error = %v, want ErrCellNotFound", err)
	}
}
