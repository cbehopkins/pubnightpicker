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
	venueCollection := firestoreClient.Collection("venues")
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
			ID               string   `json:"id"`
			VisibleGroupIDs  []string `json:"visible_group_ids"`
			PreviousVenueIDs []string `json:"previous_venue_ids"`
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

	created := doVenueRequest(t, client, http.MethodPost, baseURL+"/api/venues", "Bearer valid-token", `{"name":"New Pub","venue_type":"pub","address":"Main Road","location_id":"north"}`)
	if created.StatusCode != http.StatusCreated {
		body := readVenueResponseBody(t, created)
		t.Fatalf("create venue status=%d body=%s", created.StatusCode, body)
	}
	var createdVenue struct {
		ID string `json:"id"`
	}
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

	patched := doVenueRequest(t, client, http.MethodPatch, baseURL+"/api/venues/"+createdVenue.ID, "Bearer valid-token", `{"name":"Renamed Pub"}`)
	if patched.StatusCode != http.StatusOK {
		body := readVenueResponseBody(t, patched)
		t.Fatalf("patch venue status=%d body=%s", patched.StatusCode, body)
	}
	patched.Body.Close()
	patchedFrame := readVenueSSEFrame(t, streamReader)
	if !strings.Contains(patchedFrame.data, "Renamed Pub") || strings.Contains(patchedFrame.data, "New Pub") {
		t.Fatalf("stream did not reflect Firestore venue edit: %+v", patchedFrame)
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
	stored, err := venueCollection.Doc(createdVenue.ID).Get(ctx)
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
}

type venueSSEFrame struct {
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
