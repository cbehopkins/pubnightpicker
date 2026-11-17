package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

func TestGroupsHTTPProjectionTracksFirestoreChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-last-orders-groups-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer firestoreClient.Close()
	groups := firestoreClient.Collection("groups")
	publicGroup := groups.Doc("opaque-public-id")
	privateGroup := groups.Doc("opaque-private-id")
	if _, err := publicGroup.Set(ctx, map[string]any{"name": "Original", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	if _, err := privateGroup.Set(ctx, map[string]any{"name": "Private", "visibility": "private"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = publicGroup.Delete(cleanupCtx)
		_, _ = privateGroup.Delete(cleanupCtx)
	}()

	config := testConfig(t, t.TempDir()+"/groups-emulator.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	config.EnableFirestore = true
	config.FirestoreProjectID = projectID
	config.GroupSource = nil
	config.HTTPAddr = "127.0.0.1:0"
	config.AllowAuthEmulator = true
	config.AuthProjectID = projectID
	application, err := app.New(config)
	if err != nil {
		t.Fatalf("new app with Firestore Groups source: %v", err)
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

	client := &http.Client{Timeout: 2 * time.Second}
	baseURL := "http://" + application.HTTPAddr()
	waitForVenueHTTP(t, ctx, client, baseURL)
	initial := waitForGroups(t, ctx, client, baseURL, []string{"opaque-public-id"})
	if initial[0].Name != "Original" || initial[0].Visibility != "public" {
		t.Fatalf("initial Groups = %+v", initial)
	}

	if _, err := publicGroup.Update(ctx, []firestore.Update{{Path: "name", Value: "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	waitForGroupQuery(t, ctx, client, baseURL, "Original", nil)
	waitForGroupQuery(t, ctx, client, baseURL, "Renamed", []string{"opaque-public-id"})

	addedGroup := groups.NewDoc()
	if _, err := addedGroup.Set(ctx, map[string]any{"name": "Added", "visibility": "public"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_, _ = addedGroup.Delete(cleanupCtx)
	}()
	waitForGroups(t, ctx, client, baseURL, []string{addedGroup.ID, "opaque-public-id"})

	if _, err := publicGroup.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	waitForGroups(t, ctx, client, baseURL, []string{addedGroup.ID})
	if _, err := addedGroup.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := privateGroup.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	waitForGroups(t, ctx, client, baseURL, []string{})

	health, err := client.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health after removing last Group status=%d, want HTTP 200", health.StatusCode)
	}
}

type groupHTTPResult struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

func waitForGroups(t *testing.T, ctx context.Context, client *http.Client, baseURL string, wantIDs []string) []groupHTTPResult {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		got := getGroups(t, client, baseURL, "")
		ids := make([]string, len(got))
		for index, group := range got {
			ids[index] = group.ID
		}
		if slices.Equal(ids, wantIDs) {
			return got
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for Groups %v: %v", wantIDs, ctx.Err())
		case <-deadline.C:
			t.Fatalf("Groups IDs = %v, want %v", ids, wantIDs)
		case <-ticker.C:
		}
	}
}

func waitForGroupQuery(t *testing.T, ctx context.Context, client *http.Client, baseURL, name string, wantIDs []string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		got := getGroups(t, client, baseURL, name)
		ids := make([]string, len(got))
		for index, group := range got {
			ids[index] = group.ID
		}
		if slices.Equal(ids, wantIDs) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for Group name %q: %v", name, ctx.Err())
		case <-deadline.C:
			t.Fatalf("Group query %q returned IDs %v, want %v", name, ids, wantIDs)
		case <-ticker.C:
		}
	}
}

func getGroups(t *testing.T, client *http.Client, baseURL, name string) []groupHTTPResult {
	t.Helper()
	url := baseURL + "/api/groups"
	if name != "" {
		url += "?name=" + name
	}
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d", url, response.StatusCode)
	}
	var result struct {
		Groups []groupHTTPResult `json:"groups"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Groups == nil {
		t.Fatalf("GET %s returned null Groups", url)
	}
	return result.Groups
}
