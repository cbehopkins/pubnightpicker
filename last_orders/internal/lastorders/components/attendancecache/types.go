package attendancecache

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Entry struct {
	CanCome    []string          `json:"can_come"`
	CannotCome []string          `json:"cannot_come"`
	ETA        map[string]string `json:"eta"`
}

type Snapshot struct {
	Entries  map[string]Entry
	Revision *time.Time
	Sequence int64
}

type Projection struct {
	Entries map[string]Entry
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

var ErrNotFound = errors.New("attendance Meet ID is required")
var ErrUnavailable = errors.New("attendance projection unavailable")
var ErrRetired = errors.New("attendance projection entry retired")
var ErrStale = errors.New("stale attendance source update")

func ProjectionFromDocument(document Document) (Projection, error) {
	if document.ID == "" {
		return Projection{}, fmt.Errorf("attendance document ID is required")
	}
	projection := Projection{Entries: make(map[string]Entry)}
	if !document.Exists {
		return projection, nil
	}
	if document.Data == nil {
		return Projection{}, fmt.Errorf("attendance document data is required")
	}
	for venueID, rawEntry := range document.Data {
		if venueID == "" {
			return Projection{}, fmt.Errorf("attendance Venue ID must not be empty")
		}
		entryData, ok := rawEntry.(map[string]any)
		if !ok {
			return Projection{}, fmt.Errorf("attendance Venue %q must be an object", venueID)
		}
		canCome, err := attendanceUIDs(venueID, "canCome", entryData["canCome"])
		if err != nil {
			return Projection{}, err
		}
		cannotCome, err := attendanceUIDs(venueID, "cannotCome", entryData["cannotCome"])
		if err != nil {
			return Projection{}, err
		}
		etas, err := attendanceETAs(venueID, entryData["eta"])
		if err != nil {
			return Projection{}, err
		}
		projection.Entries[venueID] = Entry{CanCome: canCome, CannotCome: cannotCome, ETA: etas}
	}
	return projection, nil
}

func attendanceUIDs(venueID, field string, value any) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	var values []any
	switch items := value.(type) {
	case []string:
		values = make([]any, len(items))
		for index, item := range items {
			values[index] = item
		}
	case []any:
		values = items
	default:
		return nil, fmt.Errorf("attendance Venue %q field %q must be an array", venueID, field)
	}
	unique := make(map[string]struct{}, len(values))
	for index, value := range values {
		uid, ok := value.(string)
		if !ok || uid == "" {
			return nil, fmt.Errorf("attendance Venue %q field %q item %d must be a non-empty UID", venueID, field, index)
		}
		unique[uid] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for uid := range unique {
		result = append(result, uid)
	}
	sort.Strings(result)
	return result, nil
}

func attendanceETAs(venueID string, value any) (map[string]string, error) {
	etas := make(map[string]string)
	if value == nil {
		return etas, nil
	}
	values, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("attendance Venue %q field %q must be an object", venueID, "eta")
	}
	for uid, rawETA := range values {
		eta, ok := rawETA.(string)
		if uid == "" || !ok || len(eta) != 5 || eta[2] != ':' {
			return nil, fmt.Errorf("attendance Venue %q ETA for UID %q is invalid", venueID, uid)
		}
		if _, err := time.Parse("15:04", eta); err != nil {
			return nil, fmt.Errorf("attendance Venue %q ETA for UID %q is invalid", venueID, uid)
		}
		etas[uid] = eta
	}
	return etas, nil
}
