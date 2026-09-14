// Package truths declares durable application observations, independent of their
// source, and dispatches each to the Plugins that react to it. See ADR-0009 and
// ADR-0010: a Truth is a concrete Go type, and dispatch uses Cellar's native
// per-type Fanout rather than a shared, string-keyed envelope.
package truths

import (
	"context"
	"fmt"
	"sync"

	"cellar/pkg/cellar"
)

// Registry declares which Cellar handlers should receive a Truth of type T. One
// Registry backs exactly one native cellar.Fanout[T], keyed by its own durable
// HandlerName.
//
// Registration happens once at startup, mirroring how Cellar handlers themselves
// are registered; it is not persisted.
type Registry[T any] struct {
	name cellar.HandlerName

	mu       sync.RWMutex
	handlers []cellar.HandlerName
}

// NewRegistry declares the durable Cellar handler name used to fan a Truth of
// type T out to its registered handlers.
func NewRegistry[T any](name cellar.HandlerName) *Registry[T] {
	return &Registry[T]{name: name}
}

// Name returns the Fanout's durable Cellar handler name.
func (r *Registry[T]) Name() cellar.HandlerName {
	return r.name
}

// Register declares that the given handlers should receive this Truth.
// Registering the same handler more than once (e.g. because the owning
// process is started multiple times within one Go process, as in tests) is a
// no-op rather than a duplicate delivery.
func (r *Registry[T]) Register(handlerNames ...cellar.HandlerName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, handlerName := range handlerNames {
		if !containsHandlerName(r.handlers, handlerName) {
			r.handlers = append(r.handlers, handlerName)
		}
	}
}

func containsHandlerName(handlers []cellar.HandlerName, target cellar.HandlerName) bool {
	for _, handler := range handlers {
		if handler == target {
			return true
		}
	}
	return false
}

func (r *Registry[T]) targets() []cellar.HandlerName {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]cellar.HandlerName(nil), r.handlers...)
}

// Fanout builds the native Cellar Fanout which delivers this Truth to its
// registered handlers. It must be registered with the Cellar runtime once at
// startup, after all Plugins have called Register.
func (r *Registry[T]) Fanout() (*cellar.Fanout[T], error) {
	if r == nil {
		return nil, fmt.Errorf("truth registry is nil")
	}
	return cellar.NewFanout[T](r.name, cellar.FanoutExpanderFunc[T](
		func(ctx context.Context, parentID cellar.CellID, truth T) ([]cellar.FanoutTarget, error) {
			_ = ctx
			_ = parentID
			targets := r.targets()
			out := make([]cellar.FanoutTarget, 0, len(targets))
			for _, handlerName := range targets {
				cell, err := cellar.NewCellDefinition(handlerName, truth)
				if err != nil {
					return nil, err
				}
				out = append(out, cellar.FanoutTarget{Key: string(handlerName), Cell: cell})
			}
			return out, nil
		},
	))
}

// Envelope carries an arbitrary Truth's dispatch target and JSON payload through
// the generic, reusable Idempotency Sequence (docs/cdd/0001-idempotency.md),
// which must remain agnostic of any particular Truth's Go type.
type Envelope struct {
	FanoutName cellar.HandlerName `json:"fanout_name"`
	Payload    []byte             `json:"payload"`
}

// NewEnvelope marshals truth and addresses it at the Fanout registered under name.
func NewEnvelope[T any](name cellar.HandlerName, truth T) (Envelope, error) {
	payload, err := cellar.JSONCodec[T]().Marshal(truth)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal truth for %q: %w", name, err)
	}
	return Envelope{FanoutName: name, Payload: payload}, nil
}

// CellRequest builds a durable request which, once executed, fans the enveloped
// Truth out to its registered handlers via its own native Fanout.
func (e Envelope) CellRequest() (cellar.CellRequest, error) {
	if e.FanoutName == "" {
		return cellar.CellRequest{}, fmt.Errorf("envelope fanout name is required")
	}
	return cellar.CellRequest{Steps: []cellar.CellStep{{HandlerName: e.FanoutName, Payload: e.Payload}}}, nil
}
