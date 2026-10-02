package autocomplete

import (
	"testing"

	"last_orders/internal/lastorders/truths"
)

func TestValidCandidateTruth(t *testing.T) {
	valid := truths.PollAutoCompletionDue{Poll: truths.PollAutoCompletionSnapshot{
		PollID: "poll-1", PollDate: "2026-09-01", Completed: false,
	}}
	if !validCandidateTruth(valid) {
		t.Fatal("open poll Truth should be valid")
	}
	valid.Poll.Completed = true
	if validCandidateTruth(valid) {
		t.Fatal("completed poll Truth should be invalid")
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		venueIDs   []string
		voteCounts map[string]int
		food       map[string]bool
		exists     map[string]bool
		want       Decision
	}{
		{name: "single venue", venueIDs: []string{"venue-1"}, want: Decision{SelectedVenueID: "venue-1"}},
		{name: "no votes", venueIDs: []string{"venue-1", "venue-2"}, want: Decision{Reason: ReasonNoVotes}},
		{name: "tie", venueIDs: []string{"venue-1", "venue-2"}, voteCounts: map[string]int{"venue-1": 2, "venue-2": 2}, want: Decision{Reason: ReasonTie}},
		{name: "missing venue", venueIDs: []string{"venue-1", "venue-2"}, voteCounts: map[string]int{"venue-1": 3}, want: Decision{Reason: ReasonWinnerVenueMissing}},
		{name: "not food eligible", venueIDs: []string{"venue-1", "venue-2"}, voteCounts: map[string]int{"venue-1": 3}, exists: map[string]bool{"venue-1": true}, want: Decision{Reason: ReasonWinnerNotFoodEligible}},
		{name: "clear winner", venueIDs: []string{"venue-1", "venue-2"}, voteCounts: map[string]int{"venue-1": 3, "venue-2": 1}, exists: map[string]bool{"venue-1": true}, food: map[string]bool{"venue-1": true}, want: Decision{SelectedVenueID: "venue-1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Decide(test.venueIDs, test.voteCounts, test.food, test.exists); got != test.want {
				t.Fatalf("Decide() = %+v, want %+v", got, test.want)
			}
		})
	}
}
