package venuecache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"last_orders/internal/lastorders/basestore"
)

type Snapshot struct {
	Venues []VenueProjection
	State  basestore.SyncState
}

type Service struct {
	store  *Store
	source Source
	logger *slog.Logger
}

func NewService(store *Store, source Source, logger *slog.Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("venue cache store is required")
	}
	if source == nil {
		return nil, fmt.Errorf("venue source is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, source: source, logger: logger}, nil
}

func (s *Service) Get(ctx context.Context, venueID string) (VenueProjection, error) {
	projection, err := s.store.Get(ctx, venueID)
	if err == nil {
		return projection, nil
	}
	if !errors.Is(err, ErrCacheMiss) {
		return VenueProjection{}, fmt.Errorf("read venue cache: %w", err)
	}

	doc, err := s.source.Get(ctx, venueID)
	if err != nil {
		return VenueProjection{}, err
	}
	projection, err = ProjectionFromDocument(doc)
	if err != nil {
		return VenueProjection{}, err
	}
	if projection.DeletedOn != "" {
		return VenueProjection{}, ErrNotFound
	}
	if err := s.store.Put(ctx, projection); err != nil {
		s.logger.Warn("populate venue cache failed", "venue_id", venueID, "err", err)
	}
	return projection, nil
}

// ListEventVenues currently forwards to the authoritative source. The API is
// intentionally owned by the cache so local listing can be added later without
// changing recurrence consumers.
func (s *Service) ListEventVenues(ctx context.Context) ([]VenueProjection, error) {
	docs, err := s.source.ListEventVenues(ctx)
	if err != nil {
		return nil, err
	}
	venues := make([]VenueProjection, 0, len(docs))
	for _, doc := range docs {
		projection, err := ProjectionFromDocument(doc)
		if err != nil {
			return nil, err
		}
		venues = append(venues, projection)
	}
	return venues, nil
}

func (s *Service) ListSnapshot(ctx context.Context) (Snapshot, error) {
	venues, state, err := s.store.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read venue snapshot: %w", err)
	}
	return Snapshot{Venues: venues, State: state}, nil
}

func (s *Service) CanManage(ctx context.Context, uid string) (bool, error) {
	writer, ok := s.source.(Writer)
	if !ok {
		return false, fmt.Errorf("venue writes are unavailable")
	}
	return writer.CanManage(ctx, uid)
}

func (s *Service) Create(ctx context.Context, fields map[string]any) (VenueProjection, error) {
	writer, ok := s.source.(Writer)
	if !ok {
		return VenueProjection{}, fmt.Errorf("venue writes are unavailable")
	}
	doc, err := writer.Create(ctx, fields)
	if err != nil {
		return VenueProjection{}, err
	}
	projection, err := ProjectionFromDocument(doc)
	if err != nil {
		return VenueProjection{}, err
	}
	if err := s.store.Put(ctx, projection); err != nil {
		return VenueProjection{}, fmt.Errorf("persist created venue projection: %w", err)
	}
	return projection, nil
}

func (s *Service) Patch(ctx context.Context, venueID string, fields map[string]any) (VenueProjection, error) {
	writer, ok := s.source.(Writer)
	if !ok {
		return VenueProjection{}, fmt.Errorf("venue writes are unavailable")
	}
	doc, err := writer.Patch(ctx, venueID, fields)
	if err != nil {
		return VenueProjection{}, err
	}
	projection, err := ProjectionFromDocument(doc)
	if err != nil {
		return VenueProjection{}, err
	}
	if err := s.store.Put(ctx, projection); err != nil {
		return VenueProjection{}, fmt.Errorf("persist updated venue projection: %w", err)
	}
	return projection, nil
}

func (s *Service) Delete(ctx context.Context, venueID string) error {
	writer, ok := s.source.(Writer)
	if !ok {
		return fmt.Errorf("venue writes are unavailable")
	}
	if _, err := writer.SoftDelete(ctx, venueID); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, venueID); err != nil {
		return fmt.Errorf("persist deleted venue projection: %w", err)
	}
	return nil
}

func (s *Service) SourceWatch(ctx context.Context) (ChangeStream, error) {
	return s.source.Watch(ctx)
}
