package autocomplete

import (
	"context"
	"fmt"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/truths"

	"cloud.google.com/go/firestore"
)

const HandlerDiscovery cellar.HandlerName = "autocomplete.discovery"

const discoveryComponent = "autocomplete.discovery"

type DiscoveryHandler struct {
	Client *firestore.Client
	Logger *slog.Logger
}

func (handler DiscoveryHandler) Handle(ctx context.Context, truth truths.DailyPollAutoCompleteDue) cellar.Result {
	if handler.Client == nil {
		return cellar.ErrorResult{Message: "autocomplete firestore client is nil"}
	}
	if truth.ObservedOn == "" {
		return cellar.Complete{}
	}
	docs, err := handler.Client.Collection("polls").Where("completed", "==", false).Where("date", "==", truth.ObservedOn).Documents(ctx).GetAll()
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

func discoveredTruth(doc *firestore.DocumentSnapshot, observedOn string) (truths.PollAutoCompletionDue, error) {
	if doc == nil || doc.Ref == nil {
		return truths.PollAutoCompletionDue{}, fmt.Errorf("poll document is nil")
	}
	date, ok := doc.Data()["date"].(string)
	if !ok || date == "" {
		return truths.PollAutoCompletionDue{}, fmt.Errorf("poll %q date is invalid", doc.Ref.ID)
	}
	completed, ok := doc.Data()["completed"].(bool)
	if !ok || completed {
		return truths.PollAutoCompletionDue{}, fmt.Errorf("poll %q completed is not false", doc.Ref.ID)
	}
	return truths.PollAutoCompletionDue{Poll: truths.PollAutoCompletionSnapshot{
		PollID: doc.Ref.ID, PollDate: date, Completed: completed, VenueIDs: venueIDs(doc.Data()["pubs"]), ObservedOn: observedOn,
	}}, nil
}
