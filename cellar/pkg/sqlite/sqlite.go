package sqlite

import (
	"database/sql"

	internal "cellar/internal/sqlite"
	"cellar/pkg/cellar"
)

// Store persists cell lifecycle state in SQLite.
type Store = internal.Store

// NewStore creates a SQLite-backed store and ensures schema availability.
func NewStore(db *sql.DB, allocator cellar.CellIDAllocator) (*Store, error) {
	return internal.NewStore(db, allocator)
}

// Open opens a SQLite database at the given path and initialises schema.
func Open(path string, allocator cellar.CellIDAllocator) (*Store, error) {
	return internal.Open(path, allocator)
}
