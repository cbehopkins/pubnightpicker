package durableemail

import (
	"context"
	"database/sql"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
	"email_clients/clients"
	"email_clients/clients/dummy"
)

type sentEmail struct {
	recipient string
	message   string
	headers   map[string]string
}

// countingClient records how many provider requests the handler issued.
type countingClient struct {
	inner clients.EmailClient
	calls atomic.Int64
}

func (c *countingClient) Send(ctx context.Context, email clients.Email) (clients.SendResult, error) {
	c.calls.Add(1)
	return c.inner.Send(ctx, email)
}

// stubVerifier answers verification from a fixed set of found recipients.
type stubVerifier struct {
	found    map[string]string
	requests atomic.Int64
	err      error
}

func (v *stubVerifier) Verify(ctx context.Context, request clients.VerifyRequest) (clients.VerifyResult, error) {
	v.requests.Add(1)
	if v.err != nil {
		return clients.VerifyResult{}, v.err
	}
	if pmuid, ok := v.found[request.Recipient]; ok {
		return clients.VerifyResult{Found: true, PMUID: pmuid}, nil
	}
	return clients.VerifyResult{}, nil
}

func TestSendSequenceDeliversAndRecordsAcceptance(t *testing.T) {
	db := openTestDB(t)

	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatalf("initialise Cellar store: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("initialise durable email store: %v", err)
	}

	sent := make(chan sentEmail, 4)
	client := &countingClient{inner: dummy.NewClient(func(emailAddress, message string, headers map[string]string) (dummy.Response, error) {
		sent <- sentEmail{recipient: emailAddress, message: message, headers: headers}
		return dummy.Response{}, nil
	})}

	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	if err := runtime.Register(HandlerSetup, SetupHandler{Store: store}); err != nil {
		t.Fatalf("register setup handler: %v", err)
	}
	if err := runtime.Register(HandlerPost, PostHandler{Store: store, Client: client}); err != nil {
		t.Fatalf("register post handler: %v", err)
	}
	verifier := &stubVerifier{}
	if err := runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier}); err != nil {
		t.Fatalf("register verify handler: %v", err)
	}

	request := SendRequest{
		IdempotencyToken: "send-1",
		SenderEmail:      "sender@example.com",
		SenderName:       "Sender",
		Subject:          "Pub night",
		Text:             "Hello",
		Variables:        map[string]any{"event": "Friday"},
		Headers:          map[string]string{"X-Common": "value"},
		Recipients: []SendRecipient{
			{Email: "alice@example.com", Name: "Alice", Variables: map[string]any{"name": "Alice"}},
			{Email: "bob@example.com", Name: "Bob"},
		},
	}
	if _, err := runtime.AddSequence(NewSendSequence(request)...); err != nil {
		t.Fatalf("add send sequence: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- runtime.Start(context.Background()) }()

	delivered := make([]sentEmail, 0, len(request.Recipients))
	for range request.Recipients {
		select {
		case email := <-sent:
			delivered = append(delivered, email)
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for provider send")
		}
	}
	waitForIdle(t, cellarStore)

	if err := runtime.Stop(); err != nil {
		t.Fatalf("stop runtime: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("run runtime: %v", err)
	}

	select {
	case extra := <-sent:
		t.Fatalf("unexpected additional send to %q", extra.recipient)
	default:
	}

	if calls := client.calls.Load(); calls != 1 {
		t.Errorf("provider requests = %d, want 1 for a batched send", calls)
	}
	if requests := verifier.requests.Load(); requests != 0 {
		t.Errorf("verification requests = %d, want 0 when every recipient was accepted", requests)
	}

	sort.Slice(delivered, func(i, j int) bool { return delivered[i].recipient < delivered[j].recipient })
	if delivered[0].recipient != "alice@example.com" || delivered[1].recipient != "bob@example.com" {
		t.Fatalf("delivered recipients = %q and %q", delivered[0].recipient, delivered[1].recipient)
	}
	for _, email := range delivered {
		if email.message != "Hello" {
			t.Errorf("message for %q = %q, want %q", email.recipient, email.message, "Hello")
		}
		if email.headers["X-Common"] != "value" {
			t.Errorf("common header missing for %q", email.recipient)
		}
	}

	assertRequestRow(t, db, request)
	messageID := requestMessageID(t, db, request.IdempotencyToken)
	for _, email := range delivered {
		state, pmuid, submittedAt := progressRow(t, db, request.IdempotencyToken, email.recipient)
		if state != StateAccepted {
			t.Errorf("state for %q = %q, want %q", email.recipient, state, StateAccepted)
		}
		if !pmuid.Valid || pmuid.String == "" {
			t.Errorf("pmuid for %q is not recorded", email.recipient)
		}
		if !submittedAt.Valid {
			t.Errorf("submitted_at for %q is not recorded", email.recipient)
		}
		if email.headers[MessageIDHeader] != messageID {
			t.Errorf("%s for %q = %q, want %q", MessageIDHeader, email.recipient, email.headers[MessageIDHeader], messageID)
		}
	}
}

func requestMessageID(t *testing.T, db *sql.DB, token string) string {
	t.Helper()
	var messageID string
	if err := db.QueryRow(`SELECT message_id FROM email_requests WHERE idempotency_token = ?`, token).Scan(&messageID); err != nil {
		t.Fatalf("read request message ID: %v", err)
	}
	if messageID == "" {
		t.Fatal("request message ID is empty")
	}
	return messageID
}

func waitForIdle(t *testing.T, store cellar.Store) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		active, err := store.ListActive()
		if err != nil {
			t.Fatalf("list active cells: %v", err)
		}
		if len(active) == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for the send sequence to finish")
}

func assertRequestRow(t *testing.T, db *sql.DB, request SendRequest) {
	t.Helper()
	var senderEmail, senderName, subject, templateID, text, variables, headers string
	if err := db.QueryRow(`
		SELECT sender_email, sender_name, subject, template_id, text, variables, headers
		FROM email_requests
		WHERE idempotency_token = ?
	`, request.IdempotencyToken).Scan(&senderEmail, &senderName, &subject, &templateID, &text, &variables, &headers); err != nil {
		t.Fatalf("read request row: %v", err)
	}
	if senderEmail != request.SenderEmail || senderName != request.SenderName || subject != request.Subject {
		t.Errorf("unexpected sender or subject: %q %q %q", senderEmail, senderName, subject)
	}
	if templateID != "" || text != request.Text {
		t.Errorf("template_id = %q, text = %q", templateID, text)
	}
	if variables != `{"event":"Friday"}` || headers != `{"X-Common":"value"}` {
		t.Errorf("variables = %q, headers = %q", variables, headers)
	}
}

func progressRow(t *testing.T, db *sql.DB, token, recipient string) (state string, pmuid sql.NullString, submittedAt sql.NullTime) {
	t.Helper()
	if err := db.QueryRow(`
		SELECT state, pmuid, submitted_at
		FROM email_progress
		WHERE idempotency_token = ? AND recipient = ?
	`, token, recipient).Scan(&state, &pmuid, &submittedAt); err != nil {
		t.Fatalf("read progress row for %q: %v", recipient, err)
	}
	return state, pmuid, submittedAt
}
