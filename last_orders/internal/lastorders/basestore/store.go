package basestore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

type SyncState struct {
	Generation string
	Sequence   int64
}

// Store owns the physical SQLite database and exposes shared infrastructure.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS projection_sync_state (
			singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
			generation TEXT NOT NULL CHECK(generation <> ''),
			sequence INTEGER NOT NULL CHECK(typeof(sequence) = 'integer' AND sequence >= 0)
		)
	`); err != nil {
		return nil, fmt.Errorf("create sync metadata: %w", err)
	}
	generation, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("create database generation: %w", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("begin sync metadata initialisation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO projection_sync_state(singleton, generation, sequence)
		VALUES(1, ?, 0) ON CONFLICT(singleton) DO NOTHING
	`, generation.String()); err != nil {
		return nil, fmt.Errorf("initialise sync metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit sync metadata initialisation: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) ReadSyncState(ctx context.Context, tx *sql.Tx) (SyncState, error) {
	if tx == nil {
		return SyncState{}, fmt.Errorf("transaction is required")
	}
	var state SyncState
	err := tx.QueryRowContext(ctx, `
		SELECT generation, sequence FROM projection_sync_state WHERE singleton = 1
	`).Scan(&state.Generation, &state.Sequence)
	if err != nil {
		return SyncState{}, fmt.Errorf("read sync metadata: %w", err)
	}
	return state, nil
}

func (s *Store) NextSequence(ctx context.Context, tx *sql.Tx) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction is required")
	}
	var sequence int64
	err := tx.QueryRowContext(ctx, `
		UPDATE projection_sync_state SET sequence = sequence + 1
		WHERE singleton = 1 AND sequence < 9223372036854775807
		RETURNING sequence
	`).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("allocate projection sequence: %w", err)
	}
	return sequence, nil
}

func (s *Store) ChangeGeneration(ctx context.Context, tx *sql.Tx) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("transaction is required")
	}
	generation, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("create database generation: %w", err)
	}
	var value string
	err = tx.QueryRowContext(ctx, `
		UPDATE projection_sync_state SET generation = ? WHERE singleton = 1
		RETURNING generation
	`, generation.String()).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("change database generation: %w", err)
	}
	return value, nil
}

func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
