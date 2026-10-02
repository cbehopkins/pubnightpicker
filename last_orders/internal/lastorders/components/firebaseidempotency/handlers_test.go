package firebaseidempotency

import (
	"encoding/json"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

func TestNewCellRequestAssignsUniqueOwners(t *testing.T) {
	envelope, err := truths.NewEnvelope(truths.PollOpenedFanout, truths.PollObservedPayload{PollID: "duplicate-poll"})
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[cellar.CellID]struct{})
	for range 1000 {
		request, err := NewCellRequest("PollOpened", "duplicate-poll", envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := owners[request.ID]; exists {
			t.Fatalf("duplicate observation owner: %s", request.ID)
		}
		owners[request.ID] = struct{}{}
		for _, step := range request.Steps {
			var payload StepPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Owner != string(request.ID) {
				t.Fatalf("step owner = %q, want %q", payload.Owner, request.ID)
			}
		}
	}
}
