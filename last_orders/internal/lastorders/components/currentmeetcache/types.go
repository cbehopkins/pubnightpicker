package currentmeetcache

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"last_orders/internal/lastorders/components/meetcache"
)

type QuerySnapshot struct {
	Documents []meetcache.Document
	ReadTime  time.Time
}

type Stream interface {
	Next() (QuerySnapshot, error)
	Stop()
}

type Source interface {
	Watch(context.Context, string, string) (Stream, error)
}

type VenueSummary struct {
	ID         string
	Status     string
	Name       string
	Address    string
	WebsiteURL *string
	ImageURL   *string
	VenueType  string
}

type MeetView struct {
	Meet           meetcache.Projection
	Venue          *VenueSummary
	MissingVenueID string
}

type Snapshot struct {
	Date       string
	Generation string
	Sequence   int64
	Meets      []MeetView
}

func validateSnapshot(groupID, date string, source QuerySnapshot) ([]meetcache.Projection, error) {
	meets := make([]meetcache.Projection, 0, len(source.Documents))
	seen := make(map[string]struct{}, len(source.Documents))
	for _, document := range source.Documents {
		projection, err := meetcache.ProjectionFromDocument(document)
		if err != nil {
			return nil, fmt.Errorf("validate current Meet %q: %w", document.ID, err)
		}
		if projection.GroupID != groupID || !projection.VotingComplete || projection.Date < date {
			return nil, fmt.Errorf("current Meet %q does not match the watched Group and date", projection.ID)
		}
		if _, exists := seen[projection.ID]; exists {
			return nil, fmt.Errorf("duplicate current Meet %q", projection.ID)
		}
		seen[projection.ID] = struct{}{}
		meets = append(meets, projection)
	}
	slices.SortFunc(meets, func(left, right meetcache.Projection) int {
		if left.Date < right.Date {
			return -1
		}
		if left.Date > right.Date {
			return 1
		}
		if left.ID < right.ID {
			return -1
		}
		if left.ID > right.ID {
			return 1
		}
		return 0
	})
	return meets, nil
}

var ErrUnavailable = errors.New("current-Meets projection unavailable")
var ErrRetired = errors.New("current-Meets projection retired")
