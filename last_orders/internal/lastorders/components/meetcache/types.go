package meetcache

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"
)

type Projection struct {
	ID                string
	GroupID           string
	Date              string
	VotingComplete    bool
	SelectedVenueID   string
	CandidateVenueIDs []string
	RestaurantID      string
	RestaurantTime    string
	PreviousVenueIDs  []string
}

func (projection Projection) Equal(other Projection) bool {
	return projection.ID == other.ID &&
		projection.GroupID == other.GroupID &&
		projection.Date == other.Date &&
		projection.VotingComplete == other.VotingComplete &&
		projection.SelectedVenueID == other.SelectedVenueID &&
		slices.Equal(projection.CandidateVenueIDs, other.CandidateVenueIDs) &&
		projection.RestaurantID == other.RestaurantID &&
		projection.RestaurantTime == other.RestaurantTime &&
		slices.Equal(projection.PreviousVenueIDs, other.PreviousVenueIDs)
}

type Document struct {
	ID      string
	Exists  bool
	Data    map[string]any
	Version time.Time
}

type Stream interface {
	Next() (Document, error)
	Stop()
}

type Source interface {
	Watch(context.Context, string) (Stream, error)
}

var ErrNotFound = fmt.Errorf("meet not found")
var ErrUnavailable = fmt.Errorf("meet projection unavailable")

func ProjectionFromDocument(document Document) (Projection, error) {
	if document.ID == "" {
		return Projection{}, fmt.Errorf("meet document ID is required")
	}
	groupID, ok := document.Data["groupId"].(string)
	if !ok || groupID == "" {
		return Projection{}, fmt.Errorf("meet %q field %q must be a non-empty string", document.ID, "groupId")
	}
	date, err := meetDate(document.Data["date"])
	if err != nil {
		return Projection{}, fmt.Errorf("meet %q field %q: %w", document.ID, "date", err)
	}
	selectedVenueID, err := optionalString(document.Data, "selected")
	if err != nil {
		return Projection{}, err
	}
	votingComplete := VotingComplete(document.ID, selectedVenueID)
	restaurantID, err := optionalString(document.Data, "restaurant")
	if err != nil {
		return Projection{}, err
	}
	restaurantTime, err := optionalString(document.Data, "restaurant_time")
	if err != nil {
		return Projection{}, err
	}
	candidateVenueIDs, err := optionalIDs(document.Data, "pubs")
	if err != nil {
		return Projection{}, err
	}
	previousVenueIDsField := "previous_pubs"
	if _, exists := document.Data[previousVenueIDsField]; !exists {
		previousVenueIDsField = "previous_venue_ids"
	}
	previousVenueIDs, err := optionalStringSlice(document.Data, previousVenueIDsField)
	if err != nil {
		return Projection{}, err
	}
	return Projection{
		ID:                document.ID,
		GroupID:           groupID,
		Date:              date,
		VotingComplete:    votingComplete,
		SelectedVenueID:   selectedVenueID,
		CandidateVenueIDs: candidateVenueIDs,
		RestaurantID:      restaurantID,
		RestaurantTime:    restaurantTime,
		PreviousVenueIDs:  previousVenueIDs,
	}, nil
}

// VotingComplete derives the voting state from its selected Venue ID.
func VotingComplete(meetID, selectedVenueID string) bool {
	if selectedVenueID == "any" {
		slog.Default().Error("Meet has unexpected selected Venue ID", "meet_id", meetID, "selected_venue_id", selectedVenueID)
	}
	return selectedVenueID != ""
}

func meetDate(value any) (string, error) {
	switch date := value.(type) {
	case string:
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return "", fmt.Errorf("must be a YYYY-MM-DD date")
		}
		return date, nil
	case time.Time:
		return date.Format(time.DateOnly), nil
	default:
		return "", fmt.Errorf("must be a date string or timestamp")
	}
}

func optionalString(fields map[string]any, name string) (string, error) {
	value, exists := fields[name]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string or null", name)
	}
	return text, nil
}

func optionalIDs(fields map[string]any, name string) ([]string, error) {
	value, exists := fields[name]
	if !exists || value == nil {
		return []string{}, nil
	}
	candidates, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("field %q must be an object", name)
	}
	ids := make([]string, 0, len(candidates))
	for id := range candidates {
		if id != "" && id != "any" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func optionalStringSlice(fields map[string]any, name string) ([]string, error) {
	value, exists := fields[name]
	if !exists || value == nil {
		return []string{}, nil
	}
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...), nil
	case []any:
		ids := make([]string, len(values))
		for index, value := range values {
			id, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("field %q item %d must be a string", name, index)
			}
			ids[index] = id
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("field %q must be an array of strings", name)
	}
}
