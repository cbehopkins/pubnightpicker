package venuecache

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

func TestFirestoreWatchCompleteBaselineBeforeDeltas(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	for _, populated := range []bool{false, true} {
		name := "empty"
		if populated {
			name = "populated"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			project := "venue-watch-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
			client, err := firestore.NewClient(ctx, project)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			t.Cleanup(func() {
				cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
				defer finish()
				for _, venueID := range []string{"first", "second", "third"} {
					if _, err := client.Collection(collection).Doc(venueID).Delete(cleanup); err != nil {
						t.Errorf("delete emulator venue %s: %v", venueID, err)
					}
				}
			})
			want := make(map[string]string)
			if populated {
				for _, venueID := range []string{"first", "second", "third"} {
					venueName := "Venue " + venueID
					if _, err := client.Collection(collection).Doc(venueID).Set(ctx, map[string]any{"name": venueName}); err != nil {
						t.Fatal(err)
					}
					want[venueID] = venueName
				}
			}
			source, err := NewFirestoreSource(client)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := source.Watch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Stop()
			assertFirestoreBaseline(t, stream, want)
			if _, err := client.Collection(collection).Doc("first").Set(ctx, map[string]any{"name": "Changed venue"}); err != nil {
				t.Fatal(err)
			}
			changes, err := stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			kind := ChangeAdded
			if populated {
				kind = ChangeModified
			}
			if len(changes) != 1 || changes[0].Kind != kind || changes[0].Doc.ID != "first" || changes[0].Doc.Data["name"] != "Changed venue" {
				t.Fatalf("first ordered delta = %+v; want changed first venue with kind %d", changes, kind)
			}
			if _, err := client.Collection(collection).Doc("first").Delete(ctx); err != nil {
				t.Fatal(err)
			}
			changes, err = stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 1 || changes[0].Kind != ChangeRemoved || changes[0].Doc.ID != "first" {
				t.Fatalf("second ordered delta = %+v; want removed first venue", changes)
			}
			stream.Stop()
			reopened, err := source.Watch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Stop()
			delete(want, "first")
			assertFirestoreBaseline(t, reopened, want)
		})
	}
}

func assertFirestoreBaseline(t *testing.T, stream ChangeStream, want map[string]string) {
	t.Helper()
	changes, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != len(want) {
		t.Fatalf("baseline contains %d documents; want %d", len(changes), len(want))
	}
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		name, exists := want[change.Doc.ID]
		if !exists || seen[change.Doc.ID] || change.Kind != ChangeAdded || change.Doc.Data["name"] != name {
			t.Fatalf("unexpected baseline change: %+v", change)
		}
		seen[change.Doc.ID] = true
	}
}
