package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

func TestVenueCollectionHTTPAgainstFirestoreEmulator(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if emulatorHost := strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")); emulatorHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "last-orders-venues-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })
	venueCollection := firestoreClient.Collection("pubs")
	roleCollection := firestoreClient.Collection("roles")
	for id, data := range map[string]map[string]any{
		"seed-venue": {"name": "Seed Pub", "venueType": "pub", "address": "Seed Street", "location_id": "north", "food": true, "visible_group_ids": []string{"group-a"}, "previous_venue_ids": []string{"old-pub"}},
	} {
		if _, err := venueCollection.Doc(id).Set(ctx, data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := roleCollection.Doc("canManagePubs").Set(ctx, map[string]any{"verified-user": true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = venueCollection.Doc("seed-venue").Delete(cleanupCtx)
		_, _ = roleCollection.Doc("canManagePubs").Delete(cleanupCtx)
	})

	config := testConfig(t, t.TempDir()+"/venues-emulator.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.VenueSource = nil // Use the Firestore-backed source selected by app.New.
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

	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	listResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/venues", "", "")
	if listResponse.StatusCode != http.StatusOK {
		listResponse.Body.Close()
		t.Fatalf("list seeded venue status=%d", listResponse.StatusCode)
	}
	var list struct {
		Venues []struct {
			ID               string            `json:"id"`
			VisibleGroupIDs  []string          `json:"visible_group_ids"`
			PreviousVenueIDs []string          `json:"previous_venue_ids"`
			EditTokens       map[string]string `json:"edit_tokens"`
		} `json:"venues"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&list); err != nil {
		listResponse.Body.Close()
		t.Fatal(err)
	}
	listResponse.Body.Close()
	if len(list.Venues) != 1 || list.Venues[0].ID != "seed-venue" || len(list.Venues[0].VisibleGroupIDs) != 1 || list.Venues[0].VisibleGroupIDs[0] != "group-a" || len(list.Venues[0].PreviousVenueIDs) != 1 || list.Venues[0].PreviousVenueIDs[0] != "old-pub" {
		t.Fatalf("emulator-backed venue list=%+v", list.Venues)
	}
	if list.Venues[0].EditTokens["name"] == "" {
		t.Fatalf("emulator-backed venue list omitted field tokens: %+v", list.Venues[0])
	}

	streamCtx, closeStream := context.WithCancel(ctx)
	defer closeStream()
	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodGet, baseURL+"/api/venues/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResponse, err := client.Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResponse.Body.Close()
	if streamResponse.StatusCode != http.StatusOK {
		t.Fatalf("venue stream status=%d", streamResponse.StatusCode)
	}
	streamReader := bufio.NewReader(streamResponse.Body)
	initial := readVenueSSEFrame(t, streamReader)
	if initial.event != "venues" || !strings.Contains(initial.data, "seed-venue") {
		t.Fatalf("initial emulator stream frame=%+v", initial)
	}
	var initialSnapshot struct {
		Venues []struct {
			ID         string            `json:"id"`
			EditTokens map[string]string `json:"edit_tokens"`
		} `json:"venues"`
	}
	if err := json.Unmarshal([]byte(initial.data), &initialSnapshot); err != nil {
		t.Fatal(err)
	}
	if len(initialSnapshot.Venues) != 1 || initialSnapshot.Venues[0].ID != "seed-venue" || initialSnapshot.Venues[0].EditTokens["name"] == "" {
		t.Fatalf("initial venue stream omitted field tokens: %+v", initialSnapshot.Venues)
	}

	created := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/venues", "Bearer valid-token", `{"name":"New Pub","venue_type":"pub","address":"Main Road","location_id":"north"}`)
	if created.StatusCode != http.StatusCreated {
		body := readVenueResponseBody(t, created)
		t.Fatalf("create venue status=%d body=%s", created.StatusCode, body)
	}
	type venueRepresentation struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Address    string            `json:"address"`
		Notes      string            `json:"notes"`
		EditTokens map[string]string `json:"edit_tokens"`
	}
	var createdVenue venueRepresentation
	if err := json.NewDecoder(created.Body).Decode(&createdVenue); err != nil {
		created.Body.Close()
		t.Fatal(err)
	}
	created.Body.Close()
	if createdVenue.ID == "" {
		t.Fatal("create response did not include a Firestore venue ID")
	}
	createdFrame := readVenueSSEFrame(t, streamReader)
	if !strings.Contains(createdFrame.data, createdVenue.ID) || !strings.Contains(createdFrame.data, "New Pub") {
		t.Fatalf("stream did not include Firestore-created venue: %+v", createdFrame)
	}

	patchVenue := func(fields map[string]any, tokens map[string]string) *http.Response {
		t.Helper()
		fields["edit_tokens"] = tokens
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return doVenueRequest(t, client, http.MethodPatch, baseURL+"/api/venues/"+createdVenue.ID, "Bearer valid-token", string(body))
	}
	decodeVenue := func(response *http.Response) venueRepresentation {
		t.Helper()
		defer response.Body.Close()
		var venue venueRepresentation
		if err := json.NewDecoder(response.Body).Decode(&venue); err != nil {
			t.Fatal(err)
		}
		return venue
	}

	addressToken := createdVenue.EditTokens["address"]
	notesToken := createdVenue.EditTokens["notes"]
	if addressToken == "" || notesToken == "" {
		t.Fatalf("create response omitted editable-field tokens: %+v", createdVenue.EditTokens)
	}
	addressResponse := patchVenue(map[string]any{"address": "Concurrent Road"}, map[string]string{"address": addressToken})
	if addressResponse.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, addressResponse)
		t.Fatalf("first independent patch status=%d body=%s", addressResponse.StatusCode, body)
	}
	addressFrame := readVenueSSEFrame(t, streamReader)
	if !strings.Contains(addressFrame.data, "Concurrent Road") {
		t.Fatalf("venue stream omitted first independent edit: %+v", addressFrame)
	}

	notesResponse := patchVenue(map[string]any{"notes": "Kept independent note"}, map[string]string{"notes": notesToken})
	if notesResponse.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, notesResponse)
		t.Fatalf("second independent patch status=%d body=%s", notesResponse.StatusCode, body)
	}
	mergedVenue := decodeVenue(notesResponse)
	if mergedVenue.Address != "Concurrent Road" || mergedVenue.Notes != "Kept independent note" {
		t.Fatalf("disjoint edits were not both retained: %+v", mergedVenue)
	}
	mergedFrame := readVenueSSEFrame(t, streamReader)
	if !strings.Contains(mergedFrame.data, "Kept independent note") || !strings.Contains(mergedFrame.data, "Concurrent Road") {
		t.Fatalf("venue stream omitted merged venue: %+v", mergedFrame)
	}

	staleNameToken := mergedVenue.EditTokens["name"]
	winnerResponse := patchVenue(map[string]any{"name": "Manager One Name"}, map[string]string{"name": staleNameToken})
	if winnerResponse.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, winnerResponse)
		t.Fatalf("first same-field patch status=%d body=%s", winnerResponse.StatusCode, body)
	}
	winnerVenue := decodeVenue(winnerResponse)
	if winnerVenue.Name != "Manager One Name" {
		t.Fatalf("first same-field patch returned venue=%+v", winnerVenue)
	}
	winnerFrame := readVenueSSEFrame(t, streamReader)
	if !strings.Contains(winnerFrame.data, "Manager One Name") {
		t.Fatalf("venue stream omitted first same-field edit: %+v", winnerFrame)
	}

	conflictResponse := patchVenue(
		map[string]any{"name": "Manager Two Name", "notes": "Must not be partially applied"},
		map[string]string{"name": staleNameToken, "notes": mergedVenue.EditTokens["notes"]},
	)
	if conflictResponse.StatusCode != http.StatusConflict {
		body := readVenueResponseBody(t, conflictResponse)
		t.Fatalf("divergent same-field patch status=%d body=%s, want 409", conflictResponse.StatusCode, body)
	}
	var conflict struct {
		Error struct {
			Code   string   `json:"code"`
			Fields []string `json:"fields"`
		} `json:"error"`
		CurrentVenue venueRepresentation `json:"current_venue"`
	}
	if err := json.NewDecoder(conflictResponse.Body).Decode(&conflict); err != nil {
		conflictResponse.Body.Close()
		t.Fatal(err)
	}
	conflictResponse.Body.Close()
	if conflict.Error.Code != "field_conflict" || len(conflict.Error.Fields) != 1 || conflict.Error.Fields[0] != "name" {
		t.Fatalf("conflict response=%+v, want field_conflict for name", conflict)
	}
	if conflict.CurrentVenue.Name != "Manager One Name" || conflict.CurrentVenue.Notes != "Kept independent note" {
		t.Fatalf("conflict did not include authoritative current venue: %+v", conflict.CurrentVenue)
	}
	stored, err := venueCollection.Doc(createdVenue.ID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Data()["name"] != "Manager One Name" || stored.Data()["notes"] != "Kept independent note" {
		t.Fatalf("conflicting multi-field patch partially changed Firestore: %v", stored.Data())
	}

	convergedResponse := patchVenue(map[string]any{"name": "Manager One Name"}, map[string]string{"name": staleNameToken})
	if convergedResponse.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, convergedResponse)
		t.Fatalf("already-converged patch status=%d body=%s", convergedResponse.StatusCode, body)
	}
	convergedVenue := decodeVenue(convergedResponse)
	if convergedVenue.Name != "Manager One Name" || convergedVenue.Address != "Concurrent Road" || convergedVenue.Notes != "Kept independent note" {
		t.Fatalf("already-converged patch did not return full current venue: %+v", convergedVenue)
	}

	deleted := doVenueRequest(t, client, http.MethodDelete, baseURL+"/api/venues/"+createdVenue.ID, "Bearer valid-token", "")
	if deleted.StatusCode != http.StatusNoContent {
		body := readVenueResponseBody(t, deleted)
		t.Fatalf("delete venue status=%d body=%s", deleted.StatusCode, body)
	}
	deleted.Body.Close()
	deletedFrame := readVenueSSEFrame(t, streamReader)
	if strings.Contains(deletedFrame.data, createdVenue.ID) {
		t.Fatalf("stream still contains soft-deleted Firestore venue: %+v", deletedFrame)
	}
	stored, err = venueCollection.Doc(createdVenue.ID).Get(ctx)
	if err != nil {
		t.Fatalf("soft-deleted Firestore document disappeared: %v", err)
	}
	if _, ok := stored.Data()["deleted_on"]; !ok {
		t.Fatalf("Firestore document has no deleted_on tombstone: %v", stored.Data())
	}

	listAfterDelete := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/venues", "", "")
	defer listAfterDelete.Body.Close()
	var remaining struct {
		Venues []struct {
			ID string `json:"id"`
		} `json:"venues"`
	}
	if err := json.NewDecoder(listAfterDelete.Body).Decode(&remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining.Venues) != 1 || remaining.Venues[0].ID != "seed-venue" {
		t.Fatalf("venues after soft delete=%+v", remaining.Venues)
	}

	readReconnectSnapshot := func(cursor string) venueSSEFrame {
		reconnectCtx, cancelReconnect := context.WithCancel(ctx)
		request, err := http.NewRequestWithContext(reconnectCtx, http.MethodGet, baseURL+"/api/venues/stream", nil)
		if err != nil {
			cancelReconnect()
			t.Fatal(err)
		}
		request.Header.Set("Last-Event-ID", cursor)
		response, err := client.Do(request)
		if err != nil {
			cancelReconnect()
			t.Fatal(err)
		}
		defer response.Body.Close()
		defer cancelReconnect()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("reconnect stream status=%d", response.StatusCode)
		}
		if contentType := response.Header.Get("Content-Type"); contentType != "text/event-stream; charset=utf-8" {
			t.Fatalf("reconnect stream Content-Type=%q", contentType)
		}
		return readVenueSSEFrame(t, bufio.NewReader(response.Body))
	}
	assertFullReconnectSnapshot := func(frame venueSSEFrame) {
		t.Helper()
		if frame.event != "venues" || frame.id == "" {
			t.Fatalf("reconnect stream frame=%+v, want venues event with cursor ID", frame)
		}
		var snapshot struct {
			Cursor string `json:"cursor"`
			Venues []struct {
				ID string `json:"id"`
			} `json:"venues"`
		}
		if err := json.Unmarshal([]byte(frame.data), &snapshot); err != nil {
			t.Fatalf("decode reconnect snapshot %q: %v", frame.data, err)
		}
		if snapshot.Cursor != frame.id || len(snapshot.Venues) != 1 || snapshot.Venues[0].ID != "seed-venue" {
			t.Fatalf("reconnect snapshot=%+v event ID=%q, want complete current snapshot and matching cursor", snapshot, frame.id)
		}
	}

	invalidCursor := "not-a-valid-venue-cursor"
	invalidCursorFrame := readReconnectSnapshot(invalidCursor)
	if invalidCursorFrame.id == invalidCursor {
		t.Fatal("invalid cursor was not replaced with a fresh Venue stream cursor")
	}
	assertFullReconnectSnapshot(invalidCursorFrame)
	validCursorFrame := readReconnectSnapshot(invalidCursorFrame.id)
	assertFullReconnectSnapshot(validCursorFrame)
}

type venueSSEFrame struct {
	id    string
	event string
	data  string
}

func readVenueSSEFrame(t *testing.T, reader *bufio.Reader) venueSSEFrame {
	t.Helper()
	var frame venueSSEFrame
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read venue SSE frame: %v", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return frame
		}
		key, value, _ := strings.Cut(line, ": ")
		switch key {
		case "id":
			frame.id = value
		case "event":
			frame.event = value
		case "data":
			frame.data = value
		}
	}
}

func doVenueRequest(t *testing.T, client *http.Client, method, url, authorization, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readVenueResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var body strings.Builder
	if _, err := fmt.Fscan(response.Body, &body); err != nil {
		return "<could not read response body>"
	}
	return body.String()
}

func waitForVenueHTTP(t *testing.T, ctx context.Context, client *http.Client, baseURL string) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		requestCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+"/healthz", nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				response.Body.Close()
				cancel()
				if response.StatusCode == http.StatusOK {
					return
				}
			} else {
				cancel()
			}
		} else {
			cancel()
		}
		select {
		case err := <-appRunError(ctx):
			if err != nil {
				t.Fatalf("app stopped before HTTP became ready: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("app HTTP did not become ready: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func appRunError(context.Context) <-chan error {
	return nil
}
