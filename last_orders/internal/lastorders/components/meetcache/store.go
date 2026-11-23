package meetcache

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

type Snapshot struct {
	Projection      Projection
	ResourceVersion int64
	Sequence        int64
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
		CREATE TABLE IF NOT EXISTS meet_cache (
			meet_id TEXT PRIMARY KEY,
			owner_token TEXT NOT NULL,
			valid_until INTEGER NOT NULL,
			projection_json TEXT,
			resource_version INTEGER NOT NULL DEFAULT 0,
			sequence INTEGER NOT NULL DEFAULT 0,
			firestore_update_time TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		return nil, fmt.Errorf("create Meet projection: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) Admit(ctx context.Context, meetID, owner string, validUntil time.Time) error {
	if meetID == "" || owner == "" {
		return fmt.Errorf("meet ID and owner token are required")
	}
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO meet_cache(meet_id, owner_token, valid_until)
		VALUES(?, ?, ?)
		ON CONFLICT(meet_id) DO UPDATE SET
			owner_token = excluded.owner_token,
			valid_until = excluded.valid_until,
			projection_json = NULL,
			resource_version = 0,
			sequence = 0,
			firestore_update_time = ''
	`, meetID, owner, validUntil.UnixNano())
	if err != nil {
		return fmt.Errorf("record Meet projection admission: %w", err)
	}
	return nil
}

func (s *Store) Apply(ctx context.Context, meetID, owner string, projection Projection, sourceVersion time.Time) (Snapshot, error) {
	if meetID == "" || owner == "" || projection.ID != meetID {
		return Snapshot{}, fmt.Errorf("meet ID, matching projection, and owner token are required")
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode Meet projection: %w", err)
	}
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	var storedOwner, oldSourceVersion string
	var oldJSON sql.NullString
	var resourceVersion, sequence int64
	err = tx.QueryRowContext(ctx, `
		SELECT owner_token, projection_json, resource_version, sequence, firestore_update_time
		FROM meet_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &oldJSON, &resourceVersion, &sequence, &oldSourceVersion)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && storedOwner != owner) {
		return Snapshot{}, ErrRetired
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read Meet projection owner: %w", err)
	}
	if oldSourceVersion != "" && sourceVersion.Before(parseSourceVersion(oldSourceVersion)) {
		return Snapshot{}, ErrStale
	}
	if oldJSON.Valid {
		var previous Projection
		if err := json.Unmarshal([]byte(oldJSON.String), &previous); err != nil {
			return Snapshot{}, fmt.Errorf("decode previous Meet projection: %w", err)
		}
		if previous.Equal(projection) {
			if sourceVersion.After(parseSourceVersion(oldSourceVersion)) {
				if _, err := tx.ExecContext(ctx, `UPDATE meet_cache SET firestore_update_time = ? WHERE meet_id = ? AND owner_token = ?`, formatSourceVersion(sourceVersion), meetID, owner); err != nil {
					return Snapshot{}, fmt.Errorf("advance Meet source version: %w", err)
				}
			}
			if err := tx.Commit(); err != nil {
				return Snapshot{}, err
			}
			return Snapshot{Projection: previous, ResourceVersion: resourceVersion, Sequence: sequence}, nil
		}
	}
	sequence, err = s.base.NextSequence(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	resourceVersion++
	if _, err := tx.ExecContext(ctx, `
		UPDATE meet_cache SET projection_json = ?, resource_version = ?, sequence = ?, firestore_update_time = ?
		WHERE meet_id = ? AND owner_token = ?
	`, string(encoded), resourceVersion, sequence, formatSourceVersion(sourceVersion), meetID, owner); err != nil {
		return Snapshot{}, fmt.Errorf("persist Meet projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, fmt.Errorf("commit Meet projection: %w", err)
	}
	return Snapshot{Projection: projection, ResourceVersion: resourceVersion, Sequence: sequence}, nil
}

func (s *Store) Get(ctx context.Context, meetID, owner string) (Snapshot, error) {
	var storedOwner string
	var encoded sql.NullString
	var version, sequence int64
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT owner_token, projection_json, resource_version, sequence
		FROM meet_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &encoded, &version, &sequence)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && storedOwner != owner) {
		return Snapshot{}, ErrRetired
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read Meet projection: %w", err)
	}
	if !encoded.Valid {
		return Snapshot{}, ErrUnavailable
	}
	var projection Projection
	if err := json.Unmarshal([]byte(encoded.String), &projection); err != nil {
		return Snapshot{}, fmt.Errorf("decode Meet projection: %w", err)
	}
	return Snapshot{Projection: projection, ResourceVersion: version, Sequence: sequence}, nil
}

func (s *Store) DeleteIfOwner(ctx context.Context, meetID, owner string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM meet_cache WHERE meet_id = ? AND owner_token = ?`, meetID, owner)
	return err
}

func (s *Store) Expired(ctx context.Context, now time.Time) ([]ExpiredEntry, error) {
	rows, err := s.base.DB().QueryContext(ctx, `
		SELECT meet_id, owner_token FROM meet_cache WHERE valid_until <= ? ORDER BY valid_until, meet_id
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

func parseSourceVersion(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func formatSourceVersion(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

var ErrRetired = errors.New("meet projection entry retired")
var ErrStale = errors.New("stale Meet source update")
