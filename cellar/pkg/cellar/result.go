package cellar

import (
	"context"
	"errors"
	"time"
)

// Result is a marker interface for handler outcomes interpreted by the runtime.
type Result interface {
	isResult()
}

// ApplicationTx is a narrow transaction interface supplied to application work.
//
// Implementations are intentionally generic so the public API remains database-agnostic
// while still exposing a database/sql-style transaction surface for application code.
type ApplicationTx interface {
	Exec(query string, args ...any) error
	ExecContext(ctx context.Context, query string, args ...any) error
	Query(query string, args ...any) (ApplicationRows, error)
	QueryContext(ctx context.Context, query string, args ...any) (ApplicationRows, error)
	QueryRow(query string, args ...any) ApplicationRow
	QueryRowContext(ctx context.Context, query string, args ...any) ApplicationRow
}

// ApplicationRows represents a row result set for application work.
type ApplicationRows interface {
	Close() error
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// ApplicationRow represents a single-row query result.
type ApplicationRow interface {
	Scan(dest ...any) error
}

// ApplicationWork is executed in the same transaction as Cell completion.
type ApplicationWork func(tx ApplicationTx) error

// Complete replaces a claimed cell with zero or more new cells atomically.
type Complete struct {
	NewCells        []CellRequest
	ApplicationWork []ApplicationWork
}

func (Complete) isResult() {}

// Retry returns a claimed cell to READY with an optional not-before time.
type Retry struct {
	NotBefore *time.Time
}

func (Retry) isResult() {}

// RetrySequence restarts a Cell from its first step after Delay.
type RetrySequence struct {
	Delay time.Duration
}

func (RetrySequence) isResult() {}

// Kill terminates the claimed Cell without running later steps.
type Kill struct{}

func (Kill) isResult() {}

// ErrorResult represents a handler or runtime failure that should be surfaced.
type ErrorResult struct {
	Message string
	Err     error
}

func (ErrorResult) isResult() {}

// ResultApplier applies a handler result to the runtime store.
type ResultApplier interface {
	ApplyResult(ctx context.Context, cell Cell, result Result) error
}

// MultiResultApplier applies a result to all supplied appliers in order.
type MultiResultApplier []ResultApplier

// ApplyResult invokes each configured applier in order.
func (a MultiResultApplier) ApplyResult(ctx context.Context, cell Cell, result Result) error {
	for _, applier := range a {
		if applier == nil {
			continue
		}
		if err := applier.ApplyResult(ctx, cell, result); err != nil {
			return err
		}
	}
	return nil
}

// StoreResultApplier translates handler results into store lifecycle operations.
type StoreResultApplier struct {
	store Store
}

// NewStoreResultApplier creates a result applier backed by the supplied store.
func NewStoreResultApplier(store Store) *StoreResultApplier {
	return &StoreResultApplier{store: store}
}

// ApplyResult translates the handler outcome into the corresponding store transition.
func (a *StoreResultApplier) ApplyResult(ctx context.Context, cell Cell, result Result) error {
	_ = ctx
	if a == nil || a.store == nil {
		return nil
	}

	switch result := result.(type) {
	case Complete:
		if resultStore, ok := a.store.(ResultStore); ok {
			return resultStore.ApplyResult(cell, result)
		}
		return a.store.Complete(cell.ID, result.NewCells, result.ApplicationWork...)
	case Retry:
		if resultStore, ok := a.store.(ResultStore); ok {
			return resultStore.ApplyResult(cell, result)
		}
		return a.store.Retry(cell.ID, result.NotBefore)
	case RetrySequence:
		if result.Delay < 0 {
			return errors.New("retry sequence delay must not be negative")
		}
		if resultStore, ok := a.store.(ResultStore); ok {
			return resultStore.ApplyResult(cell, result)
		}
		notBefore := time.Now().Add(result.Delay)
		return a.store.Retry(cell.ID, &notBefore)
	case Kill:
		if resultStore, ok := a.store.(ResultStore); ok {
			return resultStore.ApplyResult(cell, result)
		}
		return a.store.Complete(cell.ID, nil)
	default:
		return nil
	}
}
