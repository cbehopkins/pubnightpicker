package cellar

import "testing"

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
