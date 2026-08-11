package handlers

import (
	"context"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/counter"
)

const (
	HandlerExampleIncrement cellar.HandlerName = "example.increment"
	HandlerExampleFanout    cellar.HandlerName = "example.fanout"
)

type IncrementPayload struct {
	Counter string `json:"counter"`
	Delta   int64  `json:"delta"`
}

type FanoutPayload struct {
	Counter  string `json:"counter"`
	Children int    `json:"children"`
	Delta    int64  `json:"delta"`
}

type IncrementHandler struct {
	Counter *counter.Store
	Logger  *slog.Logger
}

func (h IncrementHandler) Handle(ctx context.Context, payload IncrementPayload) cellar.Result {
	_ = ctx
	if h.Counter == nil {
		return cellar.ErrorResult{Message: "counter store is nil"}
	}
	if payload.Delta == 0 {
		payload.Delta = 1
	}
	if h.Logger != nil {
		h.Logger.Info("example increment handler", "counter", payload.Counter, "delta", payload.Delta)
	}
	return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{h.Counter.IncrementWork(payload.Counter, payload.Delta)}}
}

type FanoutHandler struct {
	Logger *slog.Logger
}

func (h FanoutHandler) Handle(ctx context.Context, payload FanoutPayload) cellar.Result {
	_ = ctx
	children := payload.Children
	if children <= 0 {
		children = 2
	}
	delta := payload.Delta
	if delta == 0 {
		delta = 1
	}

	requests := make([]cellar.CellRequest, 0, children)
	now := time.Now().UTC()
	for i := 0; i < children; i++ {
		raw, err := cellar.JSONCodec[IncrementPayload]().Marshal(IncrementPayload{
			Counter: payload.Counter,
			Delta:   delta,
		})
		if err != nil {
			return cellar.ErrorResult{Message: "marshal fanout child payload", Err: err}
		}
		requests = append(requests, cellar.CellRequest{
			HandlerName: HandlerExampleIncrement,
			Payload:     raw,
			NotBefore:   &now,
		})
	}

	if h.Logger != nil {
		h.Logger.Info("example fanout handler", "children", children)
	}
	return cellar.Complete{NewCells: requests}
}
