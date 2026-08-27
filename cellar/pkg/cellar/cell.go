// Package cellar defines the public API for durable Cell execution primitives.
package cellar

import "time"

// CellID is an opaque identifier for a cell.
type CellID string

// HandlerName identifies the registered handler for a persisted cell.
type HandlerName string

// CellState is the persisted lifecycle state of an active cell.
type CellState string

const (
	// CellStateReady is eligible for scheduling when NotBefore permits.
	CellStateReady CellState = "READY"
	// CellStateClaimed is currently owned by the runtime for execution.
	CellStateClaimed CellState = "CLAIMED"
)

// CellStep is one ordered handler invocation in a Cell.
type CellStep struct {
	HandlerName HandlerName
	Payload     []byte
}

// Cell is the persisted execution primitive managed by Cellar.
type Cell struct {
	Steps       []CellStep
	CurrentStep int
	ID          CellID
	State       CellState
	NotBefore   *time.Time
}

// CellRequest describes new work to be persisted by the store.
// An empty ID asks the store to allocate one.
type CellRequest struct {
	Steps     []CellStep
	ID        CellID
	NotBefore *time.Time
}
