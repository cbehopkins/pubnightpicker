package cellar

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// CellIDAllocator allocates opaque cell identifiers.
type CellIDAllocator interface {
	Next() (CellID, error)
}

// UUIDAllocator allocates UUIDv7 identifiers, which are unique without any persisted state.
type UUIDAllocator struct{}

// NewUUIDAllocator creates the default allocator for production use.
func NewUUIDAllocator() *UUIDAllocator {
	return &UUIDAllocator{}
}

// Next returns a fresh time-ordered identifier.
func (a *UUIDAllocator) Next() (CellID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate uuidv7: %w", err)
	}
	return CellID(id.String()), nil
}

// SequentialAllocator is a deterministic allocator useful for tests and simulations.
// Its counter is in-memory only, so a restart reissues identifiers already in use.
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
