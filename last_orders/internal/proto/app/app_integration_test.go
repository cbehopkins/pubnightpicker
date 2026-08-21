package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/proto/app"
	"last_orders/internal/proto/firebase"
	"last_orders/internal/proto/handlers"
	"last_orders/internal/proto/idempotency"
)

func TestPhase1ObservesButDoesNotDispatchToCellar(t *testing.T) {
	t.Parallel()

	events := []firebase.Event{{
		Type:        firebase.EventPollCreated,
		PollID:      "poll-1",
		ObservedAt:  time.Now().UTC(),
		DeliveryKey: "k-1",
	}}

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseFirebaseObserve,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: time.Hour,
		ChainDelay:               time.Second,
		EnableFirebase:           true,
		FirebaseSource:           firebase.SimulatedSource{Events: events},
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 150*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.FirebaseRuns != 0 {
		t.Fatalf("phase 1 should not dispatch firebase events to cellar, got firebase_runs=%d", snapshot.FirebaseRuns)
	}
}

func TestPhase2HousekeepingSchedulesAndExecutes(t *testing.T) {
	t.Parallel()

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseHousekeepingScheduling,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        40 * time.Millisecond,
		HousekeepingScheduleLead: 20 * time.Millisecond,
		ChainDelay:               time.Second,
		EnableFirebase:           false,
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 320*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.HousekeepingRuns < 2 {
		t.Fatalf("expected housekeeping runs >= 2, got %d", snapshot.HousekeepingRuns)
	}
}

func TestPhase3HandlerCreatesFutureWork(t *testing.T) {
	t.Parallel()

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseChainedFutureWork,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        80 * time.Millisecond,
		HousekeepingScheduleLead: 15 * time.Millisecond,
		ChainDelay:               90 * time.Millisecond,
		EnableFirebase:           false,
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 450*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.FutureRuns == 0 {
		t.Fatal("expected chained future work to execute at least once")
	}
}

func TestPhase4FirebaseToCellarWithoutIdempotency(t *testing.T) {
	t.Parallel()

	events := []firebase.Event{
		{
			Type:        firebase.EventPollCreated,
			PollID:      "poll-dup",
			ObservedAt:  time.Now().UTC(),
			DeliveryKey: "dup-key",
		},
		{
			Type:        firebase.EventPollCreated,
			PollID:      "poll-dup",
			ObservedAt:  time.Now().UTC(),
			DeliveryKey: "dup-key",
		},
	}

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseFirebaseToCellar,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: time.Hour,
		ChainDelay:               time.Second,
		EnableFirebase:           true,
		FirebaseSource:           firebase.SimulatedSource{Events: events},
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 250*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.FirebaseRuns != 2 {
		t.Fatalf("expected duplicate delivery to create duplicate work before phase 5, got firebase_runs=%d", snapshot.FirebaseRuns)
	}
}

func TestPhase5IdempotencyFiltersDuplicateDelivery(t *testing.T) {
	t.Parallel()

	events := []firebase.Event{
		{
			Type:        firebase.EventPollCompleted,
			PollID:      "poll-dup",
			ObservedAt:  time.Now().UTC(),
			DeliveryKey: "dup-key-2",
		},
		{
			Type:        firebase.EventPollCompleted,
			PollID:      "poll-dup",
			ObservedAt:  time.Now().UTC(),
			DeliveryKey: "dup-key-2",
		},
	}

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseListenerIdempotency,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: time.Hour,
		ChainDelay:               time.Second,
		EnableFirebase:           true,
		FirebaseSource:           firebase.SimulatedSource{Events: events},
		Deduper:                  idempotency.NewMemoryDeduper(),
		Logger:                   testLogger(),
	})

	runFor(t, proto, 250*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.FirebaseRuns != 1 {
		t.Fatalf("expected idempotency to collapse duplicate delivery, got firebase_runs=%d", snapshot.FirebaseRuns)
	}
}

func TestPhase4FirebaseDeleteEventToCellar(t *testing.T) {
	t.Parallel()

	events := []firebase.Event{{
		Type:        firebase.EventPollDeleted,
		PollID:      "poll-deleted",
		ObservedAt:  time.Now().UTC(),
		DeliveryKey: "delete-key-1",
	}}

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseFirebaseToCellar,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: time.Hour,
		ChainDelay:               time.Second,
		EnableFirebase:           true,
		FirebaseSource:           firebase.SimulatedSource{Events: events},
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 250*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.FirebaseRuns != 1 {
		t.Fatalf("expected delete delivery to create one downstream cell, got firebase_runs=%d", snapshot.FirebaseRuns)
	}
	if snapshot.LastFirebasePollID != "poll-deleted" {
		t.Fatalf("expected last poll id to match delete event, got %q", snapshot.LastFirebasePollID)
	}
	if snapshot.HousekeepingRuns != 0 {
		t.Fatalf("phase 4 should not run housekeeping, got housekeeping_runs=%d", snapshot.HousekeepingRuns)
	}
}

func TestPhase4DoesNotRunHousekeepingWithoutFirebase(t *testing.T) {
	t.Parallel()

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseFirebaseToCellar,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        20 * time.Millisecond,
		HousekeepingScheduleLead: 10 * time.Millisecond,
		ChainDelay:               time.Second,
		EnableFirebase:           false,
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 180*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.HousekeepingRuns != 0 {
		t.Fatalf("phase 4 should not run housekeeping when firebase is disabled, got housekeeping_runs=%d", snapshot.HousekeepingRuns)
	}
}

func TestFailureHandlerRetriesThenCompletes(t *testing.T) {
	t.Parallel()

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseHousekeepingScheduling,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: time.Hour,
		ChainDelay:               time.Second,
		EnableFirebase:           false,
		EnableFailureCell:        true,
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	runFor(t, proto, 2*time.Second)

	snapshot := proto.RecorderSnapshot()
	attempts := snapshot.FailAttemptsByKey["phase-failure-test"]
	if attempts < 2 {
		t.Fatalf("expected at least 2 fail-once attempts, got %d", attempts)
	}
}

func TestRestartAroundScheduledWork(t *testing.T) {
	t.Parallel()

	store := cellar.NewMemoryStore(nil)
	cfg := app.Config{
		Phase:                    app.PhaseHousekeepingScheduling,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: 400 * time.Millisecond,
		ChainDelay:               time.Second,
		EnableFirebase:           false,
		Store:                    store,
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	}

	beforeRestart := mustPrototype(t, cfg)
	runFor(t, beforeRestart, 120*time.Millisecond)

	activeBeforeRestart, err := store.ListActive()
	if err != nil {
		t.Fatalf("list active cells before restart: %v", err)
	}
	if len(activeBeforeRestart) != 1 {
		t.Fatalf("expected one scheduled housekeeping cell before restart, got %d", len(activeBeforeRestart))
	}
	scheduledCellID := activeBeforeRestart[0].ID

	afterRestart := mustPrototype(t, cfg)
	runFor(t, afterRestart, 650*time.Millisecond)

	snapshot := afterRestart.RecorderSnapshot()
	if snapshot.HousekeepingRuns == 0 {
		t.Fatal("expected scheduled housekeeping cell to execute after restart")
	}

	activeAfterRestart, err := store.ListActive()
	if err != nil {
		t.Fatalf("list active cells after restart: %v", err)
	}
	for _, cell := range activeAfterRestart {
		if cell.ID == scheduledCellID {
			t.Fatalf("expected pre-restart cell %q to complete", scheduledCellID)
		}
	}
}

func TestAddCellAPIExecutesThroughRunner(t *testing.T) {
	t.Parallel()

	proto := mustPrototype(t, app.Config{
		Phase:                    app.PhaseHousekeepingScheduling,
		PollDelay:                5 * time.Millisecond,
		HousekeepingEvery:        time.Hour,
		HousekeepingScheduleLead: time.Hour,
		ChainDelay:               time.Second,
		EnableFirebase:           false,
		Deduper:                  idempotency.NoopDeduper{},
		Logger:                   testLogger(),
	})

	now := time.Now().UTC()
	raw, err := cellar.JSONCodec[handlers.FirebasePayload]().Marshal(handlers.FirebasePayload{
		EventType:   firebase.EventPollCreated,
		PollID:      "poll-manual",
		ObservedAt:  now,
		DeliveryKey: "manual-key",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := proto.AddCell(cellar.CellRequest{HandlerName: handlers.HandlerFirebase, Payload: raw, NotBefore: &now}); err != nil {
		t.Fatalf("add cell: %v", err)
	}

	runFor(t, proto, 120*time.Millisecond)

	snapshot := proto.RecorderSnapshot()
	if snapshot.FirebaseRuns != 1 {
		t.Fatalf("expected manually added firebase handler to run once, got %d", snapshot.FirebaseRuns)
	}
}

func mustPrototype(t *testing.T, cfg app.Config) *app.Prototype {
	t.Helper()
	prototype, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new prototype: %v", err)
	}
	return prototype
}

func runFor(t *testing.T, prototype *app.Prototype, dur time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	if err := prototype.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
