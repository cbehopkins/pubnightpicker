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

	owner, exists, err := remote.LookupOwner(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("lookup owner before create: %v", err)
	}
	if exists || owner != "" {
		t.Fatalf("owner before create = %q, exists=%t; want empty/missing", owner, exists)
	}
	released, err := remote.ReleaseKey(ctx, listener, eventKey, "execution-1")
	if err != nil {
		t.Fatalf("release already-absent key: %v", err)
	}
	if !released {
		t.Fatal("an absent claim should be reported as released")
	}

	alreadyExists, ownedByThisExecution, err := remote.CreateKey(ctx, listener, eventKey, "execution-1")
	if err != nil {
		t.Fatalf("create key first attempt: %v", err)
	}
	if alreadyExists || !ownedByThisExecution {
		t.Fatalf("first create = alreadyExists=%v ownedByThisExecution=%v; want false/true", alreadyExists, ownedByThisExecution)
	}

	exists, err = remote.HasKey(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("has key after create: %v", err)
	}
	if !exists {
		t.Fatal("key should exist after create")
	}
	owner, exists, err = remote.LookupOwner(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("lookup owner after create: %v", err)
	}
	if !exists || owner != "execution-1" {
		t.Fatalf("owner after create = %q, exists=%t; want execution-1/present", owner, exists)
	}

	alreadyExists, ownedByThisExecution, err = remote.CreateKey(ctx, listener, eventKey, "execution-1")
	if err != nil {
		t.Fatalf("create key retry attempt: %v", err)
	}
	if !alreadyExists || !ownedByThisExecution {
		t.Fatalf("retry = alreadyExists=%v ownedByThisExecution=%v; want true/true", alreadyExists, ownedByThisExecution)
	}

	alreadyExists, ownedByThisExecution, err = remote.CreateKey(ctx, listener, eventKey, "execution-2")
	if err != nil {
		t.Fatalf("create key conflicting attempt: %v", err)
	}
	if !alreadyExists || ownedByThisExecution {
		t.Fatalf("conflict = alreadyExists=%v ownedByThisExecution=%v; want true/false", alreadyExists, ownedByThisExecution)
	}
	owner, exists, err = remote.LookupOwner(ctx, listener, eventKey)
	if err != nil {
		t.Fatalf("lookup owner after conflicting create: %v", err)
	}
	if !exists || owner != "execution-1" {
		t.Fatalf("owner after conflicting create = %q, exists=%t; want original owner", owner, exists)
	}

	released, err = remote.ReleaseKey(ctx, listener, eventKey, "execution-2")
	if err != nil {
		t.Fatalf("release with competing owner: %v", err)
	}
	if released {
		t.Fatal("release succeeded for a non-owner")
	}
	owner, exists, err = remote.LookupOwner(ctx, listener, eventKey)
	if err != nil || !exists || owner != "execution-1" {
		t.Fatalf("owner after mismatched release = %q, exists=%t, err=%v", owner, exists, err)
	}

	released, err = remote.ReleaseKey(ctx, listener, eventKey, "execution-1")
	if err != nil || !released {
		t.Fatalf("release original owner = %t, %v; want confirmed release", released, err)
	}
	if _, _, err := remote.CreateKey(ctx, listener, eventKey, "execution-2"); err != nil {
		t.Fatalf("replacement claim: %v", err)
	}
	released, err = remote.ReleaseKey(ctx, listener, eventKey, "execution-1")
	if err != nil {
		t.Fatalf("delayed stale release: %v", err)
	}
	if released {
		t.Fatal("delayed release removed a replacement owner's claim")
	}
	owner, exists, err = remote.LookupOwner(ctx, listener, eventKey)
	if err != nil || !exists || owner != "execution-2" {
		t.Fatalf("replacement owner after delayed release = %q, exists=%t, err=%v", owner, exists, err)
	}
}
