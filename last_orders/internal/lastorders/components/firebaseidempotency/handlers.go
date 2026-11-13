// Package firebaseidempotency implements the Firebase-backed idempotency variant
// described in docs/cdd/0001-idempotency.md. Processing is a 3-step Cell Sequence:
// Check -> Populate Remote -> Emit Truth. The Cell's own step cursor is the durable
// processing state machine; the local Store only caches the claimed identity.
package firebaseidempotency

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"

	"github.com/google/uuid"
)

const (
	HandlerCheck          cellar.HandlerName = "firebase.idempotency.check"
	HandlerPopulateRemote cellar.HandlerName = "firebase.idempotency.populate_remote"
	HandlerReleaseRemote  cellar.HandlerName = "firebase.idempotency.release_remote"
	HandlerEmitTruth      cellar.HandlerName = "firebase.idempotency.emit_truth"
)

// StepPayload is carried by every step of the Sequence; the identity and Truth
// envelope are fixed when the Sequence is created and do not change as it progresses.
type StepPayload struct {
	Listener string          `json:"listener"`
	EventKey string          `json:"event_key"`
	Owner    string          `json:"owner"`
	Truth    truths.Envelope `json:"truth"`
}

type ReleasePayload struct {
	Listener string `json:"listener"`
	EventKey string `json:"event_key"`
	Owner    string `json:"owner"`
}

// NewCellRequest builds the 3-step Sequence which establishes idempotency for the
// given identity and, once established, emits the Truth.
func NewCellRequest(listener, eventKey string, truth truths.Envelope) (cellar.CellRequest, error) {
	owner := cellar.CellID(fmt.Sprintf("firebase-idempotency:%s:%s:%s", listener, eventKey, uuid.NewString()))
	payload, err := cellar.JSONCodec[StepPayload]().Marshal(StepPayload{Listener: listener, EventKey: eventKey, Owner: string(owner), Truth: truth})
	if err != nil {
		return cellar.CellRequest{}, err
	}
	return cellar.CellRequest{
		ID: owner,
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

	claimWork := h.Store.InsertUnlessExistsWork(payload.Listener, payload.EventKey, payload.Owner)
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
	Gate   *ClaimGate
	Store  *Store
	Logger *slog.Logger
}

func (h PopulateRemoteHandler) Handle(ctx context.Context, payload StepPayload) cellar.Result {
	if h.Gate != nil && !h.Gate.TryStartClaimAttempt() {
		return h.relinquishClaim(ctx, payload)
	}
	alreadyExists, ownedByThisExecution, err := h.Remote.CreateKey(ctx, payload.Listener, payload.EventKey, payload.Owner)
	if err != nil {
		if h.Gate != nil && h.Gate.IsClosed() {
			return h.reconcileClaim(ctx, payload, err)
		}
		return cellar.ErrorResult{Message: "populate remote idempotency key", Err: err}
	}
	if alreadyExists && !ownedByThisExecution {
		if h.Logger != nil {
			h.Logger.Info("idempotency remote key already belongs to another execution", "listener", payload.Listener, "event_key", payload.EventKey, "owner", payload.Owner)
		}
		return cellar.Kill{}
	}
	if h.Logger != nil {
		h.Logger.Info("idempotency remote key populated", "listener", payload.Listener, "event_key", payload.EventKey, "owner", payload.Owner, "already_exists", alreadyExists)
	}
	return cellar.Complete{}
}

func (h PopulateRemoteHandler) relinquishClaim(ctx context.Context, payload StepPayload) cellar.Result {
	if h.Store == nil {
		return cellar.ErrorResult{Message: "retire refused idempotency claim", Err: errors.New("idempotency store is required")}
	}
	owner, exists, err := h.Remote.LookupOwner(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "inspect refused idempotency claim", Err: err}
	}
	if exists && owner == "" {
		return cellar.ErrorResult{
			Message: "inspect refused idempotency claim",
			Err:     fmt.Errorf("remote claim owner is unresolved for %q/%q", payload.Listener, payload.EventKey),
		}
	}

	var releaseCells []cellar.CellRequest
	if exists && owner == payload.Owner {
		request, err := NewReleaseRequest(payload.Listener, payload.EventKey, payload.Owner)
		if err != nil {
			return cellar.ErrorResult{Message: "build remote idempotency release", Err: err}
		}
		releaseCells = append(releaseCells, request)
	} else if exists && h.Logger != nil {
		h.Logger.Info("idempotency claim relinquished without releasing competing owner", "listener", payload.Listener, "event_key", payload.EventKey, "owner", owner)
	} else if h.Logger != nil {
		h.Logger.Info("idempotency claim relinquished without remote claim", "listener", payload.Listener, "event_key", payload.EventKey)
	}

	return cellar.Kill{
		NewCells:        releaseCells,
		ApplicationWork: []cellar.ApplicationWork{h.Store.RetireCacheEntryWork(payload.Listener, payload.EventKey, payload.Owner)},
	}
}

func NewReleaseRequest(listener, eventKey, owner string) (cellar.CellRequest, error) {
	if listener == "" || eventKey == "" || owner == "" {
		return cellar.CellRequest{}, errors.New("listener, event key and owner are required")
	}
	payload, err := cellar.JSONCodec[ReleasePayload]().Marshal(ReleasePayload{
		Listener: listener,
		EventKey: eventKey,
		Owner:    owner,
	})
	if err != nil {
		return cellar.CellRequest{}, err
	}
	return cellar.CellRequest{
		ID:    cellar.CellID(fmt.Sprintf("firebase-idempotency-release:%s", owner)),
		Steps: []cellar.CellStep{{HandlerName: HandlerReleaseRemote, Payload: payload}},
	}, nil
}

type ReleaseRemoteHandler struct {
	Remote Remote
	Logger *slog.Logger
}

func (h ReleaseRemoteHandler) Handle(ctx context.Context, payload ReleasePayload) cellar.Result {
	released, err := h.Remote.ReleaseKey(ctx, payload.Listener, payload.EventKey, payload.Owner)
	if err != nil {
		return cellar.ErrorResult{Message: "release remote idempotency claim", Err: err}
	}
	if !released {
		if h.Logger != nil {
			h.Logger.Info("idempotency remote claim release skipped because ownership changed", "listener", payload.Listener, "event_key", payload.EventKey, "owner", payload.Owner)
		}
		return cellar.ErrorResult{
			Message: "release remote idempotency claim",
			Err:     fmt.Errorf("remote claim owner changed before release for %q/%q", payload.Listener, payload.EventKey),
		}
	}
	if h.Logger != nil {
		h.Logger.Info("idempotency remote claim released", "listener", payload.Listener, "event_key", payload.EventKey, "owner", payload.Owner)
	}
	return cellar.Complete{}
}

func (h PopulateRemoteHandler) reconcileClaim(ctx context.Context, payload StepPayload, createErr error) cellar.Result {
	owner, exists, err := h.Remote.LookupOwner(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{
			Message: "reconcile remote idempotency claim",
			Err:     errors.Join(createErr, fmt.Errorf("read persisted claim owner: %w", err)),
		}
	}
	if !exists || owner == "" {
		return cellar.ErrorResult{
			Message: "reconcile remote idempotency claim",
			Err: fmt.Errorf(
				"remote claim owner is unresolved for %q/%q (exists=%t): %w",
				payload.Listener, payload.EventKey, exists, createErr,
			),
		}
	}
	if owner != payload.Owner {
		if h.Logger != nil {
			h.Logger.Info("idempotency remote key belongs to another execution after ambiguous claim", "listener", payload.Listener, "event_key", payload.EventKey, "owner", owner)
		}
		return cellar.Kill{}
	}
	if h.Logger != nil {
		h.Logger.Info("idempotency remote key ownership confirmed after ambiguous claim", "listener", payload.Listener, "event_key", payload.EventKey, "owner", owner)
	}
	return cellar.Complete{}
}

// ClaimGate atomically prevents new remote claim attempts after shutdown begins.
type ClaimGate struct {
	mu     sync.Mutex
	closed bool
}

// TryStartClaimAttempt atomically admits a claim unless shutdown closed the gate.
func (g *ClaimGate) TryStartClaimAttempt() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.closed
}

func (g *ClaimGate) Close() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

func (g *ClaimGate) IsClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
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
