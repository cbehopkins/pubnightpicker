package durableemail

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
)

func TestVerifyMarksFoundRecipientsAccepted(t *testing.T) {
	db := openTestDB(t)
	cellarStore, store := seedSubmittedRecipient(t, db, "alice@example.com")

	verifier := &stubVerifier{found: map[string]string{"alice@example.com": "pmuid-1"}}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier}); err != nil {
		t.Fatalf("register verify handler: %v", err)
	}
	if _, err := runtime.Add(HandlerVerify, operationRequest{IdempotencyToken: "send-1"}); err != nil {
		t.Fatalf("add verify cell: %v", err)
	}

	stop := startRuntime(t, runtime)
	waitForIdle(t, cellarStore)
	stop()

	state, pmuid, _ := progressRow(t, db, "send-1", "alice@example.com")
	if state != StateAccepted {
		t.Errorf("state = %q, want %q", state, StateAccepted)
	}
	if pmuid.String != "pmuid-1" {
		t.Errorf("pmuid = %q, want %q", pmuid.String, "pmuid-1")
	}
}

func TestVerifyRetriesLaterWhenRecipientIsNotFound(t *testing.T) {
	db := openTestDB(t)
	cellarStore, store := seedSubmittedRecipient(t, db, "alice@example.com")

	verifier := &stubVerifier{}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier}); err != nil {
		t.Fatalf("register verify handler: %v", err)
	}
	if _, err := runtime.Add(HandlerVerify, operationRequest{IdempotencyToken: "send-1"}); err != nil {
		t.Fatalf("add verify cell: %v", err)
	}

	stop := startRuntime(t, runtime)
	notBefore := waitForScheduledRetry(t, cellarStore)
	stop()

	if requests := verifier.requests.Load(); requests == 0 {
		t.Fatal("verifier was never called")
	}
	if delay := time.Until(notBefore); delay <= 0 || delay > VerifyDelay {
		t.Errorf("retry delay = %v, want a positive delay no greater than %v", delay, VerifyDelay)
	}

	state, pmuid, _ := progressRow(t, db, "send-1", "alice@example.com")
	if state != StatePending {
		t.Errorf("state = %q, want %q", state, StatePending)
	}
	if pmuid.Valid {
		t.Errorf("pmuid = %q, want NULL while unverified", pmuid.String)
	}
}

func seedSubmittedRecipient(t *testing.T, db *sql.DB, recipient string) (*cellarsqlite.Store, *Store) {
	t.Helper()
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatalf("initialise Cellar store: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("initialise durable email store: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO email_requests (
			idempotency_token, message_id, sender_email, sender_name, subject,
			template_id, text, variables, headers
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "send-1", "message-1", "sender@example.com", "Sender", "Pub night", "", "Hello", `{}`, `{}`); err != nil {
		t.Fatalf("seed request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO email_progress (
			idempotency_token, recipient, recipient_name, state, variables, submitted_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, "send-1", recipient, "Alice", StatePending, `{}`, time.Now().UTC()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	return cellarStore, store
}

func startRuntime(t *testing.T, runtime *cellar.Cellar) func() {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- runtime.Start(context.Background()) }()

	var stopped bool
	return func() {
		if stopped {
			return
		}
		stopped = true
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop runtime: %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("run runtime: %v", err)
		}
	}
}

// waitForScheduledRetry returns the NotBefore of the cell once it is rescheduled.
func waitForScheduledRetry(t *testing.T, store cellar.Store) time.Time {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		active, err := store.ListActive()
		if err != nil {
			t.Fatalf("list active cells: %v", err)
		}
		for _, cell := range active {
			if cell.NotBefore != nil {
				return *cell.NotBefore
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for verification to be rescheduled")
	return time.Time{}
}
