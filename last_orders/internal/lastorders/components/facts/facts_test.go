package facts

import (
	"testing"

	"cellar/pkg/cellar"
)

func TestRegistryTargetsAreIsolatedPerFact(t *testing.T) {
	registry := NewRegistry()
	registry.Register("NewPoll", "polls.new")
	registry.Register("CompletedPoll", "polls.completed")

	newPollTargets := registry.Targets("NewPoll")
	if len(newPollTargets) != 1 || newPollTargets[0] != "polls.new" {
		t.Fatalf("unexpected NewPoll targets: %v", newPollTargets)
	}

	completedTargets := registry.Targets("CompletedPoll")
	if len(completedTargets) != 1 || completedTargets[0] != "polls.completed" {
		t.Fatalf("unexpected CompletedPoll targets: %v", completedTargets)
	}

	if targets := registry.Targets("Unknown"); len(targets) != 0 {
		t.Fatalf("expected no targets for an unregistered fact, got %v", targets)
	}
}

func TestCellRequestBuildsSingleStepFactCell(t *testing.T) {
	request, err := CellRequest("NewPoll", []byte(`{"poll_id":"1"}`))
	if err != nil {
		t.Fatalf("build cell request: %v", err)
	}
	if len(request.Steps) != 1 || request.Steps[0].HandlerName != HandlerName {
		t.Fatalf("expected single step targeting %s, got %+v", HandlerName, request.Steps)
	}
}

func TestFanoutExpanderReturnsKeyedTargetsPerRegisteredHandler(t *testing.T) {
	registry := NewRegistry()
	registry.Register("NewPoll", "polls.new", "polls.audit")

	fanout, err := Fanout(registry)
	if err != nil {
		t.Fatalf("build fanout: %v", err)
	}

	runtime := cellar.New(cellar.NewMemoryStore(nil), cellar.Config{})
	if err := fanout.Register(runtime); err != nil {
		t.Fatalf("register fanout: %v", err)
	}

	if _, err := fanout.Add(runtime, Fact{Name: "NewPoll", Payload: []byte(`{"poll_id":"1"}`)}); err != nil {
		t.Fatalf("add fanout: %v", err)
	}
}
