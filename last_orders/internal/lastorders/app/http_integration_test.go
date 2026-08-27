package app_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
)

func TestLogEndpointEndToEnd(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/http-log.db"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	a, err := app.New(app.Config{
		DBPath:            dbPath,
		PollDelay:         5 * time.Millisecond,
		Logger:            logger,
		IdempotencyRemote: firebaseidempotencytest.NewInMemoryRemoteStandIn(true),
		HTTPAddr:          "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.Close()

	if a.HTTPAddr() == "" {
		t.Fatal("expected HTTP to be enabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- a.Run(ctx) }()

	postLog(t, a.HTTPAddr(), "evt-1", "hello world")
	waitForLogLine(t, &output, "hello world")

	// A duplicate event_id must be suppressed by idempotency, not logged twice.
	postLog(t, a.HTTPAddr(), "evt-1", "hello world")
	time.Sleep(100 * time.Millisecond)
	if count := strings.Count(output.String(), "hello world"); count != 1 {
		t.Fatalf("expected duplicate event_id to be suppressed, got %d log entries", count)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

func postLog(t *testing.T, addr, eventID, message string) {
	t.Helper()
	body := `{"event_id":"` + eventID + `","message":"` + message + `"}`
	resp, err := http.Post("http://"+addr+"/log", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post /log: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
}

func waitForLogLine(t *testing.T, output *bytes.Buffer, substr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), substr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log line containing %q, got: %s", substr, output.String())
}
