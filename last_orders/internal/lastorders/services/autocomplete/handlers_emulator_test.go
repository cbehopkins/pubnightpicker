package autocomplete

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/truths"

	"cloud.google.com/go/firestore"
)

func emulatorClient(t *testing.T, ctx context.Context) *firestore.Client {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = "last-orders-emulator"
	}
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestCandidateCreatesCloseCellForCurrentClearWinnerAgainstEmulator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := emulatorClient(t, ctx)
	pollID := fmt.Sprintf("autocomplete-candidate-%d", time.Now().UnixNano())
	seedAutoCompletePoll(t, ctx, client, pollID, false, map[string]any{"venue-1": map[string]any{}, "venue-2": map[string]any{}})
	if _, err := client.Collection("votes").Doc(pollID).Set(ctx, map[string]any{"venue-1": []any{"user-1", "user-2"}, "venue-2": []any{"user-3"}}); err != nil {
		t.Fatalf("seed votes: %v", err)
	}
	if _, err := client.Collection("pubs").Doc("venue-1").Set(ctx, map[string]any{"name": "One", "food": true}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}
	if _, err := client.Collection("pubs").Doc("venue-2").Set(ctx, map[string]any{"name": "Two", "food": true}); err != nil {
		t.Fatalf("seed venue: %v", err)
	}

	result := (CandidateHandler{Client: client}).Handle(ctx, dueTruth(pollID))
	complete, ok := result.(cellar.Complete)
	if !ok || len(complete.NewCells) != 1 {
		t.Fatalf("result = %#v, want one Close Cell", result)
	}
	payload, err := cellar.JSONCodec[CompletionClosePayload]().Unmarshal(complete.NewCells[0].Steps[0].Payload)
	if err != nil {
		t.Fatalf("decode close payload: %v", err)
	}
	if payload.PollID != pollID || payload.SelectedVenueID != "venue-1" {
		t.Fatalf("close payload = %+v", payload)
	}
}

func TestCloseDoesNotOverwriteCompletedPollAgainstEmulator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := emulatorClient(t, ctx)
	pollID := fmt.Sprintf("autocomplete-close-%d", time.Now().UnixNano())
	seedAutoCompletePoll(t, ctx, client, pollID, true, map[string]any{"venue-manual": map[string]any{"name": "Manual"}})
	if _, err := client.Collection("polls").Doc(pollID).Update(ctx, []firestore.Update{{Path: "selected", Value: "venue-manual"}, {Path: "keep", Value: "unchanged"}}); err != nil {
		t.Fatalf("seed completed poll: %v", err)
	}

	result := (CloseHandler{Client: client}).Handle(ctx, CompletionClosePayload{PollID: pollID, PollDate: "2026-09-01", SelectedVenueID: "venue-auto"})
	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("result = %T, want cellar.Complete", result)
	}
	doc, err := client.Collection("polls").Doc(pollID).Get(ctx)
	if err != nil {
		t.Fatalf("read poll: %v", err)
	}
	if got := doc.Data()["selected"]; got != "venue-manual" {
		t.Fatalf("selected = %v, want manual selection", got)
	}
	if got := doc.Data()["keep"]; got != "unchanged" {
		t.Fatalf("keep = %v, want unrelated field retained", got)
	}
}

func TestDiscoveryCreatesPerPollTruthWorkAgainstEmulator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := emulatorClient(t, ctx)
	pollID := fmt.Sprintf("autocomplete-discovery-%d", time.Now().UnixNano())
	seedAutoCompletePoll(t, ctx, client, pollID, false, map[string]any{"venue-1": map[string]any{}})
	seedAutoCompletePoll(t, ctx, client, pollID+"-other-day", false, map[string]any{"venue-2": map[string]any{}})
	if _, err := client.Collection("polls").Doc(pollID+"-other-day").Update(ctx, []firestore.Update{{Path: "date", Value: "2026-09-02"}}); err != nil {
		t.Fatalf("seed other-day poll: %v", err)
	}

	result := (DiscoveryHandler{Client: client}).Handle(ctx, truths.DailyPollAutoCompleteDue{ObservedOn: "2026-09-01"})
	complete, ok := result.(cellar.Complete)
	if !ok || len(complete.NewCells) != 1 {
		t.Fatalf("result = %#v, want one locally idempotent poll Truth Cell", result)
	}
	step := complete.NewCells[0].Steps[0]
	if step.HandlerName != idempotency.HandlerCheck {
		t.Fatalf("handler = %q, want %q", step.HandlerName, idempotency.HandlerCheck)
	}
	check, err := cellar.JSONCodec[idempotency.CheckPayload]().Unmarshal(step.Payload)
	if err != nil {
		t.Fatalf("decode idempotency payload: %v", err)
	}
	if check.Key != pollID+"_2026-09-01" || check.Fact.Name != truths.PollAutoCompletionDueName {
		t.Fatalf("discovery payload = %+v", check)
	}
}

func TestCloseCompletesPollAndWritesAuditAgainstEmulator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := emulatorClient(t, ctx)
	pollID := fmt.Sprintf("autocomplete-success-%d", time.Now().UnixNano())
	seedAutoCompletePoll(t, ctx, client, pollID, false, map[string]any{"venue-1": map[string]any{}})
	if _, err := client.Collection("polls").Doc(pollID).Update(ctx, []firestore.Update{{Path: "keep", Value: "unchanged"}}); err != nil {
		t.Fatalf("seed unrelated field: %v", err)
	}

	result := (CloseHandler{Client: client}).Handle(ctx, CompletionClosePayload{PollID: pollID, PollDate: "2026-09-01", SelectedVenueID: "venue-1"})
	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("result = %T, want cellar.Complete", result)
	}
	poll, err := client.Collection("polls").Doc(pollID).Get(ctx)
	if err != nil {
		t.Fatalf("read completed poll: %v", err)
	}
	if got := poll.Data()["completed"]; got != true {
		t.Fatalf("completed = %v, want true", got)
	}
	if got := poll.Data()["selected"]; got != "venue-1" {
		t.Fatalf("selected = %v, want venue-1", got)
	}
	if got := poll.Data()["keep"]; got != "unchanged" {
		t.Fatalf("keep = %v, want unrelated field retained", got)
	}
	audits, err := client.Collection("poll_action_audit").Where("pollId", "==", pollID).Documents(ctx).GetAll()
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("audit records = %d, want 1", len(audits))
	}
	if got := audits[0].Data()["actorUid"]; got != "backend:auto" {
		t.Fatalf("actorUid = %v, want backend:auto", got)
	}
}

func seedAutoCompletePoll(t *testing.T, ctx context.Context, client *firestore.Client, pollID string, completed bool, pubs map[string]any) {
	t.Helper()
	if _, err := client.Collection("polls").Doc(pollID).Set(ctx, map[string]any{"date": "2026-09-01", "completed": completed, "pubs": pubs}); err != nil {
		t.Fatalf("seed poll: %v", err)
	}
}

func dueTruth(pollID string) truths.PollAutoCompletionDue {
	return truths.PollAutoCompletionDue{Poll: truths.PollAutoCompletionSnapshot{PollID: pollID, PollDate: "2026-09-01", Completed: false, ObservedOn: "2026-09-01"}}
}
