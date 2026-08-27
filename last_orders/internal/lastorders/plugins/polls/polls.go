package polls

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
)

const (
	HandlerNewPoll       cellar.HandlerName = "polls.new"
	HandlerCompletedPoll cellar.HandlerName = "polls.completed"
)

// Fact names registered against the handlers above.
const (
	FactNewPoll       = "NewPoll"
	FactCompletedPoll = "CompletedPoll"
)

type PollObservedPayload struct {
	PollID                 string `json:"poll_id"`
	ChangeKind             string `json:"change_kind,omitempty"`
	SelectedRestaurantID   string `json:"selected_restaurant_id,omitempty"`
	SelectedRestaurantTime string `json:"selected_restaurant_time,omitempty"`
}

type NewPollHandler struct {
	Logger *slog.Logger
}

func (h NewPollHandler) Handle(ctx context.Context, payload PollObservedPayload) cellar.Result {
	_ = ctx
	if payload.PollID == "" {
		return cellar.Complete{}
	}
	if h.Logger != nil {
		h.Logger.Info("new poll processed", "poll_id", payload.PollID)
	}
	return cellar.Complete{}
}

type CompletedPollHandler struct {
	Logger *slog.Logger
}

func (h CompletedPollHandler) Handle(ctx context.Context, payload PollObservedPayload) cellar.Result {
	_ = ctx
	if payload.PollID == "" {
		return cellar.Complete{}
	}
	if h.Logger != nil {
		h.Logger.Info("completed poll processed", "poll_id", payload.PollID, "change_kind", payload.ChangeKind)
	}
	return cellar.Complete{}
}
