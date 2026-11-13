package recurrenceplugin

import (
	"context"
	"fmt"
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

// StaleEventAdvancer recalculates the next occurrence of an event whose
// recurrence rule has moved on. Satisfied by *recurrence.Service.
type StaleEventAdvancer interface {
	AdvanceStaleEvent(ctx context.Context, eventID string) error
}

// EventPollCreator materialises the poll for a due occurrence. Satisfied by *recurrence.Service.
type EventPollCreator interface {
	CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error
}

type ObservationClaimCache interface {
	LookupOwner(context.Context, string, string) (string, bool, error)
	RetireCacheEntryWork(string, string, string) cellar.ApplicationWork
}

type EvaluateEventVenueHandler struct {
	Store         cellar.Store
	Location      *time.Location
	ClaimGate     *firebaseidempotency.ClaimGate
	ClaimCache    ObservationClaimCache
	ClaimListener string
	Logger        *slog.Logger
}

func (h EvaluateEventVenueHandler) Handle(ctx context.Context, payload truths.EventVenueObserved) cellar.Result {
	if payload.Venue.ID == "" || payload.ObservedOn == "" {
		return cellar.Complete{}
	}
	if h.claimGateClosed() {
		return h.relinquishObservation(ctx, payload)
	}
	if h.Store == nil || h.Location == nil {
		return cellar.ErrorResult{Message: "event venue evaluator dependencies are nil"}
	}

	observedOn, err := time.ParseInLocation(time.DateOnly, payload.ObservedOn, h.Location)
	if err != nil {
		return cellar.ErrorResult{Message: "parse event venue observation date", Err: err}
	}

	needsRecalculation := recurrence.NeedsRecalculation(payload.Venue.Recurrence, payload.Venue.NextOccurrenceDate, observedOn, h.Location)
	isDue := recurrence.IsDue(payload.Venue.NextOccurrenceDate, observedOn, h.Location)
	if h.claimGateClosed() {
		return h.relinquishObservation(ctx, payload)
	}
	if needsRecalculation {
		return h.createStaleEventTruth(payload.Venue)
	}
	if isDue {
		return h.createEventDueTruth(payload.Venue)
	}
	return cellar.Complete{}
}

func (h EvaluateEventVenueHandler) claimGateClosed() bool {
	return h.ClaimGate != nil && h.ClaimGate.IsClosed()
}

func (h EvaluateEventVenueHandler) relinquishObservation(ctx context.Context, payload truths.EventVenueObserved) cellar.Result {
	if h.ClaimCache == nil || h.ClaimListener == "" {
		return cellar.ErrorResult{Message: "relinquish event venue observation claim", Err: fmt.Errorf("claim cache and listener are required")}
	}
	eventKey := payload.Identity()
	owner, exists, err := h.ClaimCache.LookupOwner(ctx, h.ClaimListener, eventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "lookup event venue observation owner", Err: err}
	}
	if !exists || owner == "" {
		return cellar.ErrorResult{Message: "lookup event venue observation owner", Err: fmt.Errorf("owner is unresolved for %q/%q", h.ClaimListener, eventKey)}
	}
	releaseRequest, err := firebaseidempotency.NewReleaseRequest(h.ClaimListener, eventKey, owner)
	if err != nil {
		return cellar.ErrorResult{Message: "build event venue observation release", Err: err}
	}
	return cellar.Kill{
		NewCells:        []cellar.CellRequest{releaseRequest},
		ApplicationWork: []cellar.ApplicationWork{h.ClaimCache.RetireCacheEntryWork(h.ClaimListener, eventKey, owner)},
	}
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
	Service StaleEventAdvancer
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
	Service EventPollCreator
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
