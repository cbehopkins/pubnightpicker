package cellar

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryStoreAddPreservesID(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	request := CellRequest{
		ID:          "background-worker",
		Steps:       []CellStep{{HandlerName: "worker", Payload: []byte("payload")}},
	}

	if _, err := store.Add([]CellRequest{request}); err != nil {
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

func TestMemoryStoreAddRejectsExistingID(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	request := CellRequest{ID: "background-worker", Steps: []CellStep{{HandlerName: "worker"}}}

	if _, err := store.Add([]CellRequest{request}); err != nil {
		t.Fatalf("first Add() error = %v", err)
	}
	if _, err := store.Add([]CellRequest{request}); !errors.Is(err, ErrCellAlreadyExists) {
		t.Fatalf("second Add() error = %v, want ErrCellAlreadyExists", err)
	}
}

func TestMemoryStoreAddDuplicateIDsIsAtomic(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	requests := []CellRequest{
		{ID: "worker", Steps: []CellStep{{HandlerName: "first"}}},
		{ID: "worker", Steps: []CellStep{{HandlerName: "second"}}},
	}

	if _, err := store.Add(requests); !errors.Is(err, ErrCellAlreadyExists) {
		t.Fatalf("Add() error = %v, want ErrCellAlreadyExists", err)
	}
	if _, err := store.Get("worker"); !errors.Is(err, ErrCellNotFound) {
		t.Fatalf("Get() error = %v, want ErrCellNotFound", err)
	}
}

func TestMemoryStoreClaimNextClaimsReadyCell(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "email", Payload: []byte("hello")}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	cell, ok, err := store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if !ok {
		t.Fatalf("ClaimNext() ok = false, want true")
	}
	if cell.State != CellStateClaimed {
		t.Fatalf("ClaimNext() state = %q, want %q", cell.State, CellStateClaimed)
	}
}

func TestMemoryStoreClaimNextDoesNotReclaimClaimedCell(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "email", Payload: []byte("hello")}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, ok, err := store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("first ClaimNext() error = %v", err)
	}
	if !ok {
		t.Fatalf("first ClaimNext() ok = false, want true")
	}

	_, ok, err = store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("second ClaimNext() error = %v", err)
	}
	if ok {
		t.Fatalf("second ClaimNext() ok = true, want false")
	}
}

func TestMemoryStoreClaimNextRespectsNotBefore(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	future := time.Now().Add(10 * time.Minute)
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "email"}}, NotBefore: &future}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, ok, err := store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if ok {
		t.Fatalf("ClaimNext() ok = true, want false before not-before")
	}

	_, ok, err = store.ClaimNext(future)
	if err != nil {
		t.Fatalf("ClaimNext() at not-before error = %v", err)
	}
	if !ok {
		t.Fatalf("ClaimNext() at not-before ok = false, want true")
	}
}

func TestMemoryStoreCompleteDeletesOriginalCell(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	ids, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "email"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	cell, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", cell, ok, err)
	}

	if err := store.Complete(cell.ID, nil); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if _, err := store.Get(ids[0]); !errors.Is(err, ErrCellNotFound) {
		t.Fatalf("Get() err = %v, want ErrCellNotFound", err)
	}
}

func TestMemoryStoreCompleteWithChildrenIsAtomic(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed parent", parent, ok, err)
	}

	children := []CellRequest{
		{Steps: []CellStep{{HandlerName: "child-a", Payload: []byte("a")}}},
		{Steps: []CellStep{{HandlerName: "child-b", Payload: []byte("b")}}},
	}
	if err := store.Complete(parent.ID, children); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if _, err := store.Get(parent.ID); !errors.Is(err, ErrCellNotFound) {
		t.Fatalf("Get(parent) err = %v, want ErrCellNotFound", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("len(ListActive()) = %d, want 2", len(active))
	}
	for _, c := range active {
		if c.State != CellStateReady {
			t.Fatalf("child state = %q, want %q", c.State, CellStateReady)
		}
	}
}

func TestMemoryStoreCompleteWithAllocatedAndIdentifiedChildrenIsAtomic(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed parent", parent, ok, err)
	}

	err = store.Complete(
		parent.ID,
		[]CellRequest{
			{Steps: []CellStep{{HandlerName: "allocated-child"}}},
			{ID: "stable-child", Steps: []CellStep{{HandlerName: "identified-child"}}},
		},
	)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("len(ListActive()) = %d, want 2", len(active))
	}
	if active[0].ID != "test-2" {
		t.Fatalf("allocated child ID = %q, want %q", active[0].ID, "test-2")
	}
	if active[1].ID != "stable-child" {
		t.Fatalf("identified child ID = %q, want %q", active[1].ID, "stable-child")
	}
}

func TestMemoryStoreCompleteIdentifiedChildCollisionLeavesStateUnchanged(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := store.Add([]CellRequest{{ID: "stable-child", Steps: []CellStep{{HandlerName: "existing"}}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed parent", parent, ok, err)
	}

	err = store.Complete(parent.ID, []CellRequest{{ID: "stable-child", Steps: []CellStep{{HandlerName: "replacement"}}}})
	if !errors.Is(err, ErrCellAlreadyExists) {
		t.Fatalf("Complete() error = %v, want ErrCellAlreadyExists", err)
	}

	persisted, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if persisted.State != CellStateClaimed {
		t.Fatalf("parent state = %q, want %q", persisted.State, CellStateClaimed)
	}
}

func TestMemoryStoreCompleteFailureLeavesStateUnchanged(t *testing.T) {
	allocator := &scriptedAllocator{ids: []CellID{"p-1", "child-1"}, errAtCall: 2}
	store := NewMemoryStore(allocator)

	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "parent"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	parent, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed parent", parent, ok, err)
	}

	err = store.Complete(parent.ID, []CellRequest{{Steps: []CellStep{{HandlerName: "child"}}}})
	if err == nil {
		t.Fatalf("Complete() error = nil, want failure")
	}

	persisted, err := store.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get(parent) error = %v", err)
	}
	if persisted.State != CellStateClaimed {
		t.Fatalf("parent state = %q, want %q", persisted.State, CellStateClaimed)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("len(ListActive()) = %d, want 1", len(active))
	}
}

func TestMemoryStoreRetryReturnsClaimedCellToReadyAndPreservesID(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	ids, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "email"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	claimed, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", claimed, ok, err)
	}

	nextTime := time.Now().Add(2 * time.Minute)
	if err := store.Retry(claimed.ID, &nextTime); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}

	persisted, err := store.Get(ids[0])
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if persisted.ID != ids[0] {
		t.Fatalf("retried ID = %q, want %q", persisted.ID, ids[0])
	}
	if persisted.State != CellStateReady {
		t.Fatalf("retried state = %q, want %q", persisted.State, CellStateReady)
	}
	if persisted.NotBefore == nil || !persisted.NotBefore.Equal(nextTime) {
		t.Fatalf("retried NotBefore = %v, want %v", persisted.NotBefore, nextTime)
	}
}

func TestMemoryStoreRecoverMovesClaimedCellsToReady(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{
		{Steps: []CellStep{{HandlerName: "a"}}},
		{Steps: []CellStep{{HandlerName: "b"}}},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("first ClaimNext() = (%v, %v), want claimed", ok, err)
	}
	_, ok, err = store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("second ClaimNext() = (%v, %v), want claimed", ok, err)
	}

	if err := store.Recover(); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	for _, cell := range active {
		if cell.State != CellStateReady {
			t.Fatalf("state after Recover() = %q, want %q", cell.State, CellStateReady)
		}
	}
}

func TestMemoryStoreConcurrentClaimNextNoDoubleClaim(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "one"}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	const workers = 64
	results := make(chan CellID, workers)

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

	claimed := make([]CellID, 0, workers)
	for id := range results {
		claimed = append(claimed, id)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed count = %d, want 1", len(claimed))
	}
}

func TestMemoryStoreClaimNextReturnsPayloadCopy(t *testing.T) {
	store := NewMemoryStore(NewSequentialAllocator("test-", 1))
	_, err := store.Add([]CellRequest{{Steps: []CellStep{{HandlerName: "a", Payload: []byte("abc")}}}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	cell, ok, err := store.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("ClaimNext() = (%v, %v, %v), want claimed cell", cell, ok, err)
	}

	cell.Steps[0].Payload[0] = 'z'
	persisted, err := store.Get(cell.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(persisted.Steps[0].Payload) != "abc" {
		t.Fatalf("persisted payload = %q, want %q", string(persisted.Steps[0].Payload), "abc")
	}
}

type scriptedAllocator struct {
	mu        sync.Mutex
	ids       []CellID
	index     int
	errAtCall int
}

func (a *scriptedAllocator) Next() (CellID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.index++
	if a.errAtCall > 0 && a.index == a.errAtCall {
		return "", errors.New("allocator failure")
	}
	if a.index > len(a.ids) {
		return "", errors.New("allocator exhausted")
	}
	return a.ids[a.index-1], nil
}
