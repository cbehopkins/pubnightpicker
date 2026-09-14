package recurrenceplugin

import (
	"context"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerEvaluateEventVenue cellar.HandlerName = "recurrence.evaluate_event_venue"
	HandlerStaleEvent         cellar.HandlerName = "recurrence.stale_event"
	HandlerCreateEventPoll    cellar.HandlerName = "recurrence.create_event_poll"
)

const (
	listenerStaleEvents = "stale_events"
	listenerEventDue    = "event_due"
)

type EvaluateEventVenueHandler struct {
	Store    cellar.Store
	Location *time.Location
	Logger   *slog.Logger
}

func (h EvaluateEventVenueHandler) Handle(ctx context.Context, payload truths.EventVenueObserved) cellar.Result {
	if h.Store == nil || h.Location == nil {
		return cellar.ErrorResult{Message: "event venue evaluator dependencies are nil"}
	}
	if payload.Venue.ID == "" || payload.ObservedOn == "" {
		return cellar.Complete{}
	}

	observedOn, err := time.ParseInLocation(time.DateOnly, payload.ObservedOn, h.Location)
	if err != nil {
		return cellar.ErrorResult{Message: "parse event venue observation date", Err: err}
	}

	if recurrence.NeedsRecalculation(payload.Venue.Recurrence, payload.Venue.NextOccurrenceDate, observedOn, h.Location) {
		return h.createStaleEventTruth(payload.Venue)
	}
	if recurrence.IsDue(payload.Venue.NextOccurrenceDate, observedOn, h.Location) {
		return h.createEventDueTruth(payload.Venue)
	}
	return cellar.Complete{}
}

func (h EvaluateEventVenueHandler) createStaleEventTruth(venue recurrence.EventVenue) cellar.Result {
	envelope, err := truths.NewEnvelope(truths.StaleEventFanout, truths.StaleEvent{EventID: venue.ID, ObservedDate: venue.NextOccurrenceDate})
	if err != nil {
		return cellar.ErrorResult{Message: "build stale event envelope", Err: err}
	}
	return h.createTruth(listenerStaleEvents, recurrence.StaleEventKey(venue.ID, venue.NextOccurrenceDate, venue.Recurrence), envelope)
}

func (h EvaluateEventVenueHandler) createEventDueTruth(venue recurrence.EventVenue) cellar.Result {
	envelope, err := truths.NewEnvelope(truths.CreateEventPollFanout, truths.CreateEventPoll{EventID: venue.ID, OccurrenceDate: venue.NextOccurrenceDate})
	if err != nil {
		return cellar.ErrorResult{Message: "build create event poll envelope", Err: err}
	}
	return h.createTruth(listenerEventDue, recurrence.EventDueKey(venue.ID, venue.NextOccurrenceDate), envelope)
}

func (h EvaluateEventVenueHandler) createTruth(listener, eventKey string, envelope truths.Envelope) cellar.Result {
	request, err := firebaseidempotency.NewCellRequest(listener, eventKey, envelope)
	if err != nil {
		return cellar.ErrorResult{Message: "build idempotency cell", Err: err}
	}
	if h.Logger != nil {
		h.Logger.Info("event venue evaluation created truth", "listener", listener, "event_key", eventKey, "fanout", envelope.FanoutName)
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{request}}
}

type StaleEventHandler struct {
	Service *recurrence.Service
	Logger  *slog.Logger
}

func (h StaleEventHandler) Handle(ctx context.Context, payload truths.StaleEvent) cellar.Result {
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

func (h CreateEventPollHandler) Handle(ctx context.Context, payload truths.CreateEventPoll) cellar.Result {
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
