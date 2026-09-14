// Package firebaseidempotency implements the Firebase-backed idempotency variant
// described in docs/cdd/0001-idempotency.md. Processing is a 3-step Cell Sequence:
// Check -> Populate Remote -> Emit Truth. The Cell's own step cursor is the durable
// processing state machine; the local Store only caches the claimed identity.
package firebaseidempotency

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerCheck          cellar.HandlerName = "firebase.idempotency.check"
	HandlerPopulateRemote cellar.HandlerName = "firebase.idempotency.populate_remote"
	HandlerEmitTruth      cellar.HandlerName = "firebase.idempotency.emit_truth"
)

// StepPayload is carried by every step of the Sequence; the identity and Truth
// envelope are fixed when the Sequence is created and do not change as it progresses.
type StepPayload struct {
	Listener string          `json:"listener"`
	EventKey string          `json:"event_key"`
	Truth    truths.Envelope `json:"truth"`
}

// NewCellRequest builds the 3-step Sequence which establishes idempotency for the
// given identity and, once established, emits the Truth.
func NewCellRequest(listener, eventKey string, truth truths.Envelope) (cellar.CellRequest, error) {
	payload, err := cellar.JSONCodec[StepPayload]().Marshal(StepPayload{Listener: listener, EventKey: eventKey, Truth: truth})
	if err != nil {
		return cellar.CellRequest{}, err
	}
	return cellar.CellRequest{
		Steps: []cellar.CellStep{
			{HandlerName: HandlerCheck, Payload: payload},
			{HandlerName: HandlerPopulateRemote, Payload: payload},
			{HandlerName: HandlerEmitTruth, Payload: payload},
		},
	}, nil
}

// CheckHandler is Step 1: it claims the identity locally, consulting the remote
// authority only to distinguish a genuinely new claim from one already established
// elsewhere. See docs/cdd/0001-idempotency.md §9.
type CheckHandler struct {
	Store  *Store
	Remote Remote
	Logger *slog.Logger
}

func (h CheckHandler) Handle(ctx context.Context, payload StepPayload) cellar.Result {
	exists, err := h.Store.Exists(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "load idempotency state", Err: err}
	}
	if exists {
		return cellar.Kill{}
	}

	remoteExists, err := h.Remote.HasKey(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "check remote key", Err: err}
	}

	claimWork := h.Store.InsertUnlessExistsWork(payload.Listener, payload.EventKey)
	if remoteExists {
		// Already established elsewhere: cache the identity and stop, without emitting the Truth again.
		if h.Logger != nil {
			h.Logger.Info("idempotency observed remote establishment, terminating", "listener", payload.Listener, "event_key", payload.EventKey)
		}
		return cellar.Kill{ApplicationWork: []cellar.ApplicationWork{claimWork}}
	}

	if h.Logger != nil {
		h.Logger.Info("idempotency check claiming identity", "listener", payload.Listener, "event_key", payload.EventKey)
	}
	return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{claimWork}}
}

// PopulateRemoteHandler is Step 2: it establishes the remote key. Reaching this step
// means Step 1 determined the identity was genuinely new, so this always runs.
type PopulateRemoteHandler struct {
	Remote Remote
	Logger *slog.Logger
}

func (h PopulateRemoteHandler) Handle(ctx context.Context, payload StepPayload) cellar.Result {
	if _, err := h.Remote.CreateKey(ctx, payload.Listener, payload.EventKey); err != nil {
		return cellar.ErrorResult{Message: "populate remote idempotency key", Err: err}
	}
	if h.Logger != nil {
		h.Logger.Info("idempotency remote key populated", "listener", payload.Listener, "event_key", payload.EventKey)
	}
	return cellar.Complete{}
}

// EmitTruthHandler is Step 3: it emits the Truth. Reaching this step means Step 1
// determined the identity was genuinely new, so this always runs.
type EmitTruthHandler struct {
	Logger *slog.Logger
}

func (h EmitTruthHandler) Handle(ctx context.Context, payload StepPayload) cellar.Result {
	truthCell, err := payload.Truth.CellRequest()
	if err != nil {
		return cellar.ErrorResult{Message: "build truth cell", Err: err}
	}
	if h.Logger != nil {
		h.Logger.Info("idempotency emitting truth", "listener", payload.Listener, "event_key", payload.EventKey, "fanout", payload.Truth.FanoutName)
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{truthCell}}
}
