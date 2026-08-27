package app_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/plugins/polls"

	_ "modernc.org/sqlite"
)

func TestDatabaseOwnershipSingleSQLiteForAppAndCellar(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "backend.db")
	a := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-1"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	mustTable(t, db, "cells")
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
	a := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-atomic"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	store := a.CellarStore()
	claimed, ok, err := store.ClaimNext(time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("claim next failed: ok=%v err=%v", ok, err)
	}

	err = store.Complete(claimed.ID, []cellar.CellRequest{{}}, func(tx cellar.ApplicationTx) error {
		return tx.Exec(`INSERT INTO firebase_idempotency_records(listener, event_key) VALUES(?, ?)`, "rollback-listener", "rollback-key")
	})
	if err == nil {
		t.Fatal("expected complete to fail")
	}

	exists, err := a.IdempotencyClaimed(context.Background(), "rollback-listener", "rollback-key")
	if err != nil {
		t.Fatalf("idempotency state: %v", err)
	}
	if exists {
		t.Fatal("application work must roll back with failed completion")
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 || active[0].ID != claimed.ID || active[0].State != cellar.CellStateClaimed {
		t.Fatalf("expected claimed parent cell to remain after rollback, got %+v", active)
	}
}

func TestFactFanoutDeliversToRegisteredPollHandler(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	dbPath := filepath.Join(t.TempDir(), "fanout.db")
	a := mustNewAppWithLogger(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil, &output)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-fanout"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	runFor(t, a, 300*time.Millisecond)

	logged := output.String()
	if !strings.Contains(logged, "new poll processed") || !strings.Contains(logged, "poll-fanout") {
		t.Fatalf("expected fact to be delivered to the registered poll handler, got log: %s", logged)
	}

	active, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active cells after fact delivery, got %d", len(active))
	}
}

func TestIdempotencyDuplicateObservationSuppressed(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	dbPath := filepath.Join(t.TempDir(), "idem-duplicate.db")
	a := mustNewAppWithLogger(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil, &output)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-dup"); err != nil {
		t.Fatalf("enqueue new poll (1st): %v", err)
	}
	if err := enqueueNewPoll(t, a, "poll-dup"); err != nil {
		t.Fatalf("enqueue new poll (2nd): %v", err)
	}

	runFor(t, a, 300*time.Millisecond)

	logged := output.String()
	if count := strings.Count(logged, "new poll processed"); count != 1 {
		t.Fatalf("expected the duplicate observation to be suppressed, got %d deliveries: %s", count, logged)
	}
}

func TestIdempotencyObservedRemoteDoesNotEmitFact(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	remote := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
	remote.SeedExisting("NewPoll", "poll-observed", true)
	dbPath := filepath.Join(t.TempDir(), "idem-observed.db")
	a := mustNewAppWithLogger(t, dbPath, remote, nil, &output)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-observed"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	runFor(t, a, 300*time.Millisecond)

	if strings.Contains(output.String(), "new poll processed") {
		t.Fatal("a Fact already established remotely must not be re-emitted")
	}

	exists, err := a.IdempotencyClaimed(context.Background(), "NewPoll", "poll-observed")
	if err != nil {
		t.Fatalf("idempotency state: %v", err)
	}
	if !exists {
		t.Fatalf("expected identity cached after observing remote establishment, got exists=%v", exists)
	}

	active, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected the sequence to terminate immediately at Step 1, got %d active cells", len(active))
	}
}

func TestNewRequiresIdempotencyRemoteWhenFirestoreDisabled(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "no-remote.db")
	_, err := app.New(app.Config{
		DBPath:    dbPath,
		PollDelay: 5 * time.Millisecond,
		Logger:    testLogger(),
	})
	if err == nil {
		t.Fatal("expected app.New to fail fast without a durable idempotency remote")
	}
}

func TestStartupFailureBlocksListeners(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "startup-fail.db")
	_, err := app.New(app.Config{
		DBPath:            dbPath,
		PollDelay:         5 * time.Millisecond,
		Logger:            testLogger(),
		IdempotencyRemote: firebaseidempotencytest.NewInMemoryRemoteStandIn(true),
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
	a1 := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)

	if err := enqueueNewPoll(t, a1, "poll-restart"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
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

	a2 := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)
	defer a2.Close()

	runFor(t, a2, 300*time.Millisecond)

	active, err := a2.CellarStore().ListActive()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected recovered claimed cell to be fully processed, got %d active cells", len(active))
	}
}

func TestCloseStopsSchedulerBeforeClosingSQLite(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "shutdown.db")
	a := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Run(runCtx)
	}()

	time.Sleep(60 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after close")
	}
}

func mustNewApp(
	t *testing.T,
	dbPath string,
	remote firebaseidempotency.Remote,
	startupChecks []func(*basestore.Store) error,
) *app.App {
	t.Helper()
	return mustNewAppWithLogger(t, dbPath, remote, startupChecks, nil)
}

func mustNewAppWithLogger(
	t *testing.T,
	dbPath string,
	remote firebaseidempotency.Remote,
	startupChecks []func(*basestore.Store) error,
	logOutput io.Writer,
) *app.App {
	t.Helper()

	logger := testLogger()
	if logOutput != nil {
		logger = slog.New(slog.NewJSONHandler(logOutput, nil))
	}

	a, err := app.New(app.Config{
		DBPath:                 dbPath,
		PollDelay:              5 * time.Millisecond,
		Logger:                 logger,
		IdempotencyRemote:      remote,
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

func enqueueNewPoll(t *testing.T, application *app.App, pollID string) error {
	t.Helper()
	payload, err := cellar.JSONCodec[polls.PollObservedPayload]().Marshal(polls.PollObservedPayload{PollID: pollID})
	if err != nil {
		return err
	}
	request, err := firebaseidempotency.NewCellRequest("NewPoll", pollID, facts.Fact{Name: polls.FactNewPoll, Payload: payload})
	if err != nil {
		return err
	}
	return application.AddCell(request)
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
