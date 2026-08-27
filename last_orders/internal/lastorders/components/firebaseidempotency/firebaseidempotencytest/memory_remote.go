package firebaseidempotencytest

// Package firebaseidempotencytest provides an in-memory stand-in for
// firebaseidempotency.Remote. It exists only to support tests; production code must
// bind to a real Firebase-backed Remote (see internal/lastorders/app.New).

import (
	"context"
	"sync"
)

// MemoryRemote is an in-memory, non-durable stand-in for the external Firebase
// idempotency store, for use in tests only.
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
