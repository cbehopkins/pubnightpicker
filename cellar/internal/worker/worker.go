// Package worker provides internal worker execution implementations for cellar.
package worker

import (
	"context"

	"cellar/pkg/cellar"
)

// Worker executes claimed cells by delegating to a registered handler.
type Worker struct {
	registry cellar.Registry
}

// NewWorker creates a worker runner backed by the supplied registry.
func NewWorker(registry cellar.Registry) *Worker {
	return &Worker{registry: registry}
}

// Run executes a claimed cell using the registered handler.
func (r *Worker) Run(ctx context.Context, cell cellar.Cell) cellar.Result {
	if r.registry == nil {
		return cellar.ErrorResult{Message: "registry is nil"}
	}

	registration, ok := r.registry.Lookup(cell.HandlerName)
	if !ok || registration == nil {
		return cellar.ErrorResult{Message: "handler not registered"}
	}

	return registration.Execute(ctx, cell)
}
