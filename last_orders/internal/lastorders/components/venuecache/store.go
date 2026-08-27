package venuecache

import (
	"context"
	"database/sql"
	"fmt"

	"last_orders/internal/lastorders/basestore"
)

type Store struct {
	base *basestore.Store
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if _, err := base.DB().Exec(`
		CREATE TABLE IF NOT EXISTS venue_cache (
			venue_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			venue_type TEXT NOT NULL DEFAULT '',
			website TEXT NOT NULL DEFAULT '',
			map TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			photo_url TEXT NOT NULL DEFAULT '',
			recurrence_json TEXT NOT NULL DEFAULT '',
			next_occurrence_date TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		return nil, fmt.Errorf("create venue_cache schema: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) Get(ctx context.Context, venueID string) (VenueProjection, error) {
	var projection VenueProjection
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT venue_id, name, venue_type, website, map, address, photo_url,
			recurrence_json, next_occurrence_date
		FROM venue_cache WHERE venue_id = ?
	`, venueID).Scan(
		&projection.ID, &projection.Name, &projection.VenueType, &projection.Website,
		&projection.Map, &projection.Address, &projection.PhotoURL,
		&projection.RecurrenceJSON, &projection.NextOccurrenceDate,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return VenueProjection{}, ErrCacheMiss
		}
		return VenueProjection{}, err
	}
	return projection, nil
}

func (s *Store) Put(ctx context.Context, projection VenueProjection) error {
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO venue_cache(
			venue_id, name, venue_type, website, map, address, photo_url,
			recurrence_json, next_occurrence_date
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(venue_id) DO UPDATE SET
			name = excluded.name,
			venue_type = excluded.venue_type,
			website = excluded.website,
			map = excluded.map,
			address = excluded.address,
			photo_url = excluded.photo_url,
			recurrence_json = excluded.recurrence_json,
			next_occurrence_date = excluded.next_occurrence_date
	`, projection.ID, projection.Name, projection.VenueType, projection.Website,
		projection.Map, projection.Address, projection.PhotoURL,
		projection.RecurrenceJSON, projection.NextOccurrenceDate)
	return err
}

func (s *Store) Delete(ctx context.Context, venueID string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM venue_cache WHERE venue_id = ?`, venueID)
	return err
}
