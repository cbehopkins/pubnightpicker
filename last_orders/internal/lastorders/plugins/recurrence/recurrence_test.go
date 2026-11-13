package recurrenceplugin

import (
	"context"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/truths"
)

func TestEvaluateEventVenueHandlerCreatesStaleEventTruth(t *testing.T) {
	handler := evaluator(t)
	result := handler.Handle(context.Background(), truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-1",
			Recurrence:         map[string]any{"frequency": "once", "date": "2030-01-02"},
			NextOccurrenceDate: "",
		},
		ObservedOn: "2030-01-01",
	})

	step := emittedTruthStep(t, result)
	if step.Listener != listenerStaleEvents {
		t.Fatalf("listener = %q; want %q", step.Listener, listenerStaleEvents)
	}
	if step.EventKey != recurrence.StaleEventKey("event-1", "", map[string]any{"frequency": "once", "date": "2030-01-02"}) {
		t.Fatalf("event key = %q", step.EventKey)
	}
	if step.Truth.FanoutName != truths.StaleEventFanout {
		t.Fatalf("fanout = %q; want %q", step.Truth.FanoutName, truths.StaleEventFanout)
	}
}

func TestEvaluateEventVenueHandlerCreatesDueEventTruth(t *testing.T) {
	handler := evaluator(t)
	result := handler.Handle(context.Background(), truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-2",
			Recurrence:         map[string]any{"frequency": "once", "date": "2030-01-05"},
			NextOccurrenceDate: "2030-01-05",
		},
		ObservedOn: "2030-01-01",
	})

	step := emittedTruthStep(t, result)
	if step.Listener != listenerEventDue {
		t.Fatalf("listener = %q; want %q", step.Listener, listenerEventDue)
	}
	if step.Truth.FanoutName != truths.CreateEventPollFanout {
		t.Fatalf("fanout = %q; want %q", step.Truth.FanoutName, truths.CreateEventPollFanout)
	}
}

func TestEvaluateEventVenueHandlerUsesObservedDate(t *testing.T) {
	handler := evaluator(t)
	result := handler.Handle(context.Background(), truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-3",
			Recurrence:         map[string]any{"frequency": "once", "date": "2030-02-01"},
			NextOccurrenceDate: "2030-02-01",
		},
		ObservedOn: "2030-01-01",
	})

	complete, ok := result.(cellar.Complete)
	if !ok {
		t.Fatalf("result = %T; want cellar.Complete", result)
	}
	if len(complete.NewCells) != 0 {
		t.Fatalf("new cells = %d; want 0", len(complete.NewCells))
	}
}

func TestEvaluateEventVenueHandlerReleasesObservationDuringShutdown(t *testing.T) {
	cache := &observationClaimCache{owner: "observation-owner", exists: true}
	gate := &firebaseidempotency.ClaimGate{}
	gate.Close()
	handler := evaluator(t)
	handler.ClaimGate = gate
	handler.ClaimCache = cache
	handler.ClaimListener = "event_venue_observed"

	observation := truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-shutdown",
			NextOccurrenceDate: "2030-01-05",
		},
		ObservedOn: "2030-01-01",
	}
	result := handler.Handle(context.Background(), observation)

	kill, ok := result.(cellar.Kill)
	if !ok {
		t.Fatalf("result = %T; want cellar.Kill", result)
	}
	if len(kill.NewCells) != 1 || len(kill.NewCells[0].Steps) != 1 || kill.NewCells[0].Steps[0].HandlerName != firebaseidempotency.HandlerReleaseRemote {
		t.Fatalf("new cells = %#v; want one Firebase claim release cell", kill.NewCells)
	}
	if len(kill.ApplicationWork) != 1 {
		t.Fatalf("application work = %d; want one local cache retirement", len(kill.ApplicationWork))
	}
	payload, err := cellar.JSONCodec[firebaseidempotency.ReleasePayload]().Unmarshal(kill.NewCells[0].Steps[0].Payload)
	if err != nil {
		t.Fatalf("decode release payload: %v", err)
	}
	if payload.Listener != "event_venue_observed" || payload.EventKey != observation.Identity() || payload.Owner != cache.owner {
		t.Fatalf("release payload = %#v", payload)
	}
}

type observationClaimCache struct {
	owner  string
	exists bool
}

func (c *observationClaimCache) LookupOwner(context.Context, string, string) (string, bool, error) {
	return c.owner, c.exists, nil
}

func (*observationClaimCache) RetireCacheEntryWork(string, string, string) cellar.ApplicationWork {
	return func(cellar.ApplicationTx) error { return nil }
}

func evaluator(t *testing.T) EvaluateEventVenueHandler {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return EvaluateEventVenueHandler{Store: cellar.NewMemoryStore(nil), Location: loc}
}

func emittedTruthStep(t *testing.T, result cellar.Result) firebaseidempotency.StepPayload {
	t.Helper()
	complete, ok := result.(cellar.Complete)
	if !ok {
		t.Fatalf("result = %T; want cellar.Complete", result)
	}
	if len(complete.NewCells) != 1 || len(complete.NewCells[0].Steps) != 3 {
		t.Fatalf("new cells = %#v; want one idempotency sequence", complete.NewCells)
	}
	payload, err := cellar.JSONCodec[firebaseidempotency.StepPayload]().Unmarshal(complete.NewCells[0].Steps[0].Payload)
	if err != nil {
		t.Fatalf("decode idempotency payload: %v", err)
	}
	return payload
}
