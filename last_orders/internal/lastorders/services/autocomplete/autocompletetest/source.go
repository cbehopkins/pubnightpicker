// Package autocompletetest provides an in-memory stand-in for autocomplete.Source.
// It exists only to support tests; production code must bind to a Firestore source.
package autocompletetest

import (
	"context"
	"fmt"
	"sync"

	"last_orders/internal/lastorders/services/autocomplete"
)

// Source is an in-memory poll store. Reads of documents a test has not seeded fail
// loudly rather than returning empty state.
type Source struct {
	mu     sync.Mutex
	Polls  map[string]map[string]any
	Votes  map[string]map[string]any
	Venues map[string]map[string]any
	Audits []autocomplete.AuditEntry
}

func New() *Source {
	return &Source{
		Polls:  map[string]map[string]any{},
		Votes:  map[string]map[string]any{},
		Venues: map[string]map[string]any{},
	}
}

func (s *Source) SeedPoll(pollID string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Polls[pollID] = data
}

func (s *Source) ListOpenPollsOn(_ context.Context, date string) ([]autocomplete.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	open := make([]autocomplete.Document, 0, len(s.Polls))
	for pollID, data := range s.Polls {
		if completed, _ := data["completed"].(bool); completed {
			continue
		}
		if pollDate, _ := data["date"].(string); pollDate != date {
			continue
		}
		open = append(open, autocomplete.Document{ID: pollID, Data: data})
	}
	return open, nil
}

func (s *Source) GetPoll(_ context.Context, pollID string) (autocomplete.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.Polls[pollID]
	if !ok {
		return autocomplete.Document{}, fmt.Errorf("autocompletetest: poll %q not seeded", pollID)
	}
	return autocomplete.Document{ID: pollID, Data: data}, nil
}

func (s *Source) GetVotes(_ context.Context, pollID string) (autocomplete.Document, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.Votes[pollID]
	if !ok {
		return autocomplete.Document{}, false, nil
	}
	return autocomplete.Document{ID: pollID, Data: data}, true, nil
}

func (s *Source) GetVenue(_ context.Context, venueID string) (autocomplete.Document, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.Venues[venueID]
	if !ok {
		return autocomplete.Document{}, false, nil
	}
	return autocomplete.Document{ID: venueID, Data: data}, true, nil
}

func (s *Source) CompletePoll(_ context.Context, pollID, selectedVenueID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.Polls[pollID]
	if !ok {
		return false, fmt.Errorf("autocompletetest: poll %q not seeded", pollID)
	}
	if completed, _ := data["completed"].(bool); completed {
		return false, nil
	}
	data["completed"] = true
	data["selected"] = selectedVenueID
	return true, nil
}

func (s *Source) WriteCompletionAudit(_ context.Context, entry autocomplete.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Audits = append(s.Audits, entry)
	return nil
}
