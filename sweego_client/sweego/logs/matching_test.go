package logs

import (
	"testing"
	"time"

	"sweego_client/sweego"
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
