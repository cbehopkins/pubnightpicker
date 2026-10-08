package polls

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerPollOpened    cellar.HandlerName = "polls.poll_opened"
	HandlerPollCompleted cellar.HandlerName = "polls.poll_completed"
)

type PollOpenedHandler struct {
	Logger *slog.Logger
}

func (h PollOpenedHandler) Handle(ctx context.Context, payload truths.PollObservedPayload) cellar.Result {
	_ = ctx
	if payload.PollID == "" {
		return cellar.Complete{}
	}
	if h.Logger != nil {
		h.Logger.Info("poll opened processed", "poll_id", payload.PollID)
	}
	return cellar.Complete{}
}

type PollCompletedHandler struct {
	Logger *slog.Logger
}

func (h PollCompletedHandler) Handle(ctx context.Context, payload truths.PollObservedPayload) cellar.Result {
	_ = ctx
	if payload.PollID == "" {
		return cellar.Complete{}
	}
	if h.Logger != nil {
		h.Logger.Info("poll completed processed", "poll_id", payload.PollID, "change_kind", payload.ChangeKind)
	}
	return cellar.Complete{}
}
