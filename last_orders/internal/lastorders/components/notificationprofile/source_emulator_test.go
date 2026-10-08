package notificationprofile

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func emulatorClient(t *testing.T) (*firestore.Client, context.Context) {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "last-orders-emulator"
	}
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
