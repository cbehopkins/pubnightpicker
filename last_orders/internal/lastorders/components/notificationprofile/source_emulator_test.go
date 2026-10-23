package notificationprofile

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

func emulatorClient(t *testing.T) (*firestore.Client, context.Context) {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	projectID := "profile-" + uuid.NewString()[:16]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, ctx
}

func TestFirestoreSourceProjectsEndpointsAndDeactivates(t *testing.T) {
	client, ctx := emulatorClient(t)
	userID := fmt.Sprintf("projection_test_%d", time.Now().UnixNano())
	endpointID := "endpoint-1"

	userRef := client.Collection(usersCollection).Doc(userID)
	if _, err := userRef.Set(ctx, map[string]any{
		"webPushEnabled":  true,
		"pushPreferences": map[string]any{"pollOpens": false, "globalChat": true},
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	endpointRef := userRef.Collection(endpointsCollection).Doc(endpointID)
	if _, err := endpointRef.Set(ctx, map[string]any{
		"endpoint": "https://push.test/abc",
		"p256dh":   "key",
		"auth":     "secret",
		"active":   true,
	}); err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = endpointRef.Delete(cleanupCtx)
		_, _ = userRef.Delete(cleanupCtx)
	})

	source, err := NewFirestoreSource(client)
	if err != nil {
		t.Fatalf("new firestore source: %v", err)
	}

	// The collection group watch must attribute each endpoint to its owning user.
	endpoint := awaitEndpoint(t, ctx, source, userID, endpointID)
	if endpoint.URL != "https://push.test/abc" || endpoint.P256DH != "key" || endpoint.Auth != "secret" {
		t.Fatalf("endpoint = %+v; want the seeded subscription", endpoint)
	}
	if !endpoint.Active {
		t.Fatal("seeded endpoint should project as active")
	}

	preferences := awaitUser(t, ctx, source, userID)
	if !preferences.WebPushEnabled || preferences.PollOpens || !preferences.GlobalChat {
		t.Fatalf("preferences = %+v; want the seeded Firebase values", preferences)
	}
	if !preferences.PollCompletes {
		t.Error("unset pollCompletes should fall back to its default")
	}

	if err := source.DeactivateEndpoint(ctx, userID, endpointID); err != nil {
		t.Fatalf("deactivate endpoint: %v", err)
	}
	snapshot, err := endpointRef.Get(ctx)
	if err != nil {
		t.Fatalf("read endpoint: %v", err)
	}
	if active, _ := snapshot.Data()["active"].(bool); active {
		t.Fatal("endpoint should be inactive in Firebase after invalidation")
	}
	if _, ok := snapshot.Data()["disabledAt"]; !ok {
		t.Error("disabledAt should be stamped on invalidation")
	}
}

func awaitEndpoint(t *testing.T, ctx context.Context, source *FirestoreSource, userID, endpointID string) Endpoint {
	t.Helper()
	stream, err := source.WatchEndpoints(ctx)
	if err != nil {
		t.Fatalf("watch endpoints: %v", err)
	}
	defer stream.Stop()
	for {
		changes, err := stream.Next()
		if err != nil {
			t.Fatalf("endpoint stream: %v", err)
		}
		for _, change := range changes {
			if change.Doc.UserID != userID || change.Doc.ID != endpointID {
				continue
			}
			endpoint, err := EndpointFromDocument(change.Doc)
			if err != nil {
				t.Fatalf("project endpoint: %v", err)
			}
			return endpoint
		}
	}
}

func awaitUser(t *testing.T, ctx context.Context, source *FirestoreSource, userID string) UserPreferences {
	t.Helper()
	stream, err := source.WatchUsers(ctx)
	if err != nil {
		t.Fatalf("watch users: %v", err)
	}
	defer stream.Stop()
	for {
		changes, err := stream.Next()
		if err != nil {
			t.Fatalf("user stream: %v", err)
		}
		for _, change := range changes {
			if change.Doc.ID != userID {
				continue
			}
			preferences, err := PreferencesFromDocument(change.Doc)
			if err != nil {
				t.Fatalf("project preferences: %v", err)
			}
			return preferences
		}
	}
}

func TestFirestoreSourceThreeStreamBaselinesAndRawDeltas(t *testing.T) {
	client, ctx := emulatorClient(t)
	source, err := NewFirestoreSource(client)
	if err != nil {
		t.Fatal(err)
	}
	users := client.Collection(usersCollection)
	publicUsers := client.Collection("user-public")
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, userID := range []string{"first", "second"} {
			for _, ref := range []*firestore.DocumentRef{
				users.Doc(userID).Collection(endpointsCollection).Doc("device"),
				users.Doc(userID), publicUsers.Doc(userID),
			} {
				if _, err := ref.Delete(cleanup); err != nil {
					t.Errorf("cleanup %s: %v", ref.Path, err)
				}
			}
		}
	})
	for _, userID := range []string{"first", "second"} {
		if _, err := users.Doc(userID).Set(ctx, map[string]any{"uid": "ignored-" + userID, "webPushEnabled": true}); err != nil {
			t.Fatal(err)
		}
		if _, err := publicUsers.Doc(userID).Set(ctx, map[string]any{"uid": "ignored-" + userID, "name": nil, "votesVisible": false}); err != nil {
			t.Fatal(err)
		}
		if _, err := users.Doc(userID).Collection(endpointsCollection).Doc("device").Set(ctx, map[string]any{
			"endpoint": "https://push.test/" + userID, "p256dh": "key", "auth": "auth",
		}); err != nil {
			t.Fatal(err)
		}
	}
	private, err := source.WatchUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer private.Stop()
	public, err := source.WatchPublicUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer public.Stop()
	endpoints, err := source.WatchEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer endpoints.Stop()
	for index, stream := range []ChangeStream{private, public, endpoints} {
		changes, err := stream.Next()
		if err != nil || len(changes) != 2 {
			t.Fatalf("source %d initial baseline = %d changes, %v; want 2", index, len(changes), err)
		}
		seen := make(map[string]bool)
		for _, change := range changes {
			if change.Kind != ChangeAdded {
				t.Fatalf("source %d baseline kind = %d; want added", index, change.Kind)
			}
			userID := change.Doc.ID
			switch index {
			case 0:
				user, err := PrivateUserFromDocument(change.Doc)
				if err != nil || user.UserID != userID || user.PollOpens != nil || user.WebPushEnabled == nil || !*user.WebPushEnabled {
					t.Fatalf("raw private conversion = %+v, %v", user, err)
				}
			case 1:
				user, err := PublicUserFromDocument(change.Doc)
				if err != nil || user.UserID != userID || !user.Name.Present || user.Name.Value != nil || !user.VotesVisible.Present || user.VotesVisible.Value == nil || *user.VotesVisible.Value {
					t.Fatalf("raw public conversion = %+v, %v", user, err)
				}
			case 2:
				userID = change.Doc.UserID
				endpoint, err := EndpointContributionFromDocument(change.Doc)
				if err != nil || endpoint.EndpointID != "device" || endpoint.UserID != userID || endpoint.Active != nil {
					t.Fatalf("raw endpoint ownership/active = %+v, %v", endpoint, err)
				}
			}
			if (userID != "first" && userID != "second") || seen[userID] {
				t.Fatalf("unexpected or duplicate baseline user %q", userID)
			}
			seen[userID] = true
		}
	}
	if _, err := users.Doc("first").Update(ctx, []firestore.Update{{Path: "webPushEnabled", Value: false}}); err != nil {
		t.Fatal(err)
	}
	if changes, err := private.Next(); err != nil || len(changes) != 1 || changes[0].Kind != ChangeModified || changes[0].Doc.ID != "first" {
		t.Fatalf("private delta = %+v, %v", changes, err)
	}
	if _, err := publicUsers.Doc("first").Update(ctx, []firestore.Update{{Path: "name", Value: "Public name"}}); err != nil {
		t.Fatal(err)
	}
	if changes, err := public.Next(); err != nil || len(changes) != 1 || changes[0].Kind != ChangeModified || changes[0].Doc.ID != "first" {
		t.Fatalf("public delta = %+v, %v", changes, err)
	}
	if _, err := users.Doc("first").Collection(endpointsCollection).Doc("device").Update(ctx, []firestore.Update{{Path: "active", Value: true}}); err != nil {
		t.Fatal(err)
	}
	if changes, err := endpoints.Next(); err != nil || len(changes) != 1 || changes[0].Kind != ChangeModified || changes[0].Doc.UserID != "first" || changes[0].Doc.ID != "device" {
		t.Fatalf("endpoint delta = %+v, %v", changes, err)
	}
	if _, err := publicUsers.Doc("first").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if changes, err := public.Next(); err != nil || len(changes) != 1 || changes[0].Kind != ChangeRemoved || changes[0].Doc.ID != "first" {
		t.Fatalf("public removal = %+v, %v", changes, err)
	}
	reopened, err := source.WatchPublicUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Stop()
	if changes, err := reopened.Next(); err != nil || len(changes) != 1 || changes[0].Kind != ChangeAdded || changes[0].Doc.ID != "second" {
		t.Fatalf("reopened public baseline = %+v, %v", changes, err)
	}
	if _, err := publicUsers.Doc("second").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	empty, err := source.WatchPublicUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Stop()
	if changes, err := empty.Next(); err != nil || len(changes) != 0 {
		t.Fatalf("empty public baseline = %+v, %v", changes, err)
	}
}
