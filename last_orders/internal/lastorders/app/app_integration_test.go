package app_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/counter"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/handlers"

	_ "modernc.org/sqlite"
)

func TestDatabaseOwnershipSingleSQLiteForAppAndCellar(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "backend.db")
	a := mustNewApp(t, dbPath, firebaseidempotency.NewMemoryRemote(true), false, nil)
	defer a.Close()

	raw, err := cellar.JSONCodec[handlers.IncrementPayload]().Marshal(handlers.IncrementPayload{Counter: counter.DefaultCounter, Delta: 1})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := a.AddCell(cellar.CellRequest{HandlerName: handlers.HandlerExampleIncrement, Payload: raw}); err != nil {
		t.Fatalf("add cell: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	mustTable(t, db, "cells")
	mustTable(t, db, "app_counters")
	mustTable(t, db, "firebase_idempotency_records")

	var cells int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cells`).Scan(&cells); err != nil {
		t.Fatalf("count cells: %v", err)
	}
	if cells != 1 {
		t.Fatalf("expected one cellar row in shared db, got %d", cells)
	}
}

func TestApplicationWorkAtomicWithCellCompletionRollbackOnFailure(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "atomic.db")
	a := mustNewApp(t, dbPath, firebaseidempotency.NewMemoryRemote(true), false, nil)
	defer a.Close()

	raw, err := cellar.JSONCodec[handlers.IncrementPayload]().Marshal(handlers.IncrementPayload{Counter: counter.DefaultCounter, Delta: 5})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := a.AddCell(cellar.CellRequest{HandlerName: handlers.HandlerExampleIncrement, Payload: raw}); err != nil {
		t.Fatalf("add cell: %v", err)
	}

	store := a.CellarStore()
	claimed, ok, err := store.ClaimNext(time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("claim next failed: ok=%v err=%v", ok, err)
	}

	err = store.Complete(claimed.ID, []cellar.CellRequest{{HandlerName: ""}}, func(tx cellar.ApplicationTx) error {
		return tx.Exec(`UPDATE app_counters SET value = value + 5 WHERE name = ?`, counter.DefaultCounter)
	})
	if err == nil {
		t.Fatal("expected complete to fail")
	}

	value, err := a.CounterValue(context.Background(), counter.DefaultCounter)
	if err != nil {
		t.Fatalf("counter value: %v", err)
	}
	if value != 0 {
		t.Fatalf("application work must roll back with failed completion, got %d", value)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 || active[0].ID != claimed.ID || active[0].State != cellar.CellStateClaimed {
		t.Fatalf("expected claimed parent cell to remain after rollback, got %+v", active)
	}
}

func TestCellFanoutCreatesMultipleReplacementCellsAtomically(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "fanout.db")
	a := mustNewApp(t, dbPath, firebaseidempotency.NewMemoryRemote(true), false, nil)
	defer a.Close()

	raw, err := cellar.JSONCodec[handlers.FanoutPayload]().Marshal(handlers.FanoutPayload{
		Counter:  counter.DefaultCounter,
		Children: 2,
		Delta:    1,
	})
	if err != nil {
		t.Fatalf("marshal fanout payload: %v", err)
	}
	if err := a.AddCell(cellar.CellRequest{HandlerName: handlers.HandlerExampleFanout, Payload: raw}); err != nil {
		t.Fatalf("add fanout cell: %v", err)
	}

	runFor(t, a, 300*time.Millisecond)

	value, err := a.CounterValue(context.Background(), counter.DefaultCounter)
	if err != nil {
		t.Fatalf("counter value: %v", err)
	}
	if value != 2 {
		t.Fatalf("expected two fanout children to increment counter, got %d", value)
	}

	active, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active cells after fanout slice, got %d", len(active))
	}
}

func TestIdempotencyPendingToPresent(t *testing.T) {
	t.Parallel()

	remote := firebaseidempotency.NewMemoryRemote(true)
	remote.SeedExisting("listener-a", "event-1", true)
	a := mustNewApp(t, filepath.Join(t.TempDir(), "idem-pending-present.db"), remote, false, nil)
	defer a.Close()

	if err := enqueuePending(t, a, "listener-a", "event-1", true); err != nil {
		t.Fatalf("enqueue pending: %v", err)
	}

	runFor(t, a, 250*time.Millisecond)

	state, ok, err := a.IdempotencyState(context.Background(), "listener-a", "event-1")
	if err != nil {
		t.Fatalf("idempotency state: %v", err)
	}
	if !ok || state != firebaseidempotency.StatePresent {
		t.Fatalf("expected PRESENT state, got ok=%v state=%s", ok, state)
	}

	value, err := a.CounterValue(context.Background(), counter.DefaultCounter)
	if err != nil {
		t.Fatalf("counter value: %v", err)
	}
	if value != 0 {
		t.Fatalf("pending->present should not fanout handler work, got counter=%d", value)
	}
}

func TestIdempotencyPendingPushCheckPresentWithFanout(t *testing.T) {
	t.Parallel()

	remote := firebaseidempotency.NewMemoryRemote(true)
	a := mustNewApp(t, filepath.Join(t.TempDir(), "idem-flow.db"), remote, false, nil)
	defer a.Close()

	if err := enqueuePending(t, a, "listener-b", "event-2", true); err != nil {
		t.Fatalf("enqueue pending: %v", err)
	}

	runFor(t, a, 500*time.Millisecond)

	state, ok, err := a.IdempotencyState(context.Background(), "listener-b", "event-2")
	if err != nil {
		t.Fatalf("idempotency state: %v", err)
	}
	if !ok || state != firebaseidempotency.StatePresent {
		t.Fatalf("expected PRESENT state, got ok=%v state=%s", ok, state)
	}

	value, err := a.CounterValue(context.Background(), counter.DefaultCounter)
	if err != nil {
		t.Fatalf("counter value: %v", err)
	}
	if value != 1 {
		t.Fatalf("expected exactly one fanout increment, got %d", value)
	}
}

func TestIdempotencyPushedPushCheckPath(t *testing.T) {
	t.Parallel()

	remote := firebaseidempotency.NewMemoryRemote(false)
	a := mustNewApp(t, filepath.Join(t.TempDir(), "idem-pushed.db"), remote, false, nil)
	defer a.Close()

	if err := a.ForceIdempotencyState(context.Background(), "listener-c", "event-3", firebaseidempotency.StatePushed); err != nil {
		t.Fatalf("seed pushed state: %v", err)
	}

	checkTarget, err := incrementFanoutTarget()
	if err != nil {
		t.Fatalf("fanout target: %v", err)
	}
	raw, err := cellar.JSONCodec[firebaseidempotency.PushPayload]().Marshal(firebaseidempotency.PushPayload{
		Listener: "listener-c",
		EventKey: "event-3",
		Fanout:   []firebaseidempotency.FanoutTarget{checkTarget},
	})
	if err != nil {
		t.Fatalf("marshal push payload: %v", err)
	}
	if err := a.AddCell(cellar.CellRequest{HandlerName: firebaseidempotency.HandlerPush, Payload: raw}); err != nil {
		t.Fatalf("add push cell: %v", err)
	}

	runFor(t, a, 300*time.Millisecond)

	if remote.CreateCallCount("listener-c", "event-3") == 0 {
		t.Fatal("expected pushed->push path to attempt remote create")
	}

	state, ok, err := a.IdempotencyState(context.Background(), "listener-c", "event-3")
	if err != nil {
		t.Fatalf("idempotency state: %v", err)
	}
	if !ok || state != firebaseidempotency.StatePushed {
		t.Fatalf("expected state to remain PUSHED until visible, got ok=%v state=%s", ok, state)
	}

	active, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if countByHandler(active, firebaseidempotency.HandlerCheck) == 0 {
		t.Fatal("expected a check cell to remain scheduled/retrying")
	}
}

func TestIdempotencyPresentNoWork(t *testing.T) {
	t.Parallel()

	remote := firebaseidempotency.NewMemoryRemote(true)
	a := mustNewApp(t, filepath.Join(t.TempDir(), "idem-present.db"), remote, false, nil)
	defer a.Close()

	if err := a.ForceIdempotencyState(context.Background(), "listener-d", "event-4", firebaseidempotency.StatePresent); err != nil {
		t.Fatalf("seed present state: %v", err)
	}

	raw, err := cellar.JSONCodec[firebaseidempotency.PushPayload]().Marshal(firebaseidempotency.PushPayload{
		Listener: "listener-d",
		EventKey: "event-4",
	})
	if err != nil {
		t.Fatalf("marshal push payload: %v", err)
	}
	if err := a.AddCell(cellar.CellRequest{HandlerName: firebaseidempotency.HandlerPush, Payload: raw}); err != nil {
		t.Fatalf("add push cell: %v", err)
	}

	runFor(t, a, 200*time.Millisecond)

	if remote.CreateCallCount("listener-d", "event-4") != 0 {
		t.Fatal("present state should not issue remote push")
	}
}

func TestDuplicateCheckCreatesExactlyOneFanoutCell(t *testing.T) {
	t.Parallel()

	remote := firebaseidempotency.NewMemoryRemote(true)
	remote.SeedExisting("listener-race", "event-5", true)
	a := mustNewApp(t, filepath.Join(t.TempDir(), "idem-race.db"), remote, false, nil)
	defer a.Close()

	if err := a.ForceIdempotencyState(context.Background(), "listener-race", "event-5", firebaseidempotency.StatePushed); err != nil {
		t.Fatalf("seed pushed state: %v", err)
	}

	fanoutTarget, err := incrementFanoutTarget()
	if err != nil {
		t.Fatalf("fanout target: %v", err)
	}
	payload := firebaseidempotency.CheckPayload{
		Listener: "listener-race",
		EventKey: "event-5",
		Fanout:   []firebaseidempotency.FanoutTarget{fanoutTarget},
	}
	raw, err := cellar.JSONCodec[firebaseidempotency.CheckPayload]().Marshal(payload)
	if err != nil {
		t.Fatalf("marshal check payload: %v", err)
	}

	store := a.CellarStore()
	for i := 0; i < 2; i++ {
		if _, err := store.Add([]cellar.CellRequest{{HandlerName: firebaseidempotency.HandlerCheck, Payload: raw}}); err != nil {
			t.Fatalf("add check cell %d: %v", i, err)
		}
	}

	first, ok, err := store.ClaimNext(time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("claim first check: ok=%v err=%v", ok, err)
	}
	second, ok, err := store.ClaimNext(time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("claim second check: ok=%v err=%v", ok, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		a.Worker().Run(context.Background(), first)
	}()
	go func() {
		defer wg.Done()
		a.Worker().Run(context.Background(), second)
	}()
	wg.Wait()

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if countByHandler(active, handlers.HandlerExampleIncrement) != 1 {
		t.Fatalf("expected exactly one fanout increment cell, got %d", countByHandler(active, handlers.HandlerExampleIncrement))
	}
}

func TestStartupFailureBlocksListeners(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "startup-fail.db")
	_, err := app.New(app.Config{
		DBPath:                dbPath,
		PollDelay:             5 * time.Millisecond,
		Logger:                testLogger(),
		IdempotencyRemote:     firebaseidempotency.NewMemoryRemote(true),
		EnableExampleListener: true,
		StartupComponentChecks: []func(*basestore.Store) error{
			func(*basestore.Store) error { return errors.New("synthetic startup failure") },
		},
	})
	if err == nil {
		t.Fatal("expected startup failure")
	}

	db, openErr := sql.Open("sqlite", dbPath)
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer db.Close()

	mustTable(t, db, "cells")
	var cells int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cells`).Scan(&cells); err != nil {
		t.Fatalf("count cells: %v", err)
	}
	if cells != 0 {
		t.Fatalf("expected no listener-created cells after startup failure, got %d", cells)
	}
}

func TestRestartRecoversClaimedCells(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "restart.db")
	a1 := mustNewApp(t, dbPath, firebaseidempotency.NewMemoryRemote(true), false, nil)

	raw, err := cellar.JSONCodec[handlers.IncrementPayload]().Marshal(handlers.IncrementPayload{Counter: counter.DefaultCounter, Delta: 1})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := a1.AddCell(cellar.CellRequest{HandlerName: handlers.HandlerExampleIncrement, Payload: raw}); err != nil {
		t.Fatalf("add cell: %v", err)
	}

	claimed, ok, err := a1.CellarStore().ClaimNext(time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("claim cell before crash simulation: ok=%v err=%v", ok, err)
	}
	if claimed.State != cellar.CellStateClaimed {
		t.Fatalf("expected claimed state, got %s", claimed.State)
	}
	if err := a1.Close(); err != nil {
		t.Fatalf("close first app: %v", err)
	}

	a2 := mustNewApp(t, dbPath, firebaseidempotency.NewMemoryRemote(true), false, nil)
	defer a2.Close()

	runFor(t, a2, 300*time.Millisecond)

	value, err := a2.CounterValue(context.Background(), counter.DefaultCounter)
	if err != nil {
		t.Fatalf("counter value: %v", err)
	}
	if value != 1 {
		t.Fatalf("expected recovered claimed cell to execute once, got %d", value)
	}
}

func mustNewApp(
	t *testing.T,
	dbPath string,
	remote firebaseidempotency.Remote,
	enableExampleListener bool,
	startupChecks []func(*basestore.Store) error,
) *app.App {
	t.Helper()

	a, err := app.New(app.Config{
		DBPath:                dbPath,
		PollDelay:             5 * time.Millisecond,
		Logger:                testLogger(),
		IdempotencyRemote:     remote,
		EnableExampleListener: enableExampleListener,
		StartupComponentChecks: startupChecks,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return a
}

func runFor(t *testing.T, application *app.App, dur time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	if err := application.Run(ctx); err != nil {
		t.Fatalf("run app: %v", err)
	}
}

func enqueuePending(t *testing.T, application *app.App, listener, eventKey string, withIncrementFanout bool) error {
	t.Helper()
	targets := make([]firebaseidempotency.FanoutTarget, 0)
	if withIncrementFanout {
		target, err := incrementFanoutTarget()
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}
	payload := firebaseidempotency.PendingPayload{Listener: listener, EventKey: eventKey, Fanout: targets}
	raw, err := cellar.JSONCodec[firebaseidempotency.PendingPayload]().Marshal(payload)
	if err != nil {
		return err
	}
	return application.AddCell(cellar.CellRequest{HandlerName: firebaseidempotency.HandlerPending, Payload: raw})
}

func incrementFanoutTarget() (firebaseidempotency.FanoutTarget, error) {
	raw, err := cellar.JSONCodec[handlers.IncrementPayload]().Marshal(handlers.IncrementPayload{
		Counter: counter.DefaultCounter,
		Delta:   1,
	})
	if err != nil {
		return firebaseidempotency.FanoutTarget{}, err
	}
	return firebaseidempotency.FanoutTarget{HandlerName: handlers.HandlerExampleIncrement, Payload: raw}, nil
}

func countByHandler(cells []cellar.Cell, name cellar.HandlerName) int {
	count := 0
	for _, cell := range cells {
		if cell.HandlerName == name {
			count++
		}
	}
	return count
}

func mustTable(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&name)
	if err != nil {
		t.Fatalf("expected table %s: %v", tableName, err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
