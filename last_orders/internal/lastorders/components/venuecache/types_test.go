package venuecache

import (
	"testing"
)

func TestProjectionFromDocumentNormalisesFieldsAndPreservesRecurrence(t *testing.T) {
	projection, err := ProjectionFromDocument(Document{
		ID: "venue-1",
		Data: map[string]any{
			"name":                 "The Crown",
			"venueType":            "pub",
			"web_site":             "https://example.test",
			"map":                  "map-value",
			"address":              "1 High Street",
			"pubImage":             "image-value",
			"next_occurrence_date": "2026-08-27",
			"recurrence":           map[string]any{"frequency": "weekly", "interval": int64(1)},
		},
	})
	if err != nil {
		t.Fatalf("project document: %v", err)
	}
	if projection.ID != "venue-1" || projection.Name != "The Crown" || projection.Website != "https://example.test" || projection.PhotoURL != "image-value" {
		t.Fatalf("unexpected projection: %#v", projection)
	}
	if projection.RecurrenceJSON != `{"frequency":"weekly","interval":1}` {
		t.Fatalf("recurrence JSON = %q", projection.RecurrenceJSON)
	}
}

func TestProjectionFromDocumentRequiresName(t *testing.T) {
	if _, err := ProjectionFromDocument(Document{ID: "venue-1", Data: map[string]any{}}); err == nil {
		t.Fatal("expected missing name error")
	}
}
