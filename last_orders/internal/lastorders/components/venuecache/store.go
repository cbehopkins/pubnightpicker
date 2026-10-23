package venuecache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

type contextQueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
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
	projection, deleted, err := readVenue(ctx, s.base.DB(), venueID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && deleted) {
		return VenueProjection{}, ErrCacheMiss
	}
	if err != nil {
		return VenueProjection{}, err
	}
	return projection, nil
}

func readVenue(ctx context.Context, queryer contextQueryRower, venueID string) (VenueProjection, bool, error) {
	var projection VenueProjection
	var deleted bool
	err := queryer.QueryRowContext(ctx, `
		SELECT venue_id, name, venue_type, website, map, address, photo_url,
			recurrence_json, next_occurrence_date, notes, parking, food,
			dog_friendly, beer_garden, out_of_town, banned, deleted
		FROM venue_cache WHERE venue_id = ?
	`, venueID).Scan(
		&projection.ID, &projection.Name, &projection.VenueType, &projection.Website,
		&projection.Map, &projection.Address, &projection.PhotoURL,
		&projection.RecurrenceJSON, &projection.NextOccurrenceDate,
		&projection.Notes, &projection.Parking, &projection.Food,
		&projection.DogFriendly, &projection.BeerGarden, &projection.OutOfTown,
		&projection.Banned, &deleted,
	)
	return projection, deleted, err
}

func (s *Store) Put(ctx context.Context, projection VenueProjection) error {
	return s.ApplyBatch(ctx, []Mutation{{ID: projection.ID, Projection: &projection}}, false)
}

// ApplyBatch applies one source batch atomically. A baseline replaces the
// complete venue projection; a delta applies only its listed mutations.
func (s *Store) ApplyBatch(ctx context.Context, mutations []Mutation, baseline bool) error {
	normalised := make([]Mutation, len(mutations))
	retained := make(map[string]bool, len(mutations))
	for index, mutation := range mutations {
		if mutation.ID == "" {
			return fmt.Errorf("venue ID is required")
		}
		if baseline {
			if _, exists := retained[mutation.ID]; exists {
				return fmt.Errorf("duplicate baseline venue ID %q", mutation.ID)
			}
			retained[mutation.ID] = mutation.Projection != nil
		}
		normalised[index] = mutation
		if mutation.Projection != nil {
			if mutation.Projection.ID != mutation.ID {
				return fmt.Errorf("venue mutation ID %q does not match projection ID %q", mutation.ID, mutation.Projection.ID)
			}
			projection, err := normaliseVenue(*mutation.Projection)
			if err != nil {
				return err
			}
			normalised[index].Projection = &projection
		}
	}
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if baseline {
		rows, err := tx.QueryContext(ctx, `SELECT venue_id FROM venue_cache WHERE deleted = 0 ORDER BY venue_id`)
		if err != nil {
			return err
		}
		var missing []string
		for rows.Next() {
			var venueID string
			if err := rows.Scan(&venueID); err != nil {
				rows.Close()
				return err
			}
			if !retained[venueID] {
				missing = append(missing, venueID)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, venueID := range missing {
			if err := s.remove(ctx, tx, venueID); err != nil {
				return err
			}
		}
	}
	for _, mutation := range normalised {
		if mutation.Projection == nil {
			if err := s.remove(ctx, tx, mutation.ID); err != nil {
				return err
			}
			continue
		}
		if err := s.put(ctx, tx, *mutation.Projection); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) put(ctx context.Context, tx *sql.Tx, projection VenueProjection) error {
	current, deleted, err := readVenue(ctx, tx, projection.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && !deleted && current.Equal(projection) {
		return nil
	}
	sequence, err := s.base.NextSequence(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO venue_cache(
			venue_id, name, venue_type, website, map, address, photo_url,
			recurrence_json, next_occurrence_date, notes, parking, food,
			dog_friendly, beer_garden, out_of_town, banned, change_version
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			change_version = excluded.change_version,
			deleted = 0
	`, projection.ID, projection.Name, projection.VenueType, projection.Website,
		projection.Map, projection.Address, projection.PhotoURL,
		projection.RecurrenceJSON, projection.NextOccurrenceDate, projection.Notes,
		projection.Parking, projection.Food, projection.DogFriendly,
		projection.BeerGarden, projection.OutOfTown, projection.Banned, sequence)
	return err
}

func (s *Store) Delete(ctx context.Context, venueID string) error {
	return s.ApplyBatch(ctx, []Mutation{{ID: venueID}}, false)
}

func (s *Store) remove(ctx context.Context, tx *sql.Tx, venueID string) error {
	var deleted bool
	err := tx.QueryRowContext(ctx, `SELECT deleted FROM venue_cache WHERE venue_id = ?`, venueID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && deleted) {
		return nil
	}
	if err != nil {
		return err
	}
	sequence, err := s.base.NextSequence(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE venue_cache SET deleted = 1, change_version = ? WHERE venue_id = ?`, sequence, venueID)
	return err
}

func normaliseVenue(projection VenueProjection) (VenueProjection, error) {
	if projection.ID == "" || projection.Name == "" {
		return VenueProjection{}, fmt.Errorf("venue ID and name are required")
	}
	if projection.RecurrenceJSON != "" {
		var err error
		projection.RecurrenceJSON, err = normaliseRecurrence(json.RawMessage(projection.RecurrenceJSON))
		if err != nil {
			return VenueProjection{}, err
		}
	}
	return projection, nil
}
