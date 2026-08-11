package basestore

import (
	"database/sql"
	"fmt"

	"cellar/pkg/cellar"
	publicsqlite "cellar/pkg/sqlite"
)

// Store owns the physical SQLite database and exposes shared infrastructure.
type Store struct {
	db          *sql.DB
	cellarStore *publicsqlite.Store
}

func New(db *sql.DB, allocator cellar.CellIDAllocator) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}

	cellarStore, err := publicsqlite.NewStore(db, allocator)
	if err != nil {
		return nil, fmt.Errorf("init cellar store: %w", err)
	}

	return &Store{db: db, cellarStore: cellarStore}, nil
}

func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *Store) CellarStore() cellar.Store {
	if s == nil {
		return nil
	}
	return s.cellarStore
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
