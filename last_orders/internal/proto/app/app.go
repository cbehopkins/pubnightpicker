package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/proto/cellarruntime"
	"last_orders/internal/proto/firebase"
	"last_orders/internal/proto/handlers"
	"last_orders/internal/proto/idempotency"
)

const (
	PhaseFirebaseObserve        = 1
	PhaseHousekeepingScheduling = 2
	PhaseChainedFutureWork      = 3
	PhaseFirebaseToCellar       = 4
	PhaseListenerIdempotency    = 5
)

type Config struct {
	Phase                    int
	PollDelay                time.Duration
	HousekeepingEvery        time.Duration
	HousekeepingScheduleLead time.Duration
	ChainDelay               time.Duration
	EnableFailureCell        bool
	Store                    cellar.Store
	EnableFirebase           bool
	FirebaseSource           firebase.Source
	Deduper                  idempotency.Deduper
	Logger                   *slog.Logger
}

type Prototype struct {
	cfg         Config
	store       cellar.Store
	registry    *cellar.MemoryRegistry
	recorder    *handlers.Recorder
	worker      *cellar.Worker
	scheduler   *cellar.Scheduler
	bridgeQueue chan firebase.Event
}

type runtimeDispatcher struct {
	worker *cellar.Worker
	logger *slog.Logger
}

func (d runtimeDispatcher) Dispatch(ctx context.Context, cell cellar.Cell) error {
	if d.worker == nil {
		return nil
	}

	result := d.worker.Run(ctx, cell)
	if d.logger != nil {
		d.logger.Info("cell executed",
			"cell_id", cell.ID,
			"handler", cell.HandlerName,
			"result", resultKind(result),
		)
	}
	return nil
}

func New(cfg Config) (*Prototype, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Deduper == nil {
		cfg.Deduper = idempotency.NoopDeduper{}
	}
	if cfg.Phase < PhaseFirebaseObserve || cfg.Phase > PhaseListenerIdempotency {
		return nil, fmt.Errorf("phase must be between %d and %d", PhaseFirebaseObserve, PhaseListenerIdempotency)
	}

	store := cfg.Store
	if store == nil {
		store = cellar.NewMemoryStore(nil)
	}
	registry := cellar.NewMemoryRegistry()
	recorder := handlers.NewRecorder()

	if err := cellarruntime.RegisterJSON(registry, handlers.HandlerHousekeeping, handlers.HousekeepingHandler{Logger: cfg.Logger, Recorder: recorder}); err != nil {
		return nil, err
	}
	if err := cellarruntime.RegisterJSON(registry, handlers.HandlerFuture, handlers.FutureHandler{Logger: cfg.Logger, Recorder: recorder}); err != nil {
		return nil, err
	}
	if err := cellarruntime.RegisterJSON(registry, handlers.HandlerFirebase, handlers.FirebaseEventHandler{Logger: cfg.Logger, Recorder: recorder}); err != nil {
		return nil, err
	}
	if err := cellarruntime.RegisterJSON(registry, handlers.HandlerFailOnce, handlers.FailOnceHandler{Logger: cfg.Logger, Recorder: recorder}); err != nil {
		return nil, err
	}
	registry.Freeze()

	worker := cellar.NewWorker(registry, cellar.NewStoreResultApplier(store))
	dispatcher := runtimeDispatcher{worker: worker, logger: cfg.Logger}
	scheduler := cellar.NewScheduler(store, dispatcher, 1, cfg.PollDelay)

	return &Prototype{
		cfg:         cfg,
		store:       store,
		registry:    registry,
		recorder:    recorder,
		worker:      worker,
		scheduler:   scheduler,
		bridgeQueue: make(chan firebase.Event, 128),
	}, nil
}

func (p *Prototype) Run(ctx context.Context) error {
	if err := p.store.Recover(); err != nil {
		return err
	}

	errCh := make(chan error, 4)

	go p.scheduler.Run(ctx)

	if p.cfg.Phase == PhaseHousekeepingScheduling || p.cfg.Phase == PhaseChainedFutureWork {
		go p.runHousekeepingLoop(ctx)
	}

	if p.cfg.EnableFailureCell {
		if err := p.enqueueFailOnceCell(); err != nil {
			return err
		}
	}

	if p.cfg.Phase == PhaseFirebaseObserve && (!p.cfg.EnableFirebase || p.cfg.FirebaseSource == nil) {
		p.cfg.Logger.Warn("phase 1 running without firebase source; waiting for timeout or shutdown signal")
	}

	if p.cfg.EnableFirebase && p.cfg.FirebaseSource != nil {
		go func() {
			errCh <- p.cfg.FirebaseSource.Run(ctx.Done(), p.bridgeQueue)
		}()
		go func() {
			errCh <- p.bridgeFirebase(ctx)
		}()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if err != nil {
				return err
			}
		}
	}
}

func (p *Prototype) RecorderSnapshot() handlers.Recorder {
	return p.recorder.Snapshot()
}

func (p *Prototype) AddCell(request cellar.CellRequest) error {
	_, err := p.store.Add([]cellar.CellRequest{request})
	return err
}

func (p *Prototype) bridgeFirebase(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-p.bridgeQueue:
			p.cfg.Logger.Info("listener accepted firebase event",
				"event", event.Type,
				"poll_id", event.PollID,
				"delivery_key", event.DeliveryKey,
			)

			if p.cfg.Phase < PhaseFirebaseToCellar {
				continue
			}

			accept := true
			if p.cfg.Phase >= PhaseListenerIdempotency {
				first, err := p.cfg.Deduper.FirstDelivery(ctx, event.DeliveryKey)
				if err != nil {
					return err
				}
				accept = first
				if !accept {
					p.cfg.Logger.Info("duplicate firebase delivery ignored", "delivery_key", event.DeliveryKey)
				}
			}

			if !accept {
				continue
			}

			payload := handlers.FirebasePayload{
				EventType:   event.Type,
				PollID:      event.PollID,
				ObservedAt:  event.ObservedAt,
				DeliveryKey: event.DeliveryKey,
			}
			raw, err := cellar.JSONCodec[handlers.FirebasePayload]().Marshal(payload)
			if err != nil {
				return err
			}

			notBefore := time.Now().UTC()
			ids, err := p.store.Add([]cellar.CellRequest{{
				HandlerName: handlers.HandlerFirebase,
				Payload:     raw,
				NotBefore:   &notBefore,
			}})
			if err != nil {
				return err
			}
			if len(ids) > 0 {
				p.cfg.Logger.Info("cell created from firebase event", "cell_id", ids[0], "event", event.Type)
			}
		}
	}
}

func (p *Prototype) runHousekeepingLoop(ctx context.Context) {
	period := p.cfg.HousekeepingEvery
	if period <= 0 {
		period = 5 * time.Second
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	if err := p.enqueueHousekeeping(time.Now().UTC()); err != nil {
		p.cfg.Logger.Error("initial housekeeping enqueue failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := p.enqueueHousekeeping(now.UTC()); err != nil {
				p.cfg.Logger.Error("housekeeping enqueue failed", "error", err)
			}
		}
	}
}

func (p *Prototype) enqueueHousekeeping(now time.Time) error {
	scheduledFor := now.Add(p.cfg.HousekeepingScheduleLead)
	chainDelayMillis := int64(0)
	if p.cfg.Phase >= PhaseChainedFutureWork {
		chainDelayMillis = p.cfg.ChainDelay.Milliseconds()
	}
	payload := handlers.HousekeepingPayload{
		ScheduledAt:      scheduledFor,
		ChainDelayMillis: chainDelayMillis,
	}
	raw, err := cellar.JSONCodec[handlers.HousekeepingPayload]().Marshal(payload)
	if err != nil {
		return err
	}

	ids, err := p.store.Add([]cellar.CellRequest{{
		HandlerName: handlers.HandlerHousekeeping,
		Payload:     raw,
		NotBefore:   &scheduledFor,
	}})
	if err != nil {
		return err
	}

	if len(ids) > 0 {
		p.cfg.Logger.Info("housekeeping scheduled", "cell_id", ids[0], "scheduled_for", scheduledFor)
	}
	return nil
}

func (p *Prototype) enqueueFailOnceCell() error {
	payload := handlers.FailOncePayload{Key: "phase-failure-test", RetryDelaySeconds: 1}
	raw, err := cellar.JSONCodec[handlers.FailOncePayload]().Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = p.store.Add([]cellar.CellRequest{{
		HandlerName: handlers.HandlerFailOnce,
		Payload:     raw,
		NotBefore:   &now,
	}})
	return err
}

func resultKind(result cellar.Result) string {
	switch result.(type) {
	case cellar.Complete:
		return "complete"
	case cellar.Retry:
		return "retry"
	case cellar.ErrorResult:
		return "error"
	default:
		return "unknown"
	}
}
