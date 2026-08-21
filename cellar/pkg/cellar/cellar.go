package cellar

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Config controls Cellar's execution runtime.
type Config struct {
	PollDelay time.Duration
}

// Cellar owns handler registration and the complete cell execution runtime.
type Cellar struct {
	mu        sync.Mutex
	store     Store
	registry  *MemoryRegistry
	scheduler *Scheduler
	cancel    context.CancelFunc
	done      chan struct{}
	started   bool
}

// New creates a Cellar runtime backed by store.
func New(store Store, config Config) *Cellar {
	registry := NewMemoryRegistry()
	worker := NewWorker(registry, NewStoreResultApplier(store))
	scheduler := NewScheduler(store, workerDispatcher{worker: worker}, 1, config.PollDelay)
	return &Cellar{
		store:     store,
		registry:  registry,
		scheduler: scheduler,
	}
}

// Register binds a typed handler to a stable handler name using JSON payloads.
func (c *Cellar) Register[T any](name HandlerName, handler Handler[T]) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrCellarStarted
	}
	if handler == nil {
		return ErrHandlerNil
	}
	return c.registry.Register(name, typedJSONRegistration[T]{handler: handler})
}

// Add JSON-encodes payload and persists a cell for the named handler.
// i.e. add a cell that will run the named handler with the given payload.
func (c *Cellar) Add[T any](name HandlerName, payload T) (CellID, error) {
	if c.store == nil {
		return "", ErrStoreNil
	}
	raw, err := marshalJSON(payload)
	if err != nil {
		return "", fmt.Errorf("encode cell payload: %w", err)
	}
	ids, err := c.store.Add([]CellRequest{{HandlerName: name, Payload: raw}})
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("store returned %d cell IDs, want 1", len(ids))
	}
	return ids[0], nil
}

// Start freezes registration and executes cells until ctx is cancelled or Stop is called.
func (c *Cellar) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return ErrCellarStarted
	}
	if c.store == nil {
		c.mu.Unlock()
		return ErrStoreNil
	}
	if err := c.validateHandlers(); err != nil {
		c.mu.Unlock()
		return err
	}
	if err := c.store.Recover(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("recover store: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.registry.Freeze()
	c.started = true
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	c.mu.Unlock()

	defer close(done)
	return c.scheduler.Run(runCtx)
}

func (c *Cellar) validateHandlers() error {
	cells, err := c.store.ListActive()
	if err != nil {
		return fmt.Errorf("list active cells: %w", err)
	}
	for _, cell := range cells {
		if registration, ok := c.registry.Lookup(cell.HandlerName); !ok || registration == nil {
			return fmt.Errorf("%w: %s", ErrHandlerNotRegistered, cell.HandlerName)
		}
	}
	return nil
}

// Stop requests shutdown and waits for a running Cellar runtime to stop.
func (c *Cellar) Stop() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	cancel()
	<-done
	return nil
}

type workerDispatcher struct {
	worker *Worker
}

func (d workerDispatcher) Dispatch(ctx context.Context, cell Cell) error {
	result := d.worker.Run(ctx, cell)
	if result, ok := result.(ErrorResult); ok {
		if result.Err != nil {
			return fmt.Errorf("%s: %w", result.Message, result.Err)
		}
		return errors.New(result.Message)
	}
	return nil
}

var (
	ErrCellarStarted        = errors.New("cellar has started")
	ErrStoreNil             = errors.New("store is nil")
	ErrHandlerNotRegistered = errors.New("handler not registered")
)

type typedJSONRegistration[T any] struct {
	handler Handler[T]
}

func (r typedJSONRegistration[T]) Execute(ctx context.Context, cell Cell) Result {
	var payload T
	if err := unmarshalJSON(cell.Payload, &payload); err != nil {
		return ErrorResult{Message: "decode handler payload", Err: err}
	}
	return r.handler.Handle(ctx, payload)
}

func (r typedJSONRegistration[T]) Inspect(cell Cell) Inspection {
	var payload T
	err := unmarshalJSON(cell.Payload, &payload)
	return Inspection{
		Cell:          cloneCell(cell),
		Payload:       payload,
		PayloadFormat: "json",
		DecodeError:   err,
	}
}

var ErrHandlerNil = errors.New("handler is nil")
