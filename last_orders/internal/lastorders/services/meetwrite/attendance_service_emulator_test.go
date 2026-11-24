package meetwrite

import (
	"context"
	"errors"
	"net"
	"os"
	"slices"
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
	explicitVenueID := client.Collection("pubs").NewDoc().ID
	inheritedVenueID := client.Collection("pubs").NewDoc().ID
	restaurantVenueID := client.Collection("pubs").NewDoc().ID
	removedVenueID := client.Collection("pubs").NewDoc().ID
	unrelatedVenueID := client.Collection("pubs").NewDoc().ID
	removedVenueAudit := client.Collection(auditCollection).NewDoc()
	if _, err := meetRef.Set(ctx, map[string]any{
		"date": "2026-11-24", "completed": true, "groupId": "member-group",
		"selected": venueID, "restaurant": restaurantVenueID,
		"pubs": map[string]any{
			venueID:           map[string]any{"name": "Selected"},
			explicitVenueID:   map[string]any{"name": "Explicit response"},
			inheritedVenueID:  map[string]any{"name": "Inherited response"},
			restaurantVenueID: map[string]any{"name": "Restaurant response"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	venueRefs := []*firestore.DocumentRef{
		client.Collection("pubs").Doc(inheritedVenueID),
		client.Collection("pubs").Doc(restaurantVenueID),
	}
	if _, err := venueRefs[0].Set(ctx, map[string]any{"name": "Inherited response", "venueType": "pub"}); err != nil {
		t.Fatal(err)
	}
	if _, err := venueRefs[1].Set(ctx, map[string]any{"name": "Restaurant response", "venueType": "restaurant"}); err != nil {
		t.Fatal(err)
	}
	if _, err := attendanceRef.Set(ctx, map[string]any{
		"any": map[string]any{"canCome": []string{"other-member"}, "cannotCome": []string{}},
		venueID: map[string]any{
			"canCome":     []string{"caller", "other-member"},
			"cannotCome":  []string{"caller"},
			"eta":         map[string]any{"caller": "18:00", "other-member": "19:00"},
			"legacyField": "preserve",
		},
		explicitVenueID: map[string]any{
			"canCome": []string{"caller"}, "cannotCome": []string{},
			"eta": map[string]any{"caller": "18:10"},
		},
		inheritedVenueID: map[string]any{
			"canCome": []string{}, "cannotCome": []string{},
			"eta": map[string]any{"caller": "18:20"},
		},
		restaurantVenueID: map[string]any{
			"canCome": []string{}, "cannotCome": []string{},
			"eta": map[string]any{"caller": "18:30"},
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
		for _, venueRef := range venueRefs {
			_, _ = venueRef.Delete(cleanupCtx)
		}
	})

	if err := service.SetAttendance(ctx, meetRef.ID, "any", "caller", "not_attending"); err != nil {
		t.Fatal(err)
	}
	standingSnapshot, err := attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	standing := standingSnapshot.Data()["any"].(map[string]any)
	standingCanCome, valid := readStringSlice(standing["canCome"])
	if !valid || !slices.Equal(standingCanCome, []string{"other-member"}) {
		t.Fatalf("Any-Venue canCome=%v, want only the existing other-member response", standing["canCome"])
	}
	standingCannotCome, valid := readStringSlice(standing["cannotCome"])
	if !valid || !slices.Contains(standingCannotCome, "caller") {
		t.Fatalf("Any-Venue cannotCome=%v, want caller", standing["cannotCome"])
	}
	standingData := standingSnapshot.Data()
	for _, venueID := range []string{explicitVenueID, inheritedVenueID, restaurantVenueID} {
		entry := standingData[venueID].(map[string]any)
		etas, valid := attendanceETAs(entry["eta"])
		if !valid {
			t.Fatalf("Venue %q ETA map is invalid: %v", venueID, entry["eta"])
		}
		if venueID == explicitVenueID {
			if etas["caller"] != "18:10" {
				t.Fatalf("explicit Venue ETA=%v, want caller retained", etas)
			}
			continue
		}
		if etas["caller"] != nil {
			t.Fatalf("Venue %q inherited/restaurant ETA=%v, want caller cleared", venueID, etas)
		}
	}
	if _, err := attendanceRef.Update(ctx, []firestore.Update{
		{FieldPath: firestore.FieldPath{inheritedVenueID, "eta", "caller"}, Value: "18:20"},
		{FieldPath: firestore.FieldPath{restaurantVenueID, "eta", "caller"}, Value: "18:30"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := meetRef.Update(ctx, []firestore.Update{{Path: "completed", Value: false}}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAttendance(ctx, meetRef.ID, "any", "caller", "attending"); err != nil {
		t.Fatal(err)
	}
	standingSnapshot, err = attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	standingData = standingSnapshot.Data()
	standing = standingData["any"].(map[string]any)
	standingCanCome, valid = readStringSlice(standing["canCome"])
	if !valid || !slices.Contains(standingCanCome, "other-member") || !slices.Contains(standingCanCome, "caller") {
		t.Fatalf("Any-Venue canCome after setting caller=%v, want both members", standing["canCome"])
	}
	for venueID, expectedETA := range map[string]string{explicitVenueID: "18:10", inheritedVenueID: "18:20"} {
		entry := standingData[venueID].(map[string]any)
		etas, valid := attendanceETAs(entry["eta"])
		if !valid || etas["caller"] != expectedETA {
			t.Fatalf("Venue %q ETA after setting any to attending=%v, want caller at %s", venueID, etas, expectedETA)
		}
	}
	entry := standingData[restaurantVenueID].(map[string]any)
	etas, valid := attendanceETAs(entry["eta"])
	if !valid || etas["caller"] != nil {
		t.Fatalf("restaurant ETA after setting any to attending=%v, want caller cleared", etas)
	}
	if _, err := meetRef.Update(ctx, []firestore.Update{
		{Path: "completed", Value: true},
		{Path: "selected", Value: venueID},
		{Path: "restaurant", Value: restaurantVenueID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := attendanceRef.Update(ctx, []firestore.Update{
		{FieldPath: firestore.FieldPath{inheritedVenueID, "eta", "caller"}, Value: "18:21"},
		{FieldPath: firestore.FieldPath{restaurantVenueID, "eta", "caller"}, Value: "18:31"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAttendance(ctx, meetRef.ID, "any", "caller", "attending"); err != nil {
		t.Fatal(err)
	}
	standingSnapshot, err = attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, venueID := range []string{inheritedVenueID, restaurantVenueID} {
		unselected := standingSnapshot.Data()[venueID].(map[string]any)
		unselectedETAs, valid := attendanceETAs(unselected["eta"])
		if !valid || unselectedETAs["caller"] != nil {
			t.Fatalf("unselected Venue %q ETA=%v, want caller cleared after completion", venueID, unselectedETAs)
		}
	}
	if _, err := meetRef.Update(ctx, []firestore.Update{
		{Path: "selected", Value: inheritedVenueID},
		{Path: "restaurant", Value: inheritedVenueID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := attendanceRef.Update(ctx, []firestore.Update{{
		FieldPath: firestore.FieldPath{inheritedVenueID, "eta", "caller"}, Value: "18:22",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAttendance(ctx, meetRef.ID, "any", "caller", "attending"); err != nil {
		t.Fatal(err)
	}
	standingSnapshot, err = attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dualRole := standingSnapshot.Data()[inheritedVenueID].(map[string]any)
	dualRoleETAs, valid := attendanceETAs(dualRole["eta"])
	if !valid || dualRoleETAs["caller"] != "18:22" {
		t.Fatalf("selected main Pub/restaurant ETA=%v, want main-role ETA retained", dualRoleETAs)
	}
	if _, err := meetRef.Update(ctx, []firestore.Update{
		{Path: "selected", Value: venueID},
		{Path: "restaurant", Value: restaurantVenueID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := attendanceRef.Update(ctx, []firestore.Update{{
		FieldPath: firestore.FieldPath{restaurantVenueID, "eta", "caller"}, Value: "18:35",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := service.ClearAttendance(ctx, meetRef.ID, "any", "caller"); err != nil {
		t.Fatal(err)
	}
	standingSnapshot, err = attendanceRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	standingData = standingSnapshot.Data()
	standing = standingData["any"].(map[string]any)
	standingCanCome, valid = readStringSlice(standing["canCome"])
	standingCannotCome, cannotComeValid := readStringSlice(standing["cannotCome"])
	if !valid || !cannotComeValid || !slices.Equal(standingCanCome, []string{"other-member"}) || slices.Contains(standingCannotCome, "caller") {
		t.Fatalf("Any-Venue status after withdrawal: canCome=%v cannotCome=%v", standing["canCome"], standing["cannotCome"])
	}
	for venueID, expectedETA := range map[string]string{explicitVenueID: "18:10"} {
		entry := standingData[venueID].(map[string]any)
		etas, valid := attendanceETAs(entry["eta"])
		if !valid || etas["caller"] != expectedETA {
			t.Fatalf("Venue %q ETA after withdrawing any=%v, want caller at %s", venueID, etas, expectedETA)
		}
	}
	for _, venueID := range []string{inheritedVenueID, restaurantVenueID} {
		entry := standingData[venueID].(map[string]any)
		etas, valid := attendanceETAs(entry["eta"])
		if !valid || etas["caller"] != nil {
			t.Fatalf("Venue %q ETA after withdrawing any=%v, want caller cleared", venueID, etas)
		}
	}

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
	etas, valid = attendanceETAs(venue["eta"])
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
		{name: "invalid status", err: service.SetAttendance(context.Background(), "meet", "venue", "caller", "maybe")},
		{name: "missing Meet ID", err: service.SetAttendance(context.Background(), "", "venue", "caller", "attending")},
		{name: "missing caller UID", err: service.ClearAttendance(context.Background(), "meet", "venue", "")},
	} {
		if !errors.Is(test.err, ErrInvalidInput) {
			t.Errorf("%s error=%v, want ErrInvalidInput", test.name, test.err)
		}
	}
}
