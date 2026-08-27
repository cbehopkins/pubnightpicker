package recurrence

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"last_orders/internal/lastorders/components/venuecache"

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
	cache  *venuecache.Service
}

func NewService(client *firestore.Client, logger *slog.Logger, cache *venuecache.Service) (*Service, error) {
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
	return &Service{client: client, loc: loc, logger: logger, cache: cache}, nil
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
	if s.cache != nil {
		projections, err := s.cache.ListEventVenues(ctx)
		if err != nil {
			return nil, fmt.Errorf("list event venues through cache: %w", err)
		}
		venues := make([]EventVenue, 0, len(projections))
		for _, projection := range projections {
			eventVenue, err := eventVenueFromProjection(projection)
			if err != nil {
				return nil, fmt.Errorf("decode venue %q from cache: %w", projection.ID, err)
			}
			venues = append(venues, eventVenue)
		}
		return venues, nil
	}

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

func eventVenueFromProjection(projection venuecache.VenueProjection) (EventVenue, error) {
	var recurrence map[string]any
	if projection.RecurrenceJSON != "" {
		if err := json.Unmarshal([]byte(projection.RecurrenceJSON), &recurrence); err != nil {
			return EventVenue{}, err
		}
	}
	return EventVenue{
		ID:                 projection.ID,
		Name:               projection.Name,
		Recurrence:         recurrence,
		NextOccurrenceDate: projection.NextOccurrenceDate,
	}, nil
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
// It re-reads current Firestore state and recomputes the occurrence from today
// under the current recurrence definition, writing only when that differs from
// what is stored. This converges after a successful advance, and also picks up
// a recurrence definition that was edited without the stored date changing.
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

		raw, _ := doc.Data()["recurrence"].(map[string]any)
		current, _ := doc.Data()[NextOccurrenceField].(string)
		if !NeedsRecalculation(raw, current, s.Today(), s.loc) {
			return nil
		}

		rule, ok := ParseRule(raw)
		if !ok {
			s.logger.Warn("event venue has no usable recurrence", "event_id", eventID)
			return clearOccurrence(tx, venueRef, doc)
		}

		occurrence, ok := NextOccurrence(rule, beginningOfDay(s.Today()), s.loc)
		if !ok {
			s.logger.Warn("recurrence produced no future occurrence", "event_id", eventID, "frequency", rule.Frequency)
			return clearOccurrence(tx, venueRef, doc)
		}

		return tx.Set(venueRef, map[string]any{NextOccurrenceField: occurrence.String()}, firestore.MergeAll)
	})
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
	var venueType, current, venueName string
	var err error
	if s.cache != nil {
		projection, err := s.cache.Get(ctx, eventID)
		if err != nil {
			return err
		}
		venueType = projection.VenueType
		current = projection.NextOccurrenceDate
		venueName = projection.Name
	} else {
		venueDoc, err := s.client.Collection(venueCollection).Doc(eventID).Get(ctx)
		if err != nil {
			return err
		}
		venueType, _ = venueDoc.Data()["venueType"].(string)
		current, _ = venueDoc.Data()[NextOccurrenceField].(string)
		venueName, _ = venueDoc.Data()["name"].(string)
	}
	if venueType != "event" {
		return nil
	}
	if current != occurrenceDate {
		s.logger.Info("event occurrence superseded before poll creation", "event_id", eventID, "observed", occurrenceDate, "current", current)
		return nil
	}

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
