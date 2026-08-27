package cellar

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// FanoutTarget describes one keyed child produced by a fanout expansion.
// Cellar JSON-encodes Payload before persisting the child.
type FanoutTarget struct {
	Key         string
	HandlerName HandlerName
	Payload     any
	NotBefore   *time.Time
}

// FanoutExpander expands one typed payload into zero or more keyed child cells.
type FanoutExpander[T any] interface {
	Expand(ctx context.Context, parentID CellID, payload T) ([]FanoutTarget, error)
}

// FanoutExpanderFunc adapts a function to FanoutExpander.
type FanoutExpanderFunc[T any] func(context.Context, CellID, T) ([]FanoutTarget, error)

// Expand invokes f.
func (f FanoutExpanderFunc[T]) Expand(ctx context.Context, parentID CellID, payload T) ([]FanoutTarget, error) {
	return f(ctx, parentID, payload)
}

// Fanout defines one named, typed work expansion.
type Fanout[T any] struct {
	name     HandlerName
	expander FanoutExpander[T]
}

// NewFanout constructs a process-local fanout definition.
func NewFanout[T any](name HandlerName, expander FanoutExpander[T]) (*Fanout[T], error) {
	if name == "" {
		return nil, ErrHandlerNameRequired
	}
	if expander == nil {
		return nil, ErrFanoutExpanderNil
	}
	return &Fanout[T]{name: name, expander: expander}, nil
}

// Register binds the fanout's durable name to its process-local expander.
func (f *Fanout[T]) Register(c *Cellar) error {
	if c == nil {
		return ErrCellarNil
	}
	if f == nil {
		return ErrFanoutNil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return ErrCellarStarted
	}
	err := c.registry.Register(f.name, fanoutRegistration[T]{expander: f.expander})
	if errors.Is(err, ErrHandlerAlreadyRegistered) {
		return fmt.Errorf("%w: %s", ErrFanoutAlreadyExists, f.name)
	}
	return err
}

// Add JSON-encodes payload and persists a new instance of this fanout.
func (f *Fanout[T]) Add(c *Cellar, payload T) (CellID, error) {
	if c == nil {
		return "", ErrCellarNil
	}
	if f == nil {
		return "", ErrFanoutNil
	}
	return c.Add(f.name, payload)
}

type fanoutRegistration[T any] struct {
	expander FanoutExpander[T]
}

func (r fanoutRegistration[T]) Execute(ctx context.Context, cell Cell) Result {
	var payload T
	if err := unmarshalJSON(currentStepPayload(cell), &payload); err != nil {
		return ErrorResult{Message: "decode fanout payload", Err: err}
	}
	if r.expander == nil {
		return ErrorResult{Message: "expand fanout", Err: ErrFanoutExpanderNil}
	}

	targets, err := r.expander.Expand(ctx, cell.ID, payload)
	if err != nil {
		return ErrorResult{Message: "expand fanout", Err: err}
	}
	children, err := identifyFanoutTargets(cell.ID, targets)
	if err != nil {
		return ErrorResult{Message: "expand fanout", Err: err}
	}
	return Complete{NewCells: children}
}

func (r fanoutRegistration[T]) Inspect(cell Cell) Inspection {
	var payload T
	err := unmarshalJSON(currentStepPayload(cell), &payload)
	return Inspection{
		Cell:          cloneCell(cell),
		Payload:       payload,
		PayloadFormat: "json",
		DecodeError:   err,
	}
}

func identifyFanoutTargets(parentID CellID, targets []FanoutTarget) ([]CellRequest, error) {
	children := make([]CellRequest, 0, len(targets))
	keys := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target.Key == "" {
			return nil, ErrFanoutTargetKeyRequired
		}
		if _, exists := keys[target.Key]; exists {
			return nil, fmt.Errorf("%w: %s", ErrFanoutTargetKeyDuplicate, target.Key)
		}
		keys[target.Key] = struct{}{}
		payload, err := marshalJSON(target.Payload)
		if err != nil {
			return nil, fmt.Errorf("encode fanout target %q payload: %w", target.Key, err)
		}
		children = append(children, CellRequest{
			ID:        deriveFanoutChildID(parentID, target.Key),
			Steps:     []CellStep{{HandlerName: target.HandlerName, Payload: payload}},
			NotBefore: target.NotBefore,
		})
	}
	return children, nil
}

func deriveFanoutChildID(parentID CellID, key string) CellID {
	digest := sha256.New()
	digest.Write([]byte("cellar/fanout-child/v1"))
	var length [8]byte
	for _, part := range []string{string(parentID), key} {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		digest.Write(length[:])
		digest.Write([]byte(part))
	}
	return CellID("fanout:" + hex.EncodeToString(digest.Sum(nil)))
}

var (
	ErrFanoutNil                = errors.New("fanout is nil")
	ErrFanoutExpanderNil        = errors.New("fanout expander is nil")
	ErrFanoutAlreadyExists      = errors.New("fanout already exists")
	ErrFanoutTargetKeyRequired  = errors.New("fanout target key is required")
	ErrFanoutTargetKeyDuplicate = errors.New("fanout target key is duplicated")
)
