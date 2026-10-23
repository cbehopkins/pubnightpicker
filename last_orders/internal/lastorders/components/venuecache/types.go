package venuecache

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"cloud.google.com/go/firestore"
)

const collection = "pubs"

// VenueProjection contains the backend fields retained from a pubs document.
type VenueProjection struct {
	ID                 string
	Name               string
	VenueType          string
	Website            string
	Map                string
	Address            string
	PhotoURL           string
	RecurrenceJSON     string
	NextOccurrenceDate string
	Notes              *string
	Parking            *bool
	Food               *bool
	DogFriendly        *bool
	BeerGarden         *bool
	OutOfTown          *bool
	Banned             *bool
}

var ErrNotFound = fmt.Errorf("venue not found")
var ErrCacheMiss = fmt.Errorf("venue cache miss")

func ProjectionFromDocument(doc Document) (VenueProjection, error) {
	if doc.ID == "" {
		return VenueProjection{}, fmt.Errorf("venue document ID is required")
	}
	name, err := requiredString(doc.Data, "name")
	if err != nil {
		return VenueProjection{}, err
	}
	projection := VenueProjection{
		ID:                 doc.ID,
		Name:               name,
		VenueType:          optionalString(doc.Data, "venueType"),
		Website:            optionalString(doc.Data, "web_site"),
		Map:                optionalString(doc.Data, "map"),
		Address:            optionalString(doc.Data, "address"),
		PhotoURL:           optionalString(doc.Data, "pubImage"),
		NextOccurrenceDate: optionalString(doc.Data, "next_occurrence_date"),
	}
	projection.Notes, err = optionalField[string](doc.Data, "notes")
	if err != nil {
		return VenueProjection{}, err
	}
	for _, field := range []struct {
		name   string
		target **bool
	}{
		{"parking", &projection.Parking},
		{"food", &projection.Food},
		{"dog_friend", &projection.DogFriendly},
		{"beer_gerden", &projection.BeerGarden},
		{"out_of_town", &projection.OutOfTown},
		{"banned", &projection.Banned},
	} {
		*field.target, err = optionalField[bool](doc.Data, field.name)
		if err != nil {
			return VenueProjection{}, err
		}
	}
	if recurrence, ok := doc.Data["recurrence"]; ok {
		projection.RecurrenceJSON, err = normaliseRecurrence(recurrence)
		if err != nil {
			return VenueProjection{}, err
		}
	}
	return projection, nil
}

func FromDocument(doc *firestore.DocumentSnapshot) (VenueProjection, error) {
	if doc == nil || doc.Ref == nil {
		return VenueProjection{}, fmt.Errorf("venue document is nil")
	}
	return ProjectionFromDocument(Document{ID: doc.Ref.ID, Data: doc.Data()})
}

func requiredString(data map[string]any, field string) (string, error) {
	value, ok := data[field].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("venue field %q is required and must be a non-empty string", field)
	}
	return value, nil
}

func optionalString(data map[string]any, field string) string {
	value, _ := data[field].(string)
	return value
}

func optionalField[Value any](data map[string]any, field string) (*Value, error) {
	raw, exists := data[field]
	if !exists {
		return nil, nil
	}
	value, valid := raw.(Value)
	if !valid {
		return nil, fmt.Errorf("venue field %q has invalid type %T", field, raw)
	}
	return &value, nil
}

type projectedRecurrence struct {
	Frequency *string `json:"frequency,omitempty"`
	Date      *string `json:"date,omitempty"`
	Interval  *int64  `json:"interval,omitempty"`
	Weekdays  *[]int  `json:"weekdays,omitempty"`
	Weekday   *int    `json:"weekday,omitempty"`
	Nth       *int    `json:"nth,omitempty"`
	Month     *int    `json:"month,omitempty"`
	MonthDay  *int    `json:"month_day,omitempty"`
}

func normaliseRecurrence(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode recurrence: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || fields == nil {
		return "", fmt.Errorf("venue recurrence must be an object")
	}
	var rule projectedRecurrence
	targets := map[string]any{
		"frequency": &rule.Frequency, "date": &rule.Date, "interval": &rule.Interval,
		"weekdays": &rule.Weekdays, "weekday": &rule.Weekday, "nth": &rule.Nth,
		"month": &rule.Month, "month_day": &rule.MonthDay,
	}
	for name, raw := range fields {
		target, exists := targets[name]
		if !exists {
			return "", fmt.Errorf("unknown venue recurrence field %q", name)
		}
		if string(raw) == "null" {
			return "", fmt.Errorf("venue recurrence field %q must not be null", name)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return "", fmt.Errorf("decode venue recurrence field %q: %w", name, err)
		}
		if name == "weekdays" {
			var weekdays []json.RawMessage
			if err := json.Unmarshal(raw, &weekdays); err != nil {
				return "", fmt.Errorf("decode venue recurrence weekdays: %w", err)
			}
			for _, weekday := range weekdays {
				if string(weekday) == "null" {
					return "", fmt.Errorf("venue recurrence weekdays must not contain null")
				}
			}
		}
	}
	if rule.Frequency != nil && !slices.Contains([]string{"once", "weekly", "monthly", "yearly"}, *rule.Frequency) {
		return "", fmt.Errorf("invalid venue recurrence frequency %q", *rule.Frequency)
	}
	if rule.Date != nil {
		if _, err := time.Parse(time.DateOnly, *rule.Date); err != nil {
			return "", fmt.Errorf("invalid venue recurrence date: %w", err)
		}
	}
	if rule.Interval != nil && *rule.Interval < 1 {
		return "", fmt.Errorf("venue recurrence interval must be positive")
	}
	for _, field := range []struct {
		name    string
		value   *int
		minimum int
		maximum int
	}{
		{"weekday", rule.Weekday, 0, 6},
		{"month", rule.Month, 1, 12},
		{"month_day", rule.MonthDay, 1, 31},
	} {
		if field.value != nil && (*field.value < field.minimum || *field.value > field.maximum) {
			return "", fmt.Errorf("venue recurrence %s is out of range", field.name)
		}
	}
	if rule.Nth != nil && !slices.Contains([]int{-1, 1, 2, 3, 4}, *rule.Nth) {
		return "", fmt.Errorf("venue recurrence nth is out of range")
	}
	if rule.Weekdays != nil {
		for _, weekday := range *rule.Weekdays {
			if weekday < 0 || weekday > 6 {
				return "", fmt.Errorf("venue recurrence weekdays must be between 0 and 6")
			}
		}
		slices.Sort(*rule.Weekdays)
		*rule.Weekdays = slices.Compact(*rule.Weekdays)
	}
	encoded, err = json.Marshal(rule)
	if err != nil {
		return "", fmt.Errorf("encode normalised recurrence: %w", err)
	}
	return string(encoded), nil
}
