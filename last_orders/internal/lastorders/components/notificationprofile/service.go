package notificationprofile

import (
	"context"
	"fmt"
	"log/slog"
)

// Service is the notification-oriented interface onto the projection. The push
// pipeline treats it as read-only; endpoint invalidation is owned here because
// the projection service owns all translation to and from Firebase.
type Service struct {
	store  *Store
	source Source
	logger *slog.Logger
}

func NewService(store *Store, source Source, logger *slog.Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("notification profile store is required")
	}
	if source == nil {
		return nil, fmt.Errorf("notification profile source is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, source: source, logger: logger}, nil
}

// GetEligiblePushEndpoints reads the local projection only. It never falls back to
// Firebase: the projection is a full materialised view, not a cache.
func (s *Service) GetEligiblePushEndpoints(ctx context.Context, selector Selector) ([]Endpoint, error) {
	return s.store.EligibleEndpoints(ctx, selector)
}

// Preferences returns one user's projected preferences, for recipient resolution
// which cannot be expressed as a selector.
func (s *Service) Preferences(ctx context.Context, userID string) (UserPreferences, error) {
	return s.store.Preferences(ctx, userID)
}

// InvalidateEndpoint records that an endpoint is permanently unusable. Firebase is
// updated first so the projection converges on the authoritative state even if the
// local write is lost.
func (s *Service) InvalidateEndpoint(ctx context.Context, userID, endpointID string) error {
	if err := s.source.DeactivateEndpoint(ctx, userID, endpointID); err != nil {
		return fmt.Errorf("deactivate endpoint %q for user %q: %w", endpointID, userID, err)
	}
	if err := s.store.SetEndpointActive(ctx, userID, endpointID, false); err != nil {
		return fmt.Errorf("mark endpoint %q inactive locally: %w", endpointID, err)
	}
	return nil
}

func (s *Service) SourceWatchUsers(ctx context.Context) (ChangeStream, error) {
	return s.source.WatchUsers(ctx)
}

func (s *Service) SourceWatchEndpoints(ctx context.Context) (ChangeStream, error) {
	return s.source.WatchEndpoints(ctx)
}
