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
	db := base.DB()
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS vote_cache (
			meet_id TEXT PRIMARY KEY,
			owner_token TEXT NOT NULL,
			valid_until INTEGER NOT NULL,
			projection_json TEXT,
			revision TEXT NOT NULL DEFAULT '',
			sequence INTEGER NOT NULL DEFAULT 0
		);
	`); err != nil {
		return nil, fmt.Errorf("create voting projection: %w", err)
	}
	for _, migration := range []struct {
		column string
		sql    string
	}{
		{"revision", `ALTER TABLE vote_cache ADD COLUMN revision TEXT NOT NULL DEFAULT ''`},
		{"sequence", `ALTER TABLE vote_cache ADD COLUMN sequence INTEGER NOT NULL DEFAULT 0`},
	} {
		var count int
		if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pragma_table_info('vote_cache') WHERE name = ?`, migration.column).Scan(&count); err != nil {
			return nil, fmt.Errorf("inspect voting projection columns: %w", err)
		}
		if count == 0 {
			if _, err := db.ExecContext(context.Background(), migration.sql); err != nil {
				return nil, fmt.Errorf("migrate voting projection %s column: %w", migration.column, err)
			}
		}
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
			projection_json = NULL,
			revision = '',
			sequence = 0
	`, meetID, owner, validUntil.UnixNano())
	if err != nil {
		return fmt.Errorf("record voting projection admission: %w", err)
	}
	return nil
}

func (s *Store) Apply(ctx context.Context, meetID, owner string, projection Projection, sourceVersion time.Time) (Snapshot, error) {
	if meetID == "" || owner == "" || projection.Votes == nil {
		return Snapshot{}, fmt.Errorf("Meet ID, owner token, and vote projection are required")
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode voting projection: %w", err)
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
		SELECT owner_token, projection_json, revision, sequence FROM vote_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &oldJSON, &oldRevision, &sequence); err != nil {
		if errors.Is(err, sql.ErrNoRows) || storedOwner != owner {
			return Snapshot{}, ErrRetired
		}
		return Snapshot{}, fmt.Errorf("read voting projection owner: %w", err)
	}
	parsedOldRevision := parseRevision(oldRevision)
	if !sourceVersion.IsZero() && !parsedOldRevision.IsZero() && sourceVersion.Before(parsedOldRevision) {
		return Snapshot{}, ErrStale
	}
	if oldJSON.Valid {
		var previous Projection
		if err := json.Unmarshal([]byte(oldJSON.String), &previous); err != nil {
			return Snapshot{}, fmt.Errorf("decode previous voting projection: %w", err)
		}
		if mapsEqual(previous.Votes, projection.Votes) {
			if !sourceVersion.IsZero() && sourceVersion.After(parsedOldRevision) {
				if _, err := tx.ExecContext(ctx, `UPDATE vote_cache SET revision = ? WHERE meet_id = ? AND owner_token = ?`, formatRevision(sourceVersion), meetID, owner); err != nil {
					return Snapshot{}, fmt.Errorf("advance voting source revision: %w", err)
				}
			}
			if err := tx.Commit(); err != nil {
				return Snapshot{}, err
			}
			returnSnapshotRevision := revisionPointer(parsedOldRevision)
			if sourceVersion.After(parsedOldRevision) {
				returnSnapshotRevision = revisionPointer(sourceVersion)
			}
			return Snapshot{Votes: previous.Votes, Revision: returnSnapshotRevision, Sequence: sequence}, nil
		}
	}
	sequence, err = s.base.NextSequence(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE vote_cache SET projection_json = ?, revision = ?, sequence = ? WHERE meet_id = ? AND owner_token = ?
	`, string(encoded), formatRevision(sourceVersion), sequence, meetID, owner); err != nil {
		return Snapshot{}, fmt.Errorf("persist voting projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, fmt.Errorf("commit voting projection: %w", err)
	}
	var revision *time.Time
	if !sourceVersion.IsZero() {
		copy := sourceVersion.UTC()
		revision = &copy
	}
	return Snapshot{Votes: projection.Votes, Revision: revision, Sequence: sequence}, nil
}

func (s *Store) Get(ctx context.Context, meetID, owner string) (Snapshot, error) {
	var storedOwner string
	var encoded sql.NullString
	var revisionText string
	var sequence int64
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT owner_token, projection_json, revision, sequence FROM vote_cache WHERE meet_id = ?
	`, meetID).Scan(&storedOwner, &encoded, &revisionText, &sequence)
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
	parsedRevision := parseRevision(revisionText)
	return Snapshot{Votes: projection.Votes, Revision: revisionPointer(parsedRevision), Sequence: sequence}, nil
}

func mapsEqual(left, right map[string][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValues := range left {
		rightValues, ok := right[key]
		if !ok || len(leftValues) != len(rightValues) {
			return false
		}
		for index, value := range leftValues {
			if rightValues[index] != value {
				return false
			}
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
