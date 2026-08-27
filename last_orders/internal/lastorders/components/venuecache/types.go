package venuecache

import (
	"encoding/json"
	"fmt"

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
	if recurrence, ok := doc.Data["recurrence"]; ok {
		encoded, err := json.Marshal(recurrence)
		if err != nil {
			return VenueProjection{}, fmt.Errorf("encode recurrence: %w", err)
		}
		projection.RecurrenceJSON = string(encoded)
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
