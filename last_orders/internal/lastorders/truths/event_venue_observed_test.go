package truths

import (
	"testing"

	"last_orders/internal/lastorders/components/recurrence"
)

func TestEventVenueObservedIdentityIncludesEvidenceAndDate(t *testing.T) {
	baseline := EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-1",
			Recurrence:         map[string]any{"frequency": "weekly", "interval": 2},
			NextOccurrenceDate: "2030-01-05",
		},
		ObservedOn: "2030-01-01",
	}

	if baseline.Identity() != baseline.Identity() {
		t.Fatal("identity must be stable")
	}
	changedDate := baseline
	changedDate.ObservedOn = "2030-01-02"
	if baseline.Identity() == changedDate.Identity() {
		t.Fatal("identity must distinguish observations on different dates")
	}
	changedRecurrence := baseline
	changedRecurrence.Venue.Recurrence = map[string]any{"frequency": "weekly", "interval": 3}
	if baseline.Identity() == changedRecurrence.Identity() {
		t.Fatal("identity must distinguish changed recurrence evidence")
	}
}
