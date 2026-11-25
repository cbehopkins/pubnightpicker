package votecleanup

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

type cleanupRepositoryStub struct {
	targetUID string
	result    Result
	err       error
}

func (repository *cleanupRepositoryStub) RemoveUser(_ context.Context, targetUID string) (Result, error) {
	repository.targetUID = targetUID
	return repository.result, repository.err
}

func (repository *cleanupRepositoryStub) PreviewUser(context.Context, string) ([]string, error) {
	return nil, repository.err
}

func TestCleanerValidatesAndDelegates(t *testing.T) {
	want := errors.New("repository error")
	repository := &cleanupRepositoryStub{result: Result{UpdatedDocuments: 2}, err: want}
	cleaner := &FirestoreCleaner{repository: repository}
	if _, err := cleaner.RemoveUser(context.Background(), ""); err == nil || repository.targetUID != "" {
		t.Fatalf("empty target error=%v repository target=%q", err, repository.targetUID)
	}
	result, err := cleaner.RemoveUser(context.Background(), "target")
	if !errors.Is(err, want) || result.UpdatedDocuments != 2 || repository.targetUID != "target" {
		t.Fatalf("result=%+v error=%v repository target=%q", result, err, repository.targetUID)
	}
}

func TestFirestoreCleanerRemovesTargetVotesIdempotently(t *testing.T) {
	if testing.Short() {
		t.Skip("Firestore emulator integration test")
	}
	firestoreHost := strings.TrimSpace(os.Getenv("FIRESTORE_EMULATOR_HOST"))
	if firestoreHost == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST must be set")
	}
	hostname, _, err := net.SplitHostPort(firestoreHost)
	if err != nil {
		t.Fatalf("parse Firestore emulator host: %v", err)
	}
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		t.Fatalf("Firestore emulator host %q is not loopback", firestoreHost)
	}

	projectID := "demo-vote-cleanup-local"
	t.Setenv("FIRESTORE_EMULATOR_HOST", firestoreHost)
	t.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	cleanup, err := NewFirestoreCleaner(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Collection("votes").Doc("meet-a").Set(ctx, map[string]any{
		"venue-a": []any{"target", "other", "target"},
		"venue-b": []any{"other"},
	}); err != nil {
		t.Fatal(err)
	}

	first, err := cleanup.RemoveUser(ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	if first.UpdatedDocuments != 1 {
		t.Fatalf("first updated documents=%d, want 1", first.UpdatedDocuments)
	}

	second, err := cleanup.RemoveUser(ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	if second.UpdatedDocuments != 0 {
		t.Fatalf("retry updated documents=%d, want 0", second.UpdatedDocuments)
	}

	doc, err := client.Collection("votes").Doc("meet-a").Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := doc.Data()
	venueA, ok := data["venue-a"].([]any)
	if !ok {
		venueA = nil
	}
	if len(venueA) != 1 || venueA[0] != "other" {
		t.Fatalf("venue-a voters=%v, want [other]", venueA)
	}
	venueB, ok := data["venue-b"].([]any)
	if !ok || len(venueB) != 1 || venueB[0] != "other" {
		t.Fatalf("venue-b data=%v, want unchanged [other]", venueB)
	}
}
