package polls

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerPollCompletedEmail cellar.HandlerName = "polls.poll_completed_email"
	HandlerCompletionMarked   cellar.HandlerName = "polls.completion_action_marked"

	missingVenueRetryDelay = 5 * time.Minute
)

// VenueSource resolves venue details by ID.
type VenueSource interface {
	Get(ctx context.Context, venueID string) (venuecache.VenueProjection, error)
}

// PollCompletedEmailHandler sends Python-equivalent mailing-list and personal emails.
type PollCompletedEmailHandler struct {
	Actions    completionactions.Store
	Venues     VenueSource
	Recipients EmailRecipientSource
	Logger     *slog.Logger
}

type completionMark struct {
	Collection completionactions.Collection `json:"collection,omitempty"`
	PollID     string                       `json:"poll_id"`
	Action     completionactions.ActionType `json:"action"`
	Key        string                       `json:"key"`
}

func (h PollCompletedEmailHandler) Handle(ctx context.Context, payload truths.PollObservedPayload) cellar.Result {
	if payload.PollID == "" || payload.SelectedVenueID == "" || payload.PollDate == "" {
		h.warn("completed poll lacks email data", "poll_id", payload.PollID)
		return cellar.Complete{}
	}
	key := completionactions.Key(payload.SelectedVenueID, payload.SelectedRestaurantID, payload.SelectedRestaurantTime)
	record, err := h.Actions.Get(ctx, completionactions.CompletionCollection, payload.PollID)
	if err != nil {
		return cellar.ErrorResult{Message: "read completion actions", Err: err}
	}
	actions := []completionactions.ActionType{completionactions.ActionEmail, completionactions.ActionPersonalEmail}
	if !record.NeedsAction(actions[0], key) && !record.NeedsAction(actions[1], key) {
		return cellar.Complete{}
	}

	venue, err := h.Venues.Get(ctx, payload.SelectedVenueID)
	if errors.Is(err, venuecache.ErrNotFound) {
		h.warn("completed poll venue not found yet", "poll_id", payload.PollID, "venue_id", payload.SelectedVenueID)
		notBefore := time.Now().UTC().Add(missingVenueRetryDelay)
		return cellar.Retry{NotBefore: &notBefore}
	}
	if err != nil {
		return cellar.ErrorResult{Message: "read completed poll venue", Err: err}
	}
	restaurant, err := h.restaurant(ctx, payload)
	if err != nil {
		return cellar.ErrorResult{Message: "read completed poll restaurant", Err: err}
	}

	var cells []cellar.CellRequest
	for _, action := range actions {
		if !record.NeedsAction(action, key) {
			continue
		}
		cell, err := h.actionCell(ctx, payload, key, action, record.PreviouslyActioned(action), venue, restaurant)
		if err != nil {
			return cellar.ErrorResult{Message: fmt.Sprintf("build %s completion action", action), Err: err}
		}
		cells = append(cells, cell)
	}
	return cellar.Complete{NewCells: cells}
}

func (h PollCompletedEmailHandler) restaurant(ctx context.Context, payload truths.PollObservedPayload) (*venuecache.VenueProjection, error) {
	if payload.SelectedRestaurantID == "" {
		return nil, nil
	}
	restaurant, err := h.Venues.Get(ctx, payload.SelectedRestaurantID)
	if errors.Is(err, venuecache.ErrNotFound) {
		h.warn("completed poll restaurant not found", "poll_id", payload.PollID, "venue_id", payload.SelectedRestaurantID)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &restaurant, nil
}

func (h PollCompletedEmailHandler) actionCell(ctx context.Context, payload truths.PollObservedPayload, key string, action completionactions.ActionType, rescheduled bool, venue venuecache.VenueProjection, restaurant *venuecache.VenueProjection) (cellar.CellRequest, error) {
	mark := cellar.Step{HandlerName: HandlerCompletionMarked, Payload: completionMark{Collection: completionactions.CompletionCollection, PollID: payload.PollID, Action: action, Key: key}}

	personal := action == completionactions.ActionPersonalEmail
	recipients := []durableemail.SendRecipient{{Email: pollCompletedMailingList}}
	if personal {
		eligible, err := h.Recipients.GetEligibleEmailRecipients(ctx, notificationprofile.EmailPollCompletes)
		if err != nil {
			return cellar.CellRequest{}, err
		}
		recipients = make([]durableemail.SendRecipient, 0, len(eligible))
		for _, recipient := range eligible {
			recipients = append(recipients, durableemail.SendRecipient{
				Email:     recipient.Email,
				Variables: map[string]any{"uid": recipient.UserID},
			})
		}
	}

	steps := []cellar.Step{mark}
	if len(recipients) > 0 {
		content := newCompletionContent(venue, restaurant, payload.PollDate, payload.SelectedRestaurantTime, rescheduled, personal)
		request := durableemail.SendRequest{
			IdempotencyToken: "poll-completed:" + payload.PollID + ":" + string(action) + ":" + key,
			SenderEmail:      pollEmailSenderEmail,
			SenderName:       pollEmailSenderName,
			Subject:          content.Subject,
			Text:             content.Text,
			Variables:        content.Variables,
			Recipients:       recipients,
		}
		// The marker follows Post, so history is written only after provider acceptance.
		steps = append(durableemail.NewSendSequence(request), mark)
	}
	sequence, err := cellar.NewSequence(steps...)
	if err != nil {
		return cellar.CellRequest{}, err
	}
	return sequence.CellRequest()
}

func (h PollCompletedEmailHandler) warn(message string, args ...any) {
	if h.Logger != nil {
		h.Logger.Warn(message, args...)
	}
}

// CompletionMarkedHandler records an accepted poll action in its Firestore history.
type CompletionMarkedHandler struct {
	Actions completionactions.Store
}

func (h CompletionMarkedHandler) Handle(ctx context.Context, mark completionMark) cellar.Result {
	collection := mark.Collection
	if collection == "" {
		collection = completionactions.CompletionCollection
	}
	if err := h.Actions.Mark(ctx, collection, mark.PollID, mark.Action, mark.Key); err != nil {
		return cellar.ErrorResult{Message: "record completion action", Err: err}
	}
	return cellar.Complete{}
}
