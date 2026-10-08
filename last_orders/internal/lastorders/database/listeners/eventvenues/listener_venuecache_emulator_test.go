package eventvenues

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/components/venuecache"

	"cloud.google.com/go/firestore"
	_ "modernc.org/sqlite"
)

func TestListEventVenuesReadsProjectionThroughVenueCache(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "last-orders-emulator"
	}
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base, err := basestore.New(db)
	if err != nil {
		t.Fatalf("new base store: %v", err)
	}
	store, err := venuecache.New(base)
	if err != nil {
		t.Fatalf("new venue cache store: %v", err)
	}
	source, err := venuecache.NewFirestoreSource(client)
	if err != nil {
		t.Fatalf("new venue source: %v", err)
	}
	cache, err := venuecache.NewService(store, source, nil)
	if err != nil {
		t.Fatalf("new venue cache service: %v", err)
	}
	service, err := recurrence.NewService(client, slog.Default(), cache)
	if err != nil {
		t.Fatalf("new recurrence service: %v", err)
	}
	eventVenueSource, err := NewFirestoreSource(client)
	if err != nil {
		t.Fatalf("new event venue source: %v", err)
	}
	listener, err := New(Config{Store: cellar.NewMemoryStore(nil), Clock: service, Source: eventVenueSource, VenueCache: cache})
	if err != nil {
		t.Fatalf("new event venue listener: %v", err)
	}

	eventID := "venue-cache-recurrence-" + time.Now().UTC().Format("20060102150405.000000000")
	if _, err := client.Collection("pubs").Doc(eventID).Set(ctx, map[string]any{
		"venueType":                    "event",
		"name":                         "The Cached Arms",
		"recurrence":                   map[string]any{"frequency": "once", "date": "2030-01-02"},
		recurrence.NextOccurrenceField: "2030-01-02",
	}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	venues, err := listener.listEventVenues(ctx)
	if err != nil {
		t.Fatalf("list event venues: %v", err)
	}
	for _, venue := range venues {
		if venue.ID == eventID {
			if venue.Name != "The Cached Arms" {
				t.Fatalf("venue name = %q; want authoritative projection", venue.Name)
			}
			return
		}
	}
	t.Fatalf("cached event venue %q was not returned", eventID)
}
