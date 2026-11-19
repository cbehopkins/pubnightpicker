package autocomplete

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/truths"
)

func TestRunOncePersistsCapturedOccurrenceForLaterExecution(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	occurrence := time.Date(2026, time.July, 4, 16, 0, 0, 0, location)
	store := cellar.NewMemoryStore(nil)
	listener, err := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.RunOnce(context.Background(), occurrence); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Later execution must use the persisted occurrence, not a newer clock date.
	advancedClock := occurrence.Add(24 * time.Hour)
	if got, want := advancedClock.In(location).Format(time.DateOnly), "2026-07-05"; got != want {
		t.Fatalf("advanced clock date = %q, want %q", got, want)
	}

	cells, err := store.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 1 {
		t.Fatalf("active cells = %d, want 1", len(cells))
	}
	check, err := cellar.JSONCodec[idempotency.CheckPayload]().Unmarshal(cells[0].Steps[0].Payload)
	if err != nil {
		t.Fatalf("decode idempotency payload: %v", err)
	}
	if check.Key != "2026-07-04" {
		t.Fatalf("idempotency key = %q, want captured occurrence date", check.Key)
	}
	if check.Truth.FanoutName != truths.DailyPollAutoCompleteDueFanout {
		t.Fatalf("Truth fanout = %q, want %q", check.Truth.FanoutName, truths.DailyPollAutoCompleteDueFanout)
	}
	truth, err := cellar.JSONCodec[truths.DailyPollAutoCompleteDue]().Unmarshal(check.Truth.Payload)
	if err != nil {
		t.Fatalf("decode discovery Truth: %v", err)
	}
	if truth.ObservedOn != "2026-07-04" {
		t.Fatalf("Truth observed date = %q, want captured occurrence date", truth.ObservedOn)
	}
	if truth.Identity() != check.Key {
		t.Fatalf("Truth identity = %q, want idempotency key %q", truth.Identity(), check.Key)
	}
}
