package basestore

import (
	"database/sql"
	"fmt"
)

// Store owns the physical SQLite database and exposes shared infrastructure.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}

	return &Store{db: db}, nil
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
