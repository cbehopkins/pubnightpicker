package pastmeets

import (
	"context"
	"fmt"
	"time"

	"last_orders/internal/lastorders/components/meetcache"

	"cloud.google.com/go/firestore"
)

type Source interface {
	Query(context.Context, string, string, int, string, string) ([]meetcache.Document, error)
}

type Service struct {
	source   Source
	now      func() time.Time
	location *time.Location
}

func NewService(source Source, now func() time.Time) (*Service, error) {
	if source == nil {
		return nil, fmt.Errorf("past-Meets source is required")
	}
	if now == nil {
		now = time.Now
	}
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, fmt.Errorf("load London time zone: %w", err)
	}
	return &Service{source: source, now: now, location: location}, nil
}

func (s *Service) Today() string {
	return s.now().In(s.location).Format(time.DateOnly)
}

func (s *Service) List(ctx context.Context, groupID, beforeDate string, limit int, afterDate, afterID string) ([]meetcache.Projection, bool, error) {
	if groupID == "" || beforeDate != s.Today() || !validDate(beforeDate) || limit < 1 || limit > 100 {
		return nil, false, fmt.Errorf("invalid past-Meets query")
	}
	if (afterDate == "") != (afterID == "") {
		return nil, false, fmt.Errorf("incomplete past-Meets position")
	}
	if afterDate != "" && (!validDate(afterDate) || afterDate >= beforeDate) {
		return nil, false, fmt.Errorf("invalid past-Meets position")
	}
	documents, err := s.source.Query(ctx, groupID, beforeDate, limit+1, afterDate, afterID)
	if err != nil {
		return nil, false, fmt.Errorf("query past Meets: %w", err)
	}
	hasNext := len(documents) > limit
	if hasNext {
		documents = documents[:limit]
	}
	meets := make([]meetcache.Projection, 0, len(documents))
	for _, document := range documents {
		if !document.Exists {
			return nil, false, fmt.Errorf("past-Meets source returned a missing document")
		}
		meet, err := meetcache.ProjectionFromDocument(document)
		if err != nil {
			return nil, false, fmt.Errorf("decode past Meet %q: %w", document.ID, err)
		}
		if meet.GroupID != groupID || !meet.VotingComplete || meet.Date >= beforeDate {
			return nil, false, fmt.Errorf("past-Meets source returned an out-of-scope Meet %q", meet.ID)
		}
		meets = append(meets, meet)
	}
	return meets, hasNext, nil
}

func validDate(value string) bool {
	date, err := time.Parse(time.DateOnly, value)
	return err == nil && date.Format(time.DateOnly) == value
}

type firestoreSource struct {
	client *firestore.Client
}

func NewFirestoreSource(client *firestore.Client) (Source, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &firestoreSource{client: client}, nil
}

func (s *firestoreSource) Query(ctx context.Context, groupID, beforeDate string, limit int, afterDate, afterID string) ([]meetcache.Document, error) {
	query := s.client.Collection("polls").
		Where("groupId", "==", groupID).
		Where("completed", "==", true).
		Where("date", "<", beforeDate).
		OrderBy("date", firestore.Desc).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if afterDate != "" {
		query = query.StartAfter(afterDate, afterID)
	}
	documents, err := query.Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	result := make([]meetcache.Document, 0, len(documents))
	for _, document := range documents {
		result = append(result, meetcache.Document{ID: document.Ref.ID, Exists: true, Data: document.Data()})
	}
	return result, nil
}