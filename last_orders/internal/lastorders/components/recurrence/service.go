package recurrence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

const (
	venueCollection      = "pubs"
	pollCollection       = "polls"
	voteCollection       = "votes"
	attendanceCollection = "attendance"
	auditCollection      = "poll_action_audit"
)

type Service struct {
	client *firestore.Client
	loc    *time.Location
	logger *slog.Logger
}

func NewService(client *firestore.Client, logger *slog.Logger) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, err
	}
	return &Service{client: client, loc: loc, logger: logger}, nil
}

func (s *Service) Location() *time.Location {
	return s.loc
}

func (s *Service) Today() time.Time {
	return time.Now().In(s.loc)
}

// EventVenueQuery selects the venues both recurrence listeners observe.
func (s *Service) EventVenueQuery() firestore.Query {
	return s.client.Collection(venueCollection).Where("venueType", "==", "event")
}

func (s *Service) ListEventVenues(ctx context.Context) ([]EventVenue, error) {
	iter := s.EventVenueQuery().Documents(ctx)
	defer iter.Stop()

	venues := make([]EventVenue, 0, 32)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		venues = append(venues, EventVenueFrom(doc))
	}
	return venues, nil
}

func EventVenueFrom(doc *firestore.DocumentSnapshot) EventVenue {
	data := doc.Data()
	rule, _ := data["recurrence"].(map[string]any)
	name, _ := data["name"].(string)
	next, _ := data[NextOccurrenceField].(string)
	return EventVenue{ID: doc.Ref.ID, Name: name, Recurrence: rule, NextOccurrenceDate: next}
}

// AdvanceStaleEvent recalculates and persists the venue's next occurrence.
//
// It re-reads current Firestore state and no-ops when the venue is no longer stale,
// so replay after a successful advance converges rather than advancing again.
func (s *Service) AdvanceStaleEvent(ctx context.Context, eventID string) error {
	venueRef := s.client.Collection(venueCollection).Doc(eventID)

	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(venueRef)
		if err != nil {
			return err
		}
		if venueType, _ := doc.Data()["venueType"].(string); venueType != "event" {
			return nil
		}

		current, _ := doc.Data()[NextOccurrenceField].(string)
		if !IsStale(current, s.Today(), s.loc) {
			return nil
		}

		raw, _ := doc.Data()["recurrence"].(map[string]any)
		rule, ok := ParseRule(raw)
		if !ok {
			s.logger.Warn("event venue has no usable recurrence", "event_id", eventID)
			return clearOccurrence(tx, venueRef, doc)
		}

		occurrence, ok := NextOccurrence(rule, s.staleReference(current), s.loc)
		if !ok {
			s.logger.Warn("recurrence produced no future occurrence", "event_id", eventID, "frequency", rule.Frequency)
			return clearOccurrence(tx, venueRef, doc)
		}

		return tx.Set(venueRef, map[string]any{NextOccurrenceField: occurrence.String()}, firestore.MergeAll)
	})
}

// staleReference resolves the point the recurrence is calculated from. A stored
// occurrence advances strictly past itself; a long-dormant venue skips forward to today.
func (s *Service) staleReference(current string) time.Time {
	today := beginningOfDay(s.Today())
	stored, ok := parseDate(current, s.loc)
	if !ok {
		return today
	}
	if next := stored.AddDate(0, 0, 1); next.After(today) {
		return next
	}
	return today
}

func clearOccurrence(tx *firestore.Transaction, ref *firestore.DocumentRef, doc *firestore.DocumentSnapshot) error {
	if _, has := doc.Data()[NextOccurrenceField]; !has {
		return nil
	}
	return tx.Update(ref, []firestore.Update{{Path: NextOccurrenceField, Value: firestore.Delete}})
}

// CreateEventPoll materialises the poll for a due occurrence, or completes silently
// when a poll for (eventVenueId, occurrenceDate) already exists.
func (s *Service) CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error {
	venueDoc, err := s.client.Collection(venueCollection).Doc(eventID).Get(ctx)
	if err != nil {
		return err
	}
	if venueType, _ := venueDoc.Data()["venueType"].(string); venueType != "event" {
		return nil
	}
	if current, _ := venueDoc.Data()[NextOccurrenceField].(string); current != occurrenceDate {
		s.logger.Info("event occurrence superseded before poll creation", "event_id", eventID, "observed", occurrenceDate, "current", current)
		return nil
	}
	venueName, _ := venueDoc.Data()["name"].(string)

	polls := s.client.Collection(pollCollection)
	existing := polls.Where("eventVenueId", "==", eventID).Where("date", "==", occurrenceDate)
	// Allocated up front so transaction retries reuse the same auto-assigned ID.
	pollRef := polls.NewDoc()
	created := false

	err = s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		created = false

		matches, err := tx.Documents(existing).GetAll()
		if err != nil {
			return err
		}
		if len(matches) > 0 {
			return nil
		}

		if err := tx.Create(pollRef, map[string]any{
			"date":                occurrenceDate,
			"completed":           false,
			"pubs":                map[string]any{eventID: map[string]any{"name": venueName}},
			"eventVenueId":        eventID,
			"eventOccurrenceDate": occurrenceDate,
		}); err != nil {
			return err
		}
		if err := tx.Create(s.client.Collection(voteCollection).Doc(pollRef.ID), map[string]any{"any": []any{}}); err != nil {
			return err
		}
		if err := tx.Create(s.client.Collection(attendanceCollection).Doc(pollRef.ID), map[string]any{}); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	// Audit is observability, never the source of truth for poll existence.
	auditID := fmt.Sprintf("%s_create_%d", pollRef.ID, time.Now().UTC().UnixNano())
	if _, err := s.client.Collection(auditCollection).Doc(auditID).Set(ctx, map[string]any{
		"actionType": "create",
		"actorUid":   "backend:auto",
		"pollId":     pollRef.ID,
		"pollDate":   occurrenceDate,
		"at":         firestore.ServerTimestamp,
	}); err != nil {
		s.logger.Warn("poll creation audit failed", "poll_id", pollRef.ID, "err", err)
	}

	s.logger.Info("event poll created", "event_id", eventID, "poll_id", pollRef.ID, "occurrence", occurrenceDate)
	return nil
}
