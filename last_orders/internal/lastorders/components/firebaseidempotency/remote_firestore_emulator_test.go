package firebaseidempotency

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestFirestoreRemoteAgainstEmulator(t *testing.T) {
	t.Parallel()

	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "last-orders-emulator"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	defer client.Close()

	namespace := fmt.Sprintf("last_orders_test_%d", time.Now().UnixNano())
	remote, err := NewFirestoreRemote(client, "listener_state", namespace)
	if err != nil {
		t.Fatalf("new firestore remote: %v", err)
	}

	listener := "listener-emulator"
	eventKey := "event-1"

	exists, err := remote.HasKey(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("has key before create: %v", err)
	}
	if exists {
		t.Fatal("key should not exist before create")
	}

	alreadyExists, err := remote.CreateKey(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("create key first attempt: %v", err)
	}
	if alreadyExists {
		t.Fatal("first create should not report already exists")
	}

	exists, err = remote.HasKey(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("has key after create: %v", err)
	}
	if !exists {
		t.Fatal("key should exist after create")
	}

	alreadyExists, err = remote.CreateKey(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("create key second attempt: %v", err)
	}
	if !alreadyExists {
		t.Fatal("second create should report already exists")
	}
}
