package mailtrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"email_clients/clients"

	sdk "github.com/mailtrap/mailtrap-go"
)

var ErrAmbiguousVerification = errors.New("multiple Mailtrap messages match the correlation ID and recipient")

type Verifier struct {
	Client    *Client
	Tolerance time.Duration
}

var _ clients.EmailVerifier = (*Verifier)(nil)

func NewVerifier(client *Client, tolerance time.Duration) *Verifier {
	return &Verifier{Client: client, Tolerance: tolerance}
}

func (v *Verifier) Verify(ctx context.Context, request clients.VerifyRequest) (clients.VerifyResult, error) {
	if err := ctx.Err(); err != nil {
		return clients.VerifyResult{}, err
	}
	if v == nil || v.Client == nil || v.Client.sdk == nil {
		return clients.VerifyResult{}, errors.New("mailtrap verifier is not configured")
	}
	if v.Tolerance < 0 || request.SentAt.IsZero() || strings.TrimSpace(request.CorrelationID) == "" {
		return clients.VerifyResult{}, errors.New("mailtrap verification requires a correlation ID, SentAt and non-negative tolerance")
	}
	if err := validateAddress(request.Recipient); err != nil {
		return clients.VerifyResult{}, fmt.Errorf("invalid verification recipient: %w", err)
	}
	options := &sdk.EmailLogsListOptions{
		SentAfter:  request.SentAt.Add(-v.Tolerance).UTC().Format(time.RFC3339Nano),
		SentBefore: request.SentAt.Add(v.Tolerance).UTC().Format(time.RFC3339Nano),
		Filters: map[string]sdk.LogFilter{
			"to": {Operator: "ci_equal", Values: []string{request.Recipient}},
		},
	}
	result := clients.VerifyResult{}
	for message, err := range v.Client.sdk.EmailLogs.All(ctx, options) {
		if err != nil {
			return clients.VerifyResult{}, fmt.Errorf("mailtrap verify: %w", err)
		}
		if message == nil {
			return clients.VerifyResult{}, fmt.Errorf("%w: null email log record", ErrInvalidResponse)
		}
		correlationID, ok := message.CustomVariables[CorrelationVariable].(string)
		if !ok || correlationID != request.CorrelationID || !strings.EqualFold(message.To, request.Recipient) {
			continue
		}
		if strings.TrimSpace(message.MessageID) == "" {
			return clients.VerifyResult{}, fmt.Errorf("%w: matching email log has no message ID", ErrInvalidResponse)
		}
		if result.Found && result.PMUID != message.MessageID {
			return clients.VerifyResult{}, ErrAmbiguousVerification
		}
		result = clients.VerifyResult{Found: true, PMUID: message.MessageID}
	}
	return result, nil
}
