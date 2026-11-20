package currentmeetcache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/meetcache"
	"last_orders/internal/lastorders/components/venuecache"
)

type Store struct {
	base *basestore.Store
}

type ExpiredEntry struct {
	GroupID string
	Date    string
	Owner   string
}

func NewStore(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if _, err := base.DB().ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS current_meet_cache (
			group_id TEXT PRIMARY KEY,
			owner_token TEXT NOT NULL,
			view_date TEXT NOT NULL,
			valid_until INTEGER NOT NULL,
			projections_json TEXT,
			firestore_read_time TEXT NOT NULL DEFAULT '',
			resource_version INTEGER NOT NULL DEFAULT 0,
			sequence INTEGER NOT NULL DEFAULT 0
		);
	`); err != nil {
		return nil, fmt.Errorf("create current-Meets projection: %w", err)
	}
	if _, err := base.DB().ExecContext(context.Background(), `DELETE FROM current_meet_cache`); err != nil {
		return nil, fmt.Errorf("discard current-Meets projections from previous process: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) Admit(ctx context.Context, groupID, date, owner string, validUntil time.Time) error {
	if groupID == "" || date == "" || owner == "" {
		return fmt.Errorf("group ID, view date, and owner token are required")
	}
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO current_meet_cache(group_id, owner_token, view_date, valid_until)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(group_id) DO UPDATE SET
			owner_token = excluded.owner_token,
			view_date = excluded.view_date,
			valid_until = excluded.valid_until,
			projections_json = NULL,
			firestore_read_time = '',
			resource_version = 0,
			sequence = 0
	`, groupID, owner, date, validUntil.UnixNano())
	if err != nil {
		return fmt.Errorf("record current-Meets admission: %w", err)
	}
	return nil
}

func (s *Store) Apply(ctx context.Context, groupID, date, owner string, meets []meetcache.Projection, readTime time.Time) (int64, bool, error) {
	encoded, err := json.Marshal(meets)
	if err != nil {
		return 0, false, fmt.Errorf("encode current-Meets projection: %w", err)
	}
	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	var storedOwner, storedDate, oldReadTime string
	var oldJSON sql.NullString
	var resourceVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT owner_token, view_date, projections_json, firestore_read_time, resource_version
		FROM current_meet_cache WHERE group_id = ?
	`, groupID).Scan(&storedOwner, &storedDate, &oldJSON, &oldReadTime, &resourceVersion)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (storedOwner != owner || storedDate != date)) {
		return 0, false, ErrRetired
	}
	if err != nil {
		return 0, false, fmt.Errorf("read current-Meets owner: %w", err)
	}
	if oldReadTime != "" && readTime.Before(parseReadTime(oldReadTime)) {
		return 0, false, nil
	}
	if oldJSON.Valid {
		var previous []meetcache.Projection
		if err := json.Unmarshal([]byte(oldJSON.String), &previous); err != nil {
			return 0, false, fmt.Errorf("decode current-Meets projection: %w", err)
		}
		if equalMeets(previous, meets) {
			if readTime.After(parseReadTime(oldReadTime)) {
				if _, err := tx.ExecContext(ctx, `UPDATE current_meet_cache SET firestore_read_time = ? WHERE group_id = ? AND owner_token = ? AND view_date = ?`, formatReadTime(readTime), groupID, owner, date); err != nil {
					return 0, false, fmt.Errorf("advance current-Meets read time: %w", err)
				}
			}
			if err := tx.Commit(); err != nil {
				return 0, false, err
			}
			return 0, false, nil
		}
	}
	sequence, err := s.base.NextSequence(ctx, tx)
	if err != nil {
		return 0, false, err
	}
	resourceVersion++
	if _, err := tx.ExecContext(ctx, `
		UPDATE current_meet_cache SET projections_json = ?, firestore_read_time = ?,
			resource_version = ?, sequence = ?
		WHERE group_id = ? AND owner_token = ? AND view_date = ?
	`, string(encoded), formatReadTime(readTime), resourceVersion, sequence, groupID, owner, date); err != nil {
		return 0, false, fmt.Errorf("persist current-Meets projection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("commit current-Meets projection: %w", err)
	}
	return sequence, true, nil
}

func (s *Store) Snapshot(ctx context.Context, groupID, owner string, venues *venuecache.Store) (Snapshot, error) {
	if venues == nil {
		return Snapshot{}, ErrUnavailable
	}
	tx, err := s.base.DB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	var storedOwner, date string
	var encoded sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT owner_token, view_date, projections_json
		FROM current_meet_cache WHERE group_id = ?
	`, groupID).Scan(&storedOwner, &date, &encoded)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && storedOwner != owner) {
		return Snapshot{}, ErrRetired
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read current-Meets projection: %w", err)
	}
	if !encoded.Valid {
		return Snapshot{}, ErrUnavailable
	}
	var projections []meetcache.Projection
	if err := json.Unmarshal([]byte(encoded.String), &projections); err != nil {
		return Snapshot{}, fmt.Errorf("decode current-Meets projection: %w", err)
	}
	result := Snapshot{Date: date, Meets: make([]MeetView, 0, len(projections))}
	for _, projection := range projections {
		view := MeetView{Meet: projection}
		if projection.SelectedVenueID != "" {
			venue, exists, err := venues.GetForMeetInTx(ctx, tx, projection.SelectedVenueID)
			if err != nil {
				return Snapshot{}, fmt.Errorf("read selected Venue %q: %w", projection.SelectedVenueID, err)
			}
			if !exists {
				view.MissingVenueID = projection.SelectedVenueID
			} else {
				venueType := venue.VenueType
				if venueType == "" {
					venueType = "pub"
				}
				status := "active"
				if venue.DeletedOn != "" {
					status = "deleted"
				}
				view.Venue = &VenueSummary{
					ID: venue.ID, Status: status, Name: venue.Name, Address: venue.Address,
					WebsiteURL: optionalString(venue.Website), ImageURL: optionalString(venue.PhotoURL), VenueType: venueType,
				}
			}
		}
		result.Meets = append(result.Meets, view)
	}
	state, err := s.base.ReadSyncState(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	result.Generation = state.Generation
	result.Sequence = state.Sequence
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

func (s *Store) Renew(ctx context.Context, groupID, date, owner string, validUntil time.Time) error {
	result, err := s.base.DB().ExecContext(ctx, `
		UPDATE current_meet_cache SET valid_until = ?
		WHERE group_id = ? AND view_date = ? AND owner_token = ?
	`, validUntil.UnixNano(), groupID, date, owner)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrRetired
	}
	return nil
}

func (s *Store) Expired(ctx context.Context, now time.Time) ([]ExpiredEntry, error) {
	rows, err := s.base.DB().QueryContext(ctx, `
		SELECT group_id, view_date, owner_token FROM current_meet_cache
		WHERE valid_until <= ? ORDER BY valid_until, group_id
	`, now.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]ExpiredEntry, 0)
	for rows.Next() {
		var entry ExpiredEntry
		if err := rows.Scan(&entry.GroupID, &entry.Date, &entry.Owner); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) DeleteIfOwner(ctx context.Context, entry ExpiredEntry) error {
	_, err := s.base.DB().ExecContext(ctx, `
		DELETE FROM current_meet_cache WHERE group_id = ? AND view_date = ? AND owner_token = ?
	`, entry.GroupID, entry.Date, entry.Owner)
	return err
}

func equalMeets(left, right []meetcache.Projection) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Equal(right[index]) {
			return false
		}
	}
	return true
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func parseReadTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func formatReadTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
