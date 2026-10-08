package app_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/notificationprofile"
)

// neverReadySource models Firestore not delivering an initial snapshot.
type neverReadySource struct{}

func (neverReadySource) WatchUsers(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return blockedStream{ctx: ctx}, nil
}

func (neverReadySource) WatchEndpoints(ctx context.Context) (notificationprofile.ChangeStream, error) {
	return blockedStream{ctx: ctx}, nil
}

func (neverReadySource) DeactivateEndpoint(context.Context, string, string) error { return nil }

type blockedStream struct{ ctx context.Context }

func (s blockedStream) Next() ([]notificationprofile.Change, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (blockedStream) Stop() {}

func newApp(t *testing.T, cfg app.Config) *app.App {
	t.Helper()
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return a
}

func TestRunWaitsForNotificationProfileBeforeProcessingWork(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, filepath.Join(t.TempDir(), "not-ready.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = testLogger()
	cfg.NotificationProfileSource = neverReadySource{}
	a := newApp(t, cfg)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-waiting"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 1 {
		t.Fatalf("active work cells = %d, want the queued cell untouched before readiness", len(active))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after cancellation while waiting for readiness")
	}
}

func TestCloseReleasesRunWaitingForNotificationProfile(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, filepath.Join(t.TempDir(), "close-not-ready.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = testLogger()
	cfg.NotificationProfileSource = neverReadySource{}
	a := newApp(t, cfg)

	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background()) }()

	time.Sleep(50 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after close while waiting for readiness")
	}
}
