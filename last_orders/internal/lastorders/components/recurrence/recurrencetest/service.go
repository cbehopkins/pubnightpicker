// Package recurrencetest provides a stand-in for the recurrence service. It exists
// only to support tests; production code must bind to a real *recurrence.Service.
package recurrencetest

import (
	"context"
	"fmt"
	"time"
)

// Service is a controllable stand-in satisfying the recurrence surface required by
// app.New and the recurrence plugin handlers. Behaviour-bearing calls fail unless a
// test opts in by supplying the corresponding func.
type Service struct {
	Loc            *time.Location
	Now            time.Time
	OnAdvanceStale func(ctx context.Context, eventID string) error
	OnCreatePoll   func(ctx context.Context, eventID, occurrenceDate string) error
}

// New returns a stand-in fixed to Europe/London and the supplied date.
func New(today time.Time) (*Service, error) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, err
	}
	return &Service{Loc: loc, Now: today.In(loc)}, nil
}

func (s *Service) Location() *time.Location {
	return s.Loc
}

func (s *Service) Today() time.Time {
	return s.Now
}

func (s *Service) AdvanceStaleEvent(ctx context.Context, eventID string) error {
	if s.OnAdvanceStale == nil {
		return fmt.Errorf("recurrencetest: unexpected AdvanceStaleEvent(%q)", eventID)
	}
	return s.OnAdvanceStale(ctx, eventID)
}

func (s *Service) CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error {
	if s.OnCreatePoll == nil {
		return fmt.Errorf("recurrencetest: unexpected CreateEventPoll(%q, %q)", eventID, occurrenceDate)
	}
	return s.OnCreatePoll(ctx, eventID, occurrenceDate)
}
