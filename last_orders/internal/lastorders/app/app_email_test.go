package app_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
	durableemail "durable_email"
	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/completionactions/completionactionstest"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/notificationprofile/notificationprofiletest"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/components/venuecache/venuecachetest"
	"last_orders/internal/lastorders/database/listeners/testemail"
	"last_orders/internal/lastorders/database/listeners/testemail/testemailtest"
	"last_orders/internal/lastorders/truths"
)

func TestEmailTablesShareTheApplicationDatabase(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "email-tables.db")
	a := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)
	defer a.Close()

	db := openSQLite(t, dbPath)
	mustTable(t, db, "email_requests")
	mustTable(t, db, "email_progress")
}

func TestEmailSendSequenceDeliversThroughDummyClient(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "email-send.db")
	logs := &syncBuffer{}
	a := mustNewAppWithLogger(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil, logs)
	defer a.Close()

	request := durableemail.SendRequest{
		IdempotencyToken: "poll-opened:send-1",
		SenderEmail:      "sender@example.com",
		Subject:          "Pub night",
		Text:             "Hello",
		Recipients: []durableemail.SendRecipient{
			{Email: "alice@example.com", Name: "Alice"},
			{Email: "bob@example.com", Name: "Bob"},
		},
	}
	sequence, err := cellar.NewSequence(durableemail.NewSendSequence(request)...)
	if err != nil {
		t.Fatalf("build send sequence: %v", err)
	}
	cellRequest, err := sequence.CellRequest()
	if err != nil {
		t.Fatalf("build send cell: %v", err)
	}
	if err := a.AddCell(cellRequest); err != nil {
		t.Fatalf("add send cell: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	db := openSQLite(t, dbPath)
	waitForEmailStates(t, db, request.IdempotencyToken, durableemail.StateAccepted, len(request.Recipients))
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}

	var withoutPMUID int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM email_progress
		WHERE idempotency_token = ? AND (pmuid IS NULL OR pmuid = '')
	`, request.IdempotencyToken).Scan(&withoutPMUID); err != nil {
		t.Fatalf("count rows without PMUID: %v", err)
	}
	if withoutPMUID != 0 {
		t.Errorf("rows without PMUID = %d, want 0", withoutPMUID)
	}
	if sends := strings.Count(logs.String(), `"msg":"dummy email sent"`); sends != len(request.Recipients) {
		t.Errorf("dummy sends = %d, want %d", sends, len(request.Recipients))
	}
}

func TestRuntimeSuppressionHandlesMissingSandboxAndTestEmails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "suppressed.db")
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Email.Client = "mailtrap"
	cfg.Email.MailtrapToken = "unused-token"
	cfg.DiagnosticsSource = func(ctx context.Context, update func(bool)) error { update(true); <-ctx.Done(); return ctx.Err() }
	a := newApp(t, cfg)
	defer a.Close()
	for _, token := range []string{"poll-opened:suppressed", "test-email:suppressed"} {
		request := durableemail.SendRequest{IdempotencyToken: token, SenderEmail: "sender@example.com", Subject: "Hello", Text: "Hello", Recipients: []durableemail.SendRecipient{{Email: "alice@example.com"}}}
		sequence, err := cellar.NewSequence(durableemail.NewSendSequence(request)...)
		if err != nil {
			t.Fatal(err)
		}
		cellRequest, err := sequence.CellRequest()
		if err != nil {
			t.Fatal(err)
		}
		if err := a.AddCell(cellRequest); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	db := openSQLite(t, dbPath)
	for _, token := range []string{"poll-opened:suppressed", "test-email:suppressed"} {
		waitForEmailStates(t, db, token, durableemail.StateAccepted, 1)
		var pmuid string
		var submitted sql.NullTime
		if err := db.QueryRow(`SELECT pmuid, submitted_at FROM email_progress WHERE idempotency_token = ?`, token).Scan(&pmuid, &submitted); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(pmuid, "suppressed:") || submitted.Valid {
			t.Fatalf("pmuid=%s submitted=%v", pmuid, submitted)
		}
	}
}

func TestRunRecoversInterruptedEmailSubmissionsBeforeStarting(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "email-recovery.db")
	a := mustNewApp(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true), nil)
	defer a.Close()

	db := openSQLite(t, dbPath)
	if _, err := db.Exec(`
		INSERT INTO email_requests (
			idempotency_token, message_id, sender_email, sender_name, subject,
			template_id, text, variables, headers
		) VALUES ('send-1', 'message-1', 'sender@example.com', '', 'Pub night', '', 'Hello', '{}', '{}')
	`); err != nil {
		t.Fatalf("seed request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO email_progress (
			idempotency_token, recipient, recipient_name, state, variables, submitted_at
		) VALUES ('send-1', 'alice@example.com', 'Alice', ?, '{}', ?)
	`, durableemail.StatePending, time.Now().UTC()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}

	runFor(t, a, 50*time.Millisecond)

	var state string
	if err := db.QueryRow(`
		SELECT state FROM email_progress WHERE idempotency_token = 'send-1' AND recipient = 'alice@example.com'
	`).Scan(&state); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if state != durableemail.StateRecovery {
		t.Errorf("state = %q, want %q", state, durableemail.StateRecovery)
	}
}

func TestPollOpenedTruthEmailsOptedInRecipients(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "poll-opened-email.db")
	logs := &syncBuffer{}
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.NotificationProfileSource = &notificationprofiletest.Source{UserChanges: []notificationprofile.Change{
		userDocument("alice", map[string]any{"notificationEmail": "alice@example.com", "openPollEmailEnabled": true}),
		userDocument("bob", map[string]any{"notificationEmail": "bob@example.com", "openPollEmailEnabled": false}),
		userDocument("carol", map[string]any{"openPollEmailEnabled": true}),
	}}
	a := newApp(t, cfg)
	defer a.Close()

	// Queued before Run, so it can only see recipients if Run waits for the projection.
	if err := enqueueNewPoll(t, a, "poll-email"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	db := openSQLite(t, dbPath)
	waitForEmailStates(t, db, "poll-opened:poll-email", durableemail.StateAccepted, 1)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}

	var recipient string
	var rows int
	if err := db.QueryRow(`
		SELECT COUNT(*), MAX(recipient) FROM email_progress WHERE idempotency_token = ?
	`, "poll-opened:poll-email").Scan(&rows, &recipient); err != nil {
		t.Fatalf("read poll-opened recipients: %v", err)
	}
	if rows != 1 || recipient != "alice@example.com" {
		t.Errorf("recipients = %d (%q), want only alice@example.com", rows, recipient)
	}
	if sends := strings.Count(logs.String(), `"msg":"dummy email sent"`); sends != 1 {
		t.Errorf("dummy sends = %d, want 1", sends)
	}
}

func TestPollCompletedTruthEmailsAndRecordsCompletionActions(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "poll-completed-email.db")
	logs := &syncBuffer{}
	actions := completionactionstest.New()
	venues := venuecachetest.New()
	venues.Changes = []venuecache.Change{{Kind: venuecache.ChangeAdded, Doc: venuecache.Document{ID: "pub-1", Data: map[string]any{"name": "Red Lion"}}}}
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.CompletionActions = actions
	cfg.VenueSource = venues
	cfg.NotificationProfileSource = &notificationprofiletest.Source{UserChanges: []notificationprofile.Change{
		userDocument("alice", map[string]any{"notificationEmail": "alice@example.com", "notificationEmailEnabled": true}),
	}}
	a := newApp(t, cfg)
	defer a.Close()

	envelope, err := truths.NewEnvelope(truths.PollCompletedFanout, truths.PollObservedPayload{
		PollID: "poll-1", SelectedVenueID: "pub-1", PollDate: "2026-10-02",
	})
	if err != nil {
		t.Fatalf("build completed poll truth: %v", err)
	}
	request, err := firebaseidempotency.NewCellRequest("PollCompleted", "poll-1:pub-1", envelope)
	if err != nil {
		t.Fatalf("build idempotency cell: %v", err)
	}
	if err := a.AddCell(request); err != nil {
		t.Fatalf("add completed poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		record, err := actions.Get(context.Background(), completionactions.CompletionCollection, "poll-1")
		if err != nil {
			t.Fatalf("read completion actions: %v", err)
		}
		if !record.NeedsAction(completionactions.ActionEmail, "pub-1") && !record.NeedsAction(completionactions.ActionPersonalEmail, "pub-1") && !record.NeedsAction(completionactions.ActionPush, "pub-1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("completion actions = %+v, want email, pemail, and push recorded", record)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}

	logged := logs.String()
	if sends := strings.Count(logged, `"msg":"dummy email sent"`); sends != 2 {
		t.Errorf("dummy sends = %d, want mailing list and personal", sends)
	}
	for _, want := range []string{"ampubnight@googlegroups.com", "alice@example.com", "visiting Red Lion", "preferences/alice"} {
		if !strings.Contains(logged, want) {
			t.Errorf("logs missing %q", want)
		}
	}
}

func TestPollEmailBudgetIsSharedAndDiagnosticsRemainIndependent(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "email-budget.db")
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.EmailDailyLimit = 1
	source := testemailtest.New()
	source.Documents = []testemail.Document{{ID: "diagnostics", Data: map[string]any{"testEmailReq": "req-budget", "email": "diagnostics@example.com"}}}
	cfg.TestEmailSource = source
	application := newApp(t, cfg)
	defer application.Close()
	for _, token := range []string{"poll-opened:budget", "poll-completed:budget:email:key"} {
		request := durableemail.SendRequest{IdempotencyToken: token, SenderEmail: "sender@example.com", Subject: "Hi", Text: "Hi", Recipients: []durableemail.SendRecipient{{Email: "alice@example.com"}}}
		sequence, err := cellar.NewSequence(durableemail.NewSendSequence(request)...)
		if err != nil {
			t.Fatal(err)
		}
		cell, err := sequence.CellRequest()
		if err != nil {
			t.Fatal(err)
		}
		if err := application.AddCell(cell); err != nil {
			t.Fatal(err)
		}
	}
	db := openSQLite(t, dbPath)
	var accepted, pending int
	runUntil(t, application, func() bool {
		if err := db.QueryRow(`SELECT COUNT(*) FROM email_progress WHERE idempotency_token LIKE 'poll-%' AND state = 'Accepted'`).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM email_progress WHERE idempotency_token LIKE 'poll-%' AND state = 'Pending' AND submitted_at IS NULL`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		ack, acknowledged := source.Ack("diagnostics")
		if accepted != 1 || pending != 1 || !acknowledged || ack != "req-budget" {
			return false
		}
		for _, cell := range activeWorkCells(t, application.CellarStore()) {
			if cell.Steps[cell.CurrentStep].HandlerName == durableemail.HandlerPost && cell.NotBefore != nil && cell.NotBefore.After(time.Now()) {
				return true
			}
		}
		return false
	})
	if accepted != 1 || pending != 1 {
		t.Fatalf("poll emails: accepted %d, untouched pending %d", accepted, pending)
	}
	if ack, ok := source.Ack("diagnostics"); !ok || ack != "req-budget" {
		t.Fatalf("diagnostics ack = %s, %t", ack, ok)
	}
	store, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	active := activeWorkCells(t, store)
	deferred := 0
	for _, cell := range active {
		if cell.Steps[cell.CurrentStep].HandlerName == durableemail.HandlerPost {
			deferred++
			if cell.CurrentStep != 2 || cell.NotBefore == nil || !cell.NotBefore.After(time.Now()) {
				t.Fatalf("deferred email = %+v", cell)
			}
		}
	}
	if deferred != 1 {
		t.Fatalf("deferred email cells = %d", deferred)
	}
}

func TestApplicationRejectsNegativeDailyLimits(t *testing.T) {
	for _, emailLimit := range []bool{false, true} {
		cfg := testConfig(t, filepath.Join(t.TempDir(), "invalid-limit.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
		if emailLimit {
			cfg.EmailDailyLimit = -1
		} else {
			cfg.PushDailyLimit = -1
		}
		application, err := app.New(cfg)
		if err == nil {
			application.Close()
			t.Fatal("negative daily capacity accepted")
		}
	}
}

func TestTestEmailRequestSendsOnceThenAcknowledges(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test-email.db")
	logs := &syncBuffer{}
	source := testemailtest.New()
	source.Documents = []testemail.Document{
		{ID: "alice", Data: map[string]any{"testEmailReq": "req-1", "notificationEmail": "alice@example.com", "email": "login@example.com"}},
		{ID: "alice", Data: map[string]any{"testEmailReq": "req-1", "notificationEmail": "alice@example.com", "email": "login@example.com"}},
		{ID: "bob", Data: map[string]any{"testEmailReq": "req-2", "testEmailAck": "req-2", "email": "bob@example.com"}},
	}
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.TestEmailSource = source
	a := newApp(t, cfg)
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if requestID, ok := source.Ack("alice"); ok && requestID == "req-1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test email was not acknowledged")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}

	logged := logs.String()
	if sends := strings.Count(logged, `"msg":"dummy email sent"`); sends != 1 {
		t.Errorf("dummy sends = %d, want 1", sends)
	}
	if !strings.Contains(logged, `"recipient":"alice@example.com"`) {
		t.Errorf("logs missing alice's notification email: %s", logged)
	}
	if _, ok := source.Ack("bob"); ok {
		t.Error("an already acknowledged request must not be re-acknowledged")
	}
}

func userDocument(id string, data map[string]any) notificationprofile.Change {
	return notificationprofile.Change{Kind: notificationprofile.ChangeAdded, Doc: notificationprofile.Document{ID: id, Data: data}}
}

func waitForEmailStates(t *testing.T, db *sql.DB, token, state string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM email_progress WHERE idempotency_token = ? AND state = ?
		`, token, state).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", state, err)
		}
		if count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %s rows", want, state)
}

func openSQLite(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// syncBuffer lets the test read logs that Cellar workers write concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
