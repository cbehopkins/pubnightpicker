package venuecache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

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
	if err := s.store.Put(ctx, projection); err != nil {
		s.logger.Warn("populate venue cache failed", "venue_id", venueID, "err", err)
	}
	return projection, nil
}

func (s *Service) SourceWatch(ctx context.Context) (ChangeStream, error) {
	return s.source.Watch(ctx)
}
