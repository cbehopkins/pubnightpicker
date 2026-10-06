package polls

import (
	"context"
	"fmt"
	"log/slog"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/diagnosticsconfig"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/truths"
)

const HandlerPollOpenedEmail cellar.HandlerName = "polls.poll_opened_email"

const (
	pollEmailSenderEmail = "ampubnight@contable.co.uk"
	pollEmailSenderName  = "ampubnight notification emails"
	pollOpenedSubject    = "Pub Night voting opened"
	pollOpenedBody       = `Voting has opened for this week's pub night.
Please visit %s/active_polls
to participate in the voting.
`
)

// EmailRecipientSource selects users eligible for a kind of notification email.
type EmailRecipientSource interface {
	GetEligibleEmailRecipients(ctx context.Context, kind notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error)
}

// PollOpenedEmailHandler creates one durable email Send for a newly opened poll.
type PollOpenedEmailHandler struct {
	Actions    completionactions.Store
	Recipients EmailRecipientSource
	BaseURL    string
	Logger     *slog.Logger
}

func (h PollOpenedEmailHandler) Handle(ctx context.Context, payload truths.PollObservedPayload) cellar.Result {
	if payload.PollID == "" {
		return cellar.Complete{}
	}
	record, err := h.Actions.Get(ctx, completionactions.OpenCollection, payload.PollID)
	if err != nil {
		return cellar.ErrorResult{Message: "read poll-opened actions", Err: err}
	}
	if !record.NeedsAction(completionactions.ActionEmail, payload.PollID) {
		return cellar.Complete{}
	}
	mark := openedMark(payload.PollID, completionactions.ActionEmail)

	recipients, err := h.Recipients.GetEligibleEmailRecipients(ctx, notificationprofile.EmailPollOpens)
	if err != nil {
		return cellar.ErrorResult{Message: "select poll-opened email recipients", Err: err}
	}
	steps := []cellar.Step{mark}
	if len(recipients) == 0 {
		if h.Logger != nil {
			h.Logger.Info("no poll-opened email recipients", "poll_id", payload.PollID)
		}
	} else {
		request := durableemail.SendRequest{
			IdempotencyToken: "poll-opened:" + payload.PollID,
			SenderEmail:      pollEmailSenderEmail,
			SenderName:       pollEmailSenderName,
			Subject:          pollOpenedSubject,
			Text:             fmt.Sprintf(pollOpenedBody, h.BaseURL),
			Recipients:       make([]durableemail.SendRecipient, 0, len(recipients)),
		}
		for _, recipient := range recipients {
			request.Recipients = append(request.Recipients, durableemail.SendRecipient{UserID: recipient.UserID, Email: recipient.Email})
		}
		steps = append(personalSendSteps(request, diagnosticsconfig.PurposePollOpened, payload.CreatedByUID, h.Logger), mark)
	}

	sequence, err := cellar.NewSequence(steps...)
	if err != nil {
		return cellar.ErrorResult{Message: "build poll-opened email send", Err: err}
	}
	send, err := sequence.CellRequest()
	if err != nil {
		return cellar.ErrorResult{Message: "build poll-opened email send", Err: err}
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{send}}
}

func personalSendSteps(request durableemail.SendRequest, purpose, actorUID string, logger *slog.Logger) []cellar.Step {
	if actorUID == "" {
		if logger != nil {
			logger.Info("poll email has no actor attribution; no actor exception", "idempotency_token", request.IdempotencyToken, "purpose", purpose)
		}
		return durableemail.NewSendSequence(request)
	}
	var actor []durableemail.SendRecipient
	var others []durableemail.SendRecipient
	for _, recipient := range request.Recipients {
		if recipient.UserID == actorUID {
			actor = append(actor, recipient)
		} else {
			others = append(others, recipient)
		}
	}
	if len(actor) != 1 {
		if logger != nil {
			logger.Info("poll email actor is not an eligible recipient; no actor exception", "idempotency_token", request.IdempotencyToken, "actor_uid", actorUID, "purpose", purpose, "eligible_recipient_count", len(request.Recipients))
		}
		return durableemail.NewSendSequence(request)
	}
	var steps []cellar.Step
	if len(others) > 0 {
		remainder := request
		remainder.IdempotencyToken += ":others"
		remainder.Recipients = others
		steps = append(steps, durableemail.NewSendSequence(remainder)...)
	}
	actorRequest := request
	actorRequest.IdempotencyToken += ":actor:" + actorUID
	actorRequest.Recipients = actor
	actorRequest.Metadata = map[string]string{"purpose": purpose, "actor_uid": actorUID, "recipient_uid": actorUID}
	return append(steps, durableemail.NewSendSequence(actorRequest)...)
}

// openedMark records an open action using Python's bare poll ID key.
func openedMark(pollID string, action completionactions.ActionType) cellar.Step {
	return cellar.Step{HandlerName: HandlerCompletionMarked, Payload: completionMark{
		Collection: completionactions.OpenCollection,
		PollID:     pollID,
		Action:     action,
		Key:        pollID,
	}}
}
