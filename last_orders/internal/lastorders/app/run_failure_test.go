package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// failingRemote makes the idempotency Check step return an ErrorResult, which is
// the same shape as any handler failing against a live backend.
type failingRemote struct {
	err error
}

func (r failingRemote) CreateKey(context.Context, string, string) (bool, error) {
	return false, r.err
}

func (r failingRemote) HasKey(context.Context, string, string) (bool, error) {
	return false, r.err
}

// A single failed dispatch stops Cellar's scheduler, so Run tears the whole
// application down while its caller's context is still live.
func TestSingleHandlerFailureStopsEntireRuntime(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("backend unavailable")
	dbPath := filepath.Join(t.TempDir(), "handler-failure.db")
	a := mustNewApp(t, dbPath, failingRemote{err: handlerErr}, nil)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-failure"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- a.Run(ctx)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil; expected the dispatch failure to surface")
		}
		if !errors.Is(err, handlerErr) {
			t.Fatalf("Run error = %v; want it to wrap %v", err, handlerErr)
		}
		if ctx.Err() != nil {
			t.Fatalf("caller context was cancelled after %s; the app should have stopped on its own", time.Since(start))
		}
		t.Logf("runtime stopped after %s with: %v", time.Since(start), err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a handler failure")
	}
}
