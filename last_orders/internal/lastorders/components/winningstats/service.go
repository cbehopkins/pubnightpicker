package winningstats

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"last_orders/internal/lastorders/components/meetcache"
)

type Result struct {
	GroupID string            `json:"group_id"`
	Start   string            `json:"start_date"`
	End     string            `json:"end_date"`
	Counts  map[string]int    `json:"counts_by_venue_id"`
	Last    map[string]string `json:"last_date_by_venue_id"`
}

type Service struct {
	client *firestore.Client
	now    func() time.Time
	loc    *time.Location
}

func New(client *firestore.Client, now func() time.Time) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("winning statistics Firestore client is required")
	}
	if now == nil {
		now = time.Now
	}
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return nil, fmt.Errorf("load London time zone: %w", err)
	}
	return &Service{client: client, now: now, loc: loc}, nil
}

func (s *Service) Read(ctx context.Context, groupID string, years int) (Result, error) {
	if groupID == "" || years < 1 || years > 100 {
		return Result{}, fmt.Errorf("invalid winning statistics request")
	}
	today := s.now().In(s.loc)
	end := today.Format(time.DateOnly)
	start := today.AddDate(-years, 0, 0).Format(time.DateOnly)
	docs, err := s.client.Collection("polls").Where("groupId", "==", groupID).
		Where("completed", "==", true).Where("date", ">=", start).
		Where("date", "<=", end).Documents(ctx).GetAll()
	if err != nil {
		return Result{}, fmt.Errorf("query completed Meets: %w", err)
	}
	counts := map[string]int{}
	last := map[string]string{}
	for _, doc := range docs {
		meet, err := meetcache.ProjectionFromDocument(meetcache.Document{ID: doc.Ref.ID, Exists: true, Data: doc.Data()})
		if err != nil {
			return Result{}, fmt.Errorf("decode completed Meet %q: %w", doc.Ref.ID, err)
		}
		if meet.GroupID != groupID || !meet.VotingComplete || meet.SelectedVenueID == "any" || meet.Date < start || meet.Date > end {
			return Result{}, fmt.Errorf("completed Meet %q is inconsistent", meet.ID)
		}
		counts[meet.SelectedVenueID]++
		if meet.Date > last[meet.SelectedVenueID] {
			last[meet.SelectedVenueID] = meet.Date
		}
	}
	return Result{GroupID: groupID, Start: start, End: end, Counts: counts, Last: last}, nil
}
