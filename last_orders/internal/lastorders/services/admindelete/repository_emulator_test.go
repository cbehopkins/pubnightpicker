package admindelete

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	admindeletelistener "last_orders/internal/lastorders/database/listeners/admindelete"
)

func TestFirestoreRepositoryPersistsOutcomeAndAuditAtomically(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	projectID := fmt.Sprintf("last-orders-admin-delete-%d", time.Now().UnixNano())
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new Firestore client: %v", err)
	}
	defer client.Close()

	requestID := "request-1"
	requestRef := client.Collection(requestCollection).Doc(requestID)
	if _, err := requestRef.Set(ctx, map[string]any{"status": "pending"}); err != nil {
		t.Fatalf("seed request: %v", err)
	}
	repository, err := NewFirestoreRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	payload := OutcomePayload{
		RequestID:      requestID,
		TargetUID:      "target-1",
		RequestedByUID: "admin-1",
		Reason:         "admin_user_delete",
		Outcome:        OutcomeAuthDeleted,
		Idempotent:     true,
		At:             time.Now().UTC(),
	}
	if err := repository.PersistOutcome(ctx, payload); err != nil {
		t.Fatalf("persist outcome: %v", err)
	}
	if err := repository.PersistOutcome(ctx, payload); err != nil {
		t.Fatalf("retry persistence: %v", err)
	}

	requestSnapshot, err := requestRef.Get(ctx)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	if got := requestSnapshot.Data()["status"]; got != string(OutcomeAuthDeleted) {
		t.Fatalf("request status = %v, want %q", got, OutcomeAuthDeleted)
	}
	if _, ok := requestSnapshot.Data()["authDeletedAt"]; !ok {
		t.Fatal("request is missing authDeletedAt")
	}
	auditSnapshot, err := client.Collection(auditCollection).Doc(requestID).Get(ctx)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if got := auditSnapshot.Data()["outcome"]; got != string(OutcomeAuthDeleted) {
		t.Fatalf("audit outcome = %v, want %q", got, OutcomeAuthDeleted)
	}

	stalePayload := payload
	stalePayload.Outcome = OutcomeDryRunValidated
	if err := repository.PersistOutcome(ctx, stalePayload); err != nil {
		t.Fatalf("persist stale outcome: %v", err)
	}
	requestSnapshot, err = requestRef.Get(ctx)
	if err != nil {
		t.Fatalf("read request after stale outcome: %v", err)
	}
	auditSnapshot, err = client.Collection(auditCollection).Doc(requestID).Get(ctx)
	if err != nil {
		t.Fatalf("read audit after stale outcome: %v", err)
	}
	if got := requestSnapshot.Data()["status"]; got != string(OutcomeAuthDeleted) {
		t.Fatalf("stale write changed request status to %v", got)
	}
	if got := auditSnapshot.Data()["outcome"]; got != string(OutcomeAuthDeleted) {
		t.Fatalf("stale write changed audit outcome to %v", got)
	}

	payload.RequestID = "validated-request"
	payload.Outcome = OutcomeDryRunValidated
	validatedRef := client.Collection(requestCollection).Doc(payload.RequestID)
	if _, err := validatedRef.Set(ctx, map[string]any{"status": "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.PersistOutcome(ctx, payload); err != nil {
		t.Fatal(err)
	}
	source, err := admindeletelistener.NewFirestoreSource(client)
	if err != nil {
		t.Fatal(err)
	}
	for _, realDelete := range []bool{false, true} {
		documents, err := source.ListEligible(ctx, realDelete)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if realDelete {
			want = 1
		}
		if len(documents) != want {
			t.Fatalf("realDelete=%t: selected %v", realDelete, documents)
		}
	}
	payload.RealDelete = true
	payload.Outcome = OutcomeAuthDeleted
	if err := repository.PersistOutcome(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if err := repository.PersistOutcome(ctx, payload); err != nil {
		t.Fatal(err)
	}
	payload.RealDelete = false
	payload.Outcome = OutcomeDryRunValidated
	if err := repository.PersistOutcome(ctx, payload); err != nil {
		t.Fatal(err)
	}
	promoted, err := validatedRef.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Data()["status"] != string(OutcomeAuthDeleted) {
		t.Fatalf("promoted status = %v", promoted.Data()["status"])
	}
	auditSnapshot, err = client.Collection(auditCollection).Doc(payload.RequestID).Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := auditSnapshot.Data()["dryRunEvidence"].(map[string]any)
	if !ok || evidence["outcome"] != string(OutcomeDryRunValidated) || auditSnapshot.Data()["outcome"] != string(OutcomeAuthDeleted) {
		t.Fatalf("promotion lost audit evidence: %v", auditSnapshot.Data())
	}
}
