package attendancestats

import (
	"context"
	"fmt"
	"sort"
	"time"

	"last_orders/internal/lastorders/components/attendancecache"
	"last_orders/internal/lastorders/components/meetcache"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const attendanceCollection = "attendance"

// Result is the backend-owned historical attendance aggregate.
type Result struct {
	Counts    map[string]int    `json:"counts_by_venue_id"`
	LastDates map[string]string `json:"last_date_by_venue_id"`
}

type Service struct {
	client *firestore.Client
}

func NewService(client *firestore.Client) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("attendance statistics Firestore client is required")
	}
	return &Service{client: client}, nil
}

func (s *Service) Read(ctx context.Context, startDate, endDate string) (Result, error) {
	if !validDate(startDate) || !validDate(endDate) || startDate > endDate {
		return Result{}, fmt.Errorf("invalid attendance statistics window")
	}

	documents, err := s.client.Collection("polls").
		Where("completed", "==", true).
		Where("date", ">=", startDate).
		Where("date", "<=", endDate).
		Documents(ctx).
		GetAll()
	if err != nil {
		return Result{}, fmt.Errorf("query completed Meets: %w", err)
	}

	counts := make(map[string]int)
	lastDates := make(map[string]string)
	for _, document := range documents {
		meet, err := meetcache.ProjectionFromDocument(meetcache.Document{
			ID: document.Ref.ID, Exists: true, Data: document.Data(),
		})
		if err != nil {
			return Result{}, fmt.Errorf("decode completed Meet %q: %w", document.Ref.ID, err)
		}

		attendanceDocument, err := s.client.Collection(attendanceCollection).Doc(document.Ref.ID).Get(ctx)
		if err != nil {
			if firestoreNotFound(err) {
				attendanceDocument = nil
			} else {
				return Result{}, fmt.Errorf("read attendance for Meet %q: %w", document.Ref.ID, err)
			}
		}

		entries := make(map[string]attendancecache.Entry)
		if attendanceDocument != nil && attendanceDocument.Exists() {
			projection, err := attendancecache.ProjectionFromDocument(attendancecache.Document{
				ID: attendanceDocument.Ref.ID, Exists: true, Data: attendanceDocument.Data(),
			})
			if err != nil {
				return Result{}, fmt.Errorf("decode attendance for Meet %q: %w", document.Ref.ID, err)
			}
			entries = projection.Entries
		}

		venueIDs := make(map[string]struct{})
		for _, venueID := range meet.CandidateVenueIDs {
			if venueID != "any" {
				venueIDs[venueID] = struct{}{}
			}
		}
		for _, venueID := range meet.PreviousVenueIDs {
			if venueID != "any" {
				venueIDs[venueID] = struct{}{}
			}
		}
		if meet.SelectedVenueID != "" {
			venueIDs[meet.SelectedVenueID] = struct{}{}
		}
		if meet.RestaurantID != "" {
			venueIDs[meet.RestaurantID] = struct{}{}
		}
		for venueID := range entries {
			if venueID != "any" {
				venueIDs[venueID] = struct{}{}
			}
		}

		for venueID := range venueIDs {
			venueType := s.venueType(ctx, venueID, meet.RestaurantID)
			canCome := effectiveCanCome(entries, venueID, venueType)
			counts[venueID] += len(canCome)
			if meet.Date != "" && (lastDates[venueID] == "" || meet.Date > lastDates[venueID]) {
				lastDates[venueID] = meet.Date
			}
		}
	}

	return Result{Counts: counts, LastDates: lastDates}, nil
}

func (s *Service) venueType(ctx context.Context, venueID, restaurantID string) string {
	if venueID == restaurantID {
		return "restaurant"
	}
	document, err := s.client.Collection("pubs").Doc(venueID).Get(ctx)
	if err == nil && document.Exists() {
		if venueType, ok := document.Data()["venueType"].(string); ok && venueType != "" {
			return venueType
		}
	}
	return "pub"
}

func effectiveCanCome(entries map[string]attendancecache.Entry, venueID, venueType string) []string {
	if venueID == "" {
		return []string{}
	}
	local := entries[venueID]
	localCanCome := uniqueStrings(local.CanCome)
	localCannotCome := uniqueStrings(local.CannotCome)
	if venueType == "restaurant" {
		return exclude(localCanCome, localCannotCome)
	}

	standing := uniqueStrings(entries["any"].CanCome)
	canCome := uniqueStrings(append(standing, localCanCome...))
	canCome = exclude(canCome, localCannotCome)
	return canCome
}

func uniqueStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func exclude(values, exclusions []string) []string {
	excluded := make(map[string]struct{}, len(exclusions))
	for _, value := range exclusions {
		excluded[value] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, found := excluded[value]; !found {
			result = append(result, value)
		}
	}
	return result
}

func validDate(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && parsed.Format(time.DateOnly) == value
}

func firestoreNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
