package roleauthorization

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/sse"
)

type reconnectingSource struct {
	mu                 sync.Mutex
	calls              int
	continueChange     <-chan struct{}
	continueDisconnect <-chan struct{}
}

func (s *reconnectingSource) Watch(ctx context.Context) (Stream, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	return &reconnectingStream{ctx: ctx, failAfterBaseline: call == 1, continueChange: s.continueChange, continueDisconnect: s.continueDisconnect}, nil
}

type reconnectingStream struct {
	ctx                context.Context
	failAfterBaseline  bool
	baselineSent       bool
	changeSent         bool
	continueChange     <-chan struct{}
	continueDisconnect <-chan struct{}
}

func (s *reconnectingStream) Next() ([]Change, error) {
	if !s.baselineSent {
		s.baselineSent = true
		return nil, nil
	}
	if s.failAfterBaseline {
		if !s.changeSent {
			s.changeSent = true
			select {
			case <-s.continueChange:
			case <-s.ctx.Done():
				return nil, s.ctx.Err()
			}
			return []Change{{UIDs: []string{"user-a"}}}, nil
		}
		select {
		case <-s.continueDisconnect:
			return nil, errors.New("simulated listener disconnect")
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *reconnectingStream) Stop() {}

func TestListenerInvalidatesGlobalAdminCacheOnReconnectBaseline(t *testing.T) {
	var mu sync.Mutex
	invalidations := 0
	continueChange := make(chan struct{})
	continueDisconnect := make(chan struct{})
	hub := sse.NewHub()
	subscription := hub.Register("current-meets:group-a", "current-meets-group:group-a", "user-a")
	listener, err := New(&reconnectingSource{continueChange: continueChange, continueDisconnect: continueDisconnect}, func() {
		mu.Lock()
		invalidations++
		mu.Unlock()
	}, nil, hub)
	if err != nil {
		t.Fatal(err)
	}
	listener.retryDelay = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	waitForInvalidations := func(want int) {
		t.Helper()
		deadline := time.After(time.Second)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			mu.Lock()
			count := invalidations
			mu.Unlock()
			if count >= want {
				return
			}
			select {
			case <-deadline:
				t.Fatalf("invalidations=%d, want at least %d", count, want)
			case <-ticker.C:
			}
		}
	}
	waitForInvalidations(1)
	select {
	case <-subscription.Invalidated():
	case <-time.After(time.Second):
		t.Fatal("initial role baseline did not invalidate authenticated streams")
	}
	close(continueChange)
	waitForInvalidations(2)
	select {
	case <-subscription.Invalidated():
	case <-time.After(time.Second):
		t.Fatal("admin UID change did not invalidate its stream")
	}
	close(continueDisconnect)
	waitForInvalidations(3)
	select {
	case <-subscription.Invalidated():
	case <-time.After(time.Second):
		t.Fatal("reconnected role baseline did not invalidate authenticated streams")
	}
}
