package cellar

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// MemoryStore is an in-memory reference implementation of Store semantics.
type MemoryStore struct {
	mu        sync.Mutex
	allocator CellIDAllocator
	cells     map[CellID]Cell
	order     []CellID
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore(allocator CellIDAllocator) *MemoryStore {
	if allocator == nil {
		allocator = NewUUIDAllocator()
	}

	return &MemoryStore{
		allocator: allocator,
		cells:     make(map[CellID]Cell),
		order:     make([]CellID, 0),
	}
}

// Add persists one or more new cells atomically.
func (s *MemoryStore) Add(requests []CellRequest) ([]CellID, error) {
	identified, ids, err := s.identifyRequests(requests)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cells, err := s.buildNewCellsLocked(identified)
	if err != nil {
		return nil, err
	}

	for _, cell := range cells {
		s.cells[cell.ID] = cell
		s.order = append(s.order, cell.ID)
	}
	return ids, nil
}

// ClaimNext atomically finds and claims one runnable cell.
func (s *MemoryStore) ClaimNext(now time.Time) (Cell, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range s.order {
		cell, ok := s.cells[id]
		if !ok {
			continue
		}
		if cell.State != CellStateReady {
			continue
		}
		if cell.NotBefore != nil && cell.NotBefore.After(now) {
			continue
		}

		cell.State = CellStateClaimed
		s.cells[id] = cell
		return cloneCell(cell), true, nil
	}

	return Cell{}, false, nil
}

// Complete atomically deletes the claimed parent and adds zero or more children.
func (s *MemoryStore) Complete(cellID CellID, additions []CellRequest, applicationWork ...ApplicationWork) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	parent, ok := s.cells[cellID]
	if !ok {
		return ErrCellNotFound
	}
	if parent.State != CellStateClaimed {
		return ErrCellNotClaimed
	}

	newCells, err := s.applyResultWorkLocked(additions, applicationWork)
	if err != nil {
		return err
	}

	delete(s.cells, cellID)
	s.removeFromOrderLocked(cellID)
	for _, cell := range newCells {
		s.cells[cell.ID] = cell
		s.order = append(s.order, cell.ID)
	}
	return nil
}

// ApplyResult atomically applies a result to a claimed cell.
func (s *MemoryStore) ApplyResult(claimed Cell, result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cell, ok := s.cells[claimed.ID]
	if !ok {
		return ErrCellNotFound
	}
	if cell.State != CellStateClaimed {
		return ErrCellNotClaimed
	}
	if cell.CurrentStep != claimed.CurrentStep {
		return errors.New("cell current step changed")
	}

	complete, isComplete := result.(Complete)
	if isComplete {
		newCells, err := s.applyResultWorkLocked(complete.NewCells, complete.ApplicationWork)
		if err != nil {
			return err
		}
		cell.CurrentStep++
		if cell.CurrentStep == len(cell.Steps) {
			delete(s.cells, cell.ID)
			s.removeFromOrderLocked(cell.ID)
		} else {
			cell.State = CellStateReady
			s.cells[cell.ID] = cell
		}
		for _, newCell := range newCells {
			s.cells[newCell.ID] = newCell
			s.order = append(s.order, newCell.ID)
		}
		return nil
	}

	switch typed := result.(type) {
	case Retry:
		newCells, err := s.applyResultWorkLocked(typed.NewCells, typed.ApplicationWork)
		if err != nil {
			return err
		}
		cell.State = CellStateReady
		cell.NotBefore = cloneTimePtr(typed.NotBefore)
		for _, newCell := range newCells {
			s.cells[newCell.ID] = newCell
			s.order = append(s.order, newCell.ID)
		}
	case RetrySequence:
		if typed.Delay < 0 {
			return errors.New("retry sequence delay must not be negative")
		}
		newCells, err := s.applyResultWorkLocked(typed.NewCells, typed.ApplicationWork)
		if err != nil {
			return err
		}
		cell.CurrentStep = 0
		cell.State = CellStateReady
		cell.NotBefore = timePtr(time.Now().Add(typed.Delay))
		for _, newCell := range newCells {
			s.cells[newCell.ID] = newCell
			s.order = append(s.order, newCell.ID)
		}
	case Kill:
		newCells, err := s.applyResultWorkLocked(typed.NewCells, typed.ApplicationWork)
		if err != nil {
			return err
		}
		delete(s.cells, cell.ID)
		s.removeFromOrderLocked(cell.ID)
		for _, newCell := range newCells {
			s.cells[newCell.ID] = newCell
			s.order = append(s.order, newCell.ID)
		}
		return nil
	default:
		return errors.New("unsupported result")
	}
	s.cells[cell.ID] = cell
	return nil
}

// Retry transitions a claimed cell back to READY.
func (s *MemoryStore) Retry(cellID CellID, notBefore *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cell, ok := s.cells[cellID]
	if !ok {
		return ErrCellNotFound
	}
	if cell.State != CellStateClaimed {
		return ErrCellNotClaimed
	}

	cell.State = CellStateReady
	cell.NotBefore = cloneTimePtr(notBefore)
	s.cells[cellID] = cell
	return nil
}

// Recover transitions all claimed cells back to READY.
func (s *MemoryStore) Recover() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, cell := range s.cells {
		if cell.State == CellStateClaimed {
			cell.State = CellStateReady
			s.cells[id] = cell
		}
	}
	return nil
}

// ListActive returns all currently existing cells.
func (s *MemoryStore) ListActive() ([]Cell, error) {
	return s.ListAll()
}

// ListAll returns all currently existing cells.
func (s *MemoryStore) ListAll() ([]Cell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Cell, 0, len(s.cells))
	for _, id := range s.order {
		cell, ok := s.cells[id]
		if !ok {
			continue
		}
		out = append(out, cloneCell(cell))
	}
	return out, nil
}

// Get returns a single persisted cell by ID.
func (s *MemoryStore) Get(id CellID) (Cell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cell, ok := s.cells[id]
	if !ok {
		return Cell{}, ErrCellNotFound
	}
	return cloneCell(cell), nil
}

// ForceUpdate overwrites or inserts a cell bypassing lifecycle checks.
func (s *MemoryStore) ForceUpdate(cell Cell) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateCellForPersistence(cell); err != nil {
		return err
	}

	if _, exists := s.cells[cell.ID]; !exists {
		s.order = append(s.order, cell.ID)
	}
	s.cells[cell.ID] = cloneCell(cell)
	return nil
}

// ForceDelete removes a persisted cell bypassing lifecycle checks.
func (s *MemoryStore) ForceDelete(id CellID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cells[id]; !exists {
		return ErrCellNotFound
	}
	delete(s.cells, id)
	s.removeFromOrderLocked(id)
	return nil
}

func (s *MemoryStore) identifyRequests(requests []CellRequest) ([]CellRequest, []CellID, error) {
	identified := make([]CellRequest, 0, len(requests))
	ids := make([]CellID, 0, len(requests))

	for _, req := range requests {
		if err := validateCellRequest(req); err != nil {
			return nil, nil, err
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

func (s *MemoryStore) buildNewCellsLocked(requests []CellRequest) ([]Cell, error) {
	cells := make([]Cell, 0, len(requests))
	batchIDs := make(map[CellID]struct{}, len(requests))

	for _, req := range requests {
		if req.ID == "" {
			return nil, errors.New("cell id is required")
		}
		if err := validateCellRequest(req); err != nil {
			return nil, err
		}
		steps := requestSteps(req)

		if _, exists := s.cells[req.ID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrCellAlreadyExists, req.ID)
		}
		if _, exists := batchIDs[req.ID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrCellAlreadyExists, req.ID)
		}
		batchIDs[req.ID] = struct{}{}

		cell := Cell{
			ID:          req.ID,
			Steps:       cloneSteps(steps),
			CurrentStep: 0,
			State:       CellStateReady,
			NotBefore:   cloneTimePtr(req.NotBefore),
		}
		cells = append(cells, cell)
	}

	return cells, nil
}

func (s *MemoryStore) applyResultWorkLocked(additions []CellRequest, applicationWork []ApplicationWork) ([]Cell, error) {
	for _, work := range applicationWork {
		if work != nil {
			if err := work(noopApplicationTx{}); err != nil {
				return nil, err
			}
		}
	}
	identified, _, err := s.identifyRequests(additions)
	if err != nil {
		return nil, err
	}
	return s.buildNewCellsLocked(identified)
}

func (s *MemoryStore) removeFromOrderLocked(id CellID) {
	idx := slices.Index(s.order, id)
	if idx == -1 {
		return
	}
	s.order = append(s.order[:idx], s.order[idx+1:]...)
}

func cloneCell(cell Cell) Cell {
	cell.Steps = cloneSteps(cell.Steps)
	cell.NotBefore = cloneTimePtr(cell.NotBefore)
	return cell
}

func cloneSteps(steps []CellStep) []CellStep {
	if steps == nil {
		return nil
	}
	out := make([]CellStep, len(steps))
	for index, step := range steps {
		out[index] = CellStep{HandlerName: step.HandlerName, Payload: cloneBytes(step.Payload)}
	}
	return out
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	clone := *t
	return &clone
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func validateCellRequest(req CellRequest) error {
	steps := requestSteps(req)
	if len(steps) == 0 {
		return errors.New("handler name is required")
	}
	for _, step := range steps {
		if step.HandlerName == "" {
			return errors.New("handler name is required")
		}
	}
	return nil
}

func validateCellForPersistence(cell Cell) error {
	if cell.ID == "" {
		return errors.New("cell id is required")
	}
	steps := cell.Steps
	if len(steps) == 0 {
		return errors.New("handler name is required")
	}
	if cell.CurrentStep < 0 || cell.CurrentStep > len(steps) {
		return fmt.Errorf("invalid current step: %d", cell.CurrentStep)
	}
	for _, step := range steps {
		if step.HandlerName == "" {
			return errors.New("handler name is required")
		}
	}
	if cell.State != CellStateReady && cell.State != CellStateClaimed {
		return fmt.Errorf("invalid cell state: %q", cell.State)
	}
	return nil
}

func requestSteps(req CellRequest) []CellStep {
	return req.Steps
}

type noopApplicationTx struct{}

func (noopApplicationTx) Exec(query string, args ...any) error {
	return nil
}

func (noopApplicationTx) ExecContext(ctx context.Context, query string, args ...any) error {
	_ = ctx
	return nil
}

func (noopApplicationTx) Query(query string, args ...any) (ApplicationRows, error) {
	return nil, nil
}

func (noopApplicationTx) QueryContext(ctx context.Context, query string, args ...any) (ApplicationRows, error) {
	_ = ctx
	return nil, nil
}

func (noopApplicationTx) QueryRow(query string, args ...any) ApplicationRow {
	return nil
}

func (noopApplicationTx) QueryRowContext(ctx context.Context, query string, args ...any) ApplicationRow {
	_ = ctx
	return nil
}
