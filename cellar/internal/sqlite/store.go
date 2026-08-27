// Package sqlite provides a SQLite-backed store implementation for cellar.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
		allocator = cellar.NewUUIDAllocator()
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

	dsn := path + "?_pragma=busy_timeout(" + defaultBusyTimeout + ")&_pragma=journal_mode(WAL)"
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

// DB returns the underlying SQLite database connection for application-level access.
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// Add persists one or more READY cells atomically.
func (s *Store) Add(requests []cellar.CellRequest) ([]cellar.CellID, error) {
	identified, ids, err := s.identifyRequests(requests)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	if err := insertRequests(tx, identified); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func insertRequests(tx *sql.Tx, requests []cellar.CellRequest) error {
	for _, req := range requests {
		if req.ID == "" {
			return errors.New("cell id is required")
		}
		steps := requestSteps(req)
		if len(steps) == 0 {
			return errors.New("handler name is required")
		}
		for _, step := range steps {
			if step.HandlerName == "" {
				return errors.New("handler name is required")
			}
		}
		encodedSteps, err := json.Marshal(steps)
		if err != nil {
			return err
		}
		_, err = tx.Exec(
			`INSERT INTO cells (id, steps, current_step, state, not_before)
			 VALUES (?, ?, ?, ?, ?)`,
			string(req.ID),
			encodedSteps,
			0,
			string(cellar.CellStateReady),
			timeOrNil(req.NotBefore),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s", cellar.ErrCellAlreadyExists, req.ID)
			}
			return err
		}
	}

	return nil
}

// ClaimNext atomically claims one runnable cell.
func (s *Store) ClaimNext(now time.Time) (cellar.Cell, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return cellar.Cell{}, false, err
	}
	defer rollback(tx)

	row := tx.QueryRow(
		`SELECT id, steps, current_step, state, not_before
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
func (s *Store) Complete(cellID cellar.CellID, additions []cellar.CellRequest, applicationWork ...cellar.ApplicationWork) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)

	if err := ensureClaimed(tx, cellID); err != nil {
		return err
	}

	for _, work := range applicationWork {
		if work == nil {
			continue
		}
		if err := work(sqlTxAdapter{tx: tx}); err != nil {
			return err
		}
	}

	identified, _, err := s.identifyRequests(additions)
	if err != nil {
		return err
	}
	if err := insertRequests(tx, identified); err != nil {
		return err
	}

	_, err = tx.Exec(`DELETE FROM cells WHERE id = ?`, string(cellID))
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ApplyResult atomically applies a result to a claimed cell and its children.
func (s *Store) ApplyResult(claimed cellar.Cell, result cellar.Result) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)

	var state string
	var currentStep int
	if err := tx.QueryRow(`SELECT state, current_step FROM cells WHERE id = ?`, string(claimed.ID)).Scan(&state, &currentStep); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cellar.ErrCellNotFound
		}
		return err
	}
	if cellar.CellState(state) != cellar.CellStateClaimed {
		return cellar.ErrCellNotClaimed
	}
	if currentStep != claimed.CurrentStep {
		return errors.New("cell current step changed")
	}

	complete, isComplete := result.(cellar.Complete)
	if isComplete {
		for _, work := range complete.ApplicationWork {
			if work != nil {
				if err := work(sqlTxAdapter{tx: tx}); err != nil {
					return err
				}
			}
		}
		identified, _, err := s.identifyRequests(complete.NewCells)
		if err != nil {
			return err
		}
		if err := insertRequests(tx, identified); err != nil {
			return err
		}
		currentStep++
		if currentStep >= len(claimed.Steps) {
			if _, err := tx.Exec(`DELETE FROM cells WHERE id = ? AND state = ?`, string(claimed.ID), string(cellar.CellStateClaimed)); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`UPDATE cells SET current_step = ?, state = ? WHERE id = ? AND state = ?`, currentStep, string(cellar.CellStateReady), string(claimed.ID), string(cellar.CellStateClaimed)); err != nil {
			return err
		}
		return tx.Commit()
	}

	switch typed := result.(type) {
	case cellar.Retry:
		_, err = tx.Exec(`UPDATE cells SET state = ?, not_before = ? WHERE id = ? AND state = ?`, string(cellar.CellStateReady), timeOrNil(typed.NotBefore), string(claimed.ID), string(cellar.CellStateClaimed))
	case cellar.RetrySequence:
		if typed.Delay < 0 {
			return errors.New("retry sequence delay must not be negative")
		}
		notBefore := time.Now().Add(typed.Delay)
		_, err = tx.Exec(`UPDATE cells SET current_step = 0, state = ?, not_before = ? WHERE id = ? AND state = ?`, string(cellar.CellStateReady), timeOrNil(&notBefore), string(claimed.ID), string(cellar.CellStateClaimed))
	case cellar.Kill:
		_, err = tx.Exec(`DELETE FROM cells WHERE id = ? AND state = ?`, string(claimed.ID), string(cellar.CellStateClaimed))
	default:
		return errors.New("unsupported result")
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) identifyRequests(requests []cellar.CellRequest) ([]cellar.CellRequest, []cellar.CellID, error) {
	identified := make([]cellar.CellRequest, 0, len(requests))
	ids := make([]cellar.CellID, 0, len(requests))
	for _, req := range requests {
		if len(requestSteps(req)) == 0 {
			return nil, nil, errors.New("handler name is required")
		}
		if req.ID == "" {
			id, err := s.allocator.Next()
			if err != nil {
				return nil, nil, fmt.Errorf("allocate cell id: %w", err)
			}
			req.ID = id
		}
		identified = append(identified, req)
		ids = append(ids, req.ID)
	}
	return identified, ids, nil
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
		`SELECT id, steps, current_step, state, not_before
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
		`SELECT id, steps, current_step, state, not_before
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
	steps := cell.Steps
	if len(steps) == 0 {
		return errors.New("handler name is required")
	}
	encodedSteps, err := json.Marshal(steps)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO cells (id, steps, current_step, state, not_before)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   steps = excluded.steps,
		   current_step = excluded.current_step,
		   state = excluded.state,
		   not_before = excluded.not_before`,
		string(cell.ID),
		encodedSteps,
		cell.CurrentStep,
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
		PRAGMA busy_timeout = 5000;
		PRAGMA journal_mode = WAL;
		CREATE TABLE IF NOT EXISTS cells (
			id TEXT PRIMARY KEY,
			steps BLOB NOT NULL,
			current_step INTEGER NOT NULL DEFAULT 0,
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
		id           string
		encodedSteps []byte
		currentStep  int
		state        string
		notBefore    sql.NullTime
	)

	if err := row.Scan(&id, &encodedSteps, &currentStep, &state, &notBefore); err != nil {
		return cellar.Cell{}, err
	}
	var steps []cellar.CellStep
	if err := json.Unmarshal(encodedSteps, &steps); err != nil {
		return cellar.Cell{}, fmt.Errorf("decode cell steps: %w", err)
	}

	var notBeforePtr *time.Time
	if notBefore.Valid {
		t := notBefore.Time.UTC()
		notBeforePtr = &t
	}

	return cellar.Cell{
		ID:          cellar.CellID(id),
		Steps:       steps,
		CurrentStep: currentStep,
		State:       cellar.CellState(state),
		NotBefore:   notBeforePtr,
	}, nil
}

func requestSteps(req cellar.CellRequest) []cellar.CellStep {
	return req.Steps
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

type sqlTxAdapter struct {
	tx *sql.Tx
}

func (a sqlTxAdapter) Exec(query string, args ...any) error {
	_, err := a.tx.Exec(query, args...)
	return err
}

func (a sqlTxAdapter) ExecContext(ctx context.Context, query string, args ...any) error {
	_, err := a.tx.ExecContext(ctx, query, args...)
	return err
}

func (a sqlTxAdapter) Query(query string, args ...any) (cellar.ApplicationRows, error) {
	rows, err := a.tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return sqlRowsAdapter{rows: rows}, nil
}

func (a sqlTxAdapter) QueryContext(ctx context.Context, query string, args ...any) (cellar.ApplicationRows, error) {
	rows, err := a.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return sqlRowsAdapter{rows: rows}, nil
}

func (a sqlTxAdapter) QueryRow(query string, args ...any) cellar.ApplicationRow {
	return sqlRowAdapter{row: a.tx.QueryRow(query, args...)}
}

func (a sqlTxAdapter) QueryRowContext(ctx context.Context, query string, args ...any) cellar.ApplicationRow {
	return sqlRowAdapter{row: a.tx.QueryRowContext(ctx, query, args...)}
}

type sqlRowsAdapter struct {
	rows *sql.Rows
}

func (a sqlRowsAdapter) Close() error {
	return a.rows.Close()
}

func (a sqlRowsAdapter) Next() bool {
	return a.rows.Next()
}

func (a sqlRowsAdapter) Scan(dest ...any) error {
	return a.rows.Scan(dest...)
}

func (a sqlRowsAdapter) Err() error {
	return a.rows.Err()
}

type sqlRowAdapter struct {
	row *sql.Row
}

func (a sqlRowAdapter) Scan(dest ...any) error {
	return a.row.Scan(dest...)
}

func payloadOrEmpty(payload []byte) []byte {
	if payload == nil {
		return []byte{}
	}
	return payload
}
