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

func TestEvaluateEventVenueHandlerCreatesStaleEventFact(t *testing.T) {
	handler := evaluator(t)
	result := handler.Handle(context.Background(), truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-1",
			Recurrence:         map[string]any{"frequency": "once", "date": "2030-01-02"},
			NextOccurrenceDate: "",
		},
		ObservedOn: "2030-01-01",
	})

	step := emittedFactStep(t, result)
	if step.Listener != listenerStaleEvents {
		t.Fatalf("listener = %q; want %q", step.Listener, listenerStaleEvents)
	}
	if step.EventKey != recurrence.StaleEventKey("event-1", "", map[string]any{"frequency": "once", "date": "2030-01-02"}) {
		t.Fatalf("event key = %q", step.EventKey)
	}
	if step.Fact.Name != FactStaleEvent {
		t.Fatalf("fact = %q; want %q", step.Fact.Name, FactStaleEvent)
	}
}

func TestEvaluateEventVenueHandlerCreatesDueEventFact(t *testing.T) {
	handler := evaluator(t)
	result := handler.Handle(context.Background(), truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "event-2",
			Recurrence:         map[string]any{"frequency": "once", "date": "2030-01-05"},
			NextOccurrenceDate: "2030-01-05",
		},
		ObservedOn: "2030-01-01",
	})

	step := emittedFactStep(t, result)
	if step.Listener != listenerEventDue {
		t.Fatalf("listener = %q; want %q", step.Listener, listenerEventDue)
	}
	if step.Fact.Name != FactCreateEventPoll {
		t.Fatalf("fact = %q; want %q", step.Fact.Name, FactCreateEventPoll)
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

func evaluator(t *testing.T) EvaluateEventVenueHandler {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return EvaluateEventVenueHandler{Store: cellar.NewMemoryStore(nil), Location: loc}
}

func emittedFactStep(t *testing.T, result cellar.Result) firebaseidempotency.StepPayload {
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
