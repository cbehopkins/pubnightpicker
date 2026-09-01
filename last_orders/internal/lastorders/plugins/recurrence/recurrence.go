package recurrenceplugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerEvaluateEventVenue cellar.HandlerName = "recurrence.evaluate_event_venue"
	HandlerStaleEvent         cellar.HandlerName = "recurrence.stale_event"
	HandlerCreateEventPoll    cellar.HandlerName = "recurrence.create_event_poll"
)

// Fact names registered against the handlers above.
const (
	FactStaleEvent      = "StaleEvent"
	FactCreateEventPoll = "CreateEventPoll"
)

const (
	listenerStaleEvents = "stale_events"
	listenerEventDue    = "event_due"
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
		return h.createStaleEventFact(payload.Venue)
	}
	if recurrence.IsDue(payload.Venue.NextOccurrenceDate, observedOn, h.Location) {
		return h.createEventDueFact(payload.Venue)
	}
	return cellar.Complete{}
}

func (h EvaluateEventVenueHandler) createStaleEventFact(venue recurrence.EventVenue) cellar.Result {
	payload, err := cellar.JSONCodec[StaleEventPayload]().Marshal(StaleEventPayload{EventID: venue.ID, ObservedDate: venue.NextOccurrenceDate})
	if err != nil {
		return cellar.ErrorResult{Message: "marshal stale event payload", Err: err}
	}
	return h.createFact(listenerStaleEvents, recurrence.StaleEventKey(venue.ID, venue.NextOccurrenceDate, venue.Recurrence), FactStaleEvent, payload)
}

func (h EvaluateEventVenueHandler) createEventDueFact(venue recurrence.EventVenue) cellar.Result {
	payload, err := cellar.JSONCodec[CreateEventPollPayload]().Marshal(CreateEventPollPayload{EventID: venue.ID, OccurrenceDate: venue.NextOccurrenceDate})
	if err != nil {
		return cellar.ErrorResult{Message: "marshal create event poll payload", Err: err}
	}
	return h.createFact(listenerEventDue, recurrence.EventDueKey(venue.ID, venue.NextOccurrenceDate), FactCreateEventPoll, payload)
}

func (h EvaluateEventVenueHandler) createFact(listener, eventKey, factName string, payload []byte) cellar.Result {
	request, err := firebaseidempotency.NewCellRequest(listener, eventKey, facts.Fact{Name: factName, Payload: payload})
	if err != nil {
		return cellar.ErrorResult{Message: fmt.Sprintf("build %s idempotency cell", factName), Err: err}
	}
	if h.Logger != nil {
		h.Logger.Info("event venue evaluation created fact", "listener", listener, "event_key", eventKey, "fact", factName)
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{request}}
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
