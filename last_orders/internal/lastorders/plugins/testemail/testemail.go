// Package testemail sends diagnostics test emails for TestEmailRequested Truths.
package testemail

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"last_orders/internal/lastorders/components/ratelimit"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerTestEmail      cellar.HandlerName = "testemail.send"
	HandlerTestEmailAcked cellar.HandlerName = "testemail.acked"

	senderEmail = "ampubnight@contable.co.uk"
	senderName  = "ampubnight notification emails"
	subject     = "Pub Night Picker test email"
	body        = `This is a test email from Pub Night Picker, requested from the Diagnostics page.
If you are reading this, notification emails are reaching you.
`
)

// AckWriter records users/{uid}.testEmailAck.
type AckWriter interface {
	AckTestEmail(ctx context.Context, userID, requestID string) error
}

// Handler sends one test email, then acknowledges the request.
type Handler struct {
	Tokens ratelimit.TokenSource
	Logger *slog.Logger
}

type ackPayload struct {
	UserID    string `json:"user_id"`
	RequestID string `json:"request_id"`
}

func (h Handler) Handle(_ context.Context, request truths.TestEmailRequested) cellar.Result {
	if request.UserID == "" || request.RequestID == "" {
		return cellar.Complete{}
	}
	ack := cellar.Step{HandlerName: HandlerTestEmailAcked, Payload: ackPayload{UserID: request.UserID, RequestID: request.RequestID}}

	steps := []cellar.Step{ack}
	if request.Email == "" {
		h.logger().Warn("test email request has no email address; acknowledging", "user_id", request.UserID)
	} else {
		if wait := h.Tokens.Acquire(); wait != 0 {
			// Dropped without an ack, matching Python; the frontend times out.
			h.logger().Warn("test email rate limited; dropping request", "user_id", request.UserID, "retry_after_seconds", wait)
			return cellar.Complete{}
		}
		send := durableemail.SendRequest{
			IdempotencyToken: "test-email:" + request.UserID + ":" + request.RequestID,
			SenderEmail:      senderEmail,
			SenderName:       senderName,
			Subject:          subject,
			Text:             body,
			Recipients:       []durableemail.SendRecipient{{UserID: request.UserID, Email: request.Email}},
		}
		steps = append(durableemail.NewSendSequence(send), ack)
	}

	sequence, err := cellar.NewSequence(steps...)
	if err != nil {
		return cellar.ErrorResult{Message: "build test email send", Err: err}
	}
	cell, err := sequence.CellRequest()
	if err != nil {
		return cellar.ErrorResult{Message: "build test email send", Err: err}
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{cell}}
}

func (h Handler) logger() *slog.Logger {
	if h.Logger == nil {
		return slog.Default()
	}
	return h.Logger
}

// AckedHandler writes the acknowledgement once the send sequence has completed.
type AckedHandler struct {
	Acks AckWriter
}

func (h AckedHandler) Handle(ctx context.Context, payload ackPayload) cellar.Result {
	if err := h.Acks.AckTestEmail(ctx, payload.UserID, payload.RequestID); err != nil {
		return cellar.ErrorResult{Message: "acknowledge test email", Err: err}
	}
	return cellar.Complete{}
}
