package cellar

import "context"

// Worker executes claimed cells by delegating to a registered handler.
type Worker struct {
	registry Registry
	applier  ResultApplier
}

// NewWorker creates a worker runner backed by the supplied registry.
func NewWorker(registry Registry, appliers ...ResultApplier) *Worker {
	worker := &Worker{registry: registry}
	worker.applier = MultiResultApplier(appliers)
	return worker
}

// Run executes a claimed cell using the registered handler.
func (w *Worker) Run(ctx context.Context, cell Cell) Result {
	if w.registry == nil {
		return ErrorResult{Message: "registry is nil"}
	}

	registration, ok := w.registry.Lookup(cell.HandlerName)
	if !ok || registration == nil {
		return ErrorResult{Message: "handler not registered"}
	}

	result := registration.Execute(ctx, cell)
	if w.applier != nil {
		if err := w.applier.ApplyResult(ctx, cell, result); err != nil {
			return ErrorResult{Message: "apply result failed", Err: err}
		}
	}

	return result
}
