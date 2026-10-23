package venuecache

import (
	"slices"
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
	if !left.Equal(right) {
		t.Fatalf("equivalent documents differ: %+v and %+v", left, right)
	}
	if left.RecurrenceJSON != `{"frequency":"weekly","interval":1,"weekdays":[1,4]}` {
		t.Fatalf("normalised recurrence = %s", left.RecurrenceJSON)
	}
	if !slices.Equal(first.Data["recurrence"].(map[string]any)["weekdays"].([]any), []any{int64(4), int64(1), int64(4)}) {
		t.Fatal("normalisation mutated the source document")
	}
}

func TestVenueProjectionEqual(t *testing.T) {
	notes := "notes"
	otherNotes := "notes"
	flag := false
	otherFlag := false
	baseline := VenueProjection{
		ID: "venue-1", Name: "The Crown", VenueType: "pub", Website: "website",
		Map: "map", Address: "address", PhotoURL: "photo", RecurrenceJSON: `{}`,
		NextOccurrenceDate: "2026-10-23", Notes: &notes, Parking: &flag, Food: &flag,
		DogFriendly: &flag, BeerGarden: &flag, OutOfTown: &flag, Banned: &flag,
	}
	other := baseline
	other.Notes = &otherNotes
	other.Parking, other.Food, other.DogFriendly = &otherFlag, &otherFlag, &otherFlag
	other.BeerGarden, other.OutOfTown, other.Banned = &otherFlag, &otherFlag, &otherFlag
	if !baseline.Equal(other) || !other.Equal(baseline) {
		t.Fatal("equal optional values at different addresses must compare equal")
	}
	if !(VenueProjection{}).Equal(VenueProjection{}) {
		t.Fatal("absent optional values must compare equal")
	}
	for _, test := range []struct {
		name   string
		change func(*VenueProjection)
	}{
		{"ID", func(value *VenueProjection) { value.ID = "other" }},
		{"Name", func(value *VenueProjection) { value.Name = "other" }},
		{"VenueType", func(value *VenueProjection) { value.VenueType = "other" }},
		{"Website", func(value *VenueProjection) { value.Website = "other" }},
		{"Map", func(value *VenueProjection) { value.Map = "other" }},
		{"Address", func(value *VenueProjection) { value.Address = "other" }},
		{"PhotoURL", func(value *VenueProjection) { value.PhotoURL = "other" }},
		{"RecurrenceJSON", func(value *VenueProjection) { value.RecurrenceJSON = "other" }},
		{"NextOccurrenceDate", func(value *VenueProjection) { value.NextOccurrenceDate = "other" }},
		{"Notes", func(value *VenueProjection) { value.Notes = nil }},
		{"Parking", func(value *VenueProjection) { value.Parking = nil }},
		{"Food", func(value *VenueProjection) { value.Food = nil }},
		{"DogFriendly", func(value *VenueProjection) { value.DogFriendly = nil }},
		{"BeerGarden", func(value *VenueProjection) { value.BeerGarden = nil }},
		{"OutOfTown", func(value *VenueProjection) { value.OutOfTown = nil }},
		{"Banned", func(value *VenueProjection) { value.Banned = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := baseline
			test.change(&changed)
			if baseline.Equal(changed) || changed.Equal(baseline) {
				t.Fatalf("different %s values compared equal", test.name)
			}
		})
	}
}

func TestEqualOptionalValues(t *testing.T) {
	falseValue, trueValue := false, true
	empty, populated := "", "notes"
	if equalOptional(&falseValue, &trueValue) || equalOptional(&empty, &populated) {
		t.Fatal("different present values compared equal")
	}
	if equalOptional(nil, &falseValue) || equalOptional(&falseValue, nil) ||
		equalOptional(nil, &empty) || equalOptional(&empty, nil) {
		t.Fatal("absent values compared equal to present zero values")
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
