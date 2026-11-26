package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"

	"cloud.google.com/go/firestore"
)

func TestAttendanceSnapshotAndGroupStreamAgainstEmulators(t *testing.T) {
	if testing.Short() {
		t.Skip("Auth and Firestore emulator integration test")
	}
	firestoreHost := strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST"))
	authHost := strings.TrimSpace(os.Getenv("FIREBASE_AUTH_EMULATOR_HOST"))
	if firestoreHost == "" || authHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST and FIREBASE_AUTH_EMULATOR_HOST must be set")
	}
	assertLoopbackEmulator(t, "Firestore", firestoreHost)
	assertLoopbackEmulator(t, "Auth", authHost)

	projectID := "demo-attendance-live"
	t.Setenv("FIRESTORE_EMULATOR_HOST", firestoreHost)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", authHost)
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON", "")
	t.Setenv("FIREBASE_AUTH_PROJECT_ID", projectID)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	idToken, callerUID := createVotingEmulatorAccount(t, authHost, projectID)
	outsiderToken, _ := createVotingEmulatorAccount(t, authHost, projectID)
	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })

	group := firestoreClient.Collection("groups").Doc("private-group")
	meet := firestoreClient.Collection("polls").Doc("attendance-meet")
	attendance := firestoreClient.Collection("attendance").Doc("attendance-meet")
	callerProfile := firestoreClient.Collection("user-public").Doc(callerUID)
	otherProfile := firestoreClient.Collection("user-public").Doc("other-attendee")
	if _, err := group.Set(ctx, map[string]any{
		"name": "Private Group", "visibility": "private", "members": map[string]any{callerUID: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := meet.Set(ctx, map[string]any{
		"date": "2026-11-24", "completed": false, "groupId": "private-group", "pubs": map[string]any{"venue-1": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := attendance.Set(ctx, map[string]any{
		"venue-1": map[string]any{"canCome": []string{callerUID}, "cannotCome": []string{"other-attendee"}, "eta": map[string]any{callerUID: "19:15"}},
		"any":     map[string]any{"canCome": []string{"other-attendee"}, "cannotCome": []string{}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := callerProfile.Set(ctx, map[string]any{
		"name": "Hidden from Voting", "votesVisible": false, "email": "private@example.test", "photoUrl": "https://example.test/member.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := otherProfile.Set(ctx, map[string]any{"name": "Other Attendee", "votesVisible": false}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		for _, document := range []*firestore.DocumentRef{group, meet, attendance, callerProfile, otherProfile} {
			_, _ = document.Delete(cleanupCtx)
		}
	})

	config := testConfig(t, t.TempDir()+"/attendance-live.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.AuthProjectID = projectID
	config.AuthVerifier = nil
	config.AllowAuthEmulator = true
	config.Component = "handpump"
	config.LandlordInternalURL = "http://127.0.0.1:8082"
	config.InternalAPIToken = "test-only-internal-token-0123456789"
	config.VenueSource = nil
	config.GroupSource = nil
	config.NotificationProfileSource = nil
	config.HTTPAddr = "127.0.0.1:0"
	application, err := app.New(config)
	if err != nil {
		t.Fatalf("new HandPump with emulators: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	runCtx, stop := context.WithCancel(ctx)
	runDone := make(chan error, 1)
	go func() { runDone <- application.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("stop app: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("app did not stop")
		}
	})

	client := &http.Client{Timeout: 8 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)

	unauthenticated := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/attendance-meet/attendance", "", "")
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated attendance status=%d, want HTTP 401", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()
	denied := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/attendance-meet/attendance", "Bearer "+outsiderToken, "")
	if denied.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member attendance status=%d body=%s, want HTTP 404", denied.StatusCode, readVenueResponseBody(t, denied))
	}
	denied.Body.Close()

	snapshotResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/attendance-meet/attendance", "Bearer "+idToken, "")
	if snapshotResponse.StatusCode != http.StatusOK {
		t.Fatalf("attendance snapshot status=%d body=%s, want HTTP 200", snapshotResponse.StatusCode, readVenueResponseBody(t, snapshotResponse))
	}
	body, err := io.ReadAll(snapshotResponse.Body)
	snapshotResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var snapshot attendanceMeetSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatalf("decode attendance snapshot: %v", err)
	}
	if snapshot.MeetID != "attendance-meet" || snapshot.Cursor == "" {
		t.Fatalf("attendance snapshot identity/cursor=%+v", snapshot)
	}
	venue := snapshot.Entries["venue-1"]
	if !hasUID(venue.CanCome, callerUID) || !hasUID(venue.CannotCome, "other-attendee") || venue.ETA[callerUID] != "19:15" {
		t.Fatalf("attendance entry=%+v, want existing status arrays and ETA", venue)
	}
	if snapshot.Participants[callerUID].Name != "Hidden from Voting" || snapshot.Participants["other-attendee"].Name != "Other Attendee" {
		t.Fatalf("attendance participant profiles=%+v; attendance identities must ignore vote visibility", snapshot.Participants)
	}
	if strings.Contains(string(body), "private@example.test") || strings.Contains(string(body), "votesVisible") {
		t.Fatalf("attendance snapshot exposed private profile fields: %s", body)
	}

	streamURL := baseURL + "/api/meets/private-group/attendance/stream?meet_id=attendance-meet"
	openStream := func(lastEventID string) (*http.Response, *bufio.Reader) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+idToken)
		if lastEventID != "" {
			request.Header.Set("Last-Event-ID", lastEventID)
		}
		response, err := (&http.Client{}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("attendance stream status=%d body=%s, want HTTP 200", response.StatusCode, body)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		return response, bufio.NewReader(response.Body)
	}
	readStreamSnapshot := func(reader *bufio.Reader) attendanceGroupSnapshot {
		t.Helper()
		frame := readVenueSSEFrame(t, reader)
		if frame.event != "attendance_snapshots" || frame.id == "" {
			t.Fatalf("attendance stream frame=%+v, want a snapshot with cursor", frame)
		}
		var groupSnapshot attendanceGroupSnapshot
		if err := json.Unmarshal([]byte(frame.data), &groupSnapshot); err != nil {
			t.Fatalf("decode attendance stream snapshot: %v", err)
		}
		if groupSnapshot.Cursor != frame.id || len(groupSnapshot.Meets) != 1 || groupSnapshot.Meets[0].MeetID != "attendance-meet" {
			t.Fatalf("attendance Group snapshot=%+v, frame ID=%q", groupSnapshot, frame.id)
		}
		return groupSnapshot
	}

	firstResponse, firstReader := openStream("")
	firstSnapshot := readStreamSnapshot(firstReader)
	if !hasUID(firstSnapshot.Meets[0].Entries["venue-1"].CanCome, callerUID) {
		t.Fatalf("initial stream did not contain full attendance view: %+v", firstSnapshot)
	}

	if _, err := attendance.Set(ctx, map[string]any{
		"venue-1": map[string]any{"canCome": []string{"other-attendee"}, "cannotCome": []string{callerUID}, "eta": map[string]any{"other-attendee": "20:00"}},
		"any":     map[string]any{"canCome": []string{"other-attendee"}, "cannotCome": []string{}},
	}); err != nil {
		t.Fatal(err)
	}
	legacyUpdate := readStreamSnapshot(firstReader)
	if !hasUID(legacyUpdate.Meets[0].Entries["venue-1"].CannotCome, callerUID) || legacyUpdate.Meets[0].Entries["venue-1"].ETA["other-attendee"] != "20:00" {
		t.Fatalf("legacy Firestore write was not reflected in stream: %+v", legacyUpdate.Meets[0])
	}

	_, err = callerProfile.Update(ctx, []firestore.Update{{Path: "name", Value: "Updated Public Name"}})
	if err != nil {
		t.Fatal(err)
	}
	profileSnapshot := readStreamSnapshot(firstReader)
	if profileSnapshot.Meets[0].Participants[callerUID].Name != "Updated Public Name" {
		t.Fatalf("public profile change did not refresh attendance stream: %+v", profileSnapshot.Meets[0].Participants[callerUID])
	}

	mutation := doVenueRequest(t, client, http.MethodPut, baseURL+"/api/meet/attendance-meet/attendance/venue-1", "Bearer "+idToken, `{"status":"attending"}`)
	if mutation.StatusCode != http.StatusNoContent {
		t.Fatalf("attendance API mutation status=%d body=%s, want HTTP 204", mutation.StatusCode, readVenueResponseBody(t, mutation))
	}
	mutation.Body.Close()
	apiUpdate := readStreamSnapshot(firstReader)
	if apiUpdate.Meets[0].Entries["venue-1"].CannotCome == nil {
		t.Fatal("empty cannot_come state was encoded as null rather than an array")
	}
	if !hasUID(apiUpdate.Meets[0].Entries["venue-1"].CanCome, callerUID) || hasUID(apiUpdate.Meets[0].Entries["venue-1"].CannotCome, callerUID) {
		t.Fatalf("API write was not reflected in stream: %+v", apiUpdate.Meets[0].Entries["venue-1"])
	}
	firstResponse.Body.Close()

	reconnectedResponse, reconnectedReader := openStream(apiUpdate.Cursor)
	reconnected := readStreamSnapshot(reconnectedReader)
	if !hasUID(reconnected.Meets[0].Entries["venue-1"].CanCome, callerUID) {
		t.Fatalf("reconnected stream did not return a complete latest snapshot: %+v", reconnected.Meets[0])
	}

	if _, err := group.Update(ctx, []firestore.Update{{Path: "members", Value: map[string]any{callerUID: false}}}); err != nil {
		t.Fatal(err)
	}
	frame := readVenueSSEFrame(t, reconnectedReader)
	if frame.event != "view_reset" || frame.data != "{}" || frame.id != "" {
		t.Fatalf("membership revocation frame=%+v, want cursorless view_reset", frame)
	}
	reconnectedResponse.Body.Close()
}

type attendanceMeetSnapshot struct {
	MeetID       string                             `json:"meet_id"`
	Entries      map[string]attendanceEntrySnapshot `json:"entries"`
	Participants map[string]attendanceParticipant   `json:"participants"`
	Cursor       string                             `json:"cursor"`
}

type attendanceGroupSnapshot struct {
	Cursor string                   `json:"cursor"`
	Meets  []attendanceMeetSnapshot `json:"meets"`
}

type attendanceEntrySnapshot struct {
	CanCome    []string          `json:"can_come"`
	CannotCome []string          `json:"cannot_come"`
	ETA        map[string]string `json:"eta"`
}

type attendanceParticipant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func hasUID(values []string, uid string) bool {
	for _, value := range values {
		if value == uid {
			return true
		}
	}
	return false
}
