package durableemail

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestQueryProgressReturnsEveryRowForZeroFilter(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}

	assertRecipients(t, rows, []string{
		"alice@example.com", "bob@example.com", "alice@example.com", "carol@example.com",
	})
	assertTokens(t, rows, []string{"send-1", "send-1", "send-2", "send-2"})
}

func TestQueryProgressIncludesRequestFields(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{
		IdempotencyToken: "send-1",
		Recipient:        "alice@example.com",
	})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	row := rows[0]
	if row.RecipientName != "Alice" || row.State != StateAccepted {
		t.Fatalf("unexpected progress fields: %+v", row)
	}
	if row.Subject != "Pub night" || row.SenderEmail != "sender@example.com" ||
		row.SenderName != "Sender" || row.TemplateID != "template-1" {
		t.Fatalf("unexpected request fields: %+v", row)
	}
	if row.PMUID != "pmuid-alice" {
		t.Fatalf("PMUID = %q, want pmuid-alice", row.PMUID)
	}
	if row.SubmittedAt == nil || !row.SubmittedAt.Equal(fixtureSubmittedAt) {
		t.Fatalf("SubmittedAt = %v, want %v", row.SubmittedAt, fixtureSubmittedAt)
	}
}

func TestQueryProgressReportsAbsentProviderDataAsZeroValues(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{
		Recipient: "carol@example.com",
	})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].PMUID != "" {
		t.Errorf("PMUID = %q, want empty", rows[0].PMUID)
	}
	if rows[0].SubmittedAt != nil {
		t.Errorf("SubmittedAt = %v, want nil", rows[0].SubmittedAt)
	}
}

func TestQueryProgressFiltersByToken(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{IdempotencyToken: "send-2"})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	assertRecipients(t, rows, []string{"alice@example.com", "carol@example.com"})
}

func TestQueryProgressFiltersByRecipientAcrossOperations(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{Recipient: "alice@example.com"})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	assertTokens(t, rows, []string{"send-1", "send-2"})
}

func TestQueryProgressFiltersByStates(t *testing.T) {
	store, _ := newQueryFixture(t)

	pending, err := store.QueryProgress(context.Background(), ProgressFilter{
		States: []string{StatePending},
	})
	if err != nil {
		t.Fatalf("query pending: %v", err)
	}
	assertRecipients(t, pending, []string{"bob@example.com", "carol@example.com"})

	both, err := store.QueryProgress(context.Background(), ProgressFilter{
		States: []string{StatePending, StateAccepted},
	})
	if err != nil {
		t.Fatalf("query both states: %v", err)
	}
	if len(both) != 4 {
		t.Fatalf("rows = %d, want 4", len(both))
	}
}

func TestQueryEventsAndNewProgressStates(t *testing.T) {
	store, db := newQueryFixture(t)
	var messageID string
	if err := db.QueryRow(`SELECT message_id FROM email_requests WHERE idempotency_token = ?`, "send-1").Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	for _, event := range []DeliveryEvent{EventDelivered, EventProxyOpen, EventHumanOpen, EventHumanOpen, EventSpamComplaint} {
		if err := store.RecordEvent(context.Background(), messageID, "alice@example.com", event); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := store.QueryProgress(context.Background(), ProgressFilter{States: []string{StateSpamComplaint}})
	if err != nil || len(progress) != 1 || progress[0].Recipient != "alice@example.com" {
		t.Fatalf("spam progress = %+v, error = %v", progress, err)
	}
	page, err := store.QueryEvents(context.Background(), ProgressFilter{IdempotencyToken: "send-1", Recipient: "alice@example.com", Offset: 1, Limit: 3})
	if err != nil || len(page) != 3 {
		t.Fatalf("event page = %+v, error = %v", page, err)
	}
	for index, want := range []DeliveryEvent{EventProxyOpen, EventHumanOpen, EventHumanOpen} {
		if page[index].Event != want || page[index].RecordedAt.IsZero() {
			t.Errorf("event %d = %+v, want %s", index, page[index], want)
		}
	}
	if _, err := store.QueryEvents(context.Background(), ProgressFilter{States: []string{StateOpened}}); err == nil {
		t.Error("event history accepted a current-state filter")
	}
}

func TestQueryProgressCombinesFiltersConjunctively(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{
		IdempotencyToken: "send-1",
		States:           []string{StatePending},
	})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	assertRecipients(t, rows, []string{"bob@example.com"})
}

func TestQueryProgressReturnsEmptyResultForUnmatchedFilter(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{States: []string{"Nonexistent"}})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestQueryProgressPagesDisjointlyInStableOrder(t *testing.T) {
	store, _ := newQueryFixture(t)

	var paged []ProgressRow
	for offset := 0; offset < 4; offset += 2 {
		page, err := store.QueryProgress(context.Background(), ProgressFilter{Limit: 2, Offset: offset})
		if err != nil {
			t.Fatalf("query page at offset %d: %v", offset, err)
		}
		if len(page) != 2 {
			t.Fatalf("page at offset %d = %d rows, want 2", offset, len(page))
		}
		paged = append(paged, page...)
	}

	assertTokens(t, paged, []string{"send-1", "send-1", "send-2", "send-2"})
	assertRecipients(t, paged, []string{
		"alice@example.com", "bob@example.com", "alice@example.com", "carol@example.com",
	})

	beyond, err := store.QueryProgress(context.Background(), ProgressFilter{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("query page beyond end: %v", err)
	}
	if len(beyond) != 0 {
		t.Fatalf("rows beyond end = %d, want 0", len(beyond))
	}
}

func TestQueryProgressAppliesOffsetWithoutLimit(t *testing.T) {
	store, _ := newQueryFixture(t)

	rows, err := store.QueryProgress(context.Background(), ProgressFilter{Offset: 1})
	if err != nil {
		t.Fatalf("query progress: %v", err)
	}
	assertRecipients(t, rows, []string{"bob@example.com", "alice@example.com", "carol@example.com"})
}

var fixtureSubmittedAt = time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)

// newQueryFixture seeds two operations sharing a recipient and spanning both states.
func newQueryFixture(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := openTestDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("initialise durable email store: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO email_requests (
			idempotency_token, message_id, sender_email, sender_name, subject,
			template_id, text, variables, headers
		) VALUES
			(?, ?, ?, ?, ?, ?, ?, '{}', '{}'),
			(?, ?, ?, ?, ?, ?, ?, '{}', '{}')
	`,
		"send-1", "message-1", "sender@example.com", "Sender", "Pub night", "template-1", "Hello",
		"send-2", "message-2", "other@example.com", "Other", "Quiz night", "template-2", "Hi",
	); err != nil {
		t.Fatalf("insert requests: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO email_progress (
			idempotency_token, recipient, recipient_name, state, pmuid, variables, submitted_at
		) VALUES
			(?, ?, ?, ?, ?, '{}', ?),
			(?, ?, ?, ?, NULL, '{}', ?),
			(?, ?, ?, ?, ?, '{}', ?),
			(?, ?, ?, ?, NULL, '{}', NULL)
	`,
		"send-1", "alice@example.com", "Alice", StateAccepted, "pmuid-alice", fixtureSubmittedAt,
		"send-1", "bob@example.com", "Bob", StatePending, fixtureSubmittedAt,
		"send-2", "alice@example.com", "Alice", StateAccepted, "pmuid-alice-2", fixtureSubmittedAt,
		"send-2", "carol@example.com", "Carol", StatePending,
	); err != nil {
		t.Fatalf("insert progress: %v", err)
	}

	return store, db
}

func assertRecipients(t *testing.T, rows []ProgressRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for index := range want {
		if rows[index].Recipient != want[index] {
			t.Errorf("row %d recipient = %q, want %q", index, rows[index].Recipient, want[index])
		}
	}
}

func assertTokens(t *testing.T, rows []ProgressRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for index := range want {
		if rows[index].IdempotencyToken != want[index] {
			t.Errorf("row %d token = %q, want %q", index, rows[index].IdempotencyToken, want[index])
		}
	}
}
