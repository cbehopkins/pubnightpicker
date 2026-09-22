package autocomplete

import (
	"context"
	"fmt"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/truths"
)

const HandlerDiscovery cellar.HandlerName = "autocomplete.discovery"

const discoveryComponent = "autocomplete.discovery"

type DiscoveryHandler struct {
	Source Source
	Logger *slog.Logger
}

func (handler DiscoveryHandler) Handle(ctx context.Context, truth truths.DailyPollAutoCompleteDue) cellar.Result {
	if handler.Source == nil {
		return cellar.ErrorResult{Message: "autocomplete source is nil"}
	}
	if truth.ObservedOn == "" {
		return cellar.Complete{}
	}
	docs, err := handler.Source.ListOpenPollsOn(ctx, truth.ObservedOn)
	if err != nil {
		return cellar.ErrorResult{Message: "list eligible polls", Err: err}
	}
	requests := make([]cellar.CellRequest, 0, len(docs))
	for _, doc := range docs {
		pollTruth, err := discoveredTruth(doc, truth.ObservedOn)
		if err != nil {
			return cellar.ErrorResult{Message: "decode eligible poll", Err: err}
		}
		envelope, err := truths.NewEnvelope(truths.PollAutoCompletionDueFanout, pollTruth)
		if err != nil {
			return cellar.ErrorResult{Message: "marshal poll auto-completion truth", Err: err}
		}
		request, err := idempotency.NewCellRequest(discoveryComponent, pollTruth.Identity(), envelope)
		if err != nil {
			return cellar.ErrorResult{Message: "build poll auto-completion idempotency cell", Err: err}
		}
		requests = append(requests, request)
	}
	if handler.Logger != nil {
		handler.Logger.Info("auto-completion discovery found eligible polls", "observed_on", truth.ObservedOn, "count", len(requests))
	}
	return cellar.Complete{NewCells: requests}
}

func discoveredTruth(doc Document, observedOn string) (truths.PollAutoCompletionDue, error) {
	if doc.ID == "" {
		return truths.PollAutoCompletionDue{}, fmt.Errorf("poll document is nil")
	}
	date, ok := doc.Data["date"].(string)
	if !ok || date == "" {
		return truths.PollAutoCompletionDue{}, fmt.Errorf("poll %q date is invalid", doc.ID)
	}
	completed, ok := doc.Data["completed"].(bool)
	if !ok || completed {
		return truths.PollAutoCompletionDue{}, fmt.Errorf("poll %q completed is not false", doc.ID)
	}
	return truths.PollAutoCompletionDue{Poll: truths.PollAutoCompletionSnapshot{
		PollID: doc.ID, PollDate: date, Completed: completed, VenueIDs: venueIDs(doc.Data["pubs"]), ObservedOn: observedOn,
	}}, nil
}
