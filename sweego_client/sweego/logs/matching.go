package logs

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"sweego_client/sweego"
)

func matchingRecords(records []Record, operation RecoveryOperation, recipient, correlationID string, tolerance time.Duration) []Record {
	matches := make([]Record, 0)
	for _, record := range records {
		if !sameEmail(record.EmailTo, recipient) || !sameEmail(record.EmailFrom, operation.Sender) || record.Channel != "" && record.Channel != "email" {
			continue
		}
		created, err := parseSweegoTime(record.EmailCreation)
		if err != nil || created.Before(operation.SubmittedAt.Add(-tolerance)) || created.After(operation.SubmittedAt.Add(tolerance)) {
			continue
		}
		if operation.TransactionID != "" && record.TransactionID != "" && operation.TransactionID != record.TransactionID {
			continue
		}
		if value, ok := HeaderValue(record.Headers, sweego.PubnightMessageIDHeader); ok && value != correlationID {
			continue
		}
		matches = append(matches, record)
	}
	return matches
}

func parseSweegoTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported Sweego timestamp %q", raw)
}

func sameEmail(left, right string) bool {
	leftAddress, leftErr := mail.ParseAddress(left)
	rightAddress, rightErr := mail.ParseAddress(right)
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	return strings.EqualFold(leftAddress.Address, rightAddress.Address)
}
