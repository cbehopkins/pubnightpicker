package autocomplete_test

import (
	"context"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/services/autocomplete"
	"last_orders/internal/lastorders/services/autocomplete/autocompletetest"
	"last_orders/internal/lastorders/truths"
)

func TestDiscoveryFindsOverduePollsWithinPollsSince(t *testing.T) {
	source := autocompletetest.New()
	source.PollsSince = "2026-10-01"
	source.SeedPoll("overdue-poll", map[string]any{
		"date":      "2026-10-25",
		"completed": false,
		"pubs":      []string{"venue-1"},
	})
	source.SeedPoll("before-polls-since", map[string]any{
		"date":      "2026-09-30",
		"completed": false,
		"pubs":      []string{"venue-1"},
	})
	source.SeedPoll("future-poll", map[string]any{
		"date":      "2026-10-27",
		"completed": false,
		"pubs":      []string{"venue-1"},
	})

	result := (autocomplete.DiscoveryHandler{Source: source}).Handle(context.Background(), truths.DailyPollAutoCompleteDue{
		ObservedOn: "2026-10-26",
	})

	complete, ok := result.(cellar.Complete)
	if !ok {
		t.Fatalf("result type = %T, want cellar.Complete", result)
	}
	if len(complete.NewCells) != 1 {
		t.Fatalf("discovery created %d cells, want only the overdue poll inside PollsSince..observedOn", len(complete.NewCells))
	}
	check, err := cellar.JSONCodec[idempotency.CheckPayload]().Unmarshal(complete.NewCells[0].Steps[0].Payload)
	if err != nil {
		t.Fatalf("decode work payload: %v", err)
	}
	if check.Key != "overdue-poll_2026-10-25" {
		t.Fatalf("work key = %q, want overdue-poll_2026-10-25", check.Key)
	}
}
