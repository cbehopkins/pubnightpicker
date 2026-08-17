package firebaseidempotency

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
)

const (
	HandlerPending cellar.HandlerName = "firebase.idempotency.pending"
	HandlerPush    cellar.HandlerName = "firebase.idempotency.push"
	HandlerCheck   cellar.HandlerName = "firebase.idempotency.check"
)

type FanoutTarget struct {
	HandlerName cellar.HandlerName `json:"handler_name"`
	Payload     json.RawMessage    `json:"payload"`
}

// The PendingHandler, PushHandler, and CheckHandler are responsible for managing
// the idempotency state of events in a distributed system.
// See docs/cdd/0001-idempotency.md for a detailed explanation of the design and flow of these handlers.
type PendingPayload struct {
	Listener string         `json:"listener"`
	EventKey string         `json:"event_key"`
	Fanout   []FanoutTarget `json:"fanout"`
}

type PushPayload struct {
	Listener string         `json:"listener"`
	EventKey string         `json:"event_key"`
	Fanout   []FanoutTarget `json:"fanout"`
}

type CheckPayload struct {
	Listener string         `json:"listener"`
	EventKey string         `json:"event_key"`
	Fanout   []FanoutTarget `json:"fanout"`
}

type PendingHandler struct {
	Store  *Store
	Remote Remote
	Logger *slog.Logger
}

func (h PendingHandler) Handle(ctx context.Context, payload PendingPayload) cellar.Result {
	state, err := h.Store.CreateOrRefreshPending(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "ensure pending", Err: err}
	}
	if state == StatePresent {
		return cellar.Complete{}
	}

	exists, err := h.Remote.HasKey(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "pending has key", Err: err}
	}
	if exists {
		transitioned, err := h.Store.TransitionPendingToPresent(ctx, payload.Listener, payload.EventKey)
		if err != nil {
			return cellar.ErrorResult{Message: "transition pending to present", Err: err}
		}
		if !transitioned {
			latest, ok, stateErr := h.Store.CurrentState(ctx, payload.Listener, payload.EventKey)
			if stateErr != nil {
				return cellar.ErrorResult{Message: "load state after pending->present", Err: stateErr}
			}
			if !ok || latest != StatePresent {
				return cellar.ErrorResult{Message: "pending direct present rejected by current state"}
			}
		}
		if h.Logger != nil {
			h.Logger.Info("idempotency pending direct present", "listener", payload.Listener, "event_key", payload.EventKey)
		}
		return cellar.Complete{}
	}

	raw, err := cellar.JSONCodec[PushPayload]().Marshal(PushPayload(payload))
	if err != nil {
		return cellar.ErrorResult{Message: "marshal push payload", Err: err}
	}
	now := time.Now().UTC()
	return cellar.Complete{NewCells: []cellar.CellRequest{{
		HandlerName: HandlerPush,
		Payload:     raw,
		NotBefore:   &now,
	}}}
}

type PushHandler struct {
	Store  *Store
	Remote Remote
	Logger *slog.Logger
}

func (h PushHandler) Handle(ctx context.Context, payload PushPayload) cellar.Result {
	state, ok, err := h.Store.CurrentState(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "load local state", Err: err}
	}
	if ok && state == StatePresent {
		return cellar.Complete{}
	}

	_, err = h.Remote.CreateKey(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "push create key", Err: err}
	}

	state, err = h.Store.MarkPushedUnlessPresent(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "mark pushed", Err: err}
	}
	if state == StatePresent {
		return cellar.Complete{}
	}

	raw, err := cellar.JSONCodec[CheckPayload]().Marshal(CheckPayload(payload))
	if err != nil {
		return cellar.ErrorResult{Message: "marshal check payload", Err: err}
	}
	now := time.Now().UTC()
	return cellar.Complete{NewCells: []cellar.CellRequest{{
		HandlerName: HandlerCheck,
		Payload:     raw,
		NotBefore:   &now,
	}}}
}

type CheckHandler struct {
	Store      *Store
	Remote     Remote
	RetryDelay time.Duration
	Logger     *slog.Logger
}

func (h CheckHandler) Handle(ctx context.Context, payload CheckPayload) cellar.Result {
	state, ok, err := h.Store.CurrentState(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "load local state", Err: err}
	}
	if ok && state == StatePresent {
		return cellar.Complete{}
	}

	exists, err := h.Remote.HasKey(ctx, payload.Listener, payload.EventKey)
	if err != nil {
		return cellar.ErrorResult{Message: "check has key", Err: err}
	}
	if !exists {
		retryDelay := h.RetryDelay
		if retryDelay <= 0 {
			retryDelay = 100 * time.Millisecond
		}
		retryAt := time.Now().UTC().Add(retryDelay)
		return cellar.Retry{NotBefore: &retryAt}
	}

	if h.Logger != nil {
		h.Logger.Info("idempotency check scheduling atomic transition+fanout", "listener", payload.Listener, "event_key", payload.EventKey)
	}
	return cellar.Complete{
		NewCells: fanoutCellRequests(payload.Fanout),
		ApplicationWork: []cellar.ApplicationWork{
			h.Store.TransitionPushedToPresentWork(payload.Listener, payload.EventKey),
		},
	}
}

func fanoutCellRequests(targets []FanoutTarget) []cellar.CellRequest {
	requests := make([]cellar.CellRequest, 0, len(targets))
	now := time.Now().UTC()
	for _, target := range targets {
		requests = append(requests, cellar.CellRequest{
			HandlerName: target.HandlerName,
			Payload:     []byte(target.Payload),
			NotBefore:   &now,
		})
	}
	return requests
}
