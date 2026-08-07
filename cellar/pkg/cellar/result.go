package cellar

import "time"

// Result is a marker interface for handler outcomes interpreted by the runtime.
type Result interface {
	isResult()
}

// Complete replaces a claimed cell with zero or more new cells atomically.
type Complete struct {
	NewCells []CellRequest
}

func (Complete) isResult() {}

// Retry returns a claimed cell to READY with an optional not-before time.
type Retry struct {
	NotBefore *time.Time
}

func (Retry) isResult() {}
