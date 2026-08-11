package firebaseidempotency

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"last_orders/internal/lastorders/basestore"
)

type State string

const (
	StatePending State = "PENDING"
	StatePushed  State = "PUSHED"
	StatePresent State = "PRESENT"
)

type Store struct {
	base *basestore.Store
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	db := base.DB()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS firebase_idempotency_records (
			listener TEXT NOT NULL,
			event_key TEXT NOT NULL,
			state TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(listener, event_key)
		);
	`); err != nil {
		return nil, fmt.Errorf("create firebase_idempotency_records schema: %w", err)
	}

	return &Store{base: base}, nil
}

func (s *Store) CurrentState(ctx context.Context, listener, eventKey string) (State, bool, error) {
	var state string
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT state
		FROM firebase_idempotency_records
		WHERE listener = ? AND event_key = ?
	`, listener, eventKey).Scan(&state)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return State(state), true, nil
}

func (s *Store) EnsurePending(ctx context.Context, listener, eventKey string) (State, error) {
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO firebase_idempotency_records(listener, event_key, state, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(listener, event_key) DO UPDATE SET
			updated_at = excluded.updated_at
	`, listener, eventKey, StatePending, time.Now().UTC())
	if err != nil {
		return "", err
	}
	state, _, err := s.CurrentState(ctx, listener, eventKey)
	if err != nil {
		return "", err
	}
	return state, nil
}

func (s *Store) MarkPushedUnlessPresent(ctx context.Context, listener, eventKey string) (State, error) {
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO firebase_idempotency_records(listener, event_key, state, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(listener, event_key) DO UPDATE SET
			state = CASE
				WHEN firebase_idempotency_records.state = ? THEN firebase_idempotency_records.state
				ELSE excluded.state
			END,
			updated_at = excluded.updated_at
	`, listener, eventKey, StatePushed, time.Now().UTC(), StatePresent)
	if err != nil {
		return "", err
	}
	state, _, err := s.CurrentState(ctx, listener, eventKey)
	if err != nil {
		return "", err
	}
	return state, nil
}

func (s *Store) MarkPresent(ctx context.Context, listener, eventKey string) error {
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO firebase_idempotency_records(listener, event_key, state, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(listener, event_key) DO UPDATE SET
			state = excluded.state,
			updated_at = excluded.updated_at
	`, listener, eventKey, StatePresent, time.Now().UTC())
	return err
}

func (s *Store) MarkPresentFromPushed(ctx context.Context, listener, eventKey string) (bool, error) {
	res, err := s.base.DB().ExecContext(ctx, `
		UPDATE firebase_idempotency_records
		SET state = ?, updated_at = ?
		WHERE listener = ? AND event_key = ? AND state = ?
	`, StatePresent, time.Now().UTC(), listener, eventKey, StatePushed)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (s *Store) ForceState(ctx context.Context, listener, eventKey string, state State) error {
	_, err := s.base.DB().ExecContext(ctx, `
		INSERT INTO firebase_idempotency_records(listener, event_key, state, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(listener, event_key) DO UPDATE SET
			state = excluded.state,
			updated_at = excluded.updated_at
	`, listener, eventKey, state, time.Now().UTC())
	return err
}
