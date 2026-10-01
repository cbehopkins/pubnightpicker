package durableemail

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
	"email_clients/clients"
	"email_clients/clients/dummy"
	"email_clients/clients/sweego"
	"email_clients/clients/sweego/logs"
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

type sendClientFunc func(context.Context, clients.Email) (clients.SendResult, error)

func (send sendClientFunc) Send(ctx context.Context, email clients.Email) (clients.SendResult, error) {
	return send(ctx, email)
}

type verifyClientFunc func(context.Context, clients.VerifyRequest) (clients.VerifyResult, error)

func (verify verifyClientFunc) Verify(ctx context.Context, request clients.VerifyRequest) (clients.VerifyResult, error) {
	return verify(ctx, request)
}

func (c *countingClient) Send(ctx context.Context, email clients.Email) (clients.SendResult, error) {
	c.calls.Add(1)
	return c.inner.Send(ctx, email)
}

func TestSendSequenceWithSweego(t *testing.T) {
	var calls atomic.Int32
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/send/bulk/email" {
			t.Errorf("provider request = %s %s, want POST /send/bulk/email", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&sent); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		_, _ = w.Write([]byte(`{"swg_uids":{"alice@example.com":"uid-a","bob@example.com":"uid-b"}}`))
	}))
	t.Cleanup(server.Close)

	db := openTestDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	client := sweego.NewClient(server.URL, "token", time.Second).WithSendOptions(sweego.SendOptions{Provider: "sender.example"})
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	registerSendHandlers(t, runtime, store, client, logs.NewVerifier(logs.NewClient(client), time.Minute))
	request := SendRequest{
		IdempotencyToken: "sweego-send",
		SenderEmail:      "sender@example.com",
		Subject:          "Pub night",
		Text:             "Hello",
		Variables:        map[string]any{"event": "Friday"},
		Recipients: []SendRecipient{
			{Email: "alice@example.com", Variables: map[string]any{"name": "Alice"}},
			{Email: "bob@example.com"},
		},
	}
	if _, err := runtime.AddSequence(NewSendSequence(request)...); err != nil {
		t.Fatal(err)
	}
	stop := startRuntime(t, runtime)
	waitForIdle(t, cellarStore)
	stop()

	if calls.Load() != 1 {
		t.Errorf("provider requests = %d, want 1", calls.Load())
	}
	headers, ok := sent["headers"].(map[string]any)
	if !ok || headers[MessageIDHeader] != requestMessageID(t, db, request.IdempotencyToken) {
		t.Errorf("provider headers = %v, want durable message ID", sent["headers"])
	}
	for recipient, want := range map[string]string{"alice@example.com": "uid-a", "bob@example.com": "uid-b"} {
		state, pmuid, _ := progressRow(t, db, request.IdempotencyToken, recipient)
		if state != StateAccepted || pmuid.String != want {
			t.Errorf("%s: state = %q, PMUID = %q; want Accepted, %q", recipient, state, pmuid.String, want)
		}
	}
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
	if err := runtime.Register(HandlerRecovery, RecoveryHandler{Store: store}); err != nil {
		t.Fatalf("register recovery handler: %v", err)
	}
	fanout, err := NewRecoveryFanout(store)
	if err != nil {
		t.Fatalf("create recovery fanout: %v", err)
	}
	if err := fanout.Register(runtime); err != nil {
		t.Fatalf("register recovery fanout: %v", err)
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

func TestPostErrorSchedulesDelayedRecovery(t *testing.T) {
	db := openTestDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	client := &countingClient{inner: sendClientFunc(func(context.Context, clients.Email) (clients.SendResult, error) {
		return clients.SendResult{}, errors.New("provider outcome unknown")
	})}
	verifier := &stubVerifier{}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	registerSendHandlers(t, runtime, store, client, verifier)
	request := SendRequest{
		IdempotencyToken: "send-1", SenderEmail: "sender@example.com", Subject: "Pub night",
		Recipients: []SendRecipient{{Email: "alice@example.com"}},
	}
	if _, err := runtime.AddSequence(NewSendSequence(request)...); err != nil {
		t.Fatal(err)
	}
	stop := startRuntime(t, runtime)
	deadline := waitForRecoveryFanout(t, cellarStore)
	stop()

	if delay := time.Until(deadline); delay <= 0 || delay > VerifyDelay {
		t.Errorf("recovery delay = %v, want positive delay no greater than %v", delay, VerifyDelay)
	}
	if calls := client.calls.Load(); calls != 1 {
		t.Errorf("provider calls = %d, want 1", calls)
	}
	if requests := verifier.requests.Load(); requests != 0 {
		t.Errorf("verification requests before deadline = %d, want 0", requests)
	}
	state, _, _ := progressRow(t, db, "send-1", "alice@example.com")
	if state != StateRecoveryWaiting {
		t.Errorf("state = %q, want %q", state, StateRecoveryWaiting)
	}
	active, err := cellarStore.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	fanouts := 0
	for _, cell := range active {
		if cell.Steps[cell.CurrentStep].HandlerName == HandlerRecoveryFanout {
			fanouts++
			if cell.NotBefore == nil || cell.NotBefore.Before(deadline) {
				t.Errorf("fanout deadline = %v, want at least %v", cell.NotBefore, deadline)
			}
		}
	}
	if fanouts != 1 {
		t.Errorf("scheduled fanouts = %d, want 1", fanouts)
	}
}

func TestRecoveryVerifiesRecipientsBeforeResubmitting(t *testing.T) {
	db := openTestDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	var submissions [][]string
	client := sendClientFunc(func(_ context.Context, email clients.Email) (clients.SendResult, error) {
		addresses := make([]string, 0, len(email.To))
		for _, recipient := range email.To {
			addresses = append(addresses, recipient.Email)
		}
		submissions = append(submissions, addresses)
		if len(submissions) == 1 {
			_, err := db.Exec(`UPDATE email_progress SET submitted_at = ? WHERE idempotency_token = ?`,
				time.Now().UTC().Add(-VerifyDelay-time.Second), "send-1")
			if err != nil {
				return clients.SendResult{}, err
			}
			return clients.SendResult{}, errors.New("ambiguous provider response")
		}
		return clients.SendResult{Recipients: []clients.RecipientResult{{PMUID: "pmuid-bob"}}}, nil
	})
	verifier := &stubVerifier{found: map[string]string{"alice@example.com": "pmuid-alice"}}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	registerSendHandlers(t, runtime, store, client, verifier)
	request := SendRequest{
		IdempotencyToken: "send-1", SenderEmail: "sender@example.com", Subject: "Pub night",
		Recipients: []SendRecipient{{Email: "alice@example.com"}, {Email: "bob@example.com"}},
	}
	if _, err := runtime.AddSequence(NewSendSequence(request)...); err != nil {
		t.Fatal(err)
	}
	stop := startRuntime(t, runtime)
	waitForIdle(t, cellarStore)
	stop()

	if len(submissions) != 2 || len(submissions[0]) != 2 || len(submissions[1]) != 1 || submissions[1][0] != "bob@example.com" {
		t.Errorf("submissions = %v, want [alice bob], then [bob]", submissions)
	}
	if requests := verifier.requests.Load(); requests != 2 {
		t.Errorf("verification requests = %d, want 2", requests)
	}
	for recipient, wantPMUID := range map[string]string{
		"alice@example.com": "pmuid-alice", "bob@example.com": "pmuid-bob",
	} {
		state, pmuid, _ := progressRow(t, db, "send-1", recipient)
		if state != StateAccepted || pmuid.String != wantPMUID {
			t.Errorf("%s: state = %q, PMUID = %q", recipient, state, pmuid.String)
		}
	}
}

func TestStartupRecoversCellClaimedAtPost(t *testing.T) {
	db := openTestDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	request := SendRequest{
		IdempotencyToken: "send-1", SenderEmail: "sender@example.com", Subject: "Pub night",
		Recipients: []SendRecipient{{Email: "alice@example.com"}},
	}
	sequence, err := cellar.NewSequence(NewSendSequence(request)...)
	if err != nil {
		t.Fatal(err)
	}
	cellRequest, err := sequence.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cellarStore.Add([]cellar.CellRequest{cellRequest}); err != nil {
		t.Fatal(err)
	}
	setup, ok, err := cellarStore.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("claim Setup: %v, %t", err, ok)
	}
	if err := cellarStore.ApplyResult(setup, (SetupHandler{Store: store}).Handle(context.Background(), request)); err != nil {
		t.Fatal(err)
	}
	recovery, ok, err := cellarStore.ClaimNext(time.Now())
	if err != nil || !ok {
		t.Fatalf("claim Recovery: %v, %t", err, ok)
	}
	if err := cellarStore.ApplyResult(recovery, cellar.Complete{}); err != nil {
		t.Fatal(err)
	}
	post, ok, err := cellarStore.ClaimNext(time.Now())
	if err != nil || !ok || post.CurrentStep != 2 {
		t.Fatalf("claim Post: %v, %t, step %d", err, ok, post.CurrentStep)
	}
	if err := store.markSubmitted(context.Background(), request.IdempotencyToken, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverSubmissions(context.Background()); err != nil {
		t.Fatal(err)
	}
	client := &countingClient{inner: sendClientFunc(func(context.Context, clients.Email) (clients.SendResult, error) {
		return clients.SendResult{}, errors.New("must not submit before verification")
	})}
	verifier := &stubVerifier{}
	runtime := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	registerSendHandlers(t, runtime, store, client, verifier)
	stop := startRuntime(t, runtime)
	waitForRecoveryFanout(t, cellarStore)
	stop()
	if calls := client.calls.Load(); calls != 0 {
		t.Errorf("provider calls after crash = %d, want 0", calls)
	}
	if checks := verifier.requests.Load(); checks != 0 {
		t.Errorf("verification checks before deadline = %d, want 0", checks)
	}
	state, _, _ := progressRow(t, db, request.IdempotencyToken, "alice@example.com")
	if state != StateRecoveryWaiting {
		t.Errorf("state = %q, want %q", state, StateRecoveryWaiting)
	}
}

func TestFailedVerifyKeepsPostBlockedUntilRestart(t *testing.T) {
	db := openTestDB(t)
	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	request := SendRequest{
		IdempotencyToken: "send-1", SenderEmail: "sender@example.com", Subject: "Pub night",
		Recipients: []SendRecipient{{Email: "alice@example.com"}, {Email: "bob@example.com"}},
	}
	if err := applySetup(t, cellarStore, store, request); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE email_progress SET state = ?, submitted_at = ? WHERE idempotency_token = ?`,
		StateRecovery, time.Now().UTC().Add(-VerifyDelay-time.Second), request.IdempotencyToken); err != nil {
		t.Fatal(err)
	}
	client := &countingClient{inner: sendClientFunc(func(_ context.Context, email clients.Email) (clients.SendResult, error) {
		if len(email.To) != 1 || email.To[0].Email != "bob@example.com" {
			return clients.SendResult{}, errors.New("wrong recipient for resubmission")
		}
		return clients.SendResult{Recipients: []clients.RecipientResult{{PMUID: "pmuid-bob"}}}, nil
	})}
	first := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	registerSendHandlers(t, first, store, client, verifyClientFunc(func(_ context.Context, request clients.VerifyRequest) (clients.VerifyResult, error) {
		if request.Recipient == "alice@example.com" {
			return clients.VerifyResult{Found: true, PMUID: "pmuid-alice"}, nil
		}
		return clients.VerifyResult{}, errors.New("provider log unavailable")
	}))
	if _, err := first.AddSequence(NewSendSequence(request)...); err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err == nil {
		t.Fatal("expected verification failure")
	}
	if calls := client.calls.Load(); calls != 0 {
		t.Errorf("provider calls before verification completes = %d, want 0", calls)
	}
	aliceState, alicePMUID, _ := progressRow(t, db, request.IdempotencyToken, "alice@example.com")
	bobState, _, _ := progressRow(t, db, request.IdempotencyToken, "bob@example.com")
	if aliceState != StateAccepted || alicePMUID.String != "pmuid-alice" || bobState != StateRecoveryWaiting {
		t.Errorf("states after failed Verify: alice = %q (%q), bob = %q", aliceState, alicePMUID.String, bobState)
	}

	restarted := cellar.New(cellarStore, cellar.Config{PollDelay: time.Millisecond})
	registerSendHandlers(t, restarted, store, client, &stubVerifier{})
	stop := startRuntime(t, restarted)
	waitForIdle(t, cellarStore)
	stop()
	if calls := client.calls.Load(); calls != 1 {
		t.Errorf("provider calls after verification = %d, want 1", calls)
	}
	bobState, bobPMUID, _ := progressRow(t, db, request.IdempotencyToken, "bob@example.com")
	if bobState != StateAccepted || bobPMUID.String != "pmuid-bob" {
		t.Errorf("bob after restart: state = %q, PMUID = %q", bobState, bobPMUID.String)
	}
}

func registerSendHandlers(t *testing.T, runtime *cellar.Cellar, store *Store, client clients.EmailClient, verifier clients.EmailVerifier) {
	t.Helper()
	if err := Register(runtime, store, client, verifier); err != nil {
		t.Fatal(err)
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
