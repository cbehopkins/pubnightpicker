package venuecache

import (
	"reflect"
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

func TestProjectionFromDocumentPreservesOptionalFields(t *testing.T) {
	projection, err := ProjectionFromDocument(Document{
		ID: "venue-1",
		Data: map[string]any{
			"name": "The Crown", "notes": "", "parking": false, "food": true,
			"dog_friend": true, "beer_gerden": false, "out_of_town": true, "banned": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projection.Notes == nil || *projection.Notes != "" {
		t.Fatalf("notes = %v; want present empty string", projection.Notes)
	}
	for _, field := range []struct {
		name  string
		value *bool
		want  bool
	}{
		{"parking", projection.Parking, false},
		{"food", projection.Food, true},
		{"dog_friend", projection.DogFriendly, true},
		{"beer_gerden", projection.BeerGarden, false},
		{"out_of_town", projection.OutOfTown, true},
		{"banned", projection.Banned, false},
	} {
		if field.value == nil || *field.value != field.want {
			t.Errorf("%s = %v; want present %v", field.name, field.value, field.want)
		}
	}
	absent, err := ProjectionFromDocument(Document{ID: "venue-1", Data: map[string]any{"name": "The Crown"}})
	if err != nil {
		t.Fatal(err)
	}
	if absent.Notes != nil || absent.Parking != nil || absent.Food != nil || absent.DogFriendly != nil ||
		absent.BeerGarden != nil || absent.OutOfTown != nil || absent.Banned != nil {
		t.Fatalf("absent optional fields acquired defaults: %+v", absent)
	}
}

func TestProjectionFromDocumentRejectsInvalidOptionalFields(t *testing.T) {
	for _, field := range []string{"notes", "parking", "food", "dog_friend", "beer_gerden", "out_of_town", "banned"} {
		t.Run(field, func(t *testing.T) {
			for _, invalid := range []any{nil, int64(1), []string{"invalid"}} {
				if _, err := ProjectionFromDocument(Document{
					ID: "venue-1", Data: map[string]any{"name": "The Crown", field: invalid},
				}); err == nil {
					t.Fatalf("accepted %s = %#v", field, invalid)
				}
			}
		})
	}
}

func TestProjectionNormalisesRecurrenceAndIgnoresUndeclaredFields(t *testing.T) {
	first := Document{ID: "venue-1", Data: map[string]any{
		"name": "The Crown", "uid": "legacy", "untracked": "first",
		"recurrence": map[string]any{"frequency": "weekly", "interval": int64(1), "weekdays": []any{int64(4), int64(1), int64(4)}},
	}}
	second := Document{ID: "venue-1", Data: map[string]any{
		"name": "The Crown", "untracked": "second",
		"recurrence": map[string]any{"weekdays": []int{1, 4}, "interval": float64(1), "frequency": "weekly"},
	}}
	left, err := ProjectionFromDocument(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := ProjectionFromDocument(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("equivalent documents differ: %+v and %+v", left, right)
	}
	if left.RecurrenceJSON != `{"frequency":"weekly","interval":1,"weekdays":[1,4]}` {
		t.Fatalf("normalised recurrence = %s", left.RecurrenceJSON)
	}
	if !reflect.DeepEqual(first.Data["recurrence"].(map[string]any)["weekdays"], []any{int64(4), int64(1), int64(4)}) {
		t.Fatal("normalisation mutated the source document")
	}
}

func TestProjectionRecurrencePreservesAbsenceAndEmptyValues(t *testing.T) {
	for _, test := range []struct {
		name string
		data map[string]any
		want string
	}{
		{"absent", map[string]any{"name": "The Crown"}, ""},
		{"empty object", map[string]any{"name": "The Crown", "recurrence": map[string]any{}}, `{}`},
		{"empty weekdays", map[string]any{"name": "The Crown", "recurrence": map[string]any{"weekdays": []int{}}}, `{"weekdays":[]}`},
		{"weekday zero", map[string]any{"name": "The Crown", "recurrence": map[string]any{"weekday": 0}}, `{"weekday":0}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ProjectionFromDocument(Document{ID: "venue-1", Data: test.data})
			if err != nil || got.RecurrenceJSON != test.want {
				t.Fatalf("recurrence = %q, %v; want %q", got.RecurrenceJSON, err, test.want)
			}
		})
	}
}

func TestProjectionRejectsInvalidRecurrence(t *testing.T) {
	for _, invalid := range []any{
		nil, "weekly", []any{}, map[string]any{"unknown": true},
		map[string]any{"frequency": nil}, map[string]any{"frequency": "never"},
		map[string]any{"date": "2026-02-30"}, map[string]any{"interval": 0},
		map[string]any{"interval": 1.5}, map[string]any{"weekday": 7},
		map[string]any{"month": 0}, map[string]any{"month_day": 32},
		map[string]any{"nth": 5}, map[string]any{"weekdays": []int{0, 7}},
		map[string]any{"weekdays": []any{nil}},
	} {
		if got, err := ProjectionFromDocument(Document{ID: "venue-1", Data: map[string]any{
			"name": "The Crown", "recurrence": invalid,
		}}); err == nil {
			t.Fatalf("accepted recurrence %#v: %+v", invalid, got)
		}
	}
}
