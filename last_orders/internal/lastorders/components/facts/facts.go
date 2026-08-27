// Package facts implements Fact registration and delivery.
//
// A Fact is the application-level statement emitted once the Idempotency Layer has
// established that it may be delivered. Plugins register interest in a Fact by name;
// they do not need to know which idempotency variant produced it. See
// docs/cdd/0001-idempotency.md.
package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"cellar/pkg/cellar"
)

// HandlerName is the single registered Cellar handler which fans a Fact out to all
// of its registered destinations. Every Fact is emitted as one Cell using this name.
const HandlerName cellar.HandlerName = "facts.emit"

// Fact is the durable, self-describing unit of work emitted by the Idempotency Layer.
type Fact struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// Registry records which Cellar handlers should receive a named Fact.
//
// Registration happens once at startup, mirroring how Cellar handlers themselves are
// registered; it is not persisted.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string][]cellar.HandlerName
}

// NewRegistry creates an empty Fact registry.
func NewRegistry() *Registry {
	return &Registry{handlers: map[string][]cellar.HandlerName{}}
}

// Register declares that the given handlers should receive the named Fact.
func (r *Registry) Register(fact string, handlerNames ...cellar.HandlerName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[fact] = append(append([]cellar.HandlerName(nil), r.handlers[fact]...), handlerNames...)
}

// Targets returns the handlers currently registered against the named Fact.
func (r *Registry) Targets(fact string) []cellar.HandlerName {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]cellar.HandlerName(nil), r.handlers[fact]...)
}

// Fanout builds the process-local Fanout definition which delivers registered Facts.
// It must be registered with the Cellar runtime once at startup.
func Fanout(registry *Registry) (*cellar.Fanout[Fact], error) {
	if registry == nil {
		return nil, fmt.Errorf("fact registry is required")
	}
	return cellar.NewFanout[Fact](HandlerName, cellar.FanoutExpanderFunc[Fact](
		func(ctx context.Context, parentID cellar.CellID, fact Fact) ([]cellar.FanoutTarget, error) {
			_ = ctx
			_ = parentID
			targets := registry.Targets(fact.Name)
			out := make([]cellar.FanoutTarget, 0, len(targets))
			for i, handlerName := range targets {
				out = append(out, cellar.FanoutTarget{
					Key:         fmt.Sprintf("%s-%d", handlerName, i),
					HandlerName: handlerName,
					Payload:     fact.Payload,
				})
			}
			return out, nil
		},
	))
}

// CellRequest builds a durable request which, once executed, fans the Fact out to its
// registered handlers. Idempotency components use this to emit a Fact via NewCells.
func CellRequest(name string, payload []byte) (cellar.CellRequest, error) {
	raw, err := cellar.JSONCodec[Fact]().Marshal(Fact{Name: name, Payload: payload})
	if err != nil {
		return cellar.CellRequest{}, fmt.Errorf("marshal fact %q: %w", name, err)
	}
	return cellar.CellRequest{Steps: []cellar.CellStep{{HandlerName: HandlerName, Payload: raw}}}, nil
}
