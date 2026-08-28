package main

import (
	"testing"
	"time"

	"sweego_client/sweego"
)

func TestParseBulkResponseSupportsObservedAndPerRecipientShapes(t *testing.T) {
	for _, body := range []string{
		`{"transaction_id":"T1","swg_uids":{"alice@example.com":"U1","bob@example.com":"U2"}}`,
		`{"transaction_id":"T1","messages":[{"recipient":"alice@example.com","swg_uid":"U1"},{"email":"bob@example.com","swg_uid":"U2"}]}`,
	} {
		response, err := parseBulkResponse([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if response.TransactionID != "T1" || response.SwgUIDs["alice@example.com"] != "U1" || response.SwgUIDs["bob@example.com"] != "U2" {
			t.Fatalf("unexpected parsed response: %+v", response)
		}
	}
}

func TestMatchingBulkLogsRefusesAmbiguousCandidates(t *testing.T) {
	submittedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	record := sweego.LogRecord{
		Channel:       "email",
		EmailFrom:     "sender@example.com",
		EmailTo:       "alice@example.com",
		EmailCreation: submittedAt.Format(time.RFC3339),
		SwgUID:        "U1",
	}
	operation := bulkOperation{SubmittedAt: submittedAt, Sender: sweego.EmailAddress{Email: "sender@example.com"}}
	matches := matchingBulkLogs([]sweego.LogRecord{record, record}, operation, "alice@example.com", "pn-1", recoveryOptions{tolerance: time.Minute})
	if len(matches) != 2 {
		t.Fatalf("expected ambiguity to retain both candidates, got %d", len(matches))
	}
}
