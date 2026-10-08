package polls

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/diagnosticsconfig"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/plugins/push"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerPollOpenedPush           cellar.HandlerName = "polls.poll_opened_push"
	HandlerPollCompletedPush        cellar.HandlerName = "polls.poll_completed_push"
	HandlerManualCompletionNeedPush cellar.HandlerName = "polls.manual_completion_required_push"
)

// CompleterSource lists users holding the canCompletePoll role.
type CompleterSource interface {
	CanCompletePollUserIDs(ctx context.Context) ([]string, error)
}

// ManualCompletionPushHandler mirrors Python's send_poll_manual_completion_needed_push.
type ManualCompletionPushHandler struct {
	Completers CompleterSource
	Endpoints  EndpointSource
	Push       PushPopulator
	Now        func() time.Time
}

func (h ManualCompletionPushHandler) Handle(ctx context.Context, truth truths.PollManualCompletionRequired) cellar.Result {
	if truth.PollID == "" {
		return cellar.Complete{}
	}
	completers, err := h.Completers.CanCompletePollUserIDs(ctx)
	if err != nil {
		return cellar.ErrorResult{Message: "read poll completers", Err: err}
	}
	// An empty selector audience means everyone, so no completers must mean no push.
	if len(completers) == 0 {
		return cellar.Complete{}
	}
	endpoints, err := h.Endpoints.GetEligiblePushEndpoints(ctx, notificationprofile.Selector{
		Kind: notificationprofile.KindPollCompletes, UserIDs: completers,
	})
	if err != nil {
		return cellar.ErrorResult{Message: "select manual completion push endpoints", Err: err}
	}

	now := clock(h.Now)
	message := pushPayload{
		EventType: "poll_manual_completion_required",
		PollID:    truth.PollID,
		Title:     "Poll needs manual completion",
		Body:      truth.PollDate + ": auto-complete could not choose a clear winner. Tap to complete the poll.",
		URL:       h.Push.BaseURL() + "/active_polls",
		Tag:       "poll-manual-complete:" + truth.PollID,
		SentAt:    now.UTC().Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return cellar.ErrorResult{Message: "encode push payload", Err: err}
	}
	result, err := h.Push.Populate(ctx, push.Notification{
		ID:        "poll-manual-completion:" + truth.PollID + ":" + truth.PollDate,
		Message:   encoded,
		Topic:     push.Topic("manual-complete-" + truth.PollID),
		ExpiresAt: now.Add(push.TTL(truth.PollDate, now)),
	}, endpoints)
	if err != nil {
		return cellar.ErrorResult{Message: "populate manual completion push", Err: err}
	}
	return result
}

// EndpointSource selects push endpoints eligible for a notification kind.
type EndpointSource interface {
	GetEligiblePushEndpoints(ctx context.Context, selector notificationprofile.Selector) ([]notificationprofile.Endpoint, error)
}

// PushPopulator commits a notification's endpoint population and delivery work.
type PushPopulator interface {
	BaseURL() string
	Populate(ctx context.Context, notification push.Notification, endpoints []notificationprofile.Endpoint, then ...cellar.Step) (cellar.Complete, error)
}

// pushPayload is the browser contract in react/docs/web-push-contract.md.
type pushPayload struct {
	EventType string `json:"eventType"`
	PollID    string `json:"pollId"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	Tag       string `json:"tag"`
	SentAt    string `json:"sentAt"`
}

// PollOpenedPushHandler mirrors Python's send_poll_open_push.
type PollOpenedPushHandler struct {
	Actions   completionactions.Store
	Endpoints EndpointSource
	Push      PushPopulator
	Logger    *slog.Logger
	Now       func() time.Time
}

func (h PollOpenedPushHandler) Handle(ctx context.Context, payload truths.PollObservedPayload) cellar.Result {
	if payload.PollID == "" {
		return cellar.Complete{}
	}
	record, err := h.Actions.Get(ctx, completionactions.OpenCollection, payload.PollID)
	if err != nil {
		return cellar.ErrorResult{Message: "read poll-opened actions", Err: err}
	}
	if !record.NeedsAction(completionactions.ActionPush, payload.PollID) {
		return cellar.Complete{}
	}
	endpoints, err := h.Endpoints.GetEligiblePushEndpoints(ctx, notificationprofile.Selector{Kind: notificationprofile.KindPollOpens})
	if err != nil {
		return cellar.ErrorResult{Message: "select poll-opened push endpoints", Err: err}
	}

	now := clock(h.Now)
	message := pushPayload{
		EventType: "poll_opened",
		PollID:    payload.PollID,
		Title:     "Pub Night voting opened",
		Body:      "Voting has opened for this week's pub night. Tap to open the active polls page.",
		URL:       h.Push.BaseURL() + "/active_polls",
		Tag:       "poll-open:" + payload.PollID,
	}
	return populate(ctx, h.Push, message, now, payload.PollDate, "poll-opened:"+payload.PollID, endpoints,
		openedMark(payload.PollID, completionactions.ActionPush), payload.CreatedByUID)
}

// PollCompletedPushHandler mirrors Python's send_poll_complete_push.
type PollCompletedPushHandler struct {
	Actions   completionactions.Store
	Venues    VenueSource
	Endpoints EndpointSource
	Push      PushPopulator
	Logger    *slog.Logger
	Now       func() time.Time
}

func (h PollCompletedPushHandler) Handle(ctx context.Context, payload truths.PollObservedPayload) cellar.Result {
	if payload.PollID == "" || payload.SelectedVenueID == "" || payload.PollDate == "" {
		return cellar.Complete{}
	}
	key := completionactions.Key(payload.SelectedVenueID, payload.SelectedRestaurantID, payload.SelectedRestaurantTime)
	record, err := h.Actions.Get(ctx, completionactions.CompletionCollection, payload.PollID)
	if err != nil {
		return cellar.ErrorResult{Message: "read completion actions", Err: err}
	}
	if !record.NeedsAction(completionactions.ActionPush, key) {
		return cellar.Complete{}
	}

	venue, err := h.Venues.Get(ctx, payload.SelectedVenueID)
	if errors.Is(err, venuecache.ErrNotFound) {
		notBefore := time.Now().UTC().Add(missingVenueRetryDelay)
		return cellar.Retry{NotBefore: &notBefore}
	}
	if err != nil {
		return cellar.ErrorResult{Message: "read completed poll venue", Err: err}
	}
	var restaurantName string
	if payload.SelectedRestaurantID != "" {
		restaurant, err := h.Venues.Get(ctx, payload.SelectedRestaurantID)
		switch {
		case err == nil:
			restaurantName = restaurant.Name
		case !errors.Is(err, venuecache.ErrNotFound):
			return cellar.ErrorResult{Message: "read completed poll restaurant", Err: err}
		}
	}
	endpoints, err := h.Endpoints.GetEligiblePushEndpoints(ctx, notificationprofile.Selector{Kind: notificationprofile.KindPollCompletes})
	if err != nil {
		return cellar.ErrorResult{Message: "select poll-completed push endpoints", Err: err}
	}

	message := pushPayload{
		EventType: "poll_completed",
		PollID:    payload.PollID,
		Title:     "Pub Night @ " + venue.Name,
		Body:      completedPushBody(payload.PollDate, venue.Name, restaurantName, payload.SelectedRestaurantTime),
		URL:       h.Push.BaseURL() + "/current_events",
		Tag:       "poll-complete:" + payload.PollID,
	}
	if record.PreviouslyActioned(completionactions.ActionPush) {
		message.EventType = "poll_rescheduled"
		message.Title = "Pub Night rescheduled: " + venue.Name
	}
	mark := cellar.Step{HandlerName: HandlerCompletionMarked, Payload: completionMark{
		Collection: completionactions.CompletionCollection, PollID: payload.PollID, Action: completionactions.ActionPush, Key: key,
	}}
	return populate(ctx, h.Push, message, clock(h.Now), payload.PollDate, "poll-completed:"+payload.PollID+":"+key, endpoints, mark, payload.CompletedByUID)
}

func completedPushBody(date, venue, restaurant, restaurantTime string) string {
	body := date + ": " + venue + "."
	if restaurant != "" {
		body += " Pre-pub meal at " + restaurant + "."
	}
	if restaurantTime != "" {
		body += " Meet at " + restaurantTime + "."
	}
	return strings.TrimSpace(body)
}

func populate(ctx context.Context, populator PushPopulator, message pushPayload, now time.Time, pollDate, notificationID string, endpoints []notificationprofile.Endpoint, mark cellar.Step, actorUID ...string) cellar.Result {
	message.SentAt = now.UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(message)
	if err != nil {
		return cellar.ErrorResult{Message: "encode push payload", Err: err}
	}
	var purpose, actor string
	switch message.EventType {
	case "poll_opened":
		purpose = diagnosticsconfig.PurposePollOpened
	case "poll_completed":
		purpose = diagnosticsconfig.PurposePollCompleted
	case "poll_rescheduled":
		purpose = diagnosticsconfig.PurposePollRescheduled
	}
	if len(actorUID) == 1 {
		actor = actorUID[0]
	}
	result, err := populator.Populate(ctx, push.Notification{
		Purpose:   purpose,
		ActorUID:  actor,
		ID:        notificationID,
		Message:   encoded,
		Topic:     push.Topic(message.PollID),
		ExpiresAt: now.Add(push.TTL(pollDate, now)),
	}, endpoints, mark)
	if err != nil {
		return cellar.ErrorResult{Message: "populate push notification", Err: err}
	}
	return result
}

func clock(now func() time.Time) time.Time {
	if now == nil {
		return time.Now()
	}
	return now()
}
