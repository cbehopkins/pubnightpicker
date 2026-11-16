package venueedit

import (
	"testing"

	"last_orders/internal/lastorders/components/venuecache"
)

func TestResolvePatchAcceptsBaselineAndOmitsConvergedFields(t *testing.T) {
	nameBaseline, err := CanonicalValueDigest("Original name")
	if err != nil {
		t.Fatal(err)
	}
	addressBaseline, err := CanonicalValueDigest("Old Road")
	if err != nil {
		t.Fatal(err)
	}
	fields := []venuecache.FieldUpdate{
		{APIField: "name", CanonicalValue: "New name", BaselineDigest: nameBaseline},
		{APIField: "address", CanonicalValue: "Current Road", BaselineDigest: addressBaseline},
	}

	updates, conflicts, err := ResolvePatch(venuecache.VenueProjection{
		ID: "venue-1", Name: "Original name", Address: "Current Road",
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 || len(updates) != 1 || updates[0].APIField != "name" {
		t.Fatalf("updates=%+v conflicts=%v; want only name update", updates, conflicts)
	}
}

func TestResolvePatchRejectsTheWholePlanOnDivergence(t *testing.T) {
	nameBaseline, err := CanonicalValueDigest("Original name")
	if err != nil {
		t.Fatal(err)
	}
	addressBaseline, err := CanonicalValueDigest("Old Road")
	if err != nil {
		t.Fatal(err)
	}
	fields := []venuecache.FieldUpdate{
		{APIField: "name", CanonicalValue: "New name", BaselineDigest: nameBaseline},
		{APIField: "address", CanonicalValue: "Submitted Road", BaselineDigest: addressBaseline},
	}

	updates, conflicts, err := ResolvePatch(venuecache.VenueProjection{
		ID: "venue-1", Name: "Original name", Address: "Other Road",
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 || len(conflicts) != 1 || conflicts[0] != "address" {
		t.Fatalf("updates=%+v conflicts=%v; want no updates and address conflict", updates, conflicts)
	}
}

func TestCanonicalAPIValuesAppliesVenueCompatibilityDefaults(t *testing.T) {
	values := CanonicalAPIValues(venuecache.VenueProjection{ID: "venue-1", Name: "The Crown"})
	if values["venue_type"] != "pub" || values["website_url"] != nil || values["notes"] != "" {
		t.Fatalf("canonical values=%v; want public compatibility defaults", values)
	}
	for _, field := range []string{"visible_group_ids", "previous_venue_ids"} {
		value, ok := values[field].([]string)
		if !ok || value == nil || len(value) != 0 {
			t.Errorf("%s=%#v; want non-nil empty string slice", field, values[field])
		}
	}
}
