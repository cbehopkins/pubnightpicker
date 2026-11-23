package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/votecache"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestVotingSnapshotHTTPAgainstAuthAndFirestoreEmulators(t *testing.T) {
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

	projectID := "demo-vote-snapshot-local"
	t.Setenv("FIRESTORE_EMULATOR_HOST", firestoreHost)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", authHost)
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("LAST_ORDERS_FIREBASE_CREDENTIALS_JSON", "")
	t.Setenv("FIREBASE_AUTH_PROJECT_ID", projectID)
	t.Setenv("LAST_ORDERS_VOTING_PROJECTION_DIAGNOSTICS", "true")

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	idToken, callerUID := createVotingEmulatorAccount(t, authHost, projectID)
	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })

	publicGroup := firestoreClient.Collection("groups").Doc("public-group")
	privateGroup := firestoreClient.Collection("groups").Doc("private-group")
	hiddenGroup := firestoreClient.Collection("groups").Doc("hidden-group")
	mainMeet := firestoreClient.Collection("polls").Doc("vote-meet")
	missingVotesMeet := firestoreClient.Collection("polls").Doc("missing-votes-meet")
	hiddenMeet := firestoreClient.Collection("polls").Doc("hidden-meet")
	unavailableVotesMeet := firestoreClient.Collection("polls").Doc("unavailable-votes-meet")
	venue := firestoreClient.Collection("pubs").Doc("venue-1")
	if _, err := venue.Set(ctx, map[string]any{"name": "Voting Venue", "venueType": "pub"}); err != nil {
		t.Fatal(err)
	}
	if _, err := publicGroup.Set(ctx, map[string]any{"name": "Public", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	if _, err := privateGroup.Set(ctx, map[string]any{
		"name": "Private", "visibility": "private", "members": map[string]any{callerUID: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hiddenGroup.Set(ctx, map[string]any{
		"name": "Hidden", "visibility": "private", "members": map[string]any{"someone-else": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mainMeet.Set(ctx, map[string]any{
		"date": "2026-11-20", "completed": false, "groupId": "public-group", "pubs": map[string]any{"venue-1": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := missingVotesMeet.Set(ctx, map[string]any{
		"date": "2026-11-21", "completed": false, "groupId": "private-group", "pubs": map[string]any{"venue-2": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hiddenMeet.Set(ctx, map[string]any{
		"date": "2026-11-21", "completed": false, "groupId": "hidden-group", "pubs": map[string]any{"venue-3": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := unavailableVotesMeet.Set(ctx, map[string]any{
		"date": "2026-11-22", "completed": false, "groupId": "public-group", "pubs": map[string]any{"venue-4": true},
	}); err != nil {
		t.Fatal(err)
	}
	voteSource, err := votecache.NewFirestoreSource(firestoreClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("roles").Doc("canVote").Set(ctx, map[string]any{callerUID: true}); err != nil {
		t.Fatal(err)
	}
	votes := firestoreClient.Collection("votes").Doc("vote-meet")
	if _, err := votes.Set(ctx, map[string]any{
		"venue-1":            []string{"visible-voter", "default-voter", "hidden-voter", "unconfirmed-voter", "visible-voter"},
		"any":                []string{callerUID},
		"removed-candidate":  []string{callerUID},
		"inactive-candidate": []string{"inactive-voter"},
	}); err != nil {
		t.Fatal(err)
	}
	profiles := firestoreClient.Collection("user-public")
	if _, err := profiles.Doc(callerUID).Set(ctx, map[string]any{
		"uid": "ignored-uid", "name": "Caller Display Name", "email": "caller-secret@example.test", "votesVisible": nil,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Doc("visible-voter").Set(ctx, map[string]any{
		"name": "Visible Display Name", "email": "visible-secret@example.test", "votesVisible": true,
		"photoUrl": "https://example.test/original.jpg", "customPhotoUrl": "https://example.test/custom.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Doc("default-voter").Set(ctx, map[string]any{"name": "Default Display Name"}); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Doc("hidden-voter").Set(ctx, map[string]any{
		"name": "Hidden Display Name", "email": "hidden-secret@example.test", "votesVisible": false,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		for _, document := range []*firestore.DocumentRef{
			publicGroup, privateGroup, hiddenGroup, mainMeet, missingVotesMeet, hiddenMeet, unavailableVotesMeet, votes, venue,
			profiles.Doc(callerUID), profiles.Doc("visible-voter"), profiles.Doc("default-voter"), profiles.Doc("hidden-voter"),
		} {
			_, _ = document.Delete(cleanupCtx)
		}
	})

	logs := &syncBuffer{}
	config := testConfig(t, t.TempDir()+"/vote-snapshot.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.Logger = slog.New(slog.NewJSONHandler(logs, nil))
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
	config.VotingSource = failingVoteSource{Source: voteSource, meetID: unavailableVotesMeet.ID}
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
		case <-time.After(5 * time.Second):
			t.Error("app did not stop")
		}
	})

	client := &http.Client{Timeout: 8 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	unauthenticated := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/vote-meet/votes", "", "")
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		body := readVenueResponseBody(t, unauthenticated)
		t.Fatalf("unauthenticated voting snapshot status=%d body=%s, want HTTP 401", unauthenticated.StatusCode, body)
	}
	unauthenticated.Body.Close()

	const callers = 8
	var callersDone sync.WaitGroup
	responses := make(chan *http.Response, callers)
	errorsFound := make(chan error, callers)
	for range callers {
		callersDone.Add(1)
		go func() {
			defer callersDone.Done()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/meet/vote-meet/votes", nil)
			if err != nil {
				errorsFound <- err
				return
			}
			request.Header.Set("Authorization", "Bearer "+idToken)
			response, err := client.Do(request)
			if err != nil {
				errorsFound <- err
				return
			}
			responses <- response
		}()
	}
	callersDone.Wait()
	close(responses)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent voting snapshot GET: %v", err)
	}
	var snapshotBody []byte
	for response := range responses {
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("concurrent voting snapshot status=%d body=%s, want HTTP 200", response.StatusCode, body)
		}
		snapshotBody = body
	}
	if got := strings.Count(logs.String(), "voting projection admitted"); got != 1 {
		t.Fatalf("concurrent GET admitted %d voting projections, want one", got)
	}
	putResponse := doVenueRequest(t, client, http.MethodPut, baseURL+"/api/meet/vote-meet/votes/venue-1", "Bearer "+idToken, "")
	putBody := readVenueResponseBody(t, putResponse)
	if putResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT vote status=%d body=%s, want HTTP 204", putResponse.StatusCode, putBody)
	}
	putRevision := putResponse.Header.Get("X-Vote-Revision")
	if _, err := time.Parse(time.RFC3339Nano, putRevision); err != nil || putRevision == "" {
		t.Fatalf("PUT vote revision=%q, want UTC RFC3339Nano", putRevision)
	}
	putResponse.Body.Close()

	retryResponse := doVenueRequest(t, client, http.MethodPut, baseURL+"/api/meet/vote-meet/votes/venue-1", "Bearer "+idToken, "")
	retryBody := readVenueResponseBody(t, retryResponse)
	if retryResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("idempotent PUT vote status=%d body=%s, want HTTP 204", retryResponse.StatusCode, retryBody)
	}
	if retryResponse.Header.Get("X-Vote-Revision") == "" {
		t.Fatal("idempotent PUT vote omitted X-Vote-Revision")
	}
	retryResponse.Body.Close()

	deleteResponse := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/meet/vote-meet/votes/venue-1", "Bearer "+idToken, "")
	deleteBody := readVenueResponseBody(t, deleteResponse)
	if deleteResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE vote status=%d body=%s, want HTTP 204", deleteResponse.StatusCode, deleteBody)
	}
	if deleteResponse.Header.Get("X-Vote-Revision") == "" {
		t.Fatal("DELETE vote omitted X-Vote-Revision")
	}
	deleteResponse.Body.Close()

	withdrawResponse := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/meet/vote-meet/votes/removed-candidate", "Bearer "+idToken, "")
	withdrawBody := readVenueResponseBody(t, withdrawResponse)
	if withdrawResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("removed-candidate withdrawal status=%d body=%s, want HTTP 204", withdrawResponse.StatusCode, withdrawBody)
	}
	withdrawResponse.Body.Close()

	missingDelete := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/meet/missing-votes-meet/votes/venue-2", "Bearer "+idToken, "")
	missingDeleteBody := readVenueResponseBody(t, missingDelete)
	if missingDelete.StatusCode != http.StatusNoContent {
		t.Fatalf("missing-document DELETE status=%d body=%s, want HTTP 204", missingDelete.StatusCode, missingDeleteBody)
	}
	if missingDelete.Header.Get("X-Vote-Revision") != "" {
		t.Fatalf("missing-document DELETE revision=%q, want none", missingDelete.Header.Get("X-Vote-Revision"))
	}
	missingDelete.Body.Close()
	missingVotesSnapshot, err := firestoreClient.Collection("votes").Doc("missing-votes-meet").Get(ctx)
	if status.Code(err) != codes.NotFound {
		if err == nil && missingVotesSnapshot.Exists() {
			t.Fatal("missing-document DELETE created a votes document")
		}
		t.Fatalf("check missing-document removal: %v", err)
	}

	deletedMeetResponse := doVenueRequest(t, client, http.MethodPut, baseURL+"/api/meet/deleted-vote-meet/votes/venue-1", "Bearer "+idToken, "")
	deletedMeetBody := readVenueResponseBody(t, deletedMeetResponse)
	if deletedMeetResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted-Meet PUT status=%d body=%s, want HTTP 404", deletedMeetResponse.StatusCode, deletedMeetBody)
	}
	deletedMeetResponse.Body.Close()

	if got := strings.Count(logs.String(), "voting projection watch opened"); got != 1 {
		t.Fatalf("concurrent GET opened %d vote document watches, want one", got)
	}

	var snapshot struct {
		Options []struct {
			ID             string `json:"id"`
			Total          int    `json:"total"`
			AnonymousCount int    `json:"anonymous_count"`
			Voters         []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				PhotoURL string `json:"photo_url"`
			} `json:"voters"`
		} `json:"options"`
		SelectedOptionIDs []string `json:"selected_option_ids"`
	}
	if err := json.Unmarshal(snapshotBody, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Options) != 2 || snapshot.Options[0].ID != "any" || snapshot.Options[1].ID != "venue-1" {
		t.Fatalf("voting options=%+v, want any and the active candidate", snapshot.Options)
	}
	anyOption := snapshot.Options[0]
	if anyOption.Total != 1 || anyOption.AnonymousCount != 0 || len(anyOption.Voters) != 1 || anyOption.Voters[0].ID != callerUID || anyOption.Voters[0].Name != "Caller Display Name" {
		t.Fatalf("any option=%+v, want the caller visible by default for null preference", anyOption)
	}
	venueOption := snapshot.Options[1]
	if venueOption.Total != 4 || venueOption.AnonymousCount != 2 || len(venueOption.Voters) != 2 ||
		venueOption.Voters[0].ID != "default-voter" || venueOption.Voters[0].Name != "Default Display Name" ||
		venueOption.Voters[1].ID != "visible-voter" || venueOption.Voters[1].Name != "Visible Display Name" ||
		venueOption.Voters[1].PhotoURL != "https://example.test/custom.jpg" {
		t.Fatalf("venue option=%+v, want four distinct votes, two anonymous, and visible summaries", venueOption)
	}
	if len(snapshot.SelectedOptionIDs) != 2 || snapshot.SelectedOptionIDs[0] != "any" || snapshot.SelectedOptionIDs[1] != "removed-candidate" {
		t.Fatalf("caller selections=%v, want any and removed-candidate before withdrawal", snapshot.SelectedOptionIDs)
	}
	for _, privateValue := range []string{
		"hidden-voter", "unconfirmed-voter", "inactive-voter", "Hidden Display Name", "ignored-uid",
		"caller-secret@example.test", "visible-secret@example.test", "hidden-secret@example.test",
	} {
		if strings.Contains(string(snapshotBody), privateValue) {
			t.Fatalf("voting snapshot leaked %q: %s", privateValue, snapshotBody)
		}
	}

	privateResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/missing-votes-meet/votes", "Bearer "+idToken, "")
	if privateResponse.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, privateResponse)
		t.Fatalf("member voting snapshot status=%d body=%s, want HTTP 200", privateResponse.StatusCode, body)
	}
	privateBodyBytes, err := io.ReadAll(privateResponse.Body)
	privateResponse.Body.Close()
	if err != nil {
		t.Fatalf("read missing vote snapshot response: %v", err)
	}
	privateBody := string(privateBodyBytes)
	var emptySnapshot struct {
		Options []struct {
			Total          int `json:"total"`
			AnonymousCount int `json:"anonymous_count"`
		} `json:"options"`
		SelectedOptionIDs []string `json:"selected_option_ids"`
	}
	if err := json.Unmarshal([]byte(privateBody), &emptySnapshot); err != nil {
		t.Fatalf("decode missing vote snapshot %q: %v", privateBody, err)
	}
	if len(emptySnapshot.Options) != 2 || emptySnapshot.Options[0].Total != 0 || emptySnapshot.Options[1].Total != 0 || len(emptySnapshot.SelectedOptionIDs) != 0 {
		t.Fatalf("missing vote snapshot=%s, want empty candidate and any options", privateBody)
	}
	missingVoteSnapshot, err := firestoreClient.Collection("votes").Doc("missing-votes-meet").Get(ctx)
	if status.Code(err) == codes.NotFound {
		missingVoteSnapshot = nil
	} else if err != nil {
		t.Fatalf("check missing vote document after GET: %v", err)
	}
	if missingVoteSnapshot != nil && missingVoteSnapshot.Exists() {
		t.Fatal("GET created the previously missing votes document")
	}
	hiddenResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/hidden-meet/votes", "Bearer "+idToken, "")
	if hiddenResponse.StatusCode != http.StatusNotFound {
		body := readVenueResponseBody(t, hiddenResponse)
		t.Fatalf("non-member voting snapshot status=%d body=%s, want HTTP 404", hiddenResponse.StatusCode, body)
	}
	hiddenResponse.Body.Close()
	unavailableResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/unavailable-votes-meet/votes", "Bearer "+idToken, "")
	if unavailableResponse.StatusCode != http.StatusServiceUnavailable {
		body := readVenueResponseBody(t, unavailableResponse)
		t.Fatalf("unavailable voting snapshot status=%d body=%s, want HTTP 503", unavailableResponse.StatusCode, body)
	}
	var unavailableBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(unavailableResponse.Body).Decode(&unavailableBody); err != nil {
		unavailableResponse.Body.Close()
		t.Fatalf("decode unavailable voting snapshot response: %v", err)
	}
	unavailableResponse.Body.Close()
	if unavailableBody.Error.Code != "projection_unavailable" {
		t.Fatalf("unavailable voting snapshot error=%q, want projection_unavailable", unavailableBody.Error.Code)
	}

	t.Run("stream reconnect and membership revocation", func(t *testing.T) {
		testVotingStreamReconnectAndRevocation(t, ctx, client, baseURL, idToken, callerUID, privateGroup, firestoreClient)
	})

	if _, err := mainMeet.Update(ctx, []firestore.Update{{Path: "completed", Value: true}}); err != nil {
		t.Fatal(err)
	}
	completedDelete := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/meet/vote-meet/votes/any", "Bearer "+idToken, "")
	completedBody := readVenueResponseBody(t, completedDelete)
	if completedDelete.StatusCode != http.StatusNoContent || completedDelete.Header.Get("X-Vote-Revision") == "" {
		t.Fatalf("completed-Meet DELETE status=%d revision=%q body=%s, want HTTP 204 with revision", completedDelete.StatusCode, completedDelete.Header.Get("X-Vote-Revision"), completedBody)
	}
	completedDelete.Body.Close()
	completedVotes, err := votes.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := completedVotes.Data()["any"]; exists {
		t.Fatal("completed-Meet DELETE did not remove the caller's vote")
	}
}

func testVotingStreamReconnectAndRevocation(t *testing.T, ctx context.Context, client *http.Client, baseURL, idToken, callerUID string, group *firestore.DocumentRef, firestoreClient *firestore.Client) {
	t.Helper()
	streamURL := baseURL + "/api/meet/missing-votes-meet/votes/stream"
	codec, err := sse.NewCursorCodec([]byte("test-only-cursor-signing-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	openStream := func(cursor string) (*http.Response, *bufio.Reader) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+idToken)
		if cursor != "" {
			request.Header.Set("Last-Event-ID", cursor)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		return response, bufio.NewReader(response.Body)
	}
	readSnapshot := func(reader *bufio.Reader, venueHasVotes bool, expectedSelections ...string) (sse.CursorClaims, time.Time) {
		t.Helper()
		frame := readVenueSSEFrame(t, reader)
		if frame.event != "voting_snapshot" || frame.id == "" {
			t.Fatalf("voting stream frame=%+v, want snapshot with cursor", frame)
		}
		var snapshot struct {
			Cursor  string `json:"cursor"`
			Options []struct {
				ID             string `json:"id"`
				Total          int    `json:"total"`
				AnonymousCount int    `json:"anonymous_count"`
				Voters         []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"voters"`
			} `json:"options"`
			SelectedOptionIDs []string `json:"selected_option_ids"`
			VoteRevision      *string  `json:"vote_revision"`
		}
		if err := json.Unmarshal([]byte(frame.data), &snapshot); err != nil {
			t.Fatalf("decode voting stream snapshot: %v", err)
		}
		if snapshot.Cursor != frame.id || len(snapshot.Options) != 2 || snapshot.Options[0].ID != "any" || snapshot.Options[1].ID != "venue-2" {
			t.Fatalf("incomplete voting snapshot=%+v, event ID=%q", snapshot, frame.id)
		}
		if len(snapshot.SelectedOptionIDs) != len(expectedSelections) {
			t.Fatalf("selections=%v, want %v", snapshot.SelectedOptionIDs, expectedSelections)
		}
		for index, expected := range expectedSelections {
			if snapshot.SelectedOptionIDs[index] != expected {
				t.Fatalf("selections=%v, want %v", snapshot.SelectedOptionIDs, expectedSelections)
			}
		}
		anyOption := snapshot.Options[0]
		if len(expectedSelections) > 0 && expectedSelections[0] == "any" {
			if anyOption.Total != 1 || anyOption.AnonymousCount != 0 || len(anyOption.Voters) != 1 || anyOption.Voters[0].ID != callerUID {
				t.Fatalf("any option=%+v, want the caller's visible vote", anyOption)
			}
		} else if anyOption.Total != 0 || anyOption.AnonymousCount != 0 || len(anyOption.Voters) != 0 {
			t.Fatalf("any option=%+v, want empty", anyOption)
		}
		venue := snapshot.Options[1]
		if venueHasVotes && (venue.Total != 2 || venue.AnonymousCount != 1 || len(venue.Voters) != 1 || venue.Voters[0].ID != callerUID || venue.Voters[0].Name != "Caller Display Name") {
			t.Fatalf("venue voting privacy=%+v, want caller visible and one anonymous voter", venue)
		}
		if !venueHasVotes && (venue.Total != 0 || venue.AnonymousCount != 0 || len(venue.Voters) != 0) {
			t.Fatalf("initial venue votes=%+v, want empty", venue)
		}
		if strings.Contains(frame.data, "hidden-voter") || strings.Contains(frame.data, "Hidden Display Name") || strings.Contains(frame.data, "caller-secret@example.test") {
			t.Fatalf("voting stream leaked hidden voter: %s", frame.data)
		}
		claims, err := codec.Decode(frame.id)
		if err != nil || claims.Stream != "voting-v1" || claims.Resource != "missing-votes-meet" || claims.Generation != "voting-v1" {
			t.Fatalf("voting cursor claims=%+v err=%v", claims, err)
		}
		var revision time.Time
		if snapshot.VoteRevision != nil {
			revision, err = time.Parse(time.RFC3339Nano, *snapshot.VoteRevision)
			if err != nil {
				t.Fatalf("invalid voting revision %q: %v", *snapshot.VoteRevision, err)
			}
		}
		return claims, revision
	}
	votes := firestoreClient.Collection("votes").Doc("missing-votes-meet")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = votes.Delete(cleanupCtx)
	})

	firstResponse, firstReader := openStream("")
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("initial voting stream status=%d", firstResponse.StatusCode)
	}
	first, _ := readSnapshot(firstReader, false)
	observerResponse, observerReader := openStream("")
	if observerResponse.StatusCode != http.StatusOK {
		t.Fatalf("observer voting stream status=%d", observerResponse.StatusCode)
	}
	readSnapshot(observerReader, false)
	firstResponse.Body.Close()

	if _, err := votes.Set(ctx, map[string]any{"venue-2": []string{callerUID, "hidden-voter"}}); err != nil {
		t.Fatal(err)
	}
	readSnapshot(observerReader, true, "venue-2")
	mutation := doVenueRequest(t, client, http.MethodPut, baseURL+"/api/meet/missing-votes-meet/votes/any", "Bearer "+idToken, "")
	mutationBody := readVenueResponseBody(t, mutation)
	if mutation.StatusCode != http.StatusNoContent || mutation.Header.Get("X-Vote-Revision") == "" {
		t.Fatalf("disconnected vote mutation status=%d revision=%q body=%s", mutation.StatusCode, mutation.Header.Get("X-Vote-Revision"), mutationBody)
	}
	updated, updatedRevision := readSnapshot(observerReader, true, "any", "venue-2")
	committedRevision, err := time.Parse(time.RFC3339Nano, mutation.Header.Get("X-Vote-Revision"))
	if err != nil || updatedRevision.Before(committedRevision) {
		t.Fatalf("stream revision=%s, committed revision=%q err=%v", updatedRevision, mutation.Header.Get("X-Vote-Revision"), err)
	}
	if updated.Sequence <= first.Sequence {
		t.Fatalf("updated cursor sequence=%d, want greater than initial %d", updated.Sequence, first.Sequence)
	}
	initialCursor, err := codec.Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	reconnectedResponse, reconnectedReader := openStream(initialCursor)
	if reconnectedResponse.StatusCode != http.StatusOK {
		t.Fatalf("reconnected voting stream status=%d", reconnectedResponse.StatusCode)
	}
	resumed, resumedRevision := readSnapshot(reconnectedReader, true, "any", "venue-2")
	if resumed.Sequence < updated.Sequence || resumedRevision.Before(updatedRevision) {
		t.Fatalf("reconnected sequence=%d revision=%s, want at least observed sequence=%d revision=%s", resumed.Sequence, resumedRevision, updated.Sequence, updatedRevision)
	}

	if _, err := group.Update(ctx, []firestore.Update{{Path: "members", Value: map[string]any{callerUID: false}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meet/missing-votes-meet/votes", "Bearer "+idToken, "")
		response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("membership revocation not observed, last status=%d", response.StatusCode)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := votes.Update(ctx, []firestore.Update{{Path: "any", Value: []string{callerUID, "hidden-voter"}}}); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []*bufio.Reader{observerReader, reconnectedReader} {
		frame := readVenueSSEFrame(t, reader)
		if frame.event != "view_reset" || frame.data != "{}" || frame.id != "" {
			t.Fatalf("membership revocation frame=%+v, want cursorless view_reset", frame)
		}
		if _, err := reader.ReadByte(); err != io.EOF {
			t.Fatalf("stream after reset error=%v, want EOF", err)
		}
	}
	denied, _ := openStream(initialCursor)
	if denied.StatusCode != http.StatusNotFound {
		body := readVenueResponseBody(t, denied)
		t.Fatalf("reconnect after revocation status=%d body=%s, want HTTP 404", denied.StatusCode, body)
	}
}

type failingVoteSource struct {
	votecache.Source
	meetID string
}

func (source failingVoteSource) Watch(ctx context.Context, meetID string) (votecache.Stream, error) {
	if meetID == source.meetID {
		return nil, errors.New("simulated vote source outage")
	}
	return source.Source.Watch(ctx, meetID)
}

func assertLoopbackEmulator(t *testing.T, name, host string) {
	t.Helper()
	hostname, _, err := net.SplitHostPort(host)
	address := net.ParseIP(hostname)
	if err != nil || (hostname != "localhost" && (address == nil || !address.IsLoopback())) {
		t.Fatalf("%s emulator host %q is not loopback", name, host)
	}
}

func createVotingEmulatorAccount(t *testing.T, host, projectID string) (string, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post(
		"http://"+host+"/identitytoolkit.googleapis.com/v1/accounts:signUp?key="+url.QueryEscape(projectID),
		"application/json",
		strings.NewReader(`{"returnSecureToken":true}`),
	)
	if err != nil {
		t.Fatalf("create Auth emulator account: %v", err)
	}
	defer response.Body.Close()
	var account struct {
		IDToken string `json:"idToken"`
		LocalID string `json:"localId"`
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("Auth emulator sign-up status=%d body=%s", response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(&account); err != nil {
		t.Fatal(err)
	}
	if account.IDToken == "" || account.LocalID == "" {
		t.Fatal("Auth emulator did not return an ID token and UID")
	}
	t.Cleanup(func() {
		deleted, err := client.Post(
			"http://"+host+"/identitytoolkit.googleapis.com/v1/accounts:delete?key="+url.QueryEscape(projectID),
			"application/json",
			strings.NewReader(`{"idToken":"`+account.IDToken+`"}`),
		)
		if err == nil {
			deleted.Body.Close()
		}
	})
	return account.IDToken, account.LocalID
}
