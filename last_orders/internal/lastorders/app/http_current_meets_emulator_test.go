package app_test

import (
	"bufio"
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
	"last_orders/internal/lastorders/components/sse"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

func TestCurrentMeetsGetAndStreamAgainstFirestoreEmulator(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-current-meets-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })

	groups := []string{"group-a", "group-b", "empty-group", "midnight-group", "private-group", "get-only-group", "renewed-group"}
	for _, groupID := range groups {
		data := map[string]any{"name": groupID, "visibility": "public"}
		if groupID == "private-group" {
			data["visibility"] = "private"
			data["members"] = map[string]any{"verified-user": true}
		}
		if _, err := firestoreClient.Collection("groups").Doc(groupID).Set(ctx, data); err != nil {
			t.Fatal(err)
		}
	}
	privateGroup := firestoreClient.Collection("groups").Doc("private-group")
	deletedVenue := firestoreClient.Collection("pubs").Doc("deleted-venue")
	if _, err := deletedVenue.Set(ctx, map[string]any{
		"name": "The Old Maypole", "venueType": "pub", "address": "Old Street",
		"web_site": "https://old-maypole.example.test", "pubImage": "https://img.example.test/old.png",
		"deleted_on": "2026-11-19T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	meets := map[string]map[string]any{
		"meet-a":             {"date": "2026-03-29", "completed": true, "groupId": "group-a", "selected": "deleted-venue", "pubs": map[string]any{"deleted-venue": true}, "previous_pubs": []string{"old-venue"}},
		"meet-b":             {"date": "2026-03-29", "completed": true, "groupId": "group-a", "selected": "missing-venue", "pubs": map[string]any{"missing-venue": true}},
		"meet-z":             {"date": "2026-03-30", "completed": true, "groupId": "group-a", "selected": "deleted-venue", "pubs": map[string]any{"deleted-venue": true}},
		"incomplete":         {"date": "2026-03-29", "completed": false, "groupId": "group-a"},
		"overdue-open":       {"date": "2020-01-01", "completed": false, "groupId": "group-a", "pubs": map[string]any{"deleted-venue": true}},
		"past":               {"date": "2026-03-28", "completed": true, "groupId": "group-a"},
		"other-group-meet":   {"date": "2026-03-29", "completed": true, "groupId": "group-b"},
		"other-group-open":   {"date": "2020-01-01", "completed": false, "groupId": "group-b"},
		"private-group-meet": {"date": "2026-03-29", "completed": true, "groupId": "private-group"},
	}
	for meetID, data := range meets {
		if _, err := firestoreClient.Collection("polls").Doc(meetID).Set(ctx, data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := firestoreClient.Collection("votes").Doc("meet-a").Set(ctx, map[string]any{"deleted-venue": []string{"member"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("attendance").Doc("meet-a").Set(ctx, map[string]any{"deleted-venue": map[string]any{"canCome": []string{"member"}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		for _, groupID := range groups {
			_, _ = firestoreClient.Collection("groups").Doc(groupID).Delete(cleanupCtx)
		}
		for meetID := range meets {
			_, _ = firestoreClient.Collection("polls").Doc(meetID).Delete(cleanupCtx)
		}
		_, _ = firestoreClient.Collection("polls").Doc("late-open-meet").Delete(cleanupCtx)
		_, _ = firestoreClient.Collection("votes").Doc("meet-a").Delete(cleanupCtx)
		_, _ = firestoreClient.Collection("attendance").Doc("meet-a").Delete(cleanupCtx)
		_, _ = deletedVenue.Delete(cleanupCtx)
	})

	t.Setenv("LAST_ORDERS_CURRENT_MEET_DIAGNOSTICS", "true")
	t.Setenv("LAST_ORDERS_OPEN_MEET_DIAGNOSTICS", "true")
	logs := &syncBuffer{}
	config := testConfig(t, t.TempDir()+"/current-meets.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.Component = "handpump"
	config.LandlordInternalURL = "http://127.0.0.1:8082"
	config.InternalAPIToken = "test-only-internal-token-0123456789"
	clock := &meetTestClock{current: time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC)}
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
			t.Error("stop app timed out")
		}
	})

	client := &http.Client{Timeout: 30 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	const concurrentReaders = 6
	var concurrentWait sync.WaitGroup
	statuses := make(chan int, concurrentReaders)
	for range concurrentReaders {
		concurrentWait.Add(1)
		go func() {
			defer concurrentWait.Done()
			request, err := http.NewRequest(http.MethodGet, baseURL+"/api/meets/group-a/current", nil)
			if err != nil {
				statuses <- 0
				return
			}
			response, err := client.Do(request)
			if err != nil {
				statuses <- 0
				return
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	concurrentWait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("concurrent current-Meets GET status=%d, want HTTP 200", status)
		}
	}
	getSnapshot := func(groupID string) struct {
		Cursor string `json:"cursor"`
		Meets  []struct {
			ID               string          `json:"id"`
			GroupID          string          `json:"group_id"`
			Date             string          `json:"date"`
			Venue            json.RawMessage `json:"venue"`
			PreviousVenueIDs []string        `json:"previous_venue_ids"`
			Votes            json.RawMessage `json:"votes"`
			Attendance       json.RawMessage `json:"attendance"`
		} `json:"meets"`
	} {
		t.Helper()
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/"+groupID+"/current", "", "")
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("current-Meets GET for %q status=%d body=%s", groupID, response.StatusCode, readVenueResponseBody(t, response))
		}
		var snapshot struct {
			Cursor string `json:"cursor"`
			Meets  []struct {
				ID               string          `json:"id"`
				GroupID          string          `json:"group_id"`
				Date             string          `json:"date"`
				Venue            json.RawMessage `json:"venue"`
				PreviousVenueIDs []string        `json:"previous_venue_ids"`
				Votes            json.RawMessage `json:"votes"`
				Attendance       json.RawMessage `json:"attendance"`
			} `json:"meets"`
		}
		if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}

	current := getSnapshot("group-a")
	if current.Cursor == "" || len(current.Meets) != 3 || current.Meets[0].ID != "meet-a" || current.Meets[1].ID != "meet-b" || current.Meets[2].ID != "meet-z" {
		t.Fatalf("current-Meets order or size is wrong: %+v", current)
	}
	var selectedVenue struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		Name       string `json:"name"`
		WebsiteURL string `json:"website_url"`
	}
	if err := json.Unmarshal(current.Meets[0].Venue, &selectedVenue); err != nil {
		t.Fatal(err)
	}
	if selectedVenue.ID != "deleted-venue" || selectedVenue.Status != "deleted" || selectedVenue.Name != "The Old Maypole" || selectedVenue.WebsiteURL != "https://old-maypole.example.test" {
		t.Fatalf("soft-deleted selected Venue=%+v", selectedVenue)
	}
	if len(current.Meets[0].PreviousVenueIDs) != 1 || current.Meets[0].PreviousVenueIDs[0] != "old-venue" {
		t.Fatalf("previous Venue IDs=%v, want [old-venue]", current.Meets[0].PreviousVenueIDs)
	}
	if err := json.Unmarshal(current.Meets[1].Venue, &selectedVenue); err != nil {
		t.Fatal(err)
	}
	if selectedVenue.ID != "missing-venue" || selectedVenue.Status != "missing" {
		t.Fatalf("missing selected Venue=%+v", selectedVenue)
	}
	for _, meet := range current.Meets {
		if len(meet.Votes) != 0 || len(meet.Attendance) != 0 {
			t.Fatalf("Meet %q response includes vote or attendance data", meet.ID)
		}
	}
	if other := getSnapshot("group-b"); len(other.Meets) != 1 || other.Meets[0].ID != "other-group-meet" {
		t.Fatalf("Group B snapshot leaked or omitted Meets: %+v", other)
	}
	getOpenSnapshot := func(groupID string) struct {
		Cursor string `json:"cursor"`
		Meets  []struct {
			ID                string   `json:"id"`
			GroupID           string   `json:"group_id"`
			Date              string   `json:"date"`
			VotingComplete    bool     `json:"voting_complete"`
			CandidateVenueIDs []string `json:"candidate_venue_ids"`
		} `json:"meets"`
	} {
		t.Helper()
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/"+groupID+"/open", "", "")
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("open-Meets GET for %q status=%d body=%s", groupID, response.StatusCode, readVenueResponseBody(t, response))
		}
		var snapshot struct {
			Cursor string `json:"cursor"`
			Meets  []struct {
				ID                string   `json:"id"`
				GroupID           string   `json:"group_id"`
				Date              string   `json:"date"`
				VotingComplete    bool     `json:"voting_complete"`
				CandidateVenueIDs []string `json:"candidate_venue_ids"`
			} `json:"meets"`
		}
		if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	open := getOpenSnapshot("group-a")
	if open.Cursor == "" || len(open.Meets) != 2 || open.Meets[0].ID != "overdue-open" || open.Meets[0].Date != "2020-01-01" || open.Meets[0].VotingComplete || open.Meets[0].GroupID != "group-a" {
		t.Fatalf("open-Meets snapshot omitted the past-dated voting-open Meet or included the wrong Group: %+v", open)
	}
	if len(open.Meets[0].CandidateVenueIDs) != 1 || open.Meets[0].CandidateVenueIDs[0] != "deleted-venue" {
		t.Fatalf("open Meet candidate Venue IDs=%v", open.Meets[0].CandidateVenueIDs)
	}
	if other := getOpenSnapshot("group-b"); len(other.Meets) != 1 || other.Meets[0].ID != "other-group-open" {
		t.Fatalf("Group B open-Meets snapshot leaked or omitted Meets: %+v", other)
	}
	if repeated := getOpenSnapshot("group-a"); len(repeated.Meets) != 2 {
		t.Fatalf("repeated open-Meets snapshot=%+v", repeated)
	}
	privateOpenDenied := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/private-group/open", "", "")
	privateOpenDenied.Body.Close()
	if privateOpenDenied.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous private open-Meets GET status=%d, want HTTP 404", privateOpenDenied.StatusCode)
	}
	if empty := getSnapshot("empty-group"); empty.Meets == nil || len(empty.Meets) != 0 {
		t.Fatalf("empty Group snapshot=%+v", empty)
	}
	privateDenied := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/private-group/current", "", "")
	privateDenied.Body.Close()
	if privateDenied.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous private-Group GET status=%d, want HTTP 404", privateDenied.StatusCode)
	}
	privateAllowed := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/private-group/current", "Bearer valid-token", "")
	if privateAllowed.StatusCode != http.StatusOK {
		t.Fatalf("member private-Group GET status=%d body=%s", privateAllowed.StatusCode, readVenueResponseBody(t, privateAllowed))
	}
	privateAllowed.Body.Close()
	privateStreamCtx, stopPrivateStream := context.WithCancel(ctx)
	privateStreamRequest, err := http.NewRequestWithContext(privateStreamCtx, http.MethodGet, baseURL+"/api/meets/private-group/current/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	privateStreamRequest.Header.Set("Authorization", "Bearer valid-token")
	privateStreamResponse, err := client.Do(privateStreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	privateStreamReader := bufio.NewReader(privateStreamResponse.Body)
	if _, err := readCurrentMeetsFrame(privateStreamReader); err != nil {
		t.Fatal(err)
	}
	privateOpenStreamCtx, stopPrivateOpenStream := context.WithCancel(ctx)
	privateOpenStreamRequest, err := http.NewRequestWithContext(privateOpenStreamCtx, http.MethodGet, baseURL+"/api/meets/private-group/open/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	privateOpenStreamRequest.Header.Set("Authorization", "Bearer valid-token")
	privateOpenStreamResponse, err := client.Do(privateOpenStreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	privateOpenStreamReader := bufio.NewReader(privateOpenStreamResponse.Body)
	if _, err := readCurrentMeetsFrame(privateOpenStreamReader); err != nil {
		t.Fatal(err)
	}
	if _, err := privateGroup.Set(ctx, map[string]any{
		"name": "private-group", "visibility": "private", "members": map[string]any{"another-user": true},
	}); err != nil {
		t.Fatal(err)
	}
	privateReset := make(chan string, 1)
	privateResetError := make(chan error, 1)
	go func() {
		frame, err := readCurrentMeetsFrame(privateStreamReader)
		if err != nil {
			privateResetError <- err
			return
		}
		privateReset <- frame
	}()
	select {
	case frame := <-privateReset:
		if !strings.Contains(frame, "event: view_reset") || !strings.Contains(frame, "data: {}") {
			t.Fatalf("private-Group membership change did not reset the stream: %s", frame)
		}
	case err := <-privateResetError:
		t.Fatalf("read private-Group reset: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("private-Group stream was not reset after membership changed")
	}
	privateOpenReset := make(chan string, 1)
	privateOpenResetError := make(chan error, 1)
	go func() {
		frame, err := readCurrentMeetsFrame(privateOpenStreamReader)
		if err != nil {
			privateOpenResetError <- err
			return
		}
		privateOpenReset <- frame
	}()
	select {
	case frame := <-privateOpenReset:
		if !strings.Contains(frame, "event: view_reset") || !strings.Contains(frame, "data: {}") {
			t.Fatalf("private-Group membership change did not reset the open stream: %s", frame)
		}
	case err := <-privateOpenResetError:
		t.Fatalf("read private open-Group reset: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("private open-Meets stream was not reset after membership changed")
	}
	stopPrivateOpenStream()
	privateOpenStreamResponse.Body.Close()
	stopPrivateStream()
	privateStreamResponse.Body.Close()
	_ = getSnapshot("group-a")
	if got := strings.Count(logs.String(), "current-Meets query watch opened"); got != 4 {
		t.Fatalf("opened %d current-Meets query watches, want one per admitted Group", got)
	}
	if got := strings.Count(logs.String(), "open-Meets query watch opened"); got != 3 {
		t.Fatalf("opened %d open-Meets query watches, want one per admitted Group view", got)
	}

	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, baseURL+"/api/meets/group-a/current/stream?after=not-a-valid-cursor", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("current-Meets stream status=%d body=%s", response.StatusCode, readVenueResponseBody(t, response))
	}
	reader := bufio.NewReader(response.Body)
	initialFrame, err := readCurrentMeetsFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(initialFrame, "event: current_meets") || !strings.Contains(initialFrame, "id: v1.") || !strings.Contains(initialFrame, `"meets":[`) {
		t.Fatalf("invalid cursor did not recover with a complete snapshot: %s", initialFrame)
	}
	openStreamCtx, stopOpenStream := context.WithCancel(ctx)
	openStreamRequest, err := http.NewRequestWithContext(openStreamCtx, http.MethodGet, baseURL+"/api/meets/group-a/open/stream?after=not-a-valid-cursor", nil)
	if err != nil {
		t.Fatal(err)
	}
	openStreamResponse, err := client.Do(openStreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	if openStreamResponse.StatusCode != http.StatusOK {
		t.Fatalf("open-Meets stream status=%d body=%s", openStreamResponse.StatusCode, readVenueResponseBody(t, openStreamResponse))
	}
	openStreamReader := bufio.NewReader(openStreamResponse.Body)
	openInitialFrame, err := readCurrentMeetsFrame(openStreamReader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(openInitialFrame, "event: open_meets") || !strings.Contains(openInitialFrame, "id: v1.") || !strings.Contains(openInitialFrame, `"date":"2020-01-01"`) {
		t.Fatalf("open stream did not recover with a complete past-dated snapshot: %s", openInitialFrame)
	}

	if _, err := firestoreClient.Collection("votes").Doc("meet-a").Set(ctx, map[string]any{"deleted-venue": []string{"member", "another"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("attendance").Doc("meet-a").Set(ctx, map[string]any{"deleted-venue": map[string]any{"canCome": []string{"member", "another"}}}); err != nil {
		t.Fatal(err)
	}
	frames := make(chan string, 1)
	frameErrors := make(chan error, 1)
	go func() {
		frame, err := readCurrentMeetsFrame(reader)
		if err != nil {
			frameErrors <- err
			return
		}
		frames <- frame
	}()
	select {
	case frame := <-frames:
		t.Fatalf("vote or attendance update triggered a current-Meets frame: %s", frame)
	case err := <-frameErrors:
		t.Fatalf("read current-Meets stream after separate vote/attendance updates: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	openFrames := make(chan string, 1)
	openFrameErrors := make(chan error, 1)
	go func() {
		frame, err := readCurrentMeetsFrame(openStreamReader)
		if err != nil {
			openFrameErrors <- err
			return
		}
		openFrames <- frame
	}()
	select {
	case frame := <-openFrames:
		t.Fatalf("vote or attendance update triggered an open-Meets frame: %s", frame)
	case err := <-openFrameErrors:
		t.Fatalf("read open-Meets stream after separate vote/attendance updates: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	lateOpenMeet := firestoreClient.Collection("polls").Doc("late-open-meet")
	if _, err := lateOpenMeet.Set(ctx, map[string]any{
		"date": "2019-12-31", "completed": false, "groupId": "group-a", "pubs": map[string]any{"venue-1": true},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case createdFrame := <-openFrames:
		if !strings.Contains(createdFrame, `"id":"late-open-meet"`) {
			t.Fatalf("creating a past-dated open Meet did not update the complete view: %s", createdFrame)
		}
	case err := <-openFrameErrors:
		t.Fatalf("read created open-Meet frame: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("creating a past-dated open Meet did not update the complete view")
	}
	if _, err := lateOpenMeet.Update(ctx, []firestore.Update{{Path: "completed", Value: true}}); err != nil {
		t.Fatal(err)
	}
	completedFrames := make(chan string, 1)
	completedFrameErrors := make(chan error, 1)
	go func() {
		frame, err := readCurrentMeetsFrame(openStreamReader)
		if err != nil {
			completedFrameErrors <- err
			return
		}
		completedFrames <- frame
	}()
	select {
	case completedFrame := <-completedFrames:
		if !strings.Contains(completedFrame, "event: open_meets") || strings.Contains(completedFrame, `"id":"late-open-meet"`) {
			t.Fatalf("completing an open Meet did not remove it from the complete view: %s", completedFrame)
		}
	case err := <-completedFrameErrors:
		t.Fatalf("read completed open-Meet frame: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("completing an open Meet did not remove it from the complete view")
	}
	stopOpenStream()
	openStreamResponse.Body.Close()
	if _, err := deletedVenue.Update(ctx, []firestore.Update{{Path: "name", Value: "The New Maypole"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-frames:
		if !strings.Contains(frame, "event: current_meets") || !strings.Contains(frame, "The New Maypole") {
			t.Fatalf("Venue change did not produce the affected full view: %s", frame)
		}
	case err := <-frameErrors:
		t.Fatalf("read Venue-updated current-Meets frame: %v", err)
	case <-time.After(10 * time.Second):
		stopStream()
		t.Fatal("selected Venue change did not reach the current-Meets stream")
	}
	stopStream()

	clock.Advance(time.Minute)
	application.NotifyScheduling()
	dstSnapshot := getSnapshot("group-a")
	codec, err := sse.NewCursorCodec([]byte("test-only-cursor-signing-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	dstClaims, err := codec.Decode(dstSnapshot.Cursor)
	if err != nil || dstClaims.Date != "2026-03-29" {
		t.Fatalf("spring DST transition changed the current date unexpectedly: claims=%+v err=%v", dstClaims, err)
	}

	clock.Advance(21*time.Hour + 59*time.Minute)
	application.NotifyScheduling()
	midnightSnapshot := getSnapshot("midnight-group")
	midnightClaims, err := codec.Decode(midnightSnapshot.Cursor)
	if err != nil || midnightClaims.Date != "2026-03-29" {
		t.Fatalf("pre-midnight cursor date=%q err=%v", midnightClaims.Date, err)
	}
	midnightCtx, stopMidnight := context.WithCancel(ctx)
	request, err = http.NewRequestWithContext(midnightCtx, http.MethodGet, baseURL+"/api/meets/midnight-group/current/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	midnightReader := bufio.NewReader(response.Body)
	if _, err := readCurrentMeetsFrame(midnightReader); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	application.NotifyScheduling()
	resetFrame := make(chan string, 1)
	resetError := make(chan error, 1)
	go func() {
		frame, err := readCurrentMeetsFrame(midnightReader)
		if err != nil {
			resetError <- err
			return
		}
		resetFrame <- frame
	}()
	select {
	case frame := <-resetFrame:
		if !strings.Contains(frame, "event: view_reset") || !strings.Contains(frame, "data: {}") {
			t.Fatalf("London midnight did not reset the old-date stream: %s", frame)
		}
	case err := <-resetError:
		t.Fatalf("read London-midnight stream reset: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("current-Meets stream was not reset at London midnight")
	}
	stopMidnight()
	response.Body.Close()
	newDateSnapshot := getSnapshot("midnight-group")
	newDateClaims, err := codec.Decode(newDateSnapshot.Cursor)
	if err != nil || newDateClaims.Date != "2026-03-30" {
		t.Fatalf("post-midnight cursor date=%q err=%v", newDateClaims.Date, err)
	}
	_ = getSnapshot("get-only-group")
	_ = getSnapshot("renewed-group")
	clock.Advance(time.Hour)
	application.NotifyScheduling()
	renewalCtx, stopRenewal := context.WithCancel(ctx)
	renewalRequest, err := http.NewRequestWithContext(renewalCtx, http.MethodGet, baseURL+"/api/meets/renewed-group/current/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	renewalResponse, err := client.Do(renewalRequest)
	if err != nil {
		t.Fatal(err)
	}
	renewalReader := bufio.NewReader(renewalResponse.Body)
	if renewalResponse.StatusCode != http.StatusOK {
		t.Fatalf("renewal stream status=%d body=%s", renewalResponse.StatusCode, readVenueResponseBody(t, renewalResponse))
	}
	if _, err := readCurrentMeetsFrame(renewalReader); err != nil {
		t.Fatal(err)
	}
	clock.Advance(5 * time.Hour)
	application.NotifyScheduling()
	waitForCurrentMeetLogCount(t, logs, "current-Meets projection evicted", 1)
	admissionsBeforeReadRenewal := strings.Count(logs.String(), "current-Meets projection admitted")
	_ = getSnapshot("get-only-group")
	if got := strings.Count(logs.String(), "current-Meets projection admitted"); got != admissionsBeforeReadRenewal+1 {
		t.Fatalf("GET-only view admissions=%d after lease expiry, want %d", got, admissionsBeforeReadRenewal+1)
	}
	admissionsBeforeRenewedRead := strings.Count(logs.String(), "current-Meets projection admitted")
	_ = getSnapshot("renewed-group")
	if got := strings.Count(logs.String(), "current-Meets projection admitted"); got != admissionsBeforeRenewedRead {
		t.Fatalf("successfully renewed view admissions=%d before=%d, want unchanged", got, admissionsBeforeRenewedRead)
	}
	evictionsBeforeRenewedExpiry := strings.Count(logs.String(), "current-Meets projection evicted")
	clock.Advance(time.Hour + time.Minute)
	application.NotifyScheduling()
	waitForCurrentMeetLogCount(t, logs, "current-Meets projection evicted", evictionsBeforeRenewedExpiry+1)
	admissionsBeforeRenewedReplacement := strings.Count(logs.String(), "current-Meets projection admitted")
	_ = getSnapshot("renewed-group")
	if got := strings.Count(logs.String(), "current-Meets projection admitted"); got != admissionsBeforeRenewedReplacement+1 {
		t.Fatalf("renewed view replacement admissions=%d, want %d", got, admissionsBeforeRenewedReplacement+1)
	}
	stopRenewal()
	renewalResponse.Body.Close()
}

func readCurrentMeetsFrame(reader *bufio.Reader) (string, error) {
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return frame.String(), err
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String(), nil
		}
	}
}

func waitForCurrentMeetLogCount(t *testing.T, logs *syncBuffer, message string, minimum int) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for strings.Count(logs.String(), message) < minimum {
		select {
		case <-deadline.C:
			t.Fatalf("current-Meets diagnostic %q occurred fewer than %d times", message, minimum)
		case <-ticker.C:
		}
	}
}
