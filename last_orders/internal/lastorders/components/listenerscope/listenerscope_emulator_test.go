package listenerscope

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

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
	defer client.Close()

	store, err := NewFirestoreStore(client, "listener_state", "last_orders")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.ResolvePollsSince(ctx, ""); err == nil {
		t.Fatal("resolving with nothing stored and nothing requested succeeded")
	}
	if _, err := store.ResolvePollsSince(ctx, "02-10-2026"); err == nil {
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
		got, err := store.ResolvePollsSince(ctx, step.requested)
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
