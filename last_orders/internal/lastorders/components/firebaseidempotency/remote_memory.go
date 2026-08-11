package firebaseidempotency

import (
	"context"
	"sync"
)

type Remote interface {
	CreateKey(ctx context.Context, listener, eventKey string) (alreadyExists bool, err error)
	HasKey(ctx context.Context, listener, eventKey string) (bool, error)
}

type MemoryRemote struct {
	mu          sync.Mutex
	exists      map[string]bool
	visible     map[string]bool
	autoVisible bool
	createCalls map[string]int
	hasCalls    map[string]int
}

func NewMemoryRemote(autoVisible bool) *MemoryRemote {
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
