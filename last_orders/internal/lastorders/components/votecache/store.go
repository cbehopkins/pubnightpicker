package votecache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"last_orders/internal/lastorders/basestore"
)

type Store struct {
	base *basestore.Store
}

type ExpiredEntry struct {
	ID    string
	Owner string
}

func NewStore(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if _, err := base.DB().ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS vote_cache (
			meet_id TEXT PRIMARY KEY,
			owner_token TEXT NOT NULL,
			valid_until INTEGER NOT NULL,
			projection_json TEXT
		);
	`); err != nil {
		return nil, fmt.Errorf("create voting projection: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) Admit(ctx context.Context, meetID, owner string, validUntil time.Time) error {
	if meetID == "" || owner == "" {
		return fmt.Errorf("Meet ID and owner token are required")
	}
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO vote_cache(meet_id, owner_token, valid_until)
		VALUES(?, ?, ?)
		ON CONFLICT(meet_id) DO UPDATE SET
			owner_token = excluded.owner_token,
			valid_until = excluded.valid_until,
			projection_json = NULL
	`, meetID, owner, validUntil.UnixNano())
	if err != nil {
		return fmt.Errorf("record voting projection admission: %w", err)
	}
	return nil
}

func (s *Store) Apply(ctx context.Context, meetID, owner string, projection Projection) error {
	if meetID == "" || owner == "" || projection.Votes == nil {
		return fmt.Errorf("Meet ID, owner token, and vote projection are required")
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return fmt.Errorf("encode voting projection: %w", err)
	}
	result, err := s.base.DB().ExecContext(ctx, `
		UPDATE vote_cache SET projection_json = ? WHERE meet_id = ? AND owner_token = ?
	`, string(encoded), meetID, owner)
	if err != nil {
		return fmt.Errorf("persist voting projection: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm voting projection owner: %w", err)
	}
	if rows != 1 {
		return ErrRetired
	}
	return nil
}

func (s *Store) Get(ctx context.Context, meetID, owner string) (Snapshot, error) {
	var storedOwner string
	var encoded sql.NullString
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT owner_token, projection_json FROM vote_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &encoded)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && storedOwner != owner) {
		return Snapshot{}, ErrRetired
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read voting projection: %w", err)
	}
	if !encoded.Valid {
		return Snapshot{}, ErrUnavailable
	}
	var projection Projection
	if err := json.Unmarshal([]byte(encoded.String), &projection); err != nil {
		return Snapshot{}, fmt.Errorf("decode voting projection: %w", err)
	}
	if projection.Votes == nil {
		return Snapshot{}, fmt.Errorf("decoded voting projection has no vote map")
	}
	return Snapshot{Votes: projection.Votes}, nil
}

func (s *Store) DeleteIfOwner(ctx context.Context, meetID, owner string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM vote_cache WHERE meet_id = ? AND owner_token = ?`, meetID, owner)
	return err
}

func (s *Store) Expired(ctx context.Context, now time.Time) ([]ExpiredEntry, error) {
	rows, err := s.base.DB().QueryContext(ctx, `
		SELECT meet_id, owner_token FROM vote_cache WHERE valid_until <= ? ORDER BY valid_until, meet_id
	`, now.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]ExpiredEntry, 0)
	for rows.Next() {
		var entry ExpiredEntry
		if err := rows.Scan(&entry.ID, &entry.Owner); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
