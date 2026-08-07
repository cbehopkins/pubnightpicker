package cellar

import (
	"fmt"
	"sync"
)

// CellIDAllocator allocates opaque cell identifiers.
type CellIDAllocator interface {
	Next() (CellID, error)
}

// SequentialAllocator is a deterministic allocator useful for tests and simulations.
type SequentialAllocator struct {
	mu     sync.Mutex
	next   uint64
	prefix string
}

// NewSequentialAllocator creates an allocator that emits IDs as "<prefix><n>".
func NewSequentialAllocator(prefix string, start uint64) *SequentialAllocator {
	return &SequentialAllocator{
		next:   start,
		prefix: prefix,
	}
}

// Next returns the next identifier from the sequence.
func (a *SequentialAllocator) Next() (CellID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	id := CellID(fmt.Sprintf("%s%d", a.prefix, a.next))
	a.next++
	return id, nil
}
