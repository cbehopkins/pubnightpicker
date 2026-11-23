package votecache

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type Projection struct {
	Votes map[string][]string
}

type Snapshot struct {
	Votes map[string][]string
}

type Document struct {
	ID     string
	Exists bool
	Data   map[string]any
}

type Stream interface {
	Next() (Document, error)
	Stop()
}

type Source interface {
	Watch(context.Context, string) (Stream, error)
}

func ProjectionFromDocument(document Document) (Projection, error) {
	if document.ID == "" {
		return Projection{}, fmt.Errorf("vote document ID is required")
	}
	projection := Projection{Votes: make(map[string][]string)}
	if !document.Exists {
		return projection, nil
	}
	if document.Data == nil {
		return Projection{}, fmt.Errorf("vote document data is required")
	}
	for optionID, value := range document.Data {
		if optionID == "" {
			return Projection{}, fmt.Errorf("vote option ID must not be empty")
		}
		var userIDs []string
		switch values := value.(type) {
		case []string:
			userIDs = append([]string(nil), values...)
		case []any:
			userIDs = make([]string, len(values))
			for index, value := range values {
				userID, ok := value.(string)
				if !ok || userID == "" {
					return Projection{}, fmt.Errorf("vote option %q item %d must be a non-empty UID", optionID, index)
				}
				userIDs[index] = userID
			}
		default:
			return Projection{}, fmt.Errorf("vote option %q must be an array of UIDs", optionID)
		}
		unique := make(map[string]struct{}, len(userIDs))
		for index, userID := range userIDs {
			if userID == "" {
				return Projection{}, fmt.Errorf("vote option %q item %d must be a non-empty UID", optionID, index)
			}
			unique[userID] = struct{}{}
		}
		distinct := make([]string, 0, len(unique))
		for userID := range unique {
			distinct = append(distinct, userID)
		}
		sort.Strings(distinct)
		projection.Votes[optionID] = distinct
	}
	return projection, nil
}

var ErrNotFound = errors.New("Meet ID is required")
var ErrUnavailable = errors.New("voting projection unavailable")
var ErrRetired = errors.New("voting projection entry retired")
