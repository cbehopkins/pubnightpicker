package sse

import (
	"sync"
	"testing"
)

func TestHubRoutesChangesAndInvalidations(t *testing.T) {
	hub := NewHub()
	a := hub.Register("group-a", "group-a", "user-a")
	a2 := hub.Register("group-a", "group-a", "user-a")
	b := hub.Register("group-b", "group-b", "user-b")
	public := hub.Register("group-a", "group-a", "")
	t.Cleanup(hub.Shutdown)

	hub.PublishChanged("group-a", 1)
	hub.PublishChanged("group-a", 2)
	if a.TakeChanged() != 2 || a2.TakeChanged() != 2 || count(b.Changed()) != 0 {
		t.Fatal("view changes were not routed and coalesced")
	}
	hub.InvalidateUID("user-a")
	if count(a.Invalidated()) != 1 || count(a2.Invalidated()) != 1 || count(b.Invalidated()) != 0 || count(public.Invalidated()) != 0 {
		t.Fatal("UID invalidation reached the wrong subscriptions")
	}
	hub.InvalidateAllAuthenticated()
	if count(b.Invalidated()) != 1 || count(public.Invalidated()) != 0 {
		t.Fatal("authenticated invalidation reached the wrong subscriptions")
	}
	hub.InvalidateScope("group-a")
	if count(public.Invalidated()) != 1 || count(a.Invalidated()) != 1 || count(b.Invalidated()) != 0 {
		t.Fatal("scope invalidation did not include anonymous streams or escaped its scope")
	}
}

func TestSubscriptionDiscardsChangesCoveredBySnapshot(t *testing.T) {
	hub := NewHub()
	sub := hub.Register("view", "scope", "uid")
	hub.PublishChanged("view", 4)
	hub.PublishChanged("view", 6)
	sub.DiscardChangesThrough(4)
	if got := sub.TakeChanged(); got != 6 {
		t.Fatalf("pending sequence=%d, want newer sequence 6", got)
	}
	hub.PublishChanged("view", 5)
	sub.DiscardChangesThrough(6)
	if got := sub.TakeChanged(); got != 0 {
		t.Fatalf("covered pending sequence=%d, want none", got)
	}
	sub.Unregister()
}

func TestHubUnregisterAndShutdownAreIdempotent(t *testing.T) {
	hub := NewHub()
	sub := hub.Register("view", "scope", "uid")
	sub.Unregister()
	sub.Unregister()
	if hub.SubscriberCount() != 0 {
		t.Fatalf("subscriber count=%d, want 0", hub.SubscriberCount())
	}
	hub.Shutdown()
	hub.Shutdown()
	select {
	case <-sub.Done():
	default:
		t.Fatal("shutdown did not signal subscription")
	}
	if late := hub.Register("view", "scope", "uid"); hub.SubscriberCount() != 0 {
		t.Fatal("registration succeeded after shutdown")
	} else {
		select {
		case <-late.Done():
		default:
			t.Fatal("late subscription was not born shut down")
		}
	}
}

func TestHubConcurrentSignalsAndUnregistration(t *testing.T) {
	hub := NewHub()
	var subs []*Subscription
	for range 40 {
		subs = append(subs, hub.Register("view", "scope", "uid"))
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				hub.PublishChanged("view", int64(1))
				hub.InvalidateUID("uid")
				hub.InvalidateAllAuthenticated()
				hub.InvalidateScope("scope")
			}
		}()
	}
	for _, sub := range subs {
		wg.Add(1)
		go func(s *Subscription) {
			defer wg.Done()
			s.Unregister()
			s.Unregister()
		}(sub)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		hub.Shutdown()
		hub.Shutdown()
	}()
	wg.Wait()
	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count=%d, want 0", got)
	}
}

func count(ch <-chan struct{}) int {
	n := 0
	for {
		select {
		case <-ch:
			n++
		default:
			return n
		}
	}
}
