package attendancecache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	_, err := base.DB().ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS attendance_cache (
			meet_id TEXT PRIMARY KEY,
			owner_token TEXT NOT NULL,
			valid_until INTEGER NOT NULL,
			projection_json TEXT,
			revision TEXT NOT NULL DEFAULT '',
			sequence INTEGER NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("create attendance projection: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) Admit(ctx context.Context, meetID, owner string, validUntil time.Time) error {
	if meetID == "" || owner == "" {
		return fmt.Errorf("meet ID and owner token are required")
	}
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO attendance_cache(meet_id, owner_token, valid_until)
		VALUES(?, ?, ?)
		ON CONFLICT(meet_id) DO UPDATE SET
			owner_token = excluded.owner_token,
			valid_until = excluded.valid_until,
			projection_json = NULL,
			revision = '',
			sequence = 0
	`, meetID, owner, validUntil.UnixNano())
	if err != nil {
		return fmt.Errorf("record attendance projection admission: %w", err)
	}
	return nil
}

func (s *Store) Apply(ctx context.Context, meetID, owner string, projection Projection, sourceVersion time.Time) (Snapshot, error) {
	if meetID == "" || owner == "" || projection.Entries == nil {
		return Snapshot{}, fmt.Errorf("meet ID, owner token, and attendance entries are required")
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode attendance projection: %w", err)
	}
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	var storedOwner, oldRevision string
	var oldJSON sql.NullString
	var sequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT owner_token, projection_json, revision, sequence FROM attendance_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &oldJSON, &oldRevision, &sequence); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, ErrRetired
		}
		return Snapshot{}, fmt.Errorf("read attendance projection owner: %w", err)
	}
	if storedOwner != owner {
		return Snapshot{}, ErrRetired
	}
	previousRevision := parseRevision(oldRevision)
	if !sourceVersion.IsZero() && !previousRevision.IsZero() && sourceVersion.Before(previousRevision) {
		return Snapshot{}, ErrStale
	}
	if oldJSON.Valid {
		var previous Projection
		if err := json.Unmarshal([]byte(oldJSON.String), &previous); err != nil {
			return Snapshot{}, fmt.Errorf("decode previous attendance projection: %w", err)
		}
		if entriesEqual(previous.Entries, projection.Entries) {
			if !sourceVersion.IsZero() && sourceVersion.After(previousRevision) {
				if _, err := tx.ExecContext(ctx, `UPDATE attendance_cache SET revision = ? WHERE meet_id = ? AND owner_token = ?`, formatRevision(sourceVersion), meetID, owner); err != nil {
					return Snapshot{}, fmt.Errorf("advance attendance source revision: %w", err)
				}
			}
			if err := tx.Commit(); err != nil {
				return Snapshot{}, err
			}
			if sourceVersion.After(previousRevision) {
				previousRevision = sourceVersion
			}
			return Snapshot{Entries: previous.Entries, Revision: revisionPointer(previousRevision), Sequence: sequence}, nil
		}
	}
	sequence, err = s.base.NextSequence(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE attendance_cache SET projection_json = ?, revision = ?, sequence = ? WHERE meet_id = ? AND owner_token = ?
	`, string(encoded), formatRevision(sourceVersion), sequence, meetID, owner); err != nil {
		return Snapshot{}, fmt.Errorf("persist attendance projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, fmt.Errorf("commit attendance projection: %w", err)
	}
	return Snapshot{Entries: projection.Entries, Revision: revisionPointer(sourceVersion), Sequence: sequence}, nil
}

func (s *Store) Get(ctx context.Context, meetID, owner string) (Snapshot, error) {
	var storedOwner, revisionText string
	var encoded sql.NullString
	var sequence int64
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT owner_token, projection_json, revision, sequence FROM attendance_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &encoded, &revisionText, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrRetired
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read attendance projection: %w", err)
	}
	if storedOwner != owner {
		return Snapshot{}, ErrRetired
	}
	if !encoded.Valid {
		return Snapshot{}, ErrUnavailable
	}
	var projection Projection
	if err := json.Unmarshal([]byte(encoded.String), &projection); err != nil {
		return Snapshot{}, fmt.Errorf("decode attendance projection: %w", err)
	}
	if projection.Entries == nil {
		return Snapshot{}, fmt.Errorf("decoded attendance projection has no entries map")
	}
	return Snapshot{Entries: projection.Entries, Revision: revisionPointer(parseRevision(revisionText)), Sequence: sequence}, nil
}

func entriesEqual(left, right map[string]Entry) bool {
	if len(left) != len(right) {
		return false
	}
	for venueID, leftEntry := range left {
		rightEntry, ok := right[venueID]
		if !ok || !slices.Equal(leftEntry.CanCome, rightEntry.CanCome) || !slices.Equal(leftEntry.CannotCome, rightEntry.CannotCome) || !maps.Equal(leftEntry.ETA, rightEntry.ETA) {
			return false
		}
	}
	return true
}

func parseRevision(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func formatRevision(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func revisionPointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func (s *Store) DeleteIfOwner(ctx context.Context, meetID, owner string) error {
	_, err := s.base.DB().ExecContext(ctx, `DELETE FROM attendance_cache WHERE meet_id = ? AND owner_token = ?`, meetID, owner)
	return err
}

func (s *Store) Expired(ctx context.Context, now time.Time) ([]ExpiredEntry, error) {
	rows, err := s.base.DB().QueryContext(ctx, `
		SELECT meet_id, owner_token FROM attendance_cache WHERE valid_until <= ? ORDER BY valid_until, meet_id
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
