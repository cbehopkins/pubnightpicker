package cellar

import (
	"sync"
	"testing"
)

func TestSequentialAllocatorNextGeneratesDistinctNonEmptyIDs(t *testing.T) {
	allocator := NewSequentialAllocator("cell-", 1)

	first, err := allocator.Next()
	if err != nil {
		t.Fatalf("Next() first error = %v", err)
	}
	if first == "" {
		t.Fatal("Next() first returned empty ID")
	}

	second, err := allocator.Next()
	if err != nil {
		t.Fatalf("Next() second error = %v", err)
	}
	if second == "" {
		t.Fatal("Next() second returned empty ID")
	}
	if first == second {
		t.Fatalf("Next() returned duplicate IDs: %q and %q", first, second)
	}
}

func TestSequentialAllocatorHonoursStartValue(t *testing.T) {
	allocator := NewSequentialAllocator("cell-", 7)

	got, err := allocator.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if got != CellID("cell-7") {
		t.Fatalf("Next() = %q, want %q", got, CellID("cell-7"))
	}
}

func TestUUIDAllocatorNextGeneratesDistinctNonEmptyIDs(t *testing.T) {
	allocator := NewUUIDAllocator()

	seen := make(map[CellID]struct{}, 1000)
	for i := range 1000 {
		id, err := allocator.Next()
		if err != nil {
			t.Fatalf("Next() iteration %d error = %v", i, err)
		}
		if id == "" {
			t.Fatalf("Next() iteration %d returned empty ID", i)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("Next() returned duplicate ID %q at iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

// Independent allocators stand in for separate process lifetimes.
func TestUUIDAllocatorNextIsUniqueAcrossInstances(t *testing.T) {
	first, err := NewUUIDAllocator().Next()
	if err != nil {
		t.Fatalf("first allocator Next() error = %v", err)
	}

	second, err := NewUUIDAllocator().Next()
	if err != nil {
		t.Fatalf("second allocator Next() error = %v", err)
	}

	if first == second {
		t.Fatalf("independent allocators returned duplicate ID %q", first)
	}
}

func TestUUIDAllocatorNextIsMonotonicallyOrdered(t *testing.T) {
	allocator := NewUUIDAllocator()

	previous, err := allocator.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}

	for i := range 100 {
		current, err := allocator.Next()
		if err != nil {
			t.Fatalf("Next() iteration %d error = %v", i, err)
		}
		if current <= previous {
			t.Fatalf("Next() iteration %d = %q, want lexicographically greater than %q", i, current, previous)
		}
		previous = current
	}
}

func TestUUIDAllocatorNextIsSafeForConcurrentUse(t *testing.T) {
	const (
		goroutines       = 8
		idsPerGoroutine  = 100
		totalExpectedIDs = goroutines * idsPerGoroutine
	)

	allocator := NewUUIDAllocator()

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		seen   = make(map[CellID]struct{}, totalExpectedIDs)
		errsMu sync.Mutex
		errs   []error
	)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range idsPerGoroutine {
				id, err := allocator.Next()
				if err != nil {
					errsMu.Lock()
					errs = append(errs, err)
					errsMu.Unlock()
					return
				}
				mu.Lock()
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	for _, err := range errs {
		t.Fatalf("Next() error = %v", err)
	}
	if len(seen) != totalExpectedIDs {
		t.Fatalf("allocated %d distinct IDs, want %d", len(seen), totalExpectedIDs)
	}
}
