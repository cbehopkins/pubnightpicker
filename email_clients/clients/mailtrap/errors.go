package mailtrap

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNoRecipients = errors.New("email has no recipients")
var ErrInvalidResponse = errors.New("Mailtrap response is unusable")

type RecipientError struct {
	Index     int
	Recipient string
	Messages  []string
}

type BatchError struct {
	Refusals       []RecipientError
	InvalidResults []RecipientError
	Messages       []string
}

func (e *BatchError) Error() string {
	parts := append([]string(nil), e.Messages...)
	for _, refusal := range e.Refusals {
		parts = append(parts, fmt.Sprintf("recipient %d %q refused: %s", refusal.Index, refusal.Recipient, strings.Join(refusal.Messages, "; ")))
	}
	for _, invalid := range e.InvalidResults {
		parts = append(parts, fmt.Sprintf("recipient %d %q has an unusable result: %s", invalid.Index, invalid.Recipient, strings.Join(invalid.Messages, "; ")))
	}
	return "Mailtrap batch: " + strings.Join(parts, "; ")
}

func (e *BatchError) Unwrap() error {
	if len(e.InvalidResults) != 0 {
		return ErrInvalidResponse
	}
	return nil
}
