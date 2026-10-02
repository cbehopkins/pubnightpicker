package durableemail

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
	"email_clients/clients/mailtrap"
	"email_clients/clients/sweego"
	"email_clients/clients/sweego/logs"

	sdk "github.com/mailtrap/mailtrap-go"
)

func TestVerifyWithMailtrapLogs(t *testing.T) {
	db := openTestDB(t)
	cellarStore, store := seedSubmittedRecipient(t, db, "alice@example.com")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/email_logs" ||
			request.URL.Query().Get("filters[to][value]") != "alice@example.com" {
			t.Errorf("provider query = %s %s", request.Method, request.URL)
		}
		_ = json.NewEncoder(w).Encode(sdk.EmailLogsList{Messages: []*sdk.EmailLogMessage{{
			MessageID: "uid-a", To: "alice@example.com",
			CustomVariables: map[string]any{mailtrap.CorrelationVariable: "message-1"},
		}}})
	}))
	t.Cleanup(server.Close)
	sdkClient, err := sdk.NewClient("test-token", sdk.WithHTTPClient(&http.Client{Timeout: time.Second}), sdk.WithBaseURL(sdk.HostGeneral, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	client, err := mailtrap.NewClient(sdkClient)
	if err != nil {
		t.Fatal(err)
	}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: mailtrap.NewVerifier(client, time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Add(HandlerVerify, verifyRequest{IdempotencyToken: "send-1", Recipient: "alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	stop := startRuntime(t, runtime)
	waitForIdle(t, cellarStore)
	stop()

	state, pmuid, _ := progressRow(t, db, "send-1", "alice@example.com")
	if state != StateAccepted || pmuid.String != "uid-a" {
		t.Errorf("state = %q, PMUID = %q; want Accepted, uid-a", state, pmuid.String)
	}
}

func TestVerifyWithSweegoLogs(t *testing.T) {
	db := openTestDB(t)
	cellarStore, store := seedSubmittedRecipient(t, db, "alice@example.com")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/logs/" {
			t.Errorf("provider request = %s %s, want POST /logs/", request.Method, request.URL.Path)
		}
		var query logs.Request
		if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
			t.Errorf("decode logs query: %v", err)
		}
		if query.SearchWord != "alice@example.com" {
			t.Errorf("logs search word = %q", query.SearchWord)
		}
		_ = json.NewEncoder(w).Encode(logs.Response{Result: []logs.Record{{
			SwgUID: "uid-a", Headers: map[string]any{"x-pubnight-message-id": "message-1"},
		}}})
	}))
	t.Cleanup(server.Close)

	client := sweego.NewClient(server.URL, "token", time.Second)
	verifier := logs.NewVerifier(logs.NewClient(client), time.Minute)
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Add(HandlerVerify, verifyRequest{IdempotencyToken: "send-1", Recipient: "alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	stop := startRuntime(t, runtime)
	waitForIdle(t, cellarStore)
	stop()

	state, pmuid, _ := progressRow(t, db, "send-1", "alice@example.com")
	if state != StateAccepted || pmuid.String != "uid-a" {
		t.Errorf("state = %q, PMUID = %q; want Accepted, uid-a", state, pmuid.String)
	}
}

func TestVerifyMarksFoundRecipientsAccepted(t *testing.T) {
	db := openTestDB(t)
	cellarStore, store := seedSubmittedRecipient(t, db, "alice@example.com")

	verifier := &stubVerifier{found: map[string]string{"alice@example.com": "pmuid-1"}}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier}); err != nil {
		t.Fatalf("register verify handler: %v", err)
	}
	if _, err := runtime.Add(HandlerVerify, verifyRequest{IdempotencyToken: "send-1", Recipient: "alice@example.com"}); err != nil {
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

func TestVerifyReturnsRecipientToPendingWhenNotFound(t *testing.T) {
	db := openTestDB(t)
	cellarStore, store := seedSubmittedRecipient(t, db, "alice@example.com")

	verifier := &stubVerifier{}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier}); err != nil {
		t.Fatalf("register verify handler: %v", err)
	}
	if _, err := runtime.Add(HandlerVerify, verifyRequest{IdempotencyToken: "send-1", Recipient: "alice@example.com"}); err != nil {
		t.Fatalf("add verify cell: %v", err)
	}

	stop := startRuntime(t, runtime)
	waitForIdle(t, cellarStore)
	stop()

	if requests := verifier.requests.Load(); requests != 1 {
		t.Errorf("verification requests = %d, want 1", requests)
	}

	state, pmuid, submittedAt := progressRow(t, db, "send-1", "alice@example.com")
	if state != StatePending {
		t.Errorf("state = %q, want %q", state, StatePending)
	}
	if pmuid.Valid {
		t.Errorf("pmuid = %q, want NULL while unverified", pmuid.String)
	}
	if submittedAt.Valid {
		t.Errorf("submitted_at = %v, want NULL before resubmission", submittedAt.Time)
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
	`, "send-1", recipient, "Alice", StateRecoveryWaiting, `{}`, time.Now().UTC()); err != nil {
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

func waitForRecoveryFanout(t *testing.T, store cellar.Store) time.Time {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		active, err := store.ListActive()
		if err != nil {
			t.Fatalf("list active cells: %v", err)
		}
		for _, cell := range active {
			if cell.Steps[cell.CurrentStep].HandlerName == HandlerRecoveryFanout && cell.NotBefore != nil {
				return *cell.NotBefore
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for recovery fanout")
	return time.Time{}
}
