package admindelete

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"cloud.google.com/go/firestore"
	admindeletelistener "last_orders/internal/lastorders/database/listeners/admindelete"
	"last_orders/internal/lastorders/services/attendancecleanup"
	"last_orders/internal/lastorders/services/votecleanup"
)

func TestDryRunLeavesVotesAndAttendanceUnchanged(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		t.Fatal(err)
	}
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		t.Fatalf("Firestore emulator host %q is not loopback", host)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	projectID := fmt.Sprintf("demo-admin-delete-%d", time.Now().UnixNano())
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	request := validTruth()
	for _, seed := range []struct {
		collection string
		data       map[string]any
	}{
		{requestCollection, map[string]any{"status": "pending"}},
		{"votes", map[string]any{"venue": []string{request.Request.TargetUID, "other"}}},
		{"attendance", map[string]any{"venue": map[string]any{"canCome": []string{request.Request.TargetUID, "other"}}}},
	} {
		if _, err := client.Collection(seed.collection).Doc(request.Request.RequestID).Set(ctx, seed.data); err != nil {
			t.Fatal(err)
		}
	}
	applicationData := map[string]map[string]any{
		"messages/message-1":                   {"uid": request.Request.TargetUID, "text": "hello"},
		"users/target-1/push_endpoints/device": {"endpoint": "private"},
		"roles/admin":                          {request.Request.TargetUID: true},
		"users/target-1":                       {"name": "target"},
		"user-public/target-1":                 {"name": "target"},
	}
	updateTimes := make(map[string]time.Time, len(applicationData))
	for path, data := range applicationData {
		result, err := client.Doc(path).Set(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		updateTimes[path] = result.UpdateTime
	}
	repository, err := NewFirestoreRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	votes, err := votecleanup.NewFirestoreCleaner(client)
	if err != nil {
		t.Fatal(err)
	}
	attendance, err := attendancecleanup.NewFirestoreCleaner(client)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	evaluator, err := New(Options{Enabled: true, DryRun: true, Repository: repository, VoteCleaner: votes, AttendanceCleaner: attendance, Logger: slog.New(slog.NewTextHandler(&output, nil))})
	if err != nil {
		t.Fatal(err)
	}
	evaluator.Handle(ctx, request)
	for _, collection := range []string{"votes", "attendance"} {
		snapshot, err := client.Collection(collection).Doc(request.Request.RequestID).Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var users []any
		if collection == "votes" {
			users = snapshot.Data()["venue"].([]any)
		} else {
			users = snapshot.Data()["venue"].(map[string]any)["canCome"].([]any)
		}
		if len(users) != 2 || users[0] != request.Request.TargetUID || users[1] != "other" {
			t.Fatalf("dry-run changed %s: %v", collection, snapshot.Data())
		}
	}
	for path, updateTime := range updateTimes {
		snapshot, err := client.Doc(path).Get(ctx)
		if err != nil || !snapshot.UpdateTime.Equal(updateTime) {
			t.Errorf("dry-run changed %s: snapshot=%v err=%v", path, snapshot, err)
		}
	}
	for _, entity := range []string{"votes/request-1", "attendance/request-1", "messages/message-1", "users/target-1/push_endpoints/device", "roles/admin", "users/target-1", "user-public/target-1", "target-1"} {
		if !strings.Contains(output.String(), entity) {
			t.Errorf("dry-run log missing %q: %s", entity, output.String())
		}
	}
	request.Request.SchemaVersion = 2
	result := evaluator.Handle(ctx, request)
	complete, ok := result.(cellar.Complete)
	if !ok || len(complete.NewCells) != 0 {
		t.Fatalf("invalid dry-run scheduled persistent outcome: %#v", result)
	}
}

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
