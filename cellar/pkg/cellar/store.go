package cellar

import (
	"errors"
	"time"
)

var (
	ErrCellNotFound      = errors.New("cell not found")
	ErrCellAlreadyExists = errors.New("cell already exists")
	ErrCellNotClaimed    = errors.New("cell is not claimed")
)

// Store exposes lifecycle-safe persistence operations.
type Store interface {
	Add(requests []CellRequest) ([]CellID, error)
	ClaimNext(now time.Time) (Cell, bool, error)
	Complete(cellID CellID, additions []CellRequest, applicationWork ...ApplicationWork) error
	Retry(cellID CellID, notBefore *time.Time) error
	Recover() error
	ListActive() ([]Cell, error)
}

// ResultStore applies a claimed handler result and its lifecycle transition atomically.
// Stores may implement this in addition to Store; the legacy Store methods remain
// available for ordinary one-step integrations.
type ResultStore interface {
	Store
	ApplyResult(cell Cell, result Result) error
}

// DebuggableStore extends Store with privileged offline administrative overrides.
type DebuggableStore interface {
	Store
	ListAll() ([]Cell, error)
	Get(id CellID) (Cell, error)
	ForceUpdate(cell Cell) error
	ForceDelete(id CellID) error
}
