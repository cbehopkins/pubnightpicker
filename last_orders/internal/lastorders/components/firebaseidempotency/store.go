package firebaseidempotency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
)

// Store is the local durable cache of Firebase idempotency identities.
//
// The Cell Sequence's own step cursor is the processing state machine (see
// docs/cdd/0001-idempotency.md §5/§16); this table only records whether an identity
// has already been claimed.
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
			owner TEXT,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(listener, event_key)
		);
	`); err != nil {
		return nil, fmt.Errorf("create firebase_idempotency_records schema: %w", err)
	}
	var ownerColumn int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('firebase_idempotency_records') WHERE name = 'owner'
	`).Scan(&ownerColumn); err != nil {
		return nil, fmt.Errorf("inspect firebase_idempotency_records owner column: %w", err)
	}
	if ownerColumn == 0 {
		if _, err := db.Exec(`ALTER TABLE firebase_idempotency_records ADD COLUMN owner TEXT`); err != nil {
			return nil, fmt.Errorf("add firebase_idempotency_records owner column: %w", err)
		}
	}

	return &Store{base: base}, nil
}

// Exists reports whether the identity has already been claimed.
func (s *Store) Exists(ctx context.Context, listener, eventKey string) (bool, error) {
	var found int
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT 1
		FROM firebase_idempotency_records
		WHERE listener = ? AND event_key = ?
	`, listener, eventKey).Scan(&found)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Store) LookupOwner(ctx context.Context, listener, eventKey string) (string, bool, error) {
	var owner sql.NullString
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT owner
		FROM firebase_idempotency_records
		WHERE listener = ? AND event_key = ?
	`, listener, eventKey).Scan(&owner)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return owner.String, true, nil
}

// InsertUnlessExistsWork claims the identity as part of the Check step's transaction.
// It rejects the transition if a concurrent claim already won, so the loser retries
// and observes the identity as already-claimed.
func (s *Store) InsertUnlessExistsWork(listener, eventKey, owner string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		if err := tx.Exec(`
			INSERT INTO firebase_idempotency_records(listener, event_key, owner, updated_at)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(listener, event_key) DO NOTHING
		`, listener, eventKey, owner, time.Now().UTC()); err != nil {
			return err
		}

		var changed int64
		if err := tx.QueryRow(`SELECT changes()`).Scan(&changed); err != nil {
			return err
		}
		if changed != 1 {
			return ErrClaimRejected
		}
		return nil
	}
}

func (s *Store) RetireCacheEntryWork(listener, eventKey, owner string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		if listener == "" || eventKey == "" || owner == "" {
			return ErrCacheOwnerMismatch
		}
		if err := tx.Exec(`
			DELETE FROM firebase_idempotency_records
			WHERE listener = ? AND event_key = ? AND owner = ?
		`, listener, eventKey, owner); err != nil {
			return err
		}
		var changed int64
		if err := tx.QueryRow(`SELECT changes()`).Scan(&changed); err != nil {
			return err
		}
		if changed != 1 {
			return ErrCacheOwnerMismatch
		}
		return nil
	}
}

var ErrClaimRejected = fmt.Errorf("idempotency claim rejected")
var ErrCacheOwnerMismatch = fmt.Errorf("idempotency cache owner mismatch")
