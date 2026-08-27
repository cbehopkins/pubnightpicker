package recurrence

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func emulatorService(t *testing.T, ctx context.Context) (*Service, *firestore.Client) {
	t.Helper()

	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "last-orders-emulator"
	}

	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	svc, err := NewService(client, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)
	if err != nil {
		t.Fatalf("new recurrence service: %v", err)
	}
	return svc, client
}

func TestAdvanceStaleEventPicksUpEditedRecurrenceAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx)
	original := beginningOfDay(svc.Today()).AddDate(0, 0, 5).Format("2006-01-02")
	edited := beginningOfDay(svc.Today()).AddDate(0, 0, 9).Format("2006-01-02")

	eventID := fmt.Sprintf("venue-edited-recurrence-%d", time.Now().UnixNano())
	venueRef := client.Collection("pubs").Doc(eventID)
	if _, err := venueRef.Set(ctx, map[string]any{
		"venueType":         "event",
		"name":              "The Rescheduled Arms",
		"recurrence":        map[string]any{"frequency": "once", "date": original},
		NextOccurrenceField: original,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	// The frontend edits the recurrence definition; next_occurrence_date is untouched
	// and is still a valid future date, so the venue is not stale by date alone.
	if _, err := venueRef.Update(ctx, []firestore.Update{{Path: "recurrence", Value: map[string]any{"frequency": "once", "date": edited}}}); err != nil {
		t.Fatalf("edit recurrence: %v", err)
	}

	if err := svc.AdvanceStaleEvent(ctx, eventID); err != nil {
		t.Fatalf("advance stale event: %v", err)
	}

	doc, err := venueRef.Get(ctx)
	if err != nil {
		t.Fatalf("read venue: %v", err)
	}
	if got := doc.Data()[NextOccurrenceField]; got != edited {
		t.Fatalf("recurrence edit was not picked up: next_occurrence_date = %v, want %s", got, edited)
	}
}

func TestAdvanceStaleEventFromMissingDateAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx)
	loc := svc.Location()
	occurrence := beginningOfDay(svc.Today()).AddDate(0, 0, 2).Format("2006-01-02")

	eventID := fmt.Sprintf("venue-stale-missing-%d", time.Now().UnixNano())
	venueRef := client.Collection("pubs").Doc(eventID)
	rule := map[string]any{"frequency": "once", "date": occurrence}
	if _, err := venueRef.Set(ctx, map[string]any{
		"venueType":  "event",
		"name":       "The Recurring Arms",
		"recurrence": rule,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	if err := svc.AdvanceStaleEvent(ctx, eventID); err != nil {
		t.Fatalf("advance stale event: %v", err)
	}

	doc, err := venueRef.Get(ctx)
	if err != nil {
		t.Fatalf("read venue: %v", err)
	}
	if got := doc.Data()[NextOccurrenceField]; got != occurrence {
		t.Fatalf("unexpected next_occurrence_date: %v", got)
	}

	// Replay must observe the advanced state and leave it alone.
	if err := svc.AdvanceStaleEvent(ctx, eventID); err != nil {
		t.Fatalf("replay advance stale event: %v", err)
	}
	doc, err = venueRef.Get(ctx)
	if err != nil {
		t.Fatalf("re-read venue: %v", err)
	}
	if got := doc.Data()[NextOccurrenceField]; got != occurrence {
		t.Fatalf("replay changed next_occurrence_date: %v", got)
	}

	if NeedsRecalculation(rule, occurrence, svc.Today(), loc) {
		t.Fatal("advanced occurrence should no longer need recalculation")
	}
}

func TestAdvanceStaleEventFromPastDateAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx)
	today := beginningOfDay(svc.Today())
	weekday := toContractWeekday(today.Weekday())
	past := today.AddDate(0, 0, -7).Format("2006-01-02")

	eventID := fmt.Sprintf("venue-stale-past-%d", time.Now().UnixNano())
	venueRef := client.Collection("pubs").Doc(eventID)
	rule := map[string]any{"frequency": "weekly", "weekday": weekday, "interval": 1}
	if _, err := venueRef.Set(ctx, map[string]any{
		"venueType":         "event",
		"name":              "The Weekly Arms",
		"recurrence":        rule,
		NextOccurrenceField: past,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	if err := svc.AdvanceStaleEvent(ctx, eventID); err != nil {
		t.Fatalf("advance stale event: %v", err)
	}

	doc, err := venueRef.Get(ctx)
	if err != nil {
		t.Fatalf("read venue: %v", err)
	}
	got, _ := doc.Data()[NextOccurrenceField].(string)
	if got == past {
		t.Fatal("stale occurrence was not advanced")
	}
	if NeedsRecalculation(rule, got, svc.Today(), svc.Location()) {
		t.Fatalf("advanced occurrence still needs recalculation: %s", got)
	}
}

func TestCreateEventPollAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx)
	occurrence := beginningOfDay(svc.Today()).AddDate(0, 0, 2).Format("2006-01-02")
	venueName := "The Materialised Arms"

	eventID := fmt.Sprintf("venue-due-%d", time.Now().UnixNano())
	if _, err := client.Collection("pubs").Doc(eventID).Set(ctx, map[string]any{
		"venueType":         "event",
		"name":              venueName,
		"recurrence":        map[string]any{"frequency": "once", "date": occurrence},
		NextOccurrenceField: occurrence,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	if !IsDue(occurrence, svc.Today(), svc.Location()) {
		t.Fatal("seeded occurrence should be inside the creation window")
	}

	if err := svc.CreateEventPoll(ctx, eventID, occurrence); err != nil {
		t.Fatalf("create event poll: %v", err)
	}

	polls, err := client.Collection("polls").
		Where("eventVenueId", "==", eventID).
		Where("date", "==", occurrence).
		Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("query polls: %v", err)
	}
	if len(polls) != 1 {
		t.Fatalf("expected exactly one poll, got %d", len(polls))
	}

	poll := polls[0]
	pollID := poll.Ref.ID
	if pollID == fmt.Sprintf("event-%s-%s", eventID, occurrence) {
		t.Fatal("poll should use a firestore-generated document id")
	}
	pubs, _ := poll.Data()["pubs"].(map[string]any)
	entry, _ := pubs[eventID].(map[string]any)
	if entry["name"] != venueName {
		t.Fatalf("unexpected pubs entry: %v", pubs[eventID])
	}
	if poll.Data()["eventOccurrenceDate"] != occurrence {
		t.Fatalf("unexpected eventOccurrenceDate: %v", poll.Data()["eventOccurrenceDate"])
	}
	if poll.Data()["completed"] != false {
		t.Fatalf("unexpected completed: %v", poll.Data()["completed"])
	}

	if _, err := client.Collection("votes").Doc(pollID).Get(ctx); err != nil {
		t.Fatalf("votes should exist: %v", err)
	}
	if _, err := client.Collection("attendance").Doc(pollID).Get(ctx); err != nil {
		t.Fatalf("attendance should exist: %v", err)
	}

	// Replay must not create a second poll or reset companion state.
	if _, err := client.Collection("votes").Doc(pollID).Set(ctx, map[string]any{"any": []any{"member-1"}}); err != nil {
		t.Fatalf("seed vote: %v", err)
	}
	if err := svc.CreateEventPoll(ctx, eventID, occurrence); err != nil {
		t.Fatalf("replay create event poll: %v", err)
	}

	polls, err = client.Collection("polls").
		Where("eventVenueId", "==", eventID).
		Where("date", "==", occurrence).
		Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("re-query polls: %v", err)
	}
	if len(polls) != 1 {
		t.Fatalf("replay created a duplicate poll, got %d", len(polls))
	}

	votes, err := client.Collection("votes").Doc(pollID).Get(ctx)
	if err != nil {
		t.Fatalf("votes should exist after replay: %v", err)
	}
	if anyVotes, ok := votes.Data()["any"].([]any); !ok || len(anyVotes) != 1 {
		t.Fatalf("replay reset votes state: %v", votes.Data()["any"])
	}
}

func TestCreateEventPollSkipsSupersededOccurrenceAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx)
	today := beginningOfDay(svc.Today())
	observed := today.AddDate(0, 0, 2).Format("2006-01-02")
	current := today.AddDate(0, 0, 5).Format("2006-01-02")

	eventID := fmt.Sprintf("venue-superseded-%d", time.Now().UnixNano())
	if _, err := client.Collection("pubs").Doc(eventID).Set(ctx, map[string]any{
		"venueType":         "event",
		"name":              "The Moved-On Arms",
		"recurrence":        map[string]any{"frequency": "once", "date": current},
		NextOccurrenceField: current,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	if err := svc.CreateEventPoll(ctx, eventID, observed); err != nil {
		t.Fatalf("create event poll: %v", err)
	}

	polls, err := client.Collection("polls").Where("eventVenueId", "==", eventID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("query polls: %v", err)
	}
	if len(polls) != 0 {
		t.Fatalf("expected no poll for a superseded occurrence, got %d", len(polls))
	}
}
