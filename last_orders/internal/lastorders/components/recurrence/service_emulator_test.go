package recurrence

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/groupcache"
	"last_orders/internal/lastorders/components/groupcache/groupcachetest"
	"last_orders/internal/lastorders/components/venuecache"

	"cloud.google.com/go/firestore"
	_ "modernc.org/sqlite"
)

func emulatorService(t *testing.T, ctx context.Context, groups ...groupcache.Group) (*Service, *firestore.Client) {
	t.Helper()

	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := fmt.Sprintf("demo-last-orders-recurrence-%d", time.Now().UnixNano())

	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	groupDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatalf("open Group cache sqlite: %v", err)
	}
	t.Cleanup(func() { _ = groupDB.Close() })
	groupBase, err := basestore.New(groupDB)
	if err != nil {
		t.Fatalf("new Group cache base store: %v", err)
	}
	groupStore, err := groupcache.NewStore(groupBase)
	if err != nil {
		t.Fatalf("new Group cache store: %v", err)
	}
	mutations := make([]groupcache.Mutation, 0, len(groups))
	for index := range groups {
		mutations = append(mutations, groupcache.Mutation{ID: groups[index].ID, Group: &groups[index]})
	}
	if err := groupStore.ApplyBatch(ctx, mutations, true); err != nil {
		t.Fatalf("seed Group projection: %v", err)
	}
	groupService, err := groupcache.NewService(groupStore, groupcachetest.New())
	if err != nil {
		t.Fatalf("new Group cache service: %v", err)
	}

	svc, err := NewService(client, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil, groupService)
	if err != nil {
		t.Fatalf("new recurrence service: %v", err)
	}
	return svc, client
}

func TestAdvanceStaleEventPicksUpEditedRecurrenceAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx, groupcache.Group{ID: "group-other-case", Name: "AmPubNight", Visibility: groupcache.VisibilityPublic})
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

	svc, client := emulatorService(t, ctx, groupcache.Group{ID: "opaque-default-group-id", Name: "ampubnight", Visibility: groupcache.VisibilityPublic})
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

func TestCreateMeetForEventOccurrenceUsesFirebaseStateWhenCacheIsStale(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx, groupcache.Group{ID: "opaque-default-group-id", Name: "ampubnight", Visibility: groupcache.VisibilityPublic})
	current := beginningOfDay(svc.Today()).AddDate(0, 0, 4).Format("2006-01-02")
	stale := beginningOfDay(svc.Today()).AddDate(0, 0, 2).Format("2006-01-02")
	eventID := fmt.Sprintf("venue-stale-cache-%d", time.Now().UnixNano())
	if _, err := client.Collection("pubs").Doc(eventID).Set(ctx, map[string]any{
		"venueType":         "event",
		"name":              "The Freshly Resolved Arms",
		"recurrence":        map[string]any{"frequency": "once", "date": current},
		NextOccurrenceField: current,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	cacheStore, err := venuecache.New(base)
	if err != nil {
		t.Fatalf("new cache store: %v", err)
	}
	if err := cacheStore.Put(context.Background(), venuecache.VenueProjection{
		ID:                 eventID,
		Name:               "stale-name",
		VenueType:          "event",
		NextOccurrenceDate: stale,
	}); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}
	cacheService, err := venuecache.NewService(cacheStore, staleSource{doc: venuecache.Document{ID: eventID, Data: map[string]any{"name": "fresh-name", "venueType": "event", "next_occurrence_date": current}}}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}
	svc.cache = cacheService

	if err := svc.CreateMeetForEventOccurrence(ctx, eventID, current); err != nil {
		t.Fatalf("create event poll with Firebase authoritative date: %v", err)
	}
	polls, err := client.Collection("polls").Where("eventVenueId", "==", eventID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("query polls: %v", err)
	}
	if len(polls) != 1 {
		t.Fatalf("expected one poll for the current authoritative occurrence, got %d", len(polls))
	}
	if stale == current {
		t.Fatal("test setup bug: stale date must differ")
	}
}

type staleSource struct {
	doc venuecache.Document
}

func (s staleSource) Get(context.Context, string) (venuecache.Document, error) { return s.doc, nil }

func (s staleSource) ListEventVenues(context.Context) ([]venuecache.Document, error) { return nil, nil }

func (s staleSource) Watch(context.Context) (venuecache.ChangeStream, error) { return nil, nil }

func TestCreateMeetForEventOccurrenceAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx, groupcache.Group{ID: "opaque-default-group-id", Name: "ampubnight", Visibility: groupcache.VisibilityPublic})
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

	if err := svc.CreateMeetForEventOccurrence(ctx, eventID, occurrence); err != nil {
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
	if poll.Data()["groupId"] != "opaque-default-group-id" {
		t.Fatalf("unexpected groupId: %v", poll.Data()["groupId"])
	}
	if poll.Data()["completed"] != false {
		t.Fatalf("unexpected completed: %v", poll.Data()["completed"])
	}
	audits, err := client.Collection(auditCollection).Where("pollId", "==", pollID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("query creation audit: %v", err)
	}
	if len(audits) != 1 || audits[0].Data()["actionType"] != "create" || audits[0].Data()["actorUid"] != "backend:auto" {
		t.Fatalf("unexpected creation audit: %+v", audits)
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
	if err := svc.CreateMeetForEventOccurrence(ctx, eventID, occurrence); err != nil {
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
	if polls[0].Data()["groupId"] != "opaque-default-group-id" {
		t.Fatalf("replay changed groupId: %v", polls[0].Data()["groupId"])
	}
	audits, err = client.Collection(auditCollection).Where("pollId", "==", pollID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("re-query creation audit: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("replay changed creation audit count: %d", len(audits))
	}

	votes, err := client.Collection("votes").Doc(pollID).Get(ctx)
	if err != nil {
		t.Fatalf("votes should exist after replay: %v", err)
	}
	if anyVotes, ok := votes.Data()["any"].([]any); !ok || len(anyVotes) != 1 {
		t.Fatalf("replay reset votes state: %v", votes.Data()["any"])
	}
}

func TestCreateMeetForEventOccurrenceFailsWithoutExactDefaultGroupAgainstEmulator(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, client := emulatorService(t, ctx, groupcache.Group{ID: "wrong-case-group", Name: "AmPubNight", Visibility: groupcache.VisibilityPublic})
	occurrence := beginningOfDay(svc.Today()).AddDate(0, 0, 2).Format("2006-01-02")
	eventID := fmt.Sprintf("venue-no-default-group-%d", time.Now().UnixNano())
	if _, err := client.Collection("pubs").Doc(eventID).Set(ctx, map[string]any{
		"venueType":         "event",
		"name":              "The Unassigned Arms",
		"recurrence":        map[string]any{"frequency": "once", "date": occurrence},
		NextOccurrenceField: occurrence,
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	if err := svc.CreateMeetForEventOccurrence(ctx, eventID, occurrence); err == nil {
		t.Fatal("expected creation to fail without the exact default Group")
	}
	polls, err := client.Collection("polls").Where("eventVenueId", "==", eventID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("query polls: %v", err)
	}
	if len(polls) != 0 {
		t.Fatalf("created an unassigned Meet without the default Group: %d", len(polls))
	}
}

func TestCreateMeetForEventOccurrenceSkipsSupersededOccurrenceAgainstEmulator(t *testing.T) {
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

	if err := svc.CreateMeetForEventOccurrence(ctx, eventID, observed); err != nil {
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
