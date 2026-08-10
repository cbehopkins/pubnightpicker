package scheduler

import (
	"context"
	"testing"
	"time"

	"cellar/pkg/cellar"
)

type stubStore struct {
	cell  cellar.Cell
	ok    bool
	err   error
	calls int
}

func (s *stubStore) ClaimNext(now time.Time) (cellar.Cell, bool, error) {
	s.calls++
	if s.calls == 1 {
		return s.cell, s.ok, s.err
	}
	return cellar.Cell{}, false, nil
}

type stubDispatcher struct {
	queued     []cellar.Cell
	onDispatch func()
}

func (d *stubDispatcher) Dispatch(ctx context.Context, cell cellar.Cell) error {
	d.queued = append(d.queued, cell)
	if d.onDispatch != nil {
		d.onDispatch()
	}
	return nil
}

func TestEngineClaimsAndDispatchesOneCell(t *testing.T) {
	store := &stubStore{cell: cellar.Cell{ID: "cell-1", State: cellar.CellStateReady}, ok: true}
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := &stubDispatcher{onDispatch: cancel}

	engine := NewScheduler(store, dispatcher, 1, time.Millisecond)
	defer cancel()

	go engine.Run(ctx)

	time.Sleep(20 * time.Millisecond)

	if store.calls == 0 {
		t.Fatal("ClaimNext() was not called")
	}
	if len(dispatcher.queued) != 1 {
		t.Fatalf("Dispatch() calls = %d, want 1", len(dispatcher.queued))
	}
	if dispatcher.queued[0].ID != "cell-1" {
		t.Fatalf("Dispatch() cell ID = %q, want %q", dispatcher.queued[0].ID, "cell-1")
	}
	if store.calls > 2 {
		t.Fatalf("ClaimNext() calls = %d, want at most 2 after first claim is consumed", store.calls)
	}
}

func TestEngineStopsWhenContextIsCancelled(t *testing.T) {
	store := &stubStore{cell: cellar.Cell{ID: "cell-2", State: cellar.CellStateReady}, ok: true}
	dispatcher := &stubDispatcher{}

	engine := NewScheduler(store, dispatcher, 1, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	go engine.Run(ctx)
	cancel()

	time.Sleep(20 * time.Millisecond)

	if store.calls > 3 {
		t.Fatalf("ClaimNext() calls = %d, want at most 3 after cancellation", store.calls)
	}
}
