package venuecache

import (
	"context"
	"database/sql"
	"fmt"

	"last_orders/internal/lastorders/basestore"
)

// Store provides access to venue cache data in the database.
// FIXME arguably this package belongs under database/projections/venuecache
type Store struct {
	base *basestore.Store
}

type Mutation struct {
	ID         string
	Projection *VenueProjection
}

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if err := migrate(context.Background(), base); err != nil {
		return nil, err
	}
	return &Store{base: base}, nil
}

func (s *Store) Get(ctx context.Context, venueID string) (VenueProjection, error) {
	var projection VenueProjection
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT venue_id, name, venue_type, website, map, address, photo_url,
			recurrence_json, next_occurrence_date, notes, parking, food,
			dog_friendly, beer_garden, out_of_town, banned
		FROM venue_cache WHERE venue_id = ? AND deleted = 0
	`, venueID).Scan(
		&projection.ID, &projection.Name, &projection.VenueType, &projection.Website,
		&projection.Map, &projection.Address, &projection.PhotoURL,
		&projection.RecurrenceJSON, &projection.NextOccurrenceDate,
		&projection.Notes, &projection.Parking, &projection.Food,
		&projection.DogFriendly, &projection.BeerGarden, &projection.OutOfTown,
		&projection.Banned,
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
	return put(ctx, s.base.DB(), projection)
}

// ApplyBatch applies one source batch atomically. A baseline replaces the
// complete venue projection; a delta applies only its listed mutations.
func (s *Store) ApplyBatch(ctx context.Context, mutations []Mutation, baseline bool) error {
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if baseline {
		if _, err := tx.ExecContext(ctx, `DELETE FROM venue_cache`); err != nil {
			return err
		}
	}
	for _, mutation := range mutations {
		if mutation.ID == "" {
			return fmt.Errorf("venue ID is required")
		}
		if mutation.Projection == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM venue_cache WHERE venue_id = ?`, mutation.ID); err != nil {
				return err
			}
			continue
		}
		if mutation.Projection.ID != mutation.ID {
			return fmt.Errorf("venue mutation ID %q does not match projection ID %q", mutation.ID, mutation.Projection.ID)
		}
		if err := put(ctx, tx, *mutation.Projection); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func put(ctx context.Context, execer contextExecer, projection VenueProjection) error {
	_, err := execer.ExecContext(ctx, `
		INSERT INTO venue_cache(
			venue_id, name, venue_type, website, map, address, photo_url,
			recurrence_json, next_occurrence_date, notes, parking, food,
			dog_friendly, beer_garden, out_of_town, banned
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(venue_id) DO UPDATE SET
			name = excluded.name,
			venue_type = excluded.venue_type,
			website = excluded.website,
			map = excluded.map,
			address = excluded.address,
			photo_url = excluded.photo_url,
			recurrence_json = excluded.recurrence_json,
			next_occurrence_date = excluded.next_occurrence_date,
			notes = excluded.notes,
			parking = excluded.parking,
			food = excluded.food,
			dog_friendly = excluded.dog_friendly,
			beer_garden = excluded.beer_garden,
			out_of_town = excluded.out_of_town,
			banned = excluded.banned,
			deleted = 0
	`, projection.ID, projection.Name, projection.VenueType, projection.Website,
		projection.Map, projection.Address, projection.PhotoURL,
		projection.RecurrenceJSON, projection.NextOccurrenceDate, projection.Notes,
		projection.Parking, projection.Food, projection.DogFriendly,
		projection.BeerGarden, projection.OutOfTown, projection.Banned)
	return err
}

func (s *Store) Delete(ctx context.Context, venueID string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM venue_cache WHERE venue_id = ?`, venueID)
	return err
}
