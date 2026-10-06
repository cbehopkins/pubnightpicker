package durableemail

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"cellar/pkg/cellar"
	"email_clients/clients"
)

const (
	HandlerSetup          cellar.HandlerName = "durable_email.setup"
	HandlerPost           cellar.HandlerName = "durable_email.post"
	HandlerRecovery       cellar.HandlerName = "durable_email.recovery"
	HandlerRecoveryFanout cellar.HandlerName = "durable_email.recovery_fanout"
	HandlerVerify         cellar.HandlerName = "durable_email.verify"
)

// VerifyDelay is the minimum interval from submission to provider verification.
const VerifyDelay = 120 * time.Second

// MessageIDHeader correlates a provider message with its progress row.
const MessageIDHeader = "X-Pubnight-Message-ID"

// SendRecipient is the recipient-specific part of a logical Send request.
type SendRecipient struct {
	UserID    string         `json:"user_id,omitempty"`
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
	Metadata         map[string]string `json:"metadata,omitempty"`
	Recipients       []SendRecipient   `json:"recipients"`
}

// NewSendSequence describes the durable steps of a logical Send operation.
func NewSendSequence(request SendRequest) []cellar.Step {
	return []cellar.Step{
		{HandlerName: HandlerSetup, Payload: request},
		{HandlerName: HandlerRecovery, Payload: operationRequest{IdempotencyToken: request.IdempotencyToken}},
		{HandlerName: HandlerPost, Payload: operationRequest{IdempotencyToken: request.IdempotencyToken}},
	}
}

// operationRequest identifies the Send operation a step acts on.
type operationRequest struct {
	IdempotencyToken string `json:"idempotency_token"`
}

// Submission describes the pending recipients in one provider submission attempt.
type Submission struct {
	IdempotencyToken string
	RecipientCount   int
	Metadata         map[string]string
}

// SubmissionGuard permits a submission, defers it by a positive delay, or fails closed.
type SubmissionGuard func(context.Context, Submission) (time.Duration, error)

type SubmissionDecision struct {
	Client     clients.EmailClient
	BestEffort bool
	Delay      time.Duration
}

type SubmissionPolicy func(context.Context, Submission) (SubmissionDecision, error)

type RegisterOptions struct {
	SubmissionGuard  SubmissionGuard
	SubmissionPolicy SubmissionPolicy
	Logger           *slog.Logger
}

// Register binds every durable email handler to runtime; call before Cellar.Start.
func Register(runtime *cellar.Cellar, store *Store, client clients.EmailClient, verifier clients.EmailVerifier, options ...RegisterOptions) error {
	if store == nil || client == nil || verifier == nil {
		return fmt.Errorf("durable email store, client, and verifier are required")
	}
	if len(options) > 1 {
		return fmt.Errorf("at most one durable email registration option set is allowed")
	}
	var opts RegisterOptions
	if len(options) == 1 {
		opts = options[0]
	}
	if err := runtime.Register(HandlerSetup, SetupHandler{Store: store}); err != nil {
		return err
	}
	if err := runtime.Register(HandlerRecovery, RecoveryHandler{Store: store}); err != nil {
		return err
	}
	fanout, err := NewRecoveryFanout(store)
	if err != nil {
		return err
	}
	if err := fanout.Register(runtime); err != nil {
		return err
	}
	if err := runtime.Register(HandlerPost, PostHandler{Store: store, Client: client, Guard: opts.SubmissionGuard, Policy: opts.SubmissionPolicy, Logger: opts.Logger}); err != nil {
		return err
	}
	return runtime.Register(HandlerVerify, VerifyHandler{Store: store, Verifier: verifier})
}

type verifyRequest struct {
	IdempotencyToken string `json:"idempotency_token"`
	Recipient        string `json:"recipient"`
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

// RecoveryHandler waits for independent verification before permitting another Post.
type RecoveryHandler struct {
	Store *Store
}

func (h RecoveryHandler) Handle(ctx context.Context, request operationRequest) cellar.Result {
	candidates, err := h.Store.recoveryCandidates(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read recovery recipients", Err: err}
	}
	if len(candidates) > 0 {
		deadline := candidates[0].submittedAt.Add(VerifyDelay)
		for _, candidate := range candidates[1:] {
			if later := candidate.submittedAt.Add(VerifyDelay); later.After(deadline) {
				deadline = later
			}
		}
		definition, err := cellar.NewCellDefinition(HandlerRecoveryFanout, request)
		if err != nil {
			return cellar.ErrorResult{Message: "prepare recovery fanout", Err: err}
		}
		child, err := definition.CellRequest()
		if err != nil {
			return cellar.ErrorResult{Message: "prepare recovery fanout", Err: err}
		}
		child.NotBefore = &deadline
		return cellar.Retry{
			NotBefore:       &deadline,
			NewCells:        []cellar.CellRequest{child},
			ApplicationWork: []cellar.ApplicationWork{h.Store.waitWork(request.IdempotencyToken)},
		}
	}
	waiting, err := h.Store.waitingRecipients(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read waiting recipients", Err: err}
	}
	if len(waiting) > 0 {
		notBefore := time.Now().UTC().Add(time.Second)
		return cellar.Retry{NotBefore: &notBefore}
	}
	return cellar.Complete{}
}

// NewRecoveryFanout registers a durable expansion into per-recipient Verify cells.
func NewRecoveryFanout(store *Store) (*cellar.Fanout[operationRequest], error) {
	return cellar.NewFanout(HandlerRecoveryFanout, cellar.FanoutExpanderFunc[operationRequest](
		func(ctx context.Context, _ cellar.CellID, request operationRequest) ([]cellar.FanoutTarget, error) {
			candidates, err := store.waitingRecipients(ctx, request.IdempotencyToken)
			if err != nil {
				return nil, err
			}
			targets := make([]cellar.FanoutTarget, 0, len(candidates))
			for _, candidate := range candidates {
				definition, err := cellar.NewCellDefinition(HandlerVerify, verifyRequest{
					IdempotencyToken: request.IdempotencyToken,
					Recipient:        candidate.recipient,
				})
				if err != nil {
					return nil, err
				}
				targets = append(targets, cellar.FanoutTarget{Key: candidate.recipient, Cell: definition})
			}
			return targets, nil
		}),
	)
}

// PostHandler submits pending recipients to the email provider.
type PostHandler struct {
	Store  *Store
	Client clients.EmailClient
	Guard  SubmissionGuard
	Policy SubmissionPolicy
	Logger *slog.Logger
}

func (h PostHandler) Handle(ctx context.Context, request operationRequest) cellar.Result {
	unresolved, err := h.Store.hasUnresolved(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read recovery state", Err: err}
	}
	if unresolved {
		return cellar.RetrySequence{}
	}
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

	submission := Submission{IdempotencyToken: request.IdempotencyToken, RecipientCount: len(pending), Metadata: maps.Clone(common.metadata)}
	decision := SubmissionDecision{Client: h.Client}
	if h.Policy != nil {
		decision, err = h.Policy(ctx, submission)
		if err != nil {
			return cellar.ErrorResult{Message: "select email submission policy", Err: err}
		}
		if decision.Client == nil {
			decision.Client = h.Client
		}
	}
	if decision.Delay < 0 {
		return cellar.ErrorResult{Message: "email submission policy returned a negative delay"}
	}
	if decision.Delay > 0 {
		notBefore := time.Now().UTC().Add(decision.Delay)
		return cellar.Retry{NotBefore: &notBefore}
	}
	if !decision.BestEffort && h.Guard != nil {
		delay, err := h.Guard(ctx, submission)
		if err != nil {
			return cellar.ErrorResult{Message: "guard email submission", Err: err}
		}
		if delay < 0 {
			return cellar.ErrorResult{Message: "email submission guard returned a negative delay"}
		}
		if delay > 0 {
			notBefore := time.Now().UTC().Add(delay)
			return cellar.Retry{NotBefore: &notBefore}
		}
	}

	if decision.BestEffort {
		if err := h.Store.acceptBestEffort(ctx, request.IdempotencyToken, common.messageID, pending); err != nil {
			return cellar.ErrorResult{Message: "record best-effort acceptance", Err: err}
		}
		result, sendErr := decision.Client.Send(ctx, newEmail(common, pending))
		if sendErr == nil {
			if len(result.Recipients) != len(pending) {
				sendErr = fmt.Errorf("best-effort send returned %d results for %d recipients", len(result.Recipients), len(pending))
			} else {
				for _, recipient := range result.Recipients {
					if recipient.PMUID == "" {
						sendErr = fmt.Errorf("best-effort send returned an empty provider identifier")
						break
					}
				}
			}
		}
		logger := h.Logger
		if logger == nil {
			logger = slog.Default()
		}
		if sendErr != nil {
			logger.Warn("best-effort email failed; already handled without retry", "idempotency_token", request.IdempotencyToken, "err", sendErr)
		} else {
			logger.Info("best-effort email handled", "idempotency_token", request.IdempotencyToken, "recipient_count", len(pending))
		}
		return cellar.Complete{}
	}

	// Recorded first so an interruption after the provider call remains verifiable.
	if err := h.Store.markSubmitted(ctx, request.IdempotencyToken, time.Now().UTC()); err != nil {
		return cellar.ErrorResult{Message: "record submission time", Err: err}
	}

	result, err := decision.Client.Send(ctx, newEmail(common, pending))
	if err != nil {
		return cellar.RetrySequence{ApplicationWork: []cellar.ApplicationWork{h.Store.recoverWork(request.IdempotencyToken)}}
	}
	if len(result.Recipients) != len(pending) {
		return cellar.RetrySequence{ApplicationWork: []cellar.ApplicationWork{h.Store.recoverWork(request.IdempotencyToken)}}
	}

	work := make([]cellar.ApplicationWork, 0, len(pending))
	for index, recipient := range pending {
		if result.Recipients[index].PMUID == "" {
			return cellar.RetrySequence{ApplicationWork: []cellar.ApplicationWork{h.Store.recoverWork(request.IdempotencyToken)}}
		}
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

func (h VerifyHandler) Handle(ctx context.Context, request verifyRequest) cellar.Result {
	submittedAt, waiting, err := h.Store.waitingRecipient(ctx, request.IdempotencyToken, request.Recipient)
	if err != nil {
		return cellar.ErrorResult{Message: "read verification recipient", Err: err}
	}
	if !waiting {
		return cellar.Complete{}
	}

	common, err := h.Store.request(ctx, request.IdempotencyToken)
	if err != nil {
		return cellar.ErrorResult{Message: "read durable email request", Err: err}
	}

	result, err := h.Verifier.Verify(ctx, clients.VerifyRequest{
		CorrelationID: common.messageID,
		Recipient:     request.Recipient,
		SentAt:        submittedAt,
	})
	if err != nil {
		return cellar.ErrorResult{Message: fmt.Sprintf("verify %q", request.Recipient), Err: err}
	}
	if !result.Found {
		return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{h.Store.absentWork(request.IdempotencyToken, request.Recipient)}}
	}
	if result.PMUID == "" {
		return cellar.ErrorResult{Message: fmt.Sprintf("verify %q returned no PMUID", request.Recipient)}
	}
	return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{h.Store.acceptRecoveredWork(request.IdempotencyToken, request.Recipient, result.PMUID)}}
}
