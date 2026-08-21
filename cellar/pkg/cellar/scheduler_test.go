package cellar

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type capacityTestStore struct {
	mu        sync.Mutex
	claims    int
	available int
	claimed   chan int
}

func (s *capacityTestStore) ClaimNext(now time.Time) (Cell, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claims >= s.available {
		return Cell{}, false, nil
	}
	s.claims++
	claim := s.claims
	s.claimed <- claim
	return Cell{ID: CellID(fmt.Sprintf("cell-%d", claim)), State: CellStateClaimed}, true, nil
}

type blockingTestDispatcher struct {
	started chan CellID
	release chan struct{}
}

func (d *blockingTestDispatcher) Dispatch(ctx context.Context, cell Cell) error {
	d.started <- cell.ID
	select {
	case <-d.release:
		return nil
	case <-ctx.Done():
		return nil
	}
}

func TestSchedulerDoesNotClaimBeyondWorkerCapacity(t *testing.T) {
	store := &capacityTestStore{available: 3, claimed: make(chan int, 3)}
	dispatcher := &blockingTestDispatcher{
		started: make(chan CellID, 3),
		release: make(chan struct{}, 3),
	}
	scheduler := NewScheduler(store, dispatcher, 2, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()

	for want := 1; want <= 2; want++ {
		select {
		case got := <-store.claimed:
			if got != want {
				t.Fatalf("claim = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("claim %d did not occur", want)
		}
	}
	select {
	case claim := <-store.claimed:
		t.Fatalf("claim %d occurred while both workers were occupied", claim)
	case <-time.After(20 * time.Millisecond):
	}

	dispatcher.release <- struct{}{}
	select {
	case got := <-store.claimed:
		if got != 3 {
			t.Fatalf("claim = %d, want 3", got)
		}
	case <-time.After(time.Second):
		t.Fatal("third claim did not occur after capacity returned")
	}

	cancel()
	for range 2 {
		dispatcher.release <- struct{}{}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop")
	}
}

func TestNewSchedulerDefaultsToOneWorker(t *testing.T) {
	scheduler := NewScheduler(&capacityTestStore{}, &blockingTestDispatcher{}, 0, time.Second)
	if scheduler.workers != 1 {
		t.Fatalf("workers = %d, want 1", scheduler.workers)
	}
}

type failingConcurrentDispatcher struct {
	wantErr        error
	siblingStarted chan struct{}
	siblingStopped chan struct{}
}

func (d *failingConcurrentDispatcher) Dispatch(ctx context.Context, cell Cell) error {
	if cell.ID == "cell-1" {
		select {
		case <-d.siblingStarted:
			return d.wantErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(d.siblingStarted)
	<-ctx.Done()
	close(d.siblingStopped)
	return nil
}

func TestSchedulerCancelsAndWaitsForConcurrentDispatchesOnError(t *testing.T) {
	want := errors.New("dispatch failed")
	store := &capacityTestStore{available: 2, claimed: make(chan int, 2)}
	dispatcher := &failingConcurrentDispatcher{
		wantErr:        want,
		siblingStarted: make(chan struct{}),
		siblingStopped: make(chan struct{}),
	}
	scheduler := NewScheduler(store, dispatcher, 2, time.Millisecond)

	err := scheduler.Run(t.Context())
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	select {
	case <-dispatcher.siblingStopped:
	default:
		t.Fatal("Run() returned before the sibling dispatch stopped")
	}
}
