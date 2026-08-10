package handlers

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"cellar/pkg/cellar"
)

const (
	HandlerHousekeeping = cellar.HandlerName("housekeeping.run")
	HandlerFuture       = cellar.HandlerName("future.echo")
	HandlerFirebase     = cellar.HandlerName("firebase.poll_event")
	HandlerFailOnce     = cellar.HandlerName("test.fail_once")
)

type HousekeepingPayload struct {
	ScheduledAt      time.Time `json:"scheduled_at"`
	ChainDelayMillis int64     `json:"chain_delay_millis"`
}

type FuturePayload struct {
	FromCellID   string    `json:"from_cell_id"`
	ScheduledFor time.Time `json:"scheduled_for"`
}

type FirebasePayload struct {
	EventType   string    `json:"event_type"`
	PollID      string    `json:"poll_id"`
	ObservedAt  time.Time `json:"observed_at"`
	DeliveryKey string    `json:"delivery_key"`
}

type FailOncePayload struct {
	Key               string `json:"key"`
	RetryDelaySeconds int    `json:"retry_delay_seconds"`
}

type Recorder struct {
	mu                 sync.Mutex
	HousekeepingRuns   int
	FutureRuns         int
	FirebaseRuns       int
	FailAttemptsByKey  map[string]int
	LastFutureDelayMS  int64
	LastFirebasePollID string
}

func NewRecorder() *Recorder {
	return &Recorder{FailAttemptsByKey: map[string]int{}}
}

func (r *Recorder) Snapshot() Recorder {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := Recorder{
		HousekeepingRuns:   r.HousekeepingRuns,
		FutureRuns:         r.FutureRuns,
		FirebaseRuns:       r.FirebaseRuns,
		FailAttemptsByKey:  map[string]int{},
		LastFutureDelayMS:  r.LastFutureDelayMS,
		LastFirebasePollID: r.LastFirebasePollID,
	}
	for k, v := range r.FailAttemptsByKey {
		clone.FailAttemptsByKey[k] = v
	}
	return clone
}

type HousekeepingHandler struct {
	Logger   *slog.Logger
	Recorder *Recorder
}

func (h HousekeepingHandler) Handle(ctx context.Context, payload HousekeepingPayload) cellar.Result {
	_ = ctx
	if h.Logger != nil {
		h.Logger.Info("housekeeping handler executed",
			"scheduled_at", payload.ScheduledAt,
			"chain_delay_millis", payload.ChainDelayMillis,
		)
	}
	if h.Recorder != nil {
		h.Recorder.mu.Lock()
		h.Recorder.HousekeepingRuns++
		h.Recorder.mu.Unlock()
	}

	if payload.ChainDelayMillis <= 0 {
		return cellar.Complete{}
	}

	runAt := time.Now().UTC().Add(time.Duration(payload.ChainDelayMillis) * time.Millisecond)
	futurePayload := FuturePayload{
		FromCellID:   "housekeeping",
		ScheduledFor: runAt,
	}
	raw, err := cellar.JSONCodec[FuturePayload]().Marshal(futurePayload)
	if err != nil {
		return cellar.ErrorResult{Message: "marshal future payload failed", Err: err}
	}

	return cellar.Complete{
		NewCells: []cellar.CellRequest{{
			HandlerName: HandlerFuture,
			Payload:     raw,
			NotBefore:   &runAt,
		}},
	}
}

type FutureHandler struct {
	Logger   *slog.Logger
	Recorder *Recorder
}

func (h FutureHandler) Handle(ctx context.Context, payload FuturePayload) cellar.Result {
	_ = ctx
	delay := time.Since(payload.ScheduledFor).Milliseconds()
	if h.Logger != nil {
		h.Logger.Info("future handler executed",
			"from_cell_id", payload.FromCellID,
			"scheduled_for", payload.ScheduledFor,
			"execution_skew_ms", delay,
		)
	}
	if h.Recorder != nil {
		h.Recorder.mu.Lock()
		h.Recorder.FutureRuns++
		h.Recorder.LastFutureDelayMS = delay
		h.Recorder.mu.Unlock()
	}
	return cellar.Complete{}
}

type FirebaseEventHandler struct {
	Logger   *slog.Logger
	Recorder *Recorder
}

func (h FirebaseEventHandler) Handle(ctx context.Context, payload FirebasePayload) cellar.Result {
	_ = ctx
	if h.Logger != nil {
		h.Logger.Info("firebase event handler executed",
			"event", payload.EventType,
			"poll_id", payload.PollID,
			"observed_at", payload.ObservedAt,
			"delivery_key", payload.DeliveryKey,
		)
	}
	if h.Recorder != nil {
		h.Recorder.mu.Lock()
		h.Recorder.FirebaseRuns++
		h.Recorder.LastFirebasePollID = payload.PollID
		h.Recorder.mu.Unlock()
	}
	return cellar.Complete{}
}

type FailOnceHandler struct {
	Logger   *slog.Logger
	Recorder *Recorder
}

func (h FailOnceHandler) Handle(ctx context.Context, payload FailOncePayload) cellar.Result {
	_ = ctx
	if h.Recorder == nil {
		return cellar.ErrorResult{Message: "recorder is required for fail-once handler"}
	}

	h.Recorder.mu.Lock()
	attempt := h.Recorder.FailAttemptsByKey[payload.Key] + 1
	h.Recorder.FailAttemptsByKey[payload.Key] = attempt
	h.Recorder.mu.Unlock()

	if h.Logger != nil {
		h.Logger.Info("fail-once handler invoked", "key", payload.Key, "attempt", attempt)
	}

	if attempt == 1 {
		retryAt := time.Now().UTC().Add(time.Duration(payload.RetryDelaySeconds) * time.Second)
		if h.Logger != nil {
			h.Logger.Warn("fail-once handler returning retry", "key", payload.Key, "retry_at", retryAt)
		}
		return cellar.Retry{NotBefore: &retryAt}
	}

	if h.Logger != nil {
		h.Logger.Info("fail-once handler completed", "key", payload.Key, "attempt", attempt)
	}
	return cellar.Complete{}
}
