// Package sqlite provides a SQLite-backed store implementation for cellar.
package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"cellar/pkg/cellar"
)

const (
	defaultBusyTimeout = "5000"
)

// Store persists cell lifecycle state in SQLite.
type Store struct {
	db        *sql.DB
	allocator cellar.CellIDAllocator
}

// NewStore creates a SQLite-backed store and ensures schema availability.
func NewStore(db *sql.DB, allocator cellar.CellIDAllocator) (*Store, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	if allocator == nil {
		allocator = cellar.NewSequentialAllocator("cell-", 1)
	}

	s := &Store{
		db:        db,
		allocator: allocator,
	}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens a SQLite database at the given path and initialises schema.
func Open(path string, allocator cellar.CellIDAllocator) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("path is required")
	}

	dsn := path + "?_pragma=busy_timeout(" + defaultBusyTimeout + ")"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	store, err := NewStore(db, allocator)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Add persists one or more READY cells atomically.
func (s *Store) Add(requests []cellar.CellRequest) ([]cellar.CellID, error) {
	ids := make([]cellar.CellID, 0, len(requests))
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	for _, req := range requests {
		if req.HandlerName == "" {
			return nil, errors.New("handler name is required")
		}

		id, err := s.allocator.Next()
		if err != nil {
			return nil, fmt.Errorf("allocate cell id: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO cells (id, handler_name, payload, state, not_before)
			 VALUES (?, ?, ?, ?, ?)`,
			string(id),
			string(req.HandlerName),
			payloadOrEmpty(req.Payload),
			string(cellar.CellStateReady),
			timeOrNil(req.NotBefore),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, fmt.Errorf("%w: %s", cellar.ErrCellAlreadyExists, id)
			}
			return nil, err
		}

		ids = append(ids, id)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

// ClaimNext atomically claims one runnable cell.
func (s *Store) ClaimNext(now time.Time) (cellar.Cell, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return cellar.Cell{}, false, err
	}
	defer rollback(tx)

	row := tx.QueryRow(
		`SELECT id, handler_name, payload, state, not_before
		 FROM cells
		 WHERE state = ?
		   AND (not_before IS NULL OR not_before <= ?)
		 ORDER BY created_at, id
		 LIMIT 1`,
		string(cellar.CellStateReady),
		now.UTC(),
	)

	cell, err := scanCell(row)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return cellar.Cell{}, false, err
		}
		return cellar.Cell{}, false, nil
	}
	if err != nil {
		return cellar.Cell{}, false, err
	}

	res, err := tx.Exec(
		`UPDATE cells
		 SET state = ?
		 WHERE id = ? AND state = ?`,
		string(cellar.CellStateClaimed),
		string(cell.ID),
		string(cellar.CellStateReady),
	)
	if err != nil {
		return cellar.Cell{}, false, err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return cellar.Cell{}, false, err
	}
	if rowsAffected == 0 {
		if err := tx.Commit(); err != nil {
			return cellar.Cell{}, false, err
		}
		return cellar.Cell{}, false, nil
	}

	if err := tx.Commit(); err != nil {
		return cellar.Cell{}, false, err
	}

	cell.State = cellar.CellStateClaimed
	return cell, true, nil
}

// Complete atomically deletes a claimed cell and adds replacement cells.
func (s *Store) Complete(cellID cellar.CellID, additions []cellar.CellRequest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)

	if err := ensureClaimed(tx, cellID); err != nil {
		return err
	}

	for _, req := range additions {
		if req.HandlerName == "" {
			return errors.New("handler name is required")
		}

		newID, err := s.allocator.Next()
		if err != nil {
			return fmt.Errorf("allocate cell id: %w", err)
		}

		_, err = tx.Exec(
			`INSERT INTO cells (id, handler_name, payload, state, not_before)
			 VALUES (?, ?, ?, ?, ?)`,
			string(newID),
			string(req.HandlerName),
			payloadOrEmpty(req.Payload),
			string(cellar.CellStateReady),
			timeOrNil(req.NotBefore),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s", cellar.ErrCellAlreadyExists, newID)
			}
			return err
		}
	}

	_, err = tx.Exec(`DELETE FROM cells WHERE id = ?`, string(cellID))
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Retry transitions a claimed cell back to READY.
func (s *Store) Retry(cellID cellar.CellID, notBefore *time.Time) error {
	res, err := s.db.Exec(
		`UPDATE cells
		 SET state = ?, not_before = ?
		 WHERE id = ? AND state = ?`,
		string(cellar.CellStateReady),
		timeOrNil(notBefore),
		string(cellID),
		string(cellar.CellStateClaimed),
	)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return classifyMissingOrNotClaimed(s.db, cellID)
	}
	return nil
}

// Recover transitions all CLAIMED cells to READY.
func (s *Store) Recover() error {
	_, err := s.db.Exec(
		`UPDATE cells
		 SET state = ?
		 WHERE state = ?`,
		string(cellar.CellStateReady),
		string(cellar.CellStateClaimed),
	)
	return err
}

// ListActive returns all currently existing cells.
func (s *Store) ListActive() ([]cellar.Cell, error) {
	rows, err := s.db.Query(
		`SELECT id, handler_name, payload, state, not_before
		 FROM cells
		 ORDER BY created_at, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanCells(rows)
}

// ListAll returns all currently existing cells.
func (s *Store) ListAll() ([]cellar.Cell, error) {
	return s.ListActive()
}

// Get returns a cell by ID.
func (s *Store) Get(id cellar.CellID) (cellar.Cell, error) {
	row := s.db.QueryRow(
		`SELECT id, handler_name, payload, state, not_before
		 FROM cells
		 WHERE id = ?`,
		string(id),
	)

	cell, err := scanCell(row)
	if errors.Is(err, sql.ErrNoRows) {
		return cellar.Cell{}, cellar.ErrCellNotFound
	}
	if err != nil {
		return cellar.Cell{}, err
	}
	return cell, nil
}

// ForceUpdate upserts a cell without lifecycle validation checks.
func (s *Store) ForceUpdate(cell cellar.Cell) error {
	if cell.ID == "" {
		return errors.New("cell id is required")
	}
	if cell.HandlerName == "" {
		return errors.New("handler name is required")
	}

	_, err := s.db.Exec(
		`INSERT INTO cells (id, handler_name, payload, state, not_before)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   handler_name = excluded.handler_name,
		   payload = excluded.payload,
		   state = excluded.state,
		   not_before = excluded.not_before`,
		string(cell.ID),
		string(cell.HandlerName),
		payloadOrEmpty(cell.Payload),
		string(cell.State),
		timeOrNil(cell.NotBefore),
	)
	return err
}

// ForceDelete deletes a cell by ID regardless of lifecycle state.
func (s *Store) ForceDelete(id cellar.CellID) error {
	res, err := s.db.Exec(`DELETE FROM cells WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return cellar.ErrCellNotFound
	}
	return nil
}

func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS cells (
			id TEXT PRIMARY KEY,
			handler_name TEXT NOT NULL,
			payload BLOB NOT NULL,
			state TEXT NOT NULL,
			not_before DATETIME NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_cells_runnable
		ON cells(state, not_before, created_at);
	`)
	return err
}

func ensureClaimed(tx *sql.Tx, cellID cellar.CellID) error {
	row := tx.QueryRow(`SELECT state FROM cells WHERE id = ?`, string(cellID))
	var state string
	if err := row.Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cellar.ErrCellNotFound
		}
		return err
	}
	if cellar.CellState(state) != cellar.CellStateClaimed {
		return cellar.ErrCellNotClaimed
	}
	return nil
}

func classifyMissingOrNotClaimed(db *sql.DB, cellID cellar.CellID) error {
	row := db.QueryRow(`SELECT state FROM cells WHERE id = ?`, string(cellID))
	var state string
	if err := row.Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cellar.ErrCellNotFound
		}
		return err
	}
	return cellar.ErrCellNotClaimed
}

func scanCells(rows *sql.Rows) ([]cellar.Cell, error) {
	results := make([]cellar.Cell, 0)
	for rows.Next() {
		cell, err := scanCell(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, cell)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCell(row scanner) (cellar.Cell, error) {
	var (
		id          string
		handlerName string
		payload     []byte
		state       string
		notBefore   sql.NullTime
	)

	if err := row.Scan(&id, &handlerName, &payload, &state, &notBefore); err != nil {
		return cellar.Cell{}, err
	}

	var notBeforePtr *time.Time
	if notBefore.Valid {
		t := notBefore.Time.UTC()
		notBeforePtr = &t
	}

	return cellar.Cell{
		ID:          cellar.CellID(id),
		HandlerName: cellar.HandlerName(handlerName),
		Payload:     payload,
		State:       cellar.CellState(state),
		NotBefore:   notBeforePtr,
	}, nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func timeOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func payloadOrEmpty(payload []byte) []byte {
	if payload == nil {
		return []byte{}
	}
	return payload
}
