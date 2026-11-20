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

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

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
		_, _ = memberMeet.Delete(cleanupCtx)
		_, _ = hiddenMeet.Delete(cleanupCtx)
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
		"private-meet":       http.StatusOK,
		"hidden-meet":        http.StatusNotFound,
		"does-not-exist":     http.StatusNotFound,
		"missing-venue-meet": http.StatusOK,
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
