package autocomplete

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerCandidate cellar.HandlerName = "autocomplete.candidate"
	HandlerClose     cellar.HandlerName = "autocomplete.close"
	HandlerAmbiguous cellar.HandlerName = "autocomplete.ambiguous"
)

type CompletionClosePayload struct {
	PollID          string `json:"poll_id"`
	PollDate        string `json:"poll_date"`
	SelectedVenueID string `json:"selected_venue_id"`
}

type CompletionAmbiguousPayload struct {
	PollID string `json:"poll_id"`
	Reason string `json:"reason"`
}

type CandidateHandler struct {
	Source Source
	Logger *slog.Logger
}

func (handler CandidateHandler) Handle(ctx context.Context, truth truths.PollAutoCompletionDue) cellar.Result {
	if handler.Source == nil {
		return cellar.ErrorResult{Message: "autocomplete source is nil"}
	}
	if !validCandidateTruth(truth) {
		return cellar.Complete{}
	}
	poll, err := handler.Source.GetPoll(ctx, truth.Poll.PollID)
	if err != nil {
		return cellar.ErrorResult{Message: "load current poll", Err: err}
	}
	if completed, _ := poll.Data["completed"].(bool); completed {
		return cellar.Complete{}
	}
	venueIDs := venueIDs(poll.Data["pubs"])
	votes, err := handler.loadVotes(ctx, truth.Poll.PollID)
	if err != nil {
		return cellar.ErrorResult{Message: "load votes", Err: err}
	}
	exists, food, err := handler.loadVenueEligibility(ctx, venueIDs)
	if err != nil {
		return cellar.ErrorResult{Message: "load venue eligibility", Err: err}
	}
	decision := Decide(venueIDs, votes, food, exists)
	if decision.IsClearWinner() {
		return newCloseCell(truth.Poll.PollID, truth.Poll.PollDate, decision.SelectedVenueID)
	}
	return newAmbiguousCell(truth.Poll.PollID, decision.Reason)
}

func (handler CandidateHandler) loadVotes(ctx context.Context, pollID string) (map[string]int, error) {
	doc, found, err := handler.Source.GetVotes(ctx, pollID)
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]int{}, nil
	}
	counts := make(map[string]int, len(doc.Data))
	for venueID, raw := range doc.Data {
		if voters, ok := raw.([]any); ok {
			counts[venueID] = len(voters)
		}
	}
	return counts, nil
}

func (handler CandidateHandler) loadVenueEligibility(ctx context.Context, venueIDs []string) (map[string]bool, map[string]bool, error) {
	exists := make(map[string]bool, len(venueIDs))
	food := make(map[string]bool, len(venueIDs))
	for _, venueID := range venueIDs {
		doc, found, err := handler.Source.GetVenue(ctx, venueID)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			continue
		}
		exists[venueID] = true
		food[venueID], _ = doc.Data["food"].(bool)
	}
	return exists, food, nil
}

type CloseHandler struct {
	Source Source
	Logger *slog.Logger
}

func (handler CloseHandler) Handle(ctx context.Context, payload CompletionClosePayload) cellar.Result {
	if handler.Source == nil {
		return cellar.ErrorResult{Message: "autocomplete source is nil"}
	}
	completed, err := handler.Source.CompletePoll(ctx, payload.PollID, payload.SelectedVenueID)
	if err != nil {
		return cellar.ErrorResult{Message: "conditionally complete poll", Err: err}
	}
	if completed {
		handler.writeAudit(ctx, payload)
	}
	return cellar.Complete{}
}

func (handler CloseHandler) writeAudit(ctx context.Context, payload CompletionClosePayload) {
	entry := AuditEntry{PollID: payload.PollID, PollDate: payload.PollDate, SelectedVenueID: payload.SelectedVenueID}
	if err := handler.Source.WriteCompletionAudit(ctx, entry); err != nil && handler.Logger != nil {
		handler.Logger.Warn("poll auto-completion audit failed", "poll_id", payload.PollID, "err", err)
	}
}

type AmbiguousHandler struct{ Logger *slog.Logger }

func (handler AmbiguousHandler) Handle(ctx context.Context, payload CompletionAmbiguousPayload) cellar.Result {
	_ = ctx
	if handler.Logger != nil {
		handler.Logger.Info("poll requires manual completion", "poll_id", payload.PollID, "reason", payload.Reason)
	}
	return cellar.Complete{}
}

func newCloseCell(pollID, pollDate, venueID string) cellar.Result {
	payload, err := cellar.JSONCodec[CompletionClosePayload]().Marshal(CompletionClosePayload{PollID: pollID, PollDate: pollDate, SelectedVenueID: venueID})
	if err != nil {
		return cellar.ErrorResult{Message: "marshal completion close payload", Err: err}
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: HandlerClose, Payload: payload}}}}}
}

func newAmbiguousCell(pollID, reason string) cellar.Result {
	payload, err := cellar.JSONCodec[CompletionAmbiguousPayload]().Marshal(CompletionAmbiguousPayload{PollID: pollID, Reason: reason})
	if err != nil {
		return cellar.ErrorResult{Message: "marshal completion ambiguous payload", Err: err}
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{{Steps: []cellar.CellStep{{HandlerName: HandlerAmbiguous, Payload: payload}}}}}
}

func venueIDs(raw any) []string {
	pubs, _ := raw.(map[string]any)
	ids := make([]string, 0, len(pubs))
	for venueID := range pubs {
		ids = append(ids, venueID)
	}
	return ids
}

func validCandidateTruth(truth truths.PollAutoCompletionDue) bool {
	return truth.Poll.PollID != "" && truth.Poll.PollDate != "" && !truth.Poll.Completed
}
