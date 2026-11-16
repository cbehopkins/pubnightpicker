package venuecache

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// CanonicalAPIValues returns the public API value of every editable venue field.
func CanonicalAPIValues(venue VenueProjection) map[string]any {
	values := map[string]any{
		"name":                 venue.Name,
		"venue_type":           venue.VenueType,
		"website_url":          optionalText(venue.Website),
		"map_url":              optionalText(venue.Map),
		"address":              venue.Address,
		"location_id":          optionalText(venue.LocationID),
		"image_url":            optionalText(venue.PhotoURL),
		"next_occurrence_date": optionalText(venue.NextOccurrenceDate),
		"notes":                textOrEmpty(venue.Notes),
		"parking":              boolOrFalse(venue.Parking),
		"food":                 boolOrFalse(venue.Food),
		"dog_friendly":         boolOrFalse(venue.DogFriendly),
		"beer_garden":          boolOrFalse(venue.BeerGarden),
		"out_of_town":          boolOrFalse(venue.OutOfTown),
		"banned":               boolOrFalse(venue.Banned),
		"visible_group_ids":    nonNilStrings(venue.VisibleGroupIDs),
		"previous_venue_ids":   nonNilStrings(venue.PreviousVenueIDs),
	}
	if values["venue_type"] == "" {
		values["venue_type"] = "pub"
	}
	if venue.RecurrenceJSON == "" {
		values["recurrence"] = nil
	} else {
		var recurrence any
		if json.Unmarshal([]byte(venue.RecurrenceJSON), &recurrence) == nil {
			values["recurrence"] = recurrence
		} else {
			values["recurrence"] = json.RawMessage(venue.RecurrenceJSON)
		}
	}
	return values
}

func CanonicalValueDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical venue value: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func optionalText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func textOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolOrFalse(value *bool) bool {
	return value != nil && *value
}

func nonNilStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}
