package testemail

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestFirestoreSourceAgainstEmulator(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A throwaway project keeps the users collection isolated from other emulator data.
	client, err := firestore.NewClient(ctx, fmt.Sprintf("last-orders-testemail-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	defer client.Close()

	users := client.Collection(usersCollection)
	if _, err := users.Doc("alice").Set(ctx, map[string]any{RequestField: "r1", "email": "alice@example.com"}); err != nil {
		t.Fatalf("seed alice: %v", err)
	}
	if _, err := users.Doc("bob").Set(ctx, map[string]any{"email": "bob@example.com"}); err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	source, err := NewFirestoreSource(client)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := source.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer stream.Stop()
	documents, err := stream.Next()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if len(documents) != 1 || documents[0].ID != "alice" {
		t.Fatalf("documents = %+v, want only alice", documents)
	}

	if err := source.AckTestEmail(ctx, "alice", "r1"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	snapshot, err := users.Doc("alice").Get(ctx)
	if err != nil {
		t.Fatalf("read alice: %v", err)
	}
	data := snapshot.Data()
	if data[AckField] != "r1" || data["email"] != "alice@example.com" {
		t.Fatalf("alice = %+v, want merged ack", data)
	}
}
