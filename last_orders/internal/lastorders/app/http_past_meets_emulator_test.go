package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

func TestPastMeetsGetAgainstFirestoreEmulatorDoesNotAdmitIndividualMeets(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	if strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST")) == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := "demo-last-orders-past-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	firestoreClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firestoreClient.Close() })

	groups := map[string]map[string]any{
		"public-group":  {"name": "Public Group", "visibility": "public"},
		"private-group": {"name": "Private Group", "visibility": "private", "members": map[string]any{"verified-user": true}},
		"hidden-group":  {"name": "Hidden Group", "visibility": "private", "members": map[string]any{"someone-else": true}},
	}
	for groupID, data := range groups {
		if _, err := firestoreClient.Collection("groups").Doc(groupID).Set(ctx, data); err != nil {
			t.Fatal(err)
		}
	}
	venue := firestoreClient.Collection("pubs").Doc("soft-deleted-venue")
	if _, err := venue.Set(ctx, map[string]any{
		"name": "Old Pub", "address": "Old Street", "web_site": "https://pub.example",
		"pubImage": "https://pub.example/photo.jpg", "venueType": "pub", "deleted_on": "2026-11-18T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	meets := map[string]map[string]any{
		"meet-a":              {"date": "2026-11-19", "completed": true, "groupId": "public-group", "selected": "soft-deleted-venue"},
		"meet-b":              {"date": "2026-11-19", "completed": true, "groupId": "public-group", "selected": "hard-missing-venue"},
		"meet-old":            {"date": "2026-11-18", "completed": true, "groupId": "public-group"},
		"meet-today":          {"date": "2026-11-20", "completed": true, "groupId": "public-group"},
		"meet-upcoming":       {"date": "2026-11-21", "completed": true, "groupId": "public-group"},
		"meet-incomplete":     {"date": "2026-11-19", "completed": false, "groupId": "public-group"},
		"meet-other-group":    {"date": "2026-11-19", "completed": true, "groupId": "hidden-group"},
		"private-meet":        {"date": "2026-11-19", "completed": true, "groupId": "private-group"},
		"hidden-private-meet": {"date": "2026-11-19", "completed": true, "groupId": "hidden-group"},
	}
	for meetID, data := range meets {
		if _, err := firestoreClient.Collection("polls").Doc(meetID).Set(ctx, data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := firestoreClient.Collection("votes").Doc("meet-a").Set(ctx, map[string]any{"soft-deleted-venue": []string{"verified-user"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := firestoreClient.Collection("attendance").Doc("meet-a").Set(ctx, map[string]any{"soft-deleted-venue": map[string]any{"canCome": []string{"verified-user"}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		for groupID := range groups {
			_, _ = firestoreClient.Collection("groups").Doc(groupID).Delete(cleanupCtx)
		}
		for meetID := range meets {
			_, _ = firestoreClient.Collection("polls").Doc(meetID).Delete(cleanupCtx)
		}
		_, _ = firestoreClient.Collection("votes").Doc("meet-a").Delete(cleanupCtx)
		_, _ = firestoreClient.Collection("attendance").Doc("meet-a").Delete(cleanupCtx)
		_, _ = venue.Delete(cleanupCtx)
	})

	t.Setenv("LAST_ORDERS_MEET_PROJECTION_DIAGNOSTICS", "true")
	logs := &syncBuffer{}
	config := testConfig(t, t.TempDir()+"/past-meets.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
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
	config.Clock = &meetTestClock{current: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	application, err := app.New(config)
	if err != nil {
		t.Fatalf("new HandPump with Firestore emulator: %v", err)
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
	getPage := func(groupID, before string) struct {
		Meets []struct {
			ID    string          `json:"id"`
			Date  string          `json:"date"`
			Venue json.RawMessage `json:"venue"`
		} `json:"meets"`
		NextCursor *string `json:"next_cursor"`
		Body       string
		Status     int
	} {
		t.Helper()
		query := url.Values{"limit": {"1"}}
		if before != "" {
			query.Set("before", before)
		}
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/"+groupID+"/past?"+query.Encode(), "Bearer valid-token", "")
		defer response.Body.Close()
		result := struct {
			Meets []struct {
				ID    string          `json:"id"`
				Date  string          `json:"date"`
				Venue json.RawMessage `json:"venue"`
			} `json:"meets"`
			NextCursor *string `json:"next_cursor"`
			Body       string
			Status     int
		}{Status: response.StatusCode}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		result.Body = string(body)
		if response.StatusCode == http.StatusOK {
			var page struct {
				Meets []struct {
					ID    string          `json:"id"`
					Date  string          `json:"date"`
					Venue json.RawMessage `json:"venue"`
				} `json:"meets"`
				NextCursor *string `json:"next_cursor"`
			}
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatal(err)
			}
			result.Meets, result.NextCursor = page.Meets, page.NextCursor
		}
		return result
	}
	first := getPage("public-group", "")
	if first.Status != http.StatusOK || len(first.Meets) != 1 || first.Meets[0].ID != "meet-a" || first.NextCursor == nil {
		t.Fatalf("first past-Meets page status=%d meets=%+v cursor=%v", first.Status, first.Meets, first.NextCursor)
	}
	var deletedVenue struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Name    string `json:"name"`
		Address string `json:"address"`
		Website string `json:"website_url"`
		Image   string `json:"image_url"`
	}
	if err := json.Unmarshal(first.Meets[0].Venue, &deletedVenue); err != nil {
		t.Fatal(err)
	}
	if deletedVenue.ID != "soft-deleted-venue" || deletedVenue.Status != "deleted" || deletedVenue.Name != "Old Pub" || deletedVenue.Address != "Old Street" || deletedVenue.Website != "https://pub.example" || deletedVenue.Image != "https://pub.example/photo.jpg" {
		t.Fatalf("soft-deleted Venue summary=%+v", deletedVenue)
	}
	repeatedFirst := getPage("public-group", "")
	if repeatedFirst.NextCursor == nil || *repeatedFirst.NextCursor != *first.NextCursor {
		t.Fatalf("same page position produced unstable cursors: first=%v repeated=%v", first.NextCursor, repeatedFirst.NextCursor)
	}
	second := getPage("public-group", *first.NextCursor)
	if second.Status != http.StatusOK || len(second.Meets) != 1 || second.Meets[0].ID != "meet-b" || second.NextCursor == nil {
		t.Fatalf("second past-Meets page status=%d meets=%+v cursor=%v", second.Status, second.Meets, second.NextCursor)
	}
	var missingVenue struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(second.Meets[0].Venue, &missingVenue); err != nil {
		t.Fatal(err)
	}
	if missingVenue.ID != "hard-missing-venue" || missingVenue.Status != "missing" {
		t.Fatalf("confirmed-missing Venue reference=%+v", missingVenue)
	}
	third := getPage("public-group", *second.NextCursor)
	if third.Status != http.StatusOK || len(third.Meets) != 1 || third.Meets[0].ID != "meet-old" || third.NextCursor != nil {
		t.Fatalf("third past-Meets page status=%d meets=%+v cursor=%v", third.Status, third.Meets, third.NextCursor)
	}
	for _, page := range []struct {
		ID    string
		Venue json.RawMessage
	}{
		{ID: first.Meets[0].ID, Venue: first.Meets[0].Venue},
		{ID: second.Meets[0].ID, Venue: second.Meets[0].Venue},
		{ID: third.Meets[0].ID, Venue: third.Meets[0].Venue},
	} {
		if strings.Contains(first.Body, "votes") || strings.Contains(first.Body, "attendance") || strings.Contains(second.Body, "votes") || strings.Contains(second.Body, "attendance") || strings.Contains(third.Body, "votes") || strings.Contains(third.Body, "attendance") {
			t.Fatalf("Meet response contains private voting or attendance data at %s", page.ID)
		}
	}
	for _, groupID := range []string{"private-group", "hidden-group"} {
		response := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/"+groupID+"/past", "", "")
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated private Group %q status=%d, want HTTP 401", groupID, response.StatusCode)
		}
	}
	privateResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/private-group/past", "Bearer valid-token", "")
	privateResponse.Body.Close()
	if privateResponse.StatusCode != http.StatusOK {
		t.Fatalf("authorised private Group status=%d, want HTTP 200", privateResponse.StatusCode)
	}
	hiddenResponse := doVenueRequest(t, client, http.MethodGet, baseURL+"/api/meets/hidden-group/past", "Bearer valid-token", "")
	hiddenResponse.Body.Close()
	if hiddenResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("unauthorised private Group status=%d, want HTTP 404", hiddenResponse.StatusCode)
	}
	if strings.Contains(logs.String(), "Meet projection admitted") || strings.Contains(logs.String(), "Meet projection watch opened") {
		t.Fatalf("past-Meets reads admitted or watched individual Meets: %s", logs.String())
	}
}
