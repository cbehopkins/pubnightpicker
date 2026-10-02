package durableemail

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
)

type schemaColumn struct {
	name               string
	typeName           string
	notNull            int
	primaryKeyPosition int
}

func TestNewStoreInitialisesSchemaAlongsideCellar(t *testing.T) {
	db := openTestDB(t)

	cellarStore, err := cellarsqlite.NewStore(db, nil)
	if err != nil {
		t.Fatalf("initialise Cellar store: %v", err)
	}
	if runtime := cellar.New(cellarStore, cellar.Config{}); runtime == nil {
		t.Fatal("Cellar runtime is nil")
	}

	if _, err := NewStore(db); err != nil {
		t.Fatalf("initialise durable email store: %v", err)
	}

	for _, table := range []string{"cells", "email_requests", "email_progress", "email_events"} {
		assertTableExists(t, db, table)
	}

	assertColumns(t, db, "email_requests", []schemaColumn{
		{name: "idempotency_token", typeName: "TEXT", notNull: 1, primaryKeyPosition: 1},
		{name: "message_id", typeName: "TEXT", notNull: 1},
		{name: "sender_email", typeName: "TEXT", notNull: 1},
		{name: "sender_name", typeName: "TEXT", notNull: 1},
		{name: "subject", typeName: "TEXT", notNull: 1},
		{name: "template_id", typeName: "TEXT", notNull: 1},
		{name: "text", typeName: "TEXT", notNull: 1},
		{name: "variables", typeName: "TEXT", notNull: 1},
		{name: "headers", typeName: "TEXT", notNull: 1},
	})
	assertColumns(t, db, "email_progress", []schemaColumn{
		{name: "idempotency_token", typeName: "TEXT", notNull: 1, primaryKeyPosition: 1},
		{name: "recipient", typeName: "TEXT", notNull: 1, primaryKeyPosition: 2},
		{name: "recipient_name", typeName: "TEXT", notNull: 1},
		{name: "state", typeName: "TEXT", notNull: 1},
		{name: "pmuid", typeName: "TEXT"},
		{name: "variables", typeName: "TEXT", notNull: 1},
		{name: "submitted_at", typeName: "DATETIME"},
	})
	assertColumns(t, db, "email_events", []schemaColumn{
		{name: "id", typeName: "INTEGER", primaryKeyPosition: 1},
		{name: "idempotency_token", typeName: "TEXT", notNull: 1},
		{name: "recipient", typeName: "TEXT", notNull: 1},
		{name: "event", typeName: "TEXT", notNull: 1},
		{name: "recorded_at", typeName: "DATETIME", notNull: 1},
	})
	assertUniqueIndex(t, db, "email_requests", "message_id")
	assertIndexedColumn(t, db, "email_progress", "recipient")
	assertIndexedColumn(t, db, "email_progress", "state")
	assertIndexedColumn(t, db, "email_events", "idempotency_token")
}

func TestRecordEventPreservesStrongerStatuses(t *testing.T) {
	store, db := newQueryFixture(t)
	var messageID string
	if err := db.QueryRow(`SELECT message_id FROM email_requests WHERE idempotency_token = ?`, "send-1").Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		event DeliveryEvent
		want  string
	}{
		{EventProxyOpen, StateSent},
		{EventSoftBounce, StateSent},
		{EventDelivered, StateDelivered},
		{EventHumanOpen, StateOpened},
		{EventSent, StateOpened},
		{EventClick, StateClicked},
		{EventSpamComplaint, StateSpamComplaint},
		{EventDelivered, StateSpamComplaint},
	} {
		if err := store.RecordEvent(context.Background(), messageID, "bob@example.com", step.event); err != nil {
			t.Fatalf("record %s: %v", step.event, err)
		}
		state, _, _ := progressRow(t, db, "send-1", "bob@example.com")
		if state != step.want {
			t.Errorf("after %s: state = %q, want %q", step.event, state, step.want)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_events WHERE idempotency_token = ? AND recipient = ?`, "send-1", "bob@example.com").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Errorf("recorded events = %d, want 8", count)
	}
}

func TestRecordEventRejectsUnknownTargetAndEvent(t *testing.T) {
	store, db := newQueryFixture(t)
	if err := store.RecordEvent(context.Background(), "missing", "alice@example.com", EventDelivered); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown message: %v, want sql.ErrNoRows", err)
	}
	if err := store.RecordEvent(context.Background(), "missing", "alice@example.com", "ListUnsubscribe"); err == nil {
		t.Error("unsupported event succeeded")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_events`).Scan(&count); err != nil || count != 0 {
		t.Errorf("recorded events = %d, error = %v; want zero", count, err)
	}
}

func TestRecordEventAdvancesRecoveryWithoutResubmitting(t *testing.T) {
	store, db := newQueryFixture(t)
	if _, err := db.Exec(`UPDATE email_progress SET state = ?, submitted_at = ? WHERE idempotency_token = ? AND recipient = ?`,
		StateRecoveryWaiting, time.Now().UTC(), "send-1", "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	var messageID string
	if err := db.QueryRow(`SELECT message_id FROM email_requests WHERE idempotency_token = ?`, "send-1").Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordEvent(context.Background(), messageID, "bob@example.com", EventSoftBounce); err != nil {
		t.Fatal(err)
	}
	state, _, _ := progressRow(t, db, "send-1", "bob@example.com")
	if state != StateSent {
		t.Errorf("state = %q, want Sent", state)
	}
}

func TestTerminalStatusesRemainExcludedFromResubmission(t *testing.T) {
	store, db := newQueryFixture(t)
	var messageID string
	if err := db.QueryRow(`SELECT message_id FROM email_requests WHERE idempotency_token = ?`, "send-1").Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE email_progress SET state = ?, submitted_at = ? WHERE idempotency_token = ? AND recipient = ?`,
		StateRefused, time.Now().UTC(), "send-1", "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordEvent(context.Background(), messageID, "alice@example.com", EventHardBounce); err != nil {
		t.Fatal(err)
	}
	for _, recipient := range []string{"alice@example.com", "bob@example.com"} {
		if err := store.RecordEvent(context.Background(), messageID, recipient, EventDelivered); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecoverSubmissions(context.Background()); err != nil {
		t.Fatal(err)
	}
	for recipient, want := range map[string]string{"alice@example.com": StateHardBounced, "bob@example.com": StateRefused} {
		state, _, _ := progressRow(t, db, "send-1", recipient)
		if state != want {
			t.Errorf("%s state = %q, want %q", recipient, state, want)
		}
		rows, err := store.QueryProgress(context.Background(), ProgressFilter{Recipient: recipient, States: []string{want}})
		if err != nil || len(rows) != 1 {
			t.Errorf("query %s in %s: rows = %+v, error = %v", recipient, want, rows, err)
		}
	}
	pending, err := store.pendingRecipients(context.Background(), "send-1")
	if err != nil || len(pending) != 0 {
		t.Errorf("pending recipients = %+v, error = %v", pending, err)
	}
}

func TestSchemaStoresReconstructableRequestData(t *testing.T) {
	db := openTestDB(t)
	if _, err := NewStore(db); err != nil {
		t.Fatalf("initialise durable email store: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO email_requests (
			idempotency_token, message_id, sender_email, sender_name, subject,
			template_id, text, variables, headers
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "send-1", "message-1", "sender@example.com", "Sender", "Pub night", "template-1", "Hello", `{"event":"Friday"}`, `{"X-Common":"value"}`); err != nil {
		t.Fatalf("insert request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO email_progress (
			idempotency_token, recipient, recipient_name, state, variables
		) VALUES
			(?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?)
	`,
		"send-1", "alice@example.com", "Alice", "Pending", `{"name":"Alice"}`,
		"send-1", "bob@example.com", "Bob", "Pending", `{"name":"Bob"}`,
	); err != nil {
		t.Fatalf("insert progress: %v", err)
	}

	rows, err := db.Query(`
		SELECT r.sender_email, r.sender_name, r.subject, r.template_id, r.text,
		       r.variables, r.headers, p.recipient, p.recipient_name,
		       p.variables, r.message_id
		FROM email_requests r
		JOIN email_progress p USING (idempotency_token)
		WHERE r.idempotency_token = ?
		ORDER BY p.recipient
	`, "send-1")
	if err != nil {
		t.Fatalf("query reconstructable data: %v", err)
	}
	defer rows.Close()

	var recipients []string
	for rows.Next() {
		var senderEmail, senderName, subject, templateID, text string
		var commonVariables, headers, recipient, recipientName, recipientVariables, messageID string
		if err := rows.Scan(
			&senderEmail, &senderName, &subject, &templateID, &text,
			&commonVariables, &headers, &recipient, &recipientName,
			&recipientVariables, &messageID,
		); err != nil {
			t.Fatalf("scan reconstructable data: %v", err)
		}
		if senderEmail != "sender@example.com" || senderName != "Sender" || subject != "Pub night" || templateID != "template-1" || text != "Hello" {
			t.Fatalf("unexpected common request data for %q", recipient)
		}
		if commonVariables != `{"event":"Friday"}` || headers != `{"X-Common":"value"}` || recipientName == "" || recipientVariables == "" || messageID == "" {
			t.Fatalf("incomplete reconstructable data for %q", recipient)
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reconstructable data: %v", err)
	}
	if len(recipients) != 2 || recipients[0] != "alice@example.com" || recipients[1] != "bob@example.com" {
		t.Fatalf("recipients = %v, want alice and bob", recipients)
	}
}

func TestNewStoreRejectsNilDatabase(t *testing.T) {
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) error = nil")
	}
}

func TestNewStoreIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if _, err := NewStore(db); err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	if _, err := NewStore(db); err != nil {
		t.Fatalf("second NewStore: %v", err)
	}
}

func TestRepeatedSetupPreservesProgressAndRejectsConflicts(t *testing.T) {
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
		Variables:  map[string]any{"event": "Friday"},
		Recipients: []SendRecipient{{Email: "alice@example.com", Name: "Alice", Variables: map[string]any{"name": "Alice"}}},
	}
	if err := applySetup(t, cellarStore, store, request); err != nil {
		t.Fatal(err)
	}
	messageID := requestMessageID(t, db, request.IdempotencyToken)
	if _, err := db.Exec(`UPDATE email_progress SET state = ?, pmuid = ? WHERE idempotency_token = ?`,
		StateAccepted, "pmuid-1", request.IdempotencyToken); err != nil {
		t.Fatal(err)
	}
	if err := applySetup(t, cellarStore, store, request); err != nil {
		t.Fatalf("repeat identical Setup: %v", err)
	}
	if got := requestMessageID(t, db, request.IdempotencyToken); got != messageID {
		t.Errorf("message ID = %q, want original %q", got, messageID)
	}
	state, pmuid, _ := progressRow(t, db, request.IdempotencyToken, "alice@example.com")
	if state != StateAccepted || pmuid.String != "pmuid-1" {
		t.Errorf("progress reset on repeat Setup: state = %q, pmuid = %q", state, pmuid.String)
	}
	changed := request
	changed.Subject = "Changed"
	if err := applySetup(t, cellarStore, store, changed); err == nil {
		t.Error("conflicting subject was accepted")
	}
	changed = request
	changed.Recipients = []SendRecipient{{Email: "bob@example.com"}}
	if err := applySetup(t, cellarStore, store, changed); err == nil {
		t.Error("conflicting recipient set was accepted")
	}
}

func TestRecoverSubmissionsOnlyMarksAttemptedPending(t *testing.T) {
	db := openTestDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		recipient string
		state     string
		at        any
	}{
		{"attempted@example.com", StatePending, time.Now().UTC()},
		{"virgin@example.com", StatePending, nil},
		{"accepted@example.com", StateAccepted, time.Now().UTC()},
	} {
		if _, err := db.Exec(`INSERT INTO email_progress
			(idempotency_token, recipient, recipient_name, state, variables, submitted_at)
			VALUES (?, ?, ?, ?, ?, ?)`, "send-1", row.recipient, "", row.state, `{}`, row.at); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := store.RecoverSubmissions(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for recipient, want := range map[string]string{
		"attempted@example.com": StateRecovery,
		"virgin@example.com":    StatePending,
		"accepted@example.com":  StateAccepted,
	} {
		state, _, _ := progressRow(t, db, "send-1", recipient)
		if state != want {
			t.Errorf("%s state = %q, want %q", recipient, state, want)
		}
	}
}

func applySetup(t *testing.T, cellarStore *cellarsqlite.Store, store *Store, request SendRequest) error {
	t.Helper()
	work, err := store.insertRequestWork(request)
	if err != nil {
		return err
	}
	definition, err := cellar.NewCellDefinition(HandlerSetup, request)
	if err != nil {
		return err
	}
	cellRequest, err := definition.CellRequest()
	if err != nil {
		return err
	}
	if _, err := cellarStore.Add([]cellar.CellRequest{cellRequest}); err != nil {
		return err
	}
	cell, ok, err := cellarStore.ClaimNext(time.Now())
	if err != nil {
		return err
	}
	if !ok {
		t.Fatal("Setup cell was not claimable")
	}
	return cellarStore.ApplyResult(cell, cellar.Complete{ApplicationWork: []cellar.ApplicationWork{work}})
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "durable-email.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
		t.Fatalf("expected table %q: %v", table, err)
	}
}

func assertColumns(t *testing.T, db *sql.DB, table string, want []schemaColumn) {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("inspect table %q: %v", table, err)
	}
	defer rows.Close()

	var got []schemaColumn
	for rows.Next() {
		var column schemaColumn
		var position int
		var defaultValue any
		if err := rows.Scan(&position, &column.name, &column.typeName, &column.notNull, &defaultValue, &column.primaryKeyPosition); err != nil {
			t.Fatalf("scan table %q: %v", table, err)
		}
		got = append(got, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table %q: %v", table, err)
	}
	if len(got) != len(want) {
		t.Fatalf("table %q columns = %v, want %v", table, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("table %q column %d = %+v, want %+v", table, index, got[index], want[index])
		}
	}
}

func assertUniqueIndex(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_list(` + table + `)`)
	if err != nil {
		t.Fatalf("inspect indexes for %q: %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index for %q: %v", table, err)
		}
		if unique != 1 {
			continue
		}
		var indexedColumn string
		if err := db.QueryRow(`SELECT name FROM pragma_index_info(?) WHERE seqno = 0`, name).Scan(&indexedColumn); err != nil {
			t.Fatalf("inspect index %q: %v", name, err)
		}
		if indexedColumn == column {
			return
		}
	}
	t.Fatalf("table %q has no unique index on %q", table, column)
}

func assertIndexedColumn(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_index_list(?) AS indexes
		JOIN pragma_index_info(indexes.name) AS columns
		WHERE columns.seqno = 0 AND columns.name = ?
	`, table, column).Scan(&count); err != nil {
		t.Fatalf("inspect indexes for %q: %v", table, err)
	}
	if count == 0 {
		t.Fatalf("table %q has no index leading with %q", table, column)
	}
}
