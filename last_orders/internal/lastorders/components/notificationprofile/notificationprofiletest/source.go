// Package notificationprofiletest provides an in-memory stand-in for
// notificationprofile.Source. It exists only to support tests; production code must
// bind to a Firestore source.
package notificationprofiletest

import (
	"context"
	"fmt"
	"sync"

	"last_orders/internal/lastorders/components/notificationprofile"
)

// Source is a controllable stand-in. The watch streams replay their configured
// changes and then idle until the context is cancelled.
type Source struct {
	mu             sync.Mutex
	UserChanges    []notificationprofile.Change
	EndpointChange []notificationprofile.Change
	OnDeactivate   func(ctx context.Context, userID, endpointID string) error
}

func New() *Source {
	return &Source{}
}

func (s *Source) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return newStream(ctx, s.UserChanges), nil
}

func (s *Source) WatchEndpoints(ctx context.Context) (notificationprofile.ChangeStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return newStream(ctx, s.EndpointChange), nil
}

func (s *Source) DeactivateEndpoint(ctx context.Context, userID, endpointID string) error {
	if s.OnDeactivate == nil {
		return fmt.Errorf("notificationprofiletest: unexpected DeactivateEndpoint(%q, %q)", userID, endpointID)
	}
	return s.OnDeactivate(ctx, userID, endpointID)
}

func newStream(ctx context.Context, changes []notificationprofile.Change) *stream {
	pending := make([]notificationprofile.Change, len(changes))
	copy(pending, changes)
	return &stream{ctx: ctx, pending: pending}
}

type stream struct {
	ctx     context.Context
	pending []notificationprofile.Change
	done    bool
}

func (s *stream) Next() ([]notificationprofile.Change, error) {
	if !s.done {
		s.done = true
		if len(s.pending) > 0 {
			return s.pending, nil
		}
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *stream) Stop() {}
