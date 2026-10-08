// Package cellar defines the public API for durable Cell execution primitives.
package cellar

import (
	"errors"
	"fmt"
	"time"
)

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

// CellDefinition describes executable work before it is persisted.
type CellDefinition interface {
	CellRequest() (CellRequest, error)
}

// Sequence is an ordered CellDefinition made from one or more typed handler invocations.
type Sequence struct {
	request CellRequest
}

// NewCellDefinition constructs a one-step CellDefinition for the named handler.
// It does not persist work or make it eligible for execution.
func NewCellDefinition(name HandlerName, payload any) (CellDefinition, error) {
	return NewSequence(Step{HandlerName: name, Payload: payload})
}

// NewSequence JSON-encodes and constructs an ordered sequence of steps.
// Payload may have a different Go type for each step.
func NewSequence(steps ...Step) (*Sequence, error) {
	request, err := cellRequestFromSteps(steps)
	if err != nil {
		return nil, err
	}
	return &Sequence{request: request}, nil
}

// CellRequest returns the store request represented by s.
func (s *Sequence) CellRequest() (CellRequest, error) {
	if s == nil {
		return CellRequest{}, ErrCellNil
	}
	return cloneCellRequest(s.request), nil
}

// CellStep is one ordered handler invocation in a Cell.
// Payload contains the JSON-encoded value expected by HandlerName.
type CellStep struct {
	HandlerName HandlerName
	Payload     []byte
}

// Cell is the persisted execution primitive managed by Cellar.
//
// CurrentStep identifies the next step to execute. A one-step Cell is the
// ordinary case; a Cell with multiple steps is an ordered sequence.
type Cell struct {
	Steps       []CellStep
	CurrentStep int
	ID          CellID
	State       CellState
	NotBefore   *time.Time
}

// CellRequest describes new work to be persisted by the store.
// Steps must contain at least one handler invocation. Use Cellar.Add or
// Cellar.AddSequence when the payloads are still typed Go values.
// An empty ID asks the store to allocate one.
type CellRequest struct {
	Steps     []CellStep
	ID        CellID
	NotBefore *time.Time
}

func cellRequestFromSteps(steps []Step) (CellRequest, error) {
	if len(steps) == 0 {
		return CellRequest{}, errors.New("sequence must contain at least one step")
	}

	requests := make([]CellStep, 0, len(steps))
	for _, step := range steps {
		if step.HandlerName == "" {
			return CellRequest{}, errors.New("handler name is required")
		}
		raw, err := marshalJSON(step.Payload)
		if err != nil {
			return CellRequest{}, fmt.Errorf("encode cell payload: %w", err)
		}
		requests = append(requests, CellStep{HandlerName: step.HandlerName, Payload: raw})
	}
	return CellRequest{Steps: requests}, nil
}

func cloneCellRequest(req CellRequest) CellRequest {
	clone := req
	clone.Steps = append([]CellStep(nil), req.Steps...)
	clone.NotBefore = cloneTimePtr(req.NotBefore)
	return clone
}

var ErrCellNil = errors.New("cell is nil")
