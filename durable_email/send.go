package durableemail

import (
	"context"
	"fmt"
	"maps"
	"time"

	"cellar/pkg/cellar"
	"email_clients/clients"
)

const (
	HandlerSetup  cellar.HandlerName = "durable_email.setup"
	HandlerPost   cellar.HandlerName = "durable_email.post"
	HandlerVerify cellar.HandlerName = "durable_email.verify"
)

// VerifyDelay is how long verification waits before looking again for
// recipients the provider has not yet reported.
const VerifyDelay = 120 * time.Second

// MessageIDHeader correlates a provider message with its progress row.
const MessageIDHeader = "X-Pubnight-Message-ID"

// SendRecipient is the recipient-specific part of a logical Send request.
type SendRecipient struct {
	Email     string         `json:"email"`
	Name      string         `json:"name"`
	Variables map[string]any `json:"variables"`
}

// SendRequest is the durable payload of a logical Send operation.
type SendRequest struct {
	IdempotencyToken string            `json:"idempotency_token"`
	SenderEmail      string            `json:"sender_email"`
	SenderName       string            `json:"sender_name"`
	Subject          string            `json:"subject"`
	TemplateID       string            `json:"template_id"`
	Text             string            `json:"text"`
	Variables        map[string]any    `json:"variables"`
	Headers          map[string]string `json:"headers"`
	Recipients       []SendRecipient   `json:"recipients"`
}

// NewSendSequence describes the durable steps of a logical Send operation.
func NewSendSequence(request SendRequest) []cellar.Step {
	return []cellar.Step{
		{HandlerName: HandlerSetup, Payload: request},
		{HandlerName: HandlerPost, Payload: operationRequest{IdempotencyToken: request.IdempotencyToken}},
		{HandlerName: HandlerVerify, Payload: operationRequest{IdempotencyToken: request.IdempotencyToken}},
	}
}

// operationRequest identifies the Send operation a step acts on.
type operationRequest struct {
	IdempotencyToken string `json:"idempotency_token"`
}

// SetupHandler records the durable request and progress rows.
type SetupHandler struct {
	Store *Store
}

func (h SetupHandler) Handle(ctx context.Context, request SendRequest) cellar.Result {
	_ = ctx

	work, err := h.Store.insertRequestWork(request)
	if err != nil {
		return cellar.ErrorResult{Message: "prepare durable email request", Err: err}
	}
	return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{work}}
}

// PostHandler submits pending recipients to the email provider.
type PostHandler struct {
	Store  *Store
	Client clients.EmailClient
}

func (h PostHandler) Handle(ctx context.Context, request operationRequest) cellar.Result {
	pending, err := h.Store.pendingRecipients(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read pending recipients", Err: err}
	}
	if len(pending) == 0 {
		return cellar.Complete{}
	}

	common, err := h.Store.request(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read durable email request", Err: err}
	}

	// Recorded first so an interruption after the provider call remains verifiable.
	if err := h.Store.markSubmitted(ctx, request.IdempotencyToken, time.Now().UTC()); err != nil {
		return cellar.ErrorResult{Message: "record submission time", Err: err}
	}

	result, err := h.Client.Send(ctx, newEmail(common, pending))
	if err != nil {
		return cellar.ErrorResult{Message: "send email", Err: err}
	}
	if len(result.Recipients) != len(pending) {
		return cellar.ErrorResult{Message: fmt.Sprintf("send returned %d results, want %d", len(result.Recipients), len(pending))}
	}

	work := make([]cellar.ApplicationWork, 0, len(pending))
	for index, recipient := range pending {
		work = append(work, h.Store.acceptWork(request.IdempotencyToken, recipient.email, result.Recipients[index].PMUID))
	}
	return cellar.Complete{ApplicationWork: work}
}

// newEmail rebuilds the provider-neutral request for all pending recipients.
func newEmail(common durableRequest, pending []durableRecipient) clients.Email {
	headers := maps.Clone(common.headers)
	if headers == nil {
		headers = make(map[string]string, 1)
	}
	headers[MessageIDHeader] = common.messageID

	recipients := make([]clients.Recipient, 0, len(pending))
	for _, recipient := range pending {
		recipients = append(recipients, clients.Recipient{
			Address:   clients.Address{Email: recipient.email, Name: recipient.name},
			Variables: recipient.variables,
		})
	}

	return clients.Email{
		From:       clients.Address{Email: common.senderEmail, Name: common.senderName},
		To:         recipients,
		Subject:    common.subject,
		TemplateID: common.templateID,
		Text:       common.text,
		Variables:  common.variables,
		Headers:    headers,
	}
}

// VerifyHandler resolves recipients the provider has not yet confirmed.
type VerifyHandler struct {
	Store    *Store
	Verifier clients.EmailVerifier
}

func (h VerifyHandler) Handle(ctx context.Context, request operationRequest) cellar.Result {
	candidates, err := h.Store.verifyCandidates(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read verification candidates", Err: err}
	}
	if len(candidates) == 0 {
		return cellar.Complete{}
	}

	common, err := h.Store.request(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read durable email request", Err: err}
	}

	work := make([]cellar.ApplicationWork, 0, len(candidates))
	outstanding := 0
	for _, candidate := range candidates {
		result, err := h.Verifier.Verify(ctx, clients.VerifyRequest{
			CorrelationID: common.messageID,
			Recipient:     candidate.recipient,
			SentAt:        candidate.submittedAt,
		})
		if err != nil {
			return cellar.ErrorResult{Message: fmt.Sprintf("verify %q", candidate.recipient), Err: err}
		}
		if !result.Found {
			outstanding++
			continue
		}
		work = append(work, h.Store.acceptWork(request.IdempotencyToken, candidate.recipient, result.PMUID))
	}

	if outstanding > 0 {
		// Retry repeats this step, so unresolved recipients are looked for again.
		notBefore := time.Now().UTC().Add(VerifyDelay)
		return cellar.Retry{NotBefore: &notBefore, ApplicationWork: work}
	}
	return cellar.Complete{ApplicationWork: work}
}
