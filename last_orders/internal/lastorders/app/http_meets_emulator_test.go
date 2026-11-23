package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/services/meetwrite"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMeetCreateHTTPAgainstFirestoreEmulator(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-meet-write-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })
	groupID := "public-group"
	if _, err := firestoreClient.Collection("groups").Doc(groupID).Set(ctx, map[string]any{"name": "Public Group", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("roles").Doc("canCreatePoll").Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("roles").Doc("canAddPubToPoll").Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("roles").Doc("canCompletePoll").Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("pubs").Doc("venue-add").Set(ctx, map[string]any{"name": "Candidate Venue", "venueType": "event"}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("pubs").Doc("venue-other").Set(ctx, map[string]any{"name": "Other Venue", "venueType": "pub"}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("pubs").Doc("restaurant-1").Set(ctx, map[string]any{"name": "Restaurant", "venueType": "restaurant"}); err != nil {
		t.Fatal(err)
	}

	config := testConfig(t, t.TempDir()+"/meet-write.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.Component = "handpump"
	config.LandlordInternalURL = "http://127.0.0.1:8082"
	config.InternalAPIToken = "test-only-internal-token-0123456789"
	config.HTTPAddr = "127.0.0.1:0"
	config.AllowAuthEmulator = true
	config.VenueSource = nil
	config.GroupSource = nil
	application, err := app.New(config)
	if err != nil {
		t.Fatalf("new HandPump: %v", err)
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
		case <-time.After(15 * time.Second):
			t.Error("app did not stop")
		}
	})

	client := &http.Client{Timeout: 8 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	unauthenticated := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/meets/"+groupID, "", `{"date":"2026-11-18"}`)
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Meet create status=%d, want HTTP 401", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()
	if _, err := firestoreClient.Collection("roles").Doc("canCreatePoll").Delete(ctx); err != nil {
		t.Fatalf("remove Meet creation permission: %v", err)
	}
	unauthorized := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/meets/"+groupID, "Bearer valid-token", `{"date":"2026-11-18"}`)
	if unauthorized.StatusCode != http.StatusForbidden {
		body := readVenueResponseBody(t, unauthorized)
		t.Fatalf("unauthorised Meet create status=%d body=%s, want HTTP 403", unauthorized.StatusCode, body)
	}
	unauthorized.Body.Close()
	if _, err := firestoreClient.Collection("roles").Doc("canCreatePoll").Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatalf("restore Meet creation permission: %v", err)
	}
	invalidCreate := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/meets/"+groupID, "Bearer valid-token", `{"date":"not-a-date"}`)
	if invalidCreate.StatusCode != http.StatusBadRequest {
		body := readVenueResponseBody(t, invalidCreate)
		t.Fatalf("invalid Meet create status=%d body=%s, want HTTP 400", invalidCreate.StatusCode, body)
	}
	invalidCreate.Body.Close()
	writer, err := meetwrite.New(firestoreClient)
	if err != nil {
		t.Fatalf("new Meet writer: %v", err)
	}
	if _, err := writer.Create(ctx, groupID, "2026-11-18", strings.Repeat("x", 1<<20)); err == nil {
		t.Fatal("Meet creation with an oversized audit actor succeeded, want transaction failure")
	}
	for _, collection := range []string{"polls", "votes", "attendance", "poll_action_audit"} {
		documents, err := firestoreClient.Collection(collection).Documents(ctx).GetAll()
		if err != nil {
			t.Fatalf("read %s after failed Meet creation: %v", collection, err)
		}
		if len(documents) != 0 {
			t.Fatalf("%s has %d documents after failed Meet creation, want none", collection, len(documents))
		}
	}

	created := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/meets/"+groupID, "Bearer valid-token", `{"date":"2026-11-18"}`)
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		body := readVenueResponseBody(t, created)
		t.Fatalf("Meet create status=%d body=%s, want HTTP 201", created.StatusCode, body)
	}
	var result struct {
		ID     string `json:"id"`
		Group  string `json:"group_id"`
		Date   string `json:"date"`
		Voting bool   `json:"voting_complete"`
	}
	if err := json.NewDecoder(created.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.Group != groupID || result.Date != "2026-11-18" || result.Voting {
		t.Fatalf("created Meet response=%+v", result)
	}
	meetSnapshot, err := firestoreClient.Collection("polls").Doc(result.ID).Get(ctx)
	if err != nil {
		t.Fatalf("read committed Meet: %v", err)
	}
	meetData := meetSnapshot.Data()
	if meetData["groupId"] != groupID || meetData["date"] != "2026-11-18" || meetData["completed"] != false {
		t.Fatalf("committed Meet data=%v", meetData)
	}
	votesSnapshot, err := firestoreClient.Collection("votes").Doc(result.ID).Get(ctx)
	if err != nil {
		t.Fatalf("read initial votes document: %v", err)
	}
	if votesData := votesSnapshot.Data(); len(votesData) != 0 {
		t.Fatalf("initial votes data=%v, want empty document", votesData)
	}
	attendanceSnapshot, err := firestoreClient.Collection("attendance").Doc(result.ID).Get(ctx)
	if err != nil {
		t.Fatalf("read initial attendance document: %v", err)
	}
	if attendanceData := attendanceSnapshot.Data(); len(attendanceData) != 0 {
		t.Fatalf("initial attendance data=%v, want empty document", attendanceData)
	}
	audits, err := firestoreClient.Collection("poll_action_audit").Where("pollId", "==", result.ID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("read creation audit: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("creation audits=%d, want 1", len(audits))
	}
	audit := audits[0].Data()
	if audit["actionType"] != "create" || audit["actorUid"] != "verified-user" || audit["groupId"] != groupID || audit["pollDate"] != "2026-11-18" {
		t.Fatalf("creation audit=%v", audit)
	}
	if _, err := firestoreClient.Collection("votes").Doc(result.ID).Set(ctx, map[string]any{"venue-add": []string{"verified-user"}}); err != nil {
		t.Fatal(err)
	}
	candidateURL := baseURL + "/api/meet/" + result.ID + "/candidates/venue-add"
	reservedCandidate := doVenueRequest(t, client, http.MethodPut, baseURL+"/api/meet/"+result.ID+"/candidates/any", "Bearer valid-token", "")
	if reservedCandidate.StatusCode != http.StatusBadRequest {
		body := readVenueResponseBody(t, reservedCandidate)
		t.Fatalf("reserved any candidate status=%d body=%s, want HTTP 400", reservedCandidate.StatusCode, body)
	}
	reservedCandidate.Body.Close()

	for _, operation := range []struct {
		method string
		url    string
	}{
		{http.MethodPut, candidateURL},
		{http.MethodPut, candidateURL},
		{http.MethodDelete, candidateURL},
		{http.MethodDelete, baseURL + "/api/meet/" + result.ID + "/candidates/absent-venue"},
	} {
		response := doVenueRequest(t, client, operation.method, operation.url, "Bearer valid-token", "")
		if response.StatusCode != http.StatusOK {
			body := readVenueResponseBody(t, response)
			t.Fatalf("candidate %s status=%d body=%s, want HTTP 200", operation.method, response.StatusCode, body)
		}
		response.Body.Close()
	}
	meetSnapshot, err = firestoreClient.Collection("polls").Doc(result.ID).Get(ctx)
	if err != nil {
		t.Fatalf("read Meet after candidate removal: %v", err)
	}
	if candidates, ok := meetSnapshot.Data()["pubs"].(map[string]any); ok && len(candidates) != 0 {
		t.Fatalf("candidate map after removal=%v, want empty", candidates)
	}
	votes, err := firestoreClient.Collection("votes").Doc(result.ID).Get(ctx)
	if err != nil || votes.Data()["venue-add"] == nil {
		t.Fatalf("removed candidate votes=%v, err=%v; want votes retained", votes, err)
	}
	audits, err = firestoreClient.Collection("poll_action_audit").Where("pollId", "==", result.ID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("read candidate audits: %v", err)
	}
	if len(audits) != 3 {
		t.Fatalf("audits after candidate no-ops=%d, want creation plus two candidate changes", len(audits))
	}
	completionURL := baseURL + "/api/meet/" + result.ID + "/complete"
	invalidCompletion := doVenueRequest(t, client, http.MethodPost, completionURL, "Bearer valid-token", `{"venue_id":"missing-venue"}`)
	if invalidCompletion.StatusCode != http.StatusNotFound {
		body := readVenueResponseBody(t, invalidCompletion)
		t.Fatalf("completion with missing Venue status=%d body=%s, want HTTP 404", invalidCompletion.StatusCode, body)
	}
	invalidCompletion.Body.Close()
	completed := doVenueRequest(t, client, http.MethodPost, completionURL, "Bearer valid-token", `{"venue_id":"venue-add"}`)
	if completed.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, completed)
		t.Fatalf("Meet completion status=%d body=%s, want HTTP 200", completed.StatusCode, body)
	}
	completed.Body.Close()
	completedMeet, err := firestoreClient.Collection("polls").Doc(result.ID).Get(ctx)
	if err != nil {
		t.Fatalf("read completed Meet: %v", err)
	}
	completedData := completedMeet.Data()
	if completedData["completed"] != true || completedData["selected"] != "venue-add" || completedData["completedByUid"] != "verified-user" {
		t.Fatalf("completed Meet data=%v", completedData)
	}
	completionRetry := doVenueRequest(t, client, http.MethodPost, completionURL, "Bearer valid-token", `{"venue_id":"venue-add"}`)
	if completionRetry.StatusCode != http.StatusConflict {
		body := readVenueResponseBody(t, completionRetry)
		t.Fatalf("repeated Meet completion status=%d body=%s, want HTTP 409", completionRetry.StatusCode, body)
	}
	completionRetry.Body.Close()
	audits, err = firestoreClient.Collection("poll_action_audit").Where("pollId", "==", result.ID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("read completion audits: %v", err)
	}
	if len(audits) != 4 {
		t.Fatalf("audits after completion retry=%d, want one completion audit", len(audits))
	}
	rescheduleURL := baseURL + "/api/meet/" + result.ID + "/reschedule"
	reschedule := func(body string, wantStatus int) {
		t.Helper()
		response := doVenueRequest(t, client, http.MethodPost, rescheduleURL, "Bearer valid-token", body)
		if response.StatusCode != wantStatus {
			responseBody := readVenueResponseBody(t, response)
			t.Fatalf("Meet reschedule body=%s status=%d, want HTTP %d", responseBody, response.StatusCode, wantStatus)
		}
		response.Body.Close()
	}
	reschedule(`{"date":"2026-11-19"}`, http.StatusBadRequest)
	reschedule(`{"venue_id":"venue-other","restaurant_id":"restaurant-1","restaurant_time":"18:30"}`, http.StatusOK)
	reschedule(`{"restaurant_time":"19:15"}`, http.StatusOK)
	reschedule(`{"venue_id":"venue-add"}`, http.StatusOK)
	reschedule(`{"venue_id":"venue-other"}`, http.StatusOK)
	reschedule(`{"restaurant_id":null}`, http.StatusOK)
	reschedule(`{"venue_id":"venue-other"}`, http.StatusOK)
	rescheduledMeet, err := firestoreClient.Collection("polls").Doc(result.ID).Get(ctx)
	if err != nil {
		t.Fatalf("read rescheduled Meet: %v", err)
	}
	rescheduledData := rescheduledMeet.Data()
	if rescheduledData["selected"] != "venue-other" || rescheduledData["completed"] != true {
		t.Fatalf("rescheduled Meet data=%v", rescheduledData)
	}
	if _, exists := rescheduledData["restaurant"]; exists {
		t.Fatalf("restaurant was not cleared: %v", rescheduledData)
	}
	if _, exists := rescheduledData["restaurant_time"]; exists {
		t.Fatalf("restaurant time was not cleared: %v", rescheduledData)
	}
	if _, exists := rescheduledData["completedByUid"]; exists {
		t.Fatalf("completion actor was not cleared: %v", rescheduledData)
	}
	history, ok := rescheduledData["previous_pubs"].([]any)
	if !ok || len(history) != 3 || history[0] != "venue-add" || history[1] != "venue-other" || history[2] != "venue-add" {
		t.Fatalf("reschedule history=%v, want [venue-add venue-other venue-add]", rescheduledData["previous_pubs"])
	}
	audits, err = firestoreClient.Collection("poll_action_audit").Where("pollId", "==", result.ID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("read reschedule audits: %v", err)
	}
	if len(audits) != 9 {
		t.Fatalf("audits after reschedule no-ops=%d, want 9", len(audits))
	}
	if _, err := firestoreClient.Collection("attendance").Doc(result.ID).Set(ctx, map[string]any{"venue-add": map[string]any{"canCome": []string{"verified-user"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("open_actions").Doc(result.ID).Set(ctx, map[string]any{"push": []string{"venue-other"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("messages").Doc(result.ID).Set(ctx, map[string]any{"text": "unrelated chat record"}); err != nil {
		t.Fatal(err)
	}
	deleteURL := baseURL + "/api/meet/" + result.ID
	deleted := doVenueRequest(t, client, http.MethodDelete, deleteURL, "Bearer valid-token", "")
	if deleted.StatusCode != http.StatusNoContent {
		body := readVenueResponseBody(t, deleted)
		t.Fatalf("Meet deletion status=%d body=%s, want HTTP 204", deleted.StatusCode, body)
	}
	deleted.Body.Close()
	for _, collection := range []string{"polls", "votes", "attendance", "open_actions"} {
		_, err := firestoreClient.Collection(collection).Doc(result.ID).Get(ctx)
		if status.Code(err) != codes.NotFound {
			t.Fatalf("%s/%s read error=%v, want not found", collection, result.ID, err)
		}
	}
	if _, err := firestoreClient.Collection("messages").Doc(result.ID).Get(ctx); err != nil {
		t.Fatalf("unrelated message was removed: %v", err)
	}
	missingDelete := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/meet/missing-meet", "Bearer valid-token", "")
	if missingDelete.StatusCode != http.StatusNotFound {
		body := readVenueResponseBody(t, missingDelete)
		t.Fatalf("missing Meet deletion status=%d body=%s, want HTTP 404", missingDelete.StatusCode, body)
	}
	missingDelete.Body.Close()

	secondCreate := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/meets/"+groupID, "Bearer valid-token", `{"date":"2026-11-18"}`)
	if secondCreate.StatusCode != http.StatusCreated {
		body := readVenueResponseBody(t, secondCreate)
		t.Fatalf("same-date Meet create status=%d body=%s, want HTTP 201", secondCreate.StatusCode, body)
	}
	var secondResult struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(secondCreate.Body).Decode(&secondResult); err != nil {
		t.Fatal(err)
	}
	secondCreate.Body.Close()
	if secondResult.ID == "" || secondResult.ID == result.ID {
		t.Fatalf("same-date Meet ID=%q, want a distinct Firestore ID", secondResult.ID)
	}
	secondDelete := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/meet/"+secondResult.ID, "Bearer valid-token", "")
	if secondDelete.StatusCode != http.StatusNoContent {
		body := readVenueResponseBody(t, secondDelete)
		t.Fatalf("Meet deletion with missing children status=%d body=%s, want HTTP 204", secondDelete.StatusCode, body)
	}
	secondDelete.Body.Close()
	secondAudits, err := firestoreClient.Collection("poll_action_audit").Where("pollId", "==", secondResult.ID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("read second Meet audits: %v", err)
	}
	if len(secondAudits) != 2 {
		t.Fatalf("second Meet audits=%d, want create and delete", len(secondAudits))
	}
}

func TestIndividualMeetGetAgainstFirestoreEmulator(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-last-orders-meets-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })
	meetID := "meet-1"
	groupID := "public-group"
	meet := firestoreClient.Collection("polls").Doc(meetID)
	group := firestoreClient.Collection("groups").Doc(groupID)
	privateGroup := firestoreClient.Collection("groups").Doc("private-group")
	hiddenGroup := firestoreClient.Collection("groups").Doc("hidden-group")
	malformedMemberGroup := firestoreClient.Collection("groups").Doc("malformed-member-group")
	adminGroup := firestoreClient.Collection("groups").Doc("admin-group")
	adminRole := firestoreClient.Collection("roles").Doc("admin")
	venue := firestoreClient.Collection("pubs").Doc("venue-1")
	if _, err := group.Set(ctx, map[string]any{"name": "Public Group", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	if _, err := venue.Set(ctx, map[string]any{"name": "Seed Pub", "venueType": "pub", "address": "Seed Street", "deleted_on": "2026-11-18T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := privateGroup.Set(ctx, map[string]any{"name": "Private Group", "visibility": "private", "members": map[string]any{"verified-user": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := hiddenGroup.Set(ctx, map[string]any{"name": "Hidden Group", "visibility": "private", "members": map[string]any{"someone-else": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := malformedMemberGroup.Set(ctx, map[string]any{"name": "Malformed Member Group", "visibility": "private", "members": map[string]any{"verified-user": "true"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := adminGroup.Set(ctx, map[string]any{"name": "Admin Group", "visibility": "private", "members": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := adminRole.Set(ctx, map[string]any{"verified-user": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := meet.Set(ctx, map[string]any{
		"date":      "2026-11-20",
		"completed": false,
		"groupId":   groupID,
		"selected":  "venue-1",
		"pubs":      map[string]any{"venue-1": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("votes").Doc(meetID).Set(ctx, map[string]any{"venue-1": []string{"verified-user"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("attendance").Doc(meetID).Set(ctx, map[string]any{"venue-1": map[string]any{"canCome": []string{"verified-user"}}}); err != nil {
		t.Fatal(err)
	}
	memberMeet := firestoreClient.Collection("polls").Doc("private-meet")
	if _, err := memberMeet.Set(ctx, map[string]any{"date": "2026-11-20", "completed": false, "groupId": "private-group"}); err != nil {
		t.Fatal(err)
	}
	hiddenMeet := firestoreClient.Collection("polls").Doc("hidden-meet")
	if _, err := hiddenMeet.Set(ctx, map[string]any{"date": "2026-11-20", "completed": false, "groupId": "hidden-group"}); err != nil {
		t.Fatal(err)
	}
	malformedMemberMeet := firestoreClient.Collection("polls").Doc("malformed-member-meet")
	if _, err := malformedMemberMeet.Set(ctx, map[string]any{"date": "2026-11-20", "completed": false, "groupId": "malformed-member-group"}); err != nil {
		t.Fatal(err)
	}
	adminMeet := firestoreClient.Collection("polls").Doc("admin-meet")
	if _, err := adminMeet.Set(ctx, map[string]any{"date": "2026-11-20", "completed": false, "groupId": "admin-group"}); err != nil {
		t.Fatal(err)
	}
	missingVenueMeet := firestoreClient.Collection("polls").Doc("missing-venue-meet")
	if _, err := missingVenueMeet.Set(ctx, map[string]any{"date": "2026-11-20", "completed": false, "groupId": groupID, "selected": "missing-venue"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = meet.Delete(cleanupCtx)
		_, _ = group.Delete(cleanupCtx)
		_, _ = privateGroup.Delete(cleanupCtx)
		_, _ = hiddenGroup.Delete(cleanupCtx)
		_, _ = malformedMemberGroup.Delete(cleanupCtx)
		_, _ = adminGroup.Delete(cleanupCtx)
		_, _ = adminRole.Delete(cleanupCtx)
		_, _ = memberMeet.Delete(cleanupCtx)
		_, _ = hiddenMeet.Delete(cleanupCtx)
		_, _ = malformedMemberMeet.Delete(cleanupCtx)
		_, _ = adminMeet.Delete(cleanupCtx)
		_, _ = missingVenueMeet.Delete(cleanupCtx)
		_, _ = firestoreClient.Collection("polls").Doc("created-after-miss").Delete(cleanupCtx)
		_, _ = venue.Delete(cleanupCtx)
		_, _ = firestoreClient.Collection("votes").Doc(meetID).Delete(cleanupCtx)
		_, _ = firestoreClient.Collection("attendance").Doc(meetID).Delete(cleanupCtx)
	})

	t.Setenv("LAST_ORDERS_MEET_PROJECTION_DIAGNOSTICS", "true")
	logs := &syncBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Meet projection diagnostics: %s", logs.String())
		}
	})
	config := testConfig(t, t.TempDir()+"/meets-emulator.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.Component = "handpump"
	config.LandlordInternalURL = "http://127.0.0.1:8082"
	config.InternalAPIToken = "test-only-internal-token-0123456789"
	clock := &meetTestClock{current: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)}
	config.Clock = clock
	config.VenueSource = nil
	config.GroupSource = nil
	config.HTTPAddr = "127.0.0.1:0"
	config.AllowAuthEmulator = true
	application, err := app.New(config)
	if err != nil {
		t.Fatalf("new app with Firestore emulator source: %v", err)
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
		case <-time.After(5 * time.Second):
			t.Error("app did not stop")
		}
	})

	client := &http.Client{Timeout: 8 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	unauthenticated := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "", "")
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Meet GET status=%d, want HTTP 401", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()
	deletedVenueResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/venues/venue-1", "", "")
	if deletedVenueResponse.StatusCode != http.StatusNotFound {
		body := readVenueResponseBody(t, deletedVenueResponse)
		t.Fatalf("ordinary Venue GET status=%d body=%s, want HTTP 404", deletedVenueResponse.StatusCode, body)
	}
	deletedVenueResponse.Body.Close()
	const callers = 8
	var callersDone sync.WaitGroup
	statuses := make(chan int, callers)
	for range callers {
		callersDone.Add(1)
		go func() {
			defer callersDone.Done()
			request, err := http.NewRequest(http.MethodGet, baseURL+"/api/meet/"+meetID, nil)
			if err != nil {
				statuses <- 0
				return
			}
			request.Header.Set("Authorization", "Bearer valid-token")
			concurrentResponse, err := client.Do(request)
			if err != nil {
				statuses <- 0
				return
			}
			_, _ = io.Copy(io.Discard, concurrentResponse.Body)
			concurrentResponse.Body.Close()
			statuses <- concurrentResponse.StatusCode
		}()
	}
	callersDone.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("concurrent Meet GET status=%d, want HTTP 200", status)
		}
	}
	if got := strings.Count(logs.String(), "Meet projection admitted"); got != 1 {
		t.Fatalf("concurrent GET admitted %d projections, want one", got)
	}
	if got := strings.Count(logs.String(), "Meet projection watch opened"); got != 1 {
		t.Fatalf("concurrent GET opened %d source watches, want one", got)
	}
	response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "Bearer valid-token", "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, response)
		t.Fatalf("get individual Meet status=%d body=%s", response.StatusCode, body)
	}
	var result map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := json.Unmarshal(result["id"], &id); err != nil || id != meetID {
		t.Fatalf("Meet ID=%q, err=%v; want %q", id, err, meetID)
	}
	var candidates []string
	if err := json.Unmarshal(result["candidate_venue_ids"], &candidates); err != nil || len(candidates) != 1 || candidates[0] != "venue-1" {
		t.Fatalf("candidate venue IDs=%v, err=%v", candidates, err)
	}
	var selectedVenue struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Address   string `json:"address"`
		VenueType string `json:"venue_type"`
	}
	if err := json.Unmarshal(result["venue"], &selectedVenue); err != nil {
		t.Fatal(err)
	}
	if selectedVenue.ID != "venue-1" || selectedVenue.Name != "Seed Pub" || selectedVenue.Address != "Seed Street" || selectedVenue.VenueType != "pub" {
		t.Fatalf("selected Venue summary=%+v", selectedVenue)
	}
	for _, privateField := range []string{"votes", "attendance", "attending_count", "not_attending_count"} {
		if _, present := result[privateField]; present {
			t.Fatalf("Meet response unexpectedly includes %q", privateField)
		}
	}
	for path, wantStatus := range map[string]int{
		"private-meet":          http.StatusOK,
		"hidden-meet":           http.StatusNotFound,
		"malformed-member-meet": http.StatusNotFound,
		"admin-meet":            http.StatusNotFound,
		"does-not-exist":        http.StatusNotFound,
		"missing-venue-meet":    http.StatusOK,
	} {
		result := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+path, "Bearer valid-token", "")
		if result.StatusCode != wantStatus {
			body := readVenueResponseBody(t, result)
			t.Fatalf("GET Meet %q status=%d body=%s, want %d", path, result.StatusCode, body, wantStatus)
		}
		if path == "missing-venue-meet" {
			var missingResponse struct {
				Venue struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"venue"`
			}
			if err := json.NewDecoder(result.Body).Decode(&missingResponse); err != nil {
				t.Fatal(err)
			}
			if missingResponse.Venue.ID != "missing-venue" || missingResponse.Venue.Status != "missing" {
				t.Fatalf("missing Venue reference=%+v", missingResponse.Venue)
			}
		}
		result.Body.Close()
	}
	if _, err := adminRole.Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatal(err)
	}
	waitForMeetStatus(t, client, baseURL, "admin-meet", http.StatusOK)
	if _, err := adminRole.Set(ctx, map[string]any{"verified-user": false}); err != nil {
		t.Fatal(err)
	}
	waitForMeetStatus(t, client, baseURL, "admin-meet", http.StatusNotFound)
	missingMeet := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/created-after-miss", "Bearer valid-token", "")
	if missingMeet.StatusCode != http.StatusNotFound {
		body := readVenueResponseBody(t, missingMeet)
		t.Fatalf("first missing Meet status=%d body=%s", missingMeet.StatusCode, body)
	}
	missingMeet.Body.Close()
	if _, err := firestoreClient.Collection("polls").Doc("created-after-miss").Set(ctx, map[string]any{"date": "2026-11-21", "completed": false, "groupId": groupID}); err != nil {
		t.Fatal(err)
	}
	createdMeet := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/created-after-miss", "Bearer valid-token", "")
	if createdMeet.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, createdMeet)
		t.Fatalf("Meet created after an initial miss status=%d body=%s", createdMeet.StatusCode, body)
	}
	createdMeet.Body.Close()
	if _, err := meet.Update(ctx, []firestore.Update{{Path: "date", Value: "2026-11-21"}}); err != nil {
		t.Fatal(err)
	}
	waitForMeetDate(t, client, baseURL, meetID, "2026-11-21")
	if _, err := venue.Update(ctx, []firestore.Update{{Path: "name", Value: "Updated Soft-deleted Pub"}}); err != nil {
		t.Fatal(err)
	}
	waitForMeetVenueName(t, client, baseURL, meetID, "Updated Soft-deleted Pub")
	if _, err := venue.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	waitForMeetVenueMissing(t, client, baseURL, meetID, "venue-1")
	failureAttemptsBefore := strings.Count(logs.String(), "Meet projection baseline attempt failed")
	if _, err := firestoreClient.Collection("polls").Doc("created-after-miss").Update(ctx, []firestore.Update{{Path: "date", Value: "not-a-date"}}); err != nil {
		t.Fatal(err)
	}
	waitForMeetProjectionLog(t, logs, "Meet projection source unavailable")
	unavailable := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/created-after-miss", "Bearer valid-token", "")
	if unavailable.StatusCode != http.StatusServiceUnavailable {
		body := readVenueResponseBody(t, unavailable)
		t.Fatalf("malformed Meet source status=%d body=%s, want HTTP 503", unavailable.StatusCode, body)
	}
	unavailable.Body.Close()
	waitForMeetProjectionLogCount(t, logs, "Meet projection baseline attempt failed", failureAttemptsBefore+2)
	if _, err := firestoreClient.Collection("polls").Doc("created-after-miss").Update(ctx, []firestore.Update{{Path: "date", Value: "2026-11-23"}}); err != nil {
		t.Fatal(err)
	}
	waitForMeetDate(t, client, baseURL, "created-after-miss", "2026-11-23")
	if _, err := meet.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	waitForMeetStatus(t, client, baseURL, meetID, http.StatusNotFound)
	if _, err := meet.Set(ctx, map[string]any{"date": "2026-12-01", "completed": false, "groupId": groupID}); err != nil {
		t.Fatal(err)
	}
	waitForMeetDate(t, client, baseURL, meetID, "2026-12-01")
	admissionsBeforeExpiry := strings.Count(logs.String(), "Meet projection admitted")
	clock.Advance(6 * time.Hour)
	application.NotifyScheduling()
	waitForMeetProjectionLog(t, logs, "Meet projection evicted")
	expiredEntry := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "Bearer valid-token", "")
	if expiredEntry.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, expiredEntry)
		t.Fatalf("Meet GET after projection expiry status=%d body=%s", expiredEntry.StatusCode, body)
	}
	expiredEntry.Body.Close()
	if got := strings.Count(logs.String(), "Meet projection admitted"); got != admissionsBeforeExpiry+1 {
		t.Fatalf("GET after expiry admitted %d projections; want one replacement after %d prior admissions", got, admissionsBeforeExpiry)
	}
}

func TestIndividualMeetProjectionStartsFreshAfterRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-last-orders-restart-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })
	group := firestoreClient.Collection("groups").Doc("public-group")
	meet := firestoreClient.Collection("polls").Doc("restart-meet")
	if _, err := group.Set(ctx, map[string]any{"name": "Public Group", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	if _, err := meet.Set(ctx, map[string]any{"date": "2026-11-20", "completed": false, "groupId": "public-group"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = group.Delete(cleanupCtx)
		_, _ = meet.Delete(cleanupCtx)
	})

	t.Setenv("LAST_ORDERS_MEET_PROJECTION_DIAGNOSTICS", "true")
	dbPath := t.TempDir() + "/restart.db"
	clock := &meetTestClock{current: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)}
	makeConfig := func(logs *syncBuffer) app.Config {
		config := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
		config.Logger = slog.New(slog.NewJSONHandler(logs, nil))
		config.EnableFirestore = true
		config.FirestoreProjectID = projectID
		config.Component = "handpump"
		config.LandlordInternalURL = "http://127.0.0.1:8082"
		config.InternalAPIToken = "test-only-internal-token-0123456789"
		config.HTTPAddr = "127.0.0.1:0"
		config.AllowAuthEmulator = true
		config.VenueSource = nil
		config.GroupSource = nil
		config.Clock = clock
		return config
	}
	start := func(logs *syncBuffer) *runningMeetAPI {
		t.Helper()
		application, err := app.New(makeConfig(logs))
		if err != nil {
			t.Fatalf("new HandPump: %v", err)
		}
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- application.Run(runCtx) }()
		server := &runningMeetAPI{application: application, stop: stop, done: done, baseURL: "http://" + application.HTTPAddr()}
		client := &http.Client{Timeout: 5 * time.Second}
		waitForVenueHTTP(t, ctx, client, server.baseURL)
		t.Cleanup(func() { server.close(t) })
		return server
	}
	get := func(server *runningMeetAPI) {
		t.Helper()
		response := doVenueRequest(t, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, server.baseURL+"/api/meet/restart-meet", "Bearer valid-token", "")
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("restart Meet GET status=%d, want HTTP 200", response.StatusCode)
		}
	}

	firstLogs := &syncBuffer{}
	first := start(firstLogs)
	get(first)
	if got := strings.Count(firstLogs.String(), "Meet projection watch opened"); got != 1 {
		t.Fatalf("first process opened %d Meet watches, want one", got)
	}
	first.close(t)

	restartedLogs := &syncBuffer{}
	restarted := start(restartedLogs)
	if strings.Contains(restartedLogs.String(), "Meet projection watch opened") {
		t.Fatal("restart eagerly restored an individual Meet watch")
	}
	get(restarted)
	if got := strings.Count(restartedLogs.String(), "Meet projection admitted"); got != 1 {
		t.Fatalf("first GET after restart admitted %d projections, want one", got)
	}
	if got := strings.Count(restartedLogs.String(), "Meet projection watch opened"); got != 1 {
		t.Fatalf("first GET after restart opened %d source watches, want one", got)
	}
}

type runningMeetAPI struct {
	application *app.App
	stop        context.CancelFunc
	done        chan error
	baseURL     string
	closed      bool
}

func (server *runningMeetAPI) close(t *testing.T) {
	t.Helper()
	if server.closed {
		return
	}
	server.stop()
	select {
	case err := <-server.done:
		if err != nil {
			t.Errorf("stop app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("app did not stop")
	}
	if err := server.application.Close(); err != nil {
		t.Errorf("close app: %v", err)
	}
	server.closed = true
}

type meetTestClock struct {
	mu      sync.RWMutex
	current time.Time
}

func (clock *meetTestClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.current
}

func (clock *meetTestClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.current = clock.current.Add(duration)
}

func waitForMeetDate(t *testing.T, client *http.Client, baseURL, meetID, want string) {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "Bearer valid-token", "")
		if response.StatusCode == http.StatusOK {
			var result struct {
				Date string `json:"date"`
			}
			err := json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if result.Date == want {
				return
			}
		} else {
			response.Body.Close()
		}
		select {
		case <-deadline.C:
			t.Fatalf("Meet %q did not reach date %q", meetID, want)
		case <-ticker.C:
		}
	}
}

func waitForMeetVenueName(t *testing.T, client *http.Client, baseURL, meetID, want string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "Bearer valid-token", "")
		if response.StatusCode == http.StatusOK {
			var result struct {
				Venue struct {
					Name string `json:"name"`
				} `json:"venue"`
			}
			err := json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if result.Venue.Name == want {
				return
			}
		} else {
			response.Body.Close()
		}
		select {
		case <-deadline.C:
			t.Fatalf("Meet %q did not show Venue name %q", meetID, want)
		case <-ticker.C:
		}
	}
}

func waitForMeetVenueMissing(t *testing.T, client *http.Client, baseURL, meetID, venueID string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "Bearer valid-token", "")
		if response.StatusCode == http.StatusOK {
			var result struct {
				Venue struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"venue"`
			}
			err := json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if result.Venue.ID == venueID && result.Venue.Status == "missing" {
				return
			}
		} else {
			response.Body.Close()
		}
		select {
		case <-deadline.C:
			t.Fatalf("Meet %q did not expose hard-deleted Venue %q as missing", meetID, venueID)
		case <-ticker.C:
		}
	}
}

func waitForMeetStatus(t *testing.T, client *http.Client, baseURL, meetID string, want int) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/"+meetID, "Bearer valid-token", "")
		status := response.StatusCode
		response.Body.Close()
		if status == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("Meet %q status=%d, want %d", meetID, status, want)
		case <-ticker.C:
		}
	}
}

func waitForMeetProjectionLog(t *testing.T, logs *syncBuffer, want string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !strings.Contains(logs.String(), want) {
		select {
		case <-deadline.C:
			t.Fatalf("Meet projection log %q was not observed", want)
		case <-ticker.C:
		}
	}
}

func waitForMeetProjectionLogCount(t *testing.T, logs *syncBuffer, want string, minimum int) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for strings.Count(logs.String(), want) < minimum {
		select {
		case <-deadline.C:
			t.Fatalf("Meet projection log %q occurred fewer than %d times", want, minimum)
		case <-ticker.C:
		}
	}
}
