package autocomplete

import (
	"context"
	"encoding/json"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

func TestAmbiguousHandlerEmitsManualCompletionTruth(t *testing.T) {
	result, ok := AmbiguousHandler{}.Handle(context.Background(), CompletionAmbiguousPayload{
		PollID: "poll-1", PollDate: "2026-10-02", Reason: "tie",
	}).(cellar.Complete)
	if !ok || len(result.NewCells) != 1 {
		t.Fatalf("result = %#v, want one Truth cell", result)
	}
	step := result.NewCells[0].Steps[0]
	if step.HandlerName != truths.PollManualCompletionRequiredFanout {
		t.Fatalf("handler = %q", step.HandlerName)
	}
	var truth truths.PollManualCompletionRequired
	if err := json.Unmarshal(step.Payload, &truth); err != nil {
		t.Fatal(err)
	}
	if truth != (truths.PollManualCompletionRequired{PollID: "poll-1", PollDate: "2026-10-02", Reason: "tie"}) {
		t.Fatalf("truth = %+v", truth)
	}
}
