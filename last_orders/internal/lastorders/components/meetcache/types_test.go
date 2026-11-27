package meetcache

import "testing"

func TestProjectionCompletionComesFromSelectedVenue(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		complete bool
	}{
		{name: "missing selection", data: map[string]any{}, complete: false},
		{name: "empty selection", data: map[string]any{"selected": ""}, complete: false},
		{name: "selected venue", data: map[string]any{"selected": "venue-1"}, complete: true},
		{name: "unexpected any sentinel", data: map[string]any{"selected": "any"}, complete: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{"groupId": "group-1", "date": "2026-11-26", "completed": !test.complete}
			for key, value := range test.data {
				data[key] = value
			}
			projection, err := ProjectionFromDocument(Document{ID: "meet-1", Data: data})
			if err != nil {
				t.Fatalf("ProjectionFromDocument() error = %v", err)
			}
			if projection.VotingComplete != test.complete {
				t.Fatalf("VotingComplete = %v, want %v", projection.VotingComplete, test.complete)
			}
		})
	}
}
