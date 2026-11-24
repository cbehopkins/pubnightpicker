package meetwrite

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestAttendanceMutationsAgainstFirestoreEmulator(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	address, err := net.ResolveTCPAddr("tcp", host)
	if err != nil || address == nil || !address.IP.IsLoopback() {
		t.Fatalf("refusing non-loopback Firestore host %q", host)
	}

	const projectID = "demo-attendance-service"
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	service, err := New(client)
	if err != nil {
		t.Fatal(err)
	}

	meetRef := client.Collection("polls").NewDoc()
	attendanceRef := client.Collection("attendance").Doc(meetRef.ID)
	venueID := client.Collection("pubs").NewDoc().ID
	removedVenueID := client.Collection("pubs").NewDoc().ID
	unrelatedVenueID := client.Collection("pubs").NewDoc().ID
	removedVenueAudit := client.Collection(auditCollection).NewDoc()
	if _, err := meetRef.Set(ctx, map[string]any{
		"date": "2026-11-24", "completed": true, "groupId": "member-group",
		"selected": venueID, "pubs": map[string]any{venueID: map[string]any{"name": "Selected"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := attendanceRef.Set(ctx, map[string]any{
		venueID: map[string]any{
			"canCome":     []string{"caller", "other-member"},
			"cannotCome":  []string{"caller"},
			"eta":         map[string]any{"caller": "18:00", "other-member": "19:00"},
			"legacyField": "preserve",
		},
		removedVenueID: map[string]any{
			"canCome":    []string{"caller", "other-member"},
			"cannotCome": []string{},
			"eta":        map[string]any{"caller": "18:15", "other-member": "19:15"},
		},
		unrelatedVenueID: map[string]any{
			"canCome": []string{"other-member"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := removedVenueAudit.Set(ctx, map[string]any{
		"actionType": "deleteVenue", "pollId": meetRef.ID, "selectedVenueId": removedVenueID,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = meetRef.Delete(cleanupCtx)
		_, _ = attendanceRef.Delete(cleanupCtx)
		_, _ = removedVenueAudit.Delete(cleanupCtx)
	})

	for range 2 {
		if err := service.SetAttendance(ctx, meetRef.ID, venueID, "caller", "not_attending"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	venue, ok := snapshot.Data()[venueID].(map[string]any)
	if !ok {
		t.Fatalf("selected Venue attendance=%T, want map", snapshot.Data()[venueID])
	}
	canCome, valid := readStringSlice(venue["canCome"])
	if !valid || len(canCome) != 1 || canCome[0] != "other-member" {
		t.Fatalf("canCome=%v, want only other-member", venue["canCome"])
	}
	cannotCome, valid := readStringSlice(venue["cannotCome"])
	if !valid || len(cannotCome) != 1 || cannotCome[0] != "caller" {
		t.Fatalf("cannotCome=%v, want only caller", venue["cannotCome"])
	}
	etas, valid := attendanceETAs(venue["eta"])
	if !valid || etas["caller"] != nil || etas["other-member"] != "19:00" {
		t.Fatalf("ETAs=%v, want caller cleared and other-member preserved", venue["eta"])
	}
	if venue["legacyField"] != "preserve" {
		t.Fatalf("legacy field=%v, want preserved", venue["legacyField"])
	}

	for range 2 {
		if err := service.ClearAttendance(ctx, meetRef.ID, removedVenueID, "caller"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err = attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	removed, ok := snapshot.Data()[removedVenueID].(map[string]any)
	if !ok {
		t.Fatalf("removed Venue attendance=%T, want map", snapshot.Data()[removedVenueID])
	}
	canCome, valid = readStringSlice(removed["canCome"])
	if !valid || len(canCome) != 1 || canCome[0] != "other-member" {
		t.Fatalf("removed Venue canCome=%v, want only other-member", removed["canCome"])
	}
	etas, valid = attendanceETAs(removed["eta"])
	if !valid || etas["caller"] != nil || etas["other-member"] != "19:15" {
		t.Fatalf("removed Venue ETAs=%v, want caller cleared and other-member preserved", removed["eta"])
	}
	if err := service.SetAttendance(ctx, meetRef.ID, unrelatedVenueID, "caller", "attending"); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("unrelated Venue set error=%v, want ErrVenueNotFound", err)
	}
	if err := service.ClearAttendance(ctx, meetRef.ID, unrelatedVenueID, "caller"); !errors.Is(err, ErrVenueNotFound) {
		t.Fatalf("unrelated Venue withdrawal error=%v, want ErrVenueNotFound", err)
	}
	if err := service.SetAttendance(ctx, "missing-meet", venueID, "caller", "attending"); !errors.Is(err, ErrMeetNotFound) {
		t.Fatalf("missing Meet set error=%v, want ErrMeetNotFound", err)
	}
}

func TestAttendanceMutationsRejectInvalidInputs(t *testing.T) {
	service := &Service{}
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "any is not a Venue", err: service.SetAttendance(context.Background(), "meet", "any", "caller", "attending")},
		{name: "invalid status", err: service.SetAttendance(context.Background(), "meet", "venue", "caller", "maybe")},
		{name: "any cannot be withdrawn through this route", err: service.ClearAttendance(context.Background(), "meet", "any", "caller")},
	} {
		if !errors.Is(test.err, ErrInvalidInput) {
			t.Errorf("%s error=%v, want ErrInvalidInput", test.name, test.err)
		}
	}
}
