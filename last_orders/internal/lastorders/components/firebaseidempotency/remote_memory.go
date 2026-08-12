package firebaseidempotency

import (
	"context"
	"sync"
)

// Remote is the external Firebase idempotency surface used by the idempotency component.
//
// Production code should bind this to a real Firebase-backed implementation.
// Early skeleton/runtime and integration tests may bind this to an in-memory stand-in.
type Remote interface {
	CreateKey(ctx context.Context, listener, eventKey string) (alreadyExists bool, err error)
	HasKey(ctx context.Context, listener, eventKey string) (bool, error)
}

// MemoryRemote is an in-memory stand-in for the external Firebase idempotency store.
//
// It exists to support early skeleton execution and integration tests before wiring a
// real Firebase implementation. It is process-local and non-durable.
type MemoryRemote struct {
	mu          sync.Mutex
	exists      map[string]bool
	visible     map[string]bool
	autoVisible bool
	createCalls map[string]int
	hasCalls    map[string]int
}

// NewInMemoryRemoteStandIn creates a process-local, in-memory Firebase idempotency stand-in.
//
// When autoVisible is true, newly created keys become visible immediately to HasKey.
// When false, tests can explicitly control visibility with SetVisible.
func NewInMemoryRemoteStandIn(autoVisible bool) *MemoryRemote {
	return &MemoryRemote{
		exists:      map[string]bool{},
		visible:     map[string]bool{},
		autoVisible: autoVisible,
		createCalls: map[string]int{},
		hasCalls:    map[string]int{},
	}
}

// NewMemoryRemote is kept for compatibility; prefer NewInMemoryRemoteStandIn for clarity.
func NewMemoryRemote(autoVisible bool) *MemoryRemote {
	return NewInMemoryRemoteStandIn(autoVisible)
}

func (r *MemoryRemote) CreateKey(ctx context.Context, listener, eventKey string) (bool, error) {
	_ = ctx
	key := listener + "::" + eventKey
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls[key]++
	if r.exists[key] {
		return true, nil
	}
	r.exists[key] = true
	r.visible[key] = r.autoVisible
	return false, nil
}

func (r *MemoryRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	_ = ctx
	key := listener + "::" + eventKey
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hasCalls[key]++
	if !r.exists[key] {
		return false, nil
	}
	return r.visible[key], nil
}

func (r *MemoryRemote) SeedExisting(listener, eventKey string, visible bool) {
	key := listener + "::" + eventKey
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exists[key] = true
	r.visible[key] = visible
}

func (r *MemoryRemote) SetVisible(listener, eventKey string, visible bool) {
	key := listener + "::" + eventKey
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exists[key] {
		r.visible[key] = visible
	}
}

func (r *MemoryRemote) CreateCallCount(listener, eventKey string) int {
	key := listener + "::" + eventKey
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCalls[key]
}
