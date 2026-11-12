package listenerscope

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestInitialPollsSinceUsesOldestPollOrFallback(t *testing.T) {
	tests := []struct {
		name         string
		oldestPoll   string
		fallbackDate string
		want         string
		wantErr      bool
	}{
		{name: "uses oldest poll", oldestPoll: "2026-03-01", fallbackDate: "2026-11-11", want: "2026-03-01"},
		{name: "uses fallback when no polls exist", fallbackDate: "2026-11-11", want: "2026-11-11"},
		{name: "rejects malformed oldest poll date", oldestPoll: "not-a-date", fallbackDate: "2026-11-11", wantErr: true},
		{name: "rejects malformed fallback date", fallbackDate: "not-a-date", wantErr: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := initialPollsSince(testCase.oldestPoll, testCase.fallbackDate)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("initialPollsSince succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("initialPollsSince: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("initialPollsSince() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestResolvePollsSinceAgainstEmulator(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := firestore.NewClient(ctx, fmt.Sprintf("last-orders-listenerscope-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Firestore client: %v", err)
		}
	})

	testID := fmt.Sprintf("%d", time.Now().UnixNano())
	stateNamespace := "listenerscope-test-" + testID
	pollCollection := client.Collection("listenerscope-test-polls-" + testID)
	store, err := NewFirestoreStore(client, "listener_state", stateNamespace)
	if err != nil {
		t.Fatal(err)
	}
	store.polls = pollCollection
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()
		for _, pollID := range []string{"newer-poll", "oldest-poll"} {
			if _, err := pollCollection.Doc(pollID).Delete(cleanupCtx); err != nil {
				t.Errorf("delete seeded poll %q: %v", pollID, err)
			}
		}
		if _, err := client.Collection("listener_state").Doc(stateNamespace).Delete(cleanupCtx); err != nil {
			t.Errorf("delete test listener state: %v", err)
		}
	})

	for pollID, date := range map[string]string{
		"newer-poll":  "2026-06-01",
		"oldest-poll": "2026-03-01",
	} {
		if _, err := pollCollection.Doc(pollID).Set(ctx, map[string]any{"date": date}); err != nil {
			t.Fatalf("seed poll %q: %v", pollID, err)
		}
	}
	initial, err := store.ResolvePollsSince(ctx, "", "2026-11-11")
	if err != nil {
		t.Fatalf("resolve from oldest poll: %v", err)
	}
	if initial != "2026-03-01" {
		t.Fatalf("initial cutoff = %q, want oldest poll date", initial)
	}
	if _, err := store.ResolvePollsSince(ctx, "02-10-2026", "2026-11-11"); err == nil {
		t.Fatal("malformed date was accepted")
	}

	steps := []struct {
		requested string
		want      string
		wantErr   bool
	}{
		{requested: "2026-10-02", want: "2026-10-02"},
		{requested: "", want: "2026-10-02"},
		{requested: "2026-10-02", want: "2026-10-02"},
		{requested: "2026-09-01", wantErr: true},
		{requested: "2026-10-16", want: "2026-10-16"},
		{requested: "", want: "2026-10-16"},
	}
	for _, step := range steps {
		got, err := store.ResolvePollsSince(ctx, step.requested, "2026-11-11")
		if step.wantErr {
			if err == nil {
				t.Fatalf("ResolvePollsSince(%q) succeeded, want error", step.requested)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ResolvePollsSince(%q): %v", step.requested, err)
		}
		if got != step.want {
			t.Fatalf("ResolvePollsSince(%q) = %q, want %q", step.requested, got, step.want)
		}
	}
}

func TestResolvePollsSinceWithoutPollsUsesFallbackAgainstEmulator(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := firestore.NewClient(ctx, fmt.Sprintf("last-orders-listenerscope-empty-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("new firestore client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Firestore client: %v", err)
		}
	})

	testID := fmt.Sprintf("%d", time.Now().UnixNano())
	stateNamespace := "listenerscope-empty-test-" + testID
	store, err := NewFirestoreStore(client, "listener_state", stateNamespace)
	if err != nil {
		t.Fatal(err)
	}
	store.polls = client.Collection("listenerscope-empty-polls-" + testID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()
		if _, err := client.Collection("listener_state").Doc(stateNamespace).Delete(cleanupCtx); err != nil {
			t.Errorf("delete test listener state: %v", err)
		}
	})

	const fallbackDate = "2026-11-11"
	got, err := store.ResolvePollsSince(ctx, "", fallbackDate)
	if err != nil {
		t.Fatalf("resolve without polls: %v", err)
	}
	if got != fallbackDate {
		t.Fatalf("ResolvePollsSince() = %q, want fallback %q", got, fallbackDate)
	}
	got, err = store.ResolvePollsSince(ctx, "", fallbackDate)
	if err != nil {
		t.Fatalf("read persisted fallback: %v", err)
	}
	if got != fallbackDate {
		t.Fatalf("persisted cutoff = %q, want %q", got, fallbackDate)
	}
}
