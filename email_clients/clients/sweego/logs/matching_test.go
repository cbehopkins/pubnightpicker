package logs

import (
	"reflect"
	"testing"
	"time"

	"email_clients/clients/sweego"
)

func TestMatchingRecordsReportsAmbiguousCandidates(t *testing.T) {
	submittedAt := time.Date(2026, 8, 17, 21, 30, 0, 0, time.UTC)
	operation := RecoveryOperation{
		TransactionID: "transaction-1",
		SubmittedAt:   submittedAt,
		Sender:        "Sender <sender@example.com>",
		Recipients:    []string{"alice@example.com"},
	}
	record := Record{
		Channel:       "email",
		EmailFrom:     "sender@example.com",
		EmailTo:       "Alice <alice@example.com>",
		EmailCreation: submittedAt.Format(time.RFC3339),
		TransactionID: "transaction-1",
		Headers:       map[string]any{sweego.PubnightMessageIDHeader: "pn-1"},
	}

	matches := matchingRecords([]Record{record, record}, operation, "alice@example.com", "pn-1", time.Minute)
	if len(matches) != 2 {
		t.Fatalf("matchingRecords() returned %d matches, want 2", len(matches))
	}
}

func TestMatchingRecordsFiltersCandidates(t *testing.T) {
	submittedAt := time.Date(2026, 8, 17, 21, 30, 0, 0, time.UTC)
	tolerance := time.Minute
	operation := RecoveryOperation{
		TransactionID: "transaction-1",
		SubmittedAt:   submittedAt,
		Sender:        "Sender <sender@example.com>",
	}
	base := Record{
		Channel:       "email",
		EmailFrom:     "SENDER@example.com",
		EmailTo:       "Alice <ALICE@example.com>",
		EmailCreation: submittedAt.Format(time.RFC3339Nano),
		TransactionID: "transaction-1",
		Headers:       map[string]any{"x-pubnight-message-id": "pn-1"},
		SwgUID:        "uid-1",
	}

	tests := []struct {
		name        string
		change      func(*Record)
		correlation string
		wantMatch   bool
	}{
		{name: "all filters pass", correlation: "pn-1", wantMatch: true},
		{name: "empty channel is accepted", correlation: "pn-1", change: func(record *Record) { record.Channel = "" }, wantMatch: true},
		{name: "wrong channel", correlation: "pn-1", change: func(record *Record) { record.Channel = "sms" }},
		{name: "wrong sender", correlation: "pn-1", change: func(record *Record) { record.EmailFrom = "other@example.com" }},
		{name: "wrong recipient", correlation: "pn-1", change: func(record *Record) { record.EmailTo = "bob@example.com" }},
		{name: "lower boundary is inclusive", correlation: "pn-1", change: func(record *Record) { record.EmailCreation = submittedAt.Add(-tolerance).Format(time.RFC3339Nano) }, wantMatch: true},
		{name: "upper boundary is inclusive", correlation: "pn-1", change: func(record *Record) { record.EmailCreation = submittedAt.Add(tolerance).Format(time.RFC3339Nano) }, wantMatch: true},
		{name: "before lower boundary", correlation: "pn-1", change: func(record *Record) {
			record.EmailCreation = submittedAt.Add(-tolerance - time.Nanosecond).Format(time.RFC3339Nano)
		}},
		{name: "after upper boundary", correlation: "pn-1", change: func(record *Record) {
			record.EmailCreation = submittedAt.Add(tolerance + time.Nanosecond).Format(time.RFC3339Nano)
		}},
		{name: "invalid timestamp", correlation: "pn-1", change: func(record *Record) { record.EmailCreation = "invalid" }},
		{name: "different transaction", correlation: "pn-1", change: func(record *Record) { record.TransactionID = "transaction-2" }},
		{name: "missing record transaction is accepted", correlation: "pn-1", change: func(record *Record) { record.TransactionID = "" }, wantMatch: true},
		{name: "different correlation header", correlation: "pn-1", change: func(record *Record) { record.Headers["x-pubnight-message-id"] = "pn-2" }},
		{name: "missing correlation header is accepted", correlation: "pn-1", change: func(record *Record) { record.Headers = nil }, wantMatch: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := base
			record.Headers = map[string]any{"x-pubnight-message-id": "pn-1"}
			if test.change != nil {
				test.change(&record)
			}
			matches := matchingRecords([]Record{record}, operation, "alice@example.com", test.correlation, tolerance)
			if got := len(matches) == 1; got != test.wantMatch {
				t.Fatalf("matchingRecords() matched=%v, want %v", got, test.wantMatch)
			}
		})
	}
}

func TestParseSweegoTime(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Time
	}{
		{name: "RFC3339", raw: "2026-08-17T21:30:00Z", want: time.Date(2026, 8, 17, 21, 30, 0, 0, time.UTC)},
		{name: "RFC3339 fractional", raw: "2026-08-17T21:30:00.123456789Z", want: time.Date(2026, 8, 17, 21, 30, 0, 123456789, time.UTC)},
		{name: "space without zone", raw: "2026-08-17 21:30:00", want: time.Date(2026, 8, 17, 21, 30, 0, 0, time.UTC)},
		{name: "T without zone", raw: "2026-08-17T21:30:00", want: time.Date(2026, 8, 17, 21, 30, 0, 0, time.UTC)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseSweegoTime(test.raw)
			if err != nil || !got.Equal(test.want) {
				t.Fatalf("parseSweegoTime(%q) = %v, %v; want %v", test.raw, got, err, test.want)
			}
		})
	}
	for _, raw := range []string{"", "not-a-time"} {
		if _, err := parseSweegoTime(raw); err == nil {
			t.Fatalf("parseSweegoTime(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestSameEmail(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		want        bool
	}{
		{name: "display name and case", left: "Alice <ALICE@Example.com>", right: "alice@example.com", want: true},
		{name: "different address", left: "alice@example.com", right: "bob@example.com"},
		{name: "malformed fallback trims and folds", left: "  not-an-address  ", right: "NOT-AN-ADDRESS", want: true},
		{name: "different malformed values", left: "bad-one", right: "bad-two"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameEmail(test.left, test.right); got != test.want {
				t.Fatalf("sameEmail(%q, %q) = %v, want %v", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestHeaderValue(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]any
		want    string
		wantOK  bool
	}{
		{name: "case insensitive", headers: map[string]any{"x-pubnight-message-id": "pn-1"}, want: "pn-1", wantOK: true},
		{name: "missing", headers: map[string]any{"subject": "hello"}},
		{name: "nil map", headers: nil},
		{name: "non-string observed value", headers: map[string]any{"X-Pubnight-Message-ID": []any{"pn-1"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := HeaderValue(test.headers, sweego.PubnightMessageIDHeader)
			if !reflect.DeepEqual([]any{got, ok}, []any{test.want, test.wantOK}) {
				t.Fatalf("HeaderValue() = %q, %v; want %q, %v", got, ok, test.want, test.wantOK)
			}
		})
	}
}
