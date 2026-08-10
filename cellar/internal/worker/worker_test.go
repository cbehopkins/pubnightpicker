package worker

import (
	"context"
	"testing"

	"cellar/pkg/cellar"
)

type stubRegistration struct {
	executed []cellar.Cell
	result   cellar.Result
}

type stubApplier struct {
	applied bool
}

func (s *stubApplier) ApplyResult(ctx context.Context, cell cellar.Cell, result cellar.Result) error {
	s.applied = true
	return nil
}

func (s *stubRegistration) Execute(ctx context.Context, cell cellar.Cell) cellar.Result {
	s.executed = append(s.executed, cell)
	return s.result
}

func (s *stubRegistration) Inspect(cell cellar.Cell) cellar.Inspection {
	return cellar.Inspection{Cell: cell, PayloadFormat: "stub"}
}

func TestRunnerRunsRegisteredRegistration(t *testing.T) {
	registration := &stubRegistration{result: cellar.Complete{}}
	registry := cellar.NewMemoryRegistry()
	if err := registry.Register("demo", registration); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	runner := NewWorker(registry)
	cell := cellar.Cell{ID: "cell-1", HandlerName: "demo"}

	result := runner.Run(context.Background(), cell)
	if len(registration.executed) != 1 {
		t.Fatalf("Execute() calls = %d, want 1", len(registration.executed))
	}
	if registration.executed[0].ID != cell.ID {
		t.Fatalf("Execute() cell ID = %q, want %q", registration.executed[0].ID, cell.ID)
	}
	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("Run() result type = %T, want cellar.Complete", result)
	}
}

func TestRunnerReturnsErrorResultForUnknownHandler(t *testing.T) {
	runner := NewWorker(cellar.NewMemoryRegistry())
	result := runner.Run(context.Background(), cellar.Cell{HandlerName: "missing"})

	if _, ok := result.(cellar.ErrorResult); !ok {
		t.Fatalf("Run() result type = %T, want cellar.ErrorResult", result)
	}
}

func TestRunnerAppliesResultThroughConfiguredApplier(t *testing.T) {
	applier := &stubApplier{}
	runner := NewWorker(cellar.NewMemoryRegistry(), applier)
	result := runner.Run(context.Background(), cellar.Cell{HandlerName: "missing"})
	if _, ok := result.(cellar.ErrorResult); !ok {
		t.Fatalf("Run() result type = %T, want cellar.ErrorResult", result)
	}
	if applier.applied {
		t.Fatal("ApplyResult() should not be called for unknown handlers")
	}
}

func TestRunnerAppliesResultToAllConfiguredAppliers(t *testing.T) {
	first := &stubApplier{}
	second := &stubApplier{}
	registry := cellar.NewMemoryRegistry()
	registration := &stubRegistration{result: cellar.Complete{}}
	if err := registry.Register("demo", registration); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	runner := NewWorker(registry, first, second)
	result := runner.Run(context.Background(), cellar.Cell{ID: "cell-1", HandlerName: "demo"})
	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("Run() result type = %T, want cellar.Complete", result)
	}
	if !first.applied {
		t.Fatal("first applier was not invoked")
	}
	if !second.applied {
		t.Fatal("second applier was not invoked")
	}
}
