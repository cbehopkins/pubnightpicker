package handlers

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/recurrence"
)

const (
	HandlerStaleEvent      cellar.HandlerName = "recurrence.stale_event"
	HandlerCreateEventPoll cellar.HandlerName = "recurrence.create_event_poll"
)

type StaleEventPayload struct {
	EventID string `json:"event_id"`
	// ObservedDate is what the listener saw; the Cell revalidates against current state.
	ObservedDate string `json:"observed_date"`
}

type CreateEventPollPayload struct {
	EventID        string `json:"event_id"`
	OccurrenceDate string `json:"occurrence_date"`
}

type StaleEventHandler struct {
	Service *recurrence.Service
	Logger  *slog.Logger
}

func (h StaleEventHandler) Handle(ctx context.Context, payload StaleEventPayload) cellar.Result {
	if h.Service == nil {
		return cellar.ErrorResult{Message: "recurrence service is nil"}
	}
	if payload.EventID == "" {
		return cellar.Complete{}
	}

	if err := h.Service.AdvanceStaleEvent(ctx, payload.EventID); err != nil {
		return cellar.ErrorResult{Message: "advance stale event", Err: err}
	}

	if h.Logger != nil {
		h.Logger.Info("stale event processed", "event_id", payload.EventID, "observed_date", payload.ObservedDate)
	}
	return cellar.Complete{}
}

type CreateEventPollHandler struct {
	Service *recurrence.Service
	Logger  *slog.Logger
}

func (h CreateEventPollHandler) Handle(ctx context.Context, payload CreateEventPollPayload) cellar.Result {
	if h.Service == nil {
		return cellar.ErrorResult{Message: "recurrence service is nil"}
	}
	if payload.EventID == "" || payload.OccurrenceDate == "" {
		return cellar.Complete{}
	}

	if err := h.Service.CreateEventPoll(ctx, payload.EventID, payload.OccurrenceDate); err != nil {
		return cellar.ErrorResult{Message: "create event poll", Err: err}
	}

	if h.Logger != nil {
		h.Logger.Info("event poll materialisation processed", "event_id", payload.EventID, "occurrence_date", payload.OccurrenceDate)
	}
	return cellar.Complete{}
}
