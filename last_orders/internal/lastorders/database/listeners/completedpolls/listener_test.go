package completedpolls

import (
	"testing"

	"last_orders/internal/lastorders/truths"
)

func TestCompletedPayloadCarriesSelectionAndDate(t *testing.T) {
	got := completedPayload(Change{Kind: ChangeModified, Doc: Document{ID: "poll-1", Data: map[string]any{
		"selected":        "pub-1",
		"date":            "2026-10-02",
		"restaurant":      "restaurant-1",
		"restaurant_time": "18:30",
		"completed":       true,
	}}})
	want := truths.PollObservedPayload{
		PollID:                 "poll-1",
		ChangeKind:             "modified",
		SelectedVenueID:        "pub-1",
		PollDate:               "2026-10-02",
		SelectedRestaurantID:   "restaurant-1",
		SelectedRestaurantTime: "18:30",
	}
	if got != want {
		t.Fatalf("completedPayload() = %+v, want %+v", got, want)
	}
}

func TestCompletedPayloadIgnoresMistypedFields(t *testing.T) {
	got := completedPayload(Change{Kind: ChangeAdded, Doc: Document{ID: "poll-1", Data: map[string]any{
		"selected": 7,
		"date":     true,
	}}})
	if got.SelectedVenueID != "" || got.PollDate != "" {
		t.Fatalf("completedPayload() = %+v, want mistyped fields left empty", got)
	}
}

func TestCompletedEventKey(t *testing.T) {
	tests := []struct {
		name                   string
		pollID                 string
		selectedVenueID        string
		selectedRestaurantID   string
		selectedRestaurantTime string
		want                   string
	}{
		{
			name:            "pub only",
			pollID:          "poll-1",
			selectedVenueID: "pub-1",
			want:            "poll-1:pub-1",
		},
		{
			name:                 "pub and restaurant",
			pollID:               "poll-1",
			selectedVenueID:      "pub-1",
			selectedRestaurantID: "restaurant-1",
			want:                 "poll-1:pub-1:restaurant-1",
		},
		{
			name:                   "pub restaurant and time",
			pollID:                 "poll-1",
			selectedVenueID:        "pub-1",
			selectedRestaurantID:   "restaurant-1",
			selectedRestaurantTime: "19:30",
			want:                   "poll-1:pub-1:restaurant-1:19:30",
		},
		{
			name:                   "time without restaurant",
			pollID:                 "poll-1",
			selectedVenueID:        "pub-1",
			selectedRestaurantTime: "19:30",
			want:                   "poll-1:pub-1:19:30",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := completedEventKey(test.pollID, test.selectedVenueID, test.selectedRestaurantID, test.selectedRestaurantTime)
			if got != test.want {
				t.Fatalf("completedEventKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompletedEventKeyChangesForReschedule(t *testing.T) {
	original := completedEventKey("poll-1", "pub-1", "restaurant-1", "19:30")

	keys := []string{
		completedEventKey("poll-1", "pub-2", "restaurant-1", "19:30"),
		completedEventKey("poll-1", "pub-1", "restaurant-2", "19:30"),
		completedEventKey("poll-1", "pub-1", "restaurant-1", "20:00"),
	}
	for _, key := range keys {
		if key == original {
			t.Fatal("changing the selected venue must change the event key")
		}
	}
}
