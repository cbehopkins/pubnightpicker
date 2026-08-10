// Package worker provides internal worker execution implementations for cellar.
package worker

import (
	"context"

	"cellar/pkg/cellar"
)

// Worker executes claimed cells by delegating to a registered handler.
type Worker struct {
	registry cellar.Registry
	applier  cellar.ResultApplier
}

// NewWorker creates a worker runner backed by the supplied registry.
func NewWorker(registry cellar.Registry, appliers ...cellar.ResultApplier) *Worker {
	worker := &Worker{registry: registry}
	worker.applier = cellar.MultiResultApplier(appliers)
	return worker
}

// Run executes a claimed cell using the registered handler.
func (w *Worker) Run(ctx context.Context, cell cellar.Cell) cellar.Result {
	if w.registry == nil {
		return cellar.ErrorResult{Message: "registry is nil"}
	}

	registration, ok := w.registry.Lookup(cell.HandlerName)
	if !ok || registration == nil {
		return cellar.ErrorResult{Message: "handler not registered"}
	}

	result := registration.Execute(ctx, cell)
	if w.applier != nil {
		if err := w.applier.ApplyResult(ctx, cell, result); err != nil {
			return cellar.ErrorResult{Message: "apply result failed", Err: err}
		}
	}

	return result
}
