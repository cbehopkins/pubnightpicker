package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/counter"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/handlers"
	examplelistener "last_orders/internal/lastorders/listeners/example"
	"last_orders/internal/lastorders/runtime"

	_ "modernc.org/sqlite"
)

type Config struct {
	DBPath                 string
	PollDelay              time.Duration
	Logger                 *slog.Logger
	IdempotencyRemote      firebaseidempotency.Remote
	EnableExampleListener  bool
	StartupComponentChecks []func(*basestore.Store) error
}

type App struct {
	logger             *slog.Logger
	baseStore          *basestore.Store
	counterStore       *counter.Store
	idempotencyStore   *firebaseidempotency.Store
	registry           *cellar.MemoryRegistry
	worker             *cellar.Worker
	scheduler          *cellar.Scheduler
	exampleListener    *examplelistener.Producer
	runCancel          context.CancelFunc
}

type runtimeDispatcher struct {
	worker *cellar.Worker
}

func (d runtimeDispatcher) Dispatch(ctx context.Context, cell cellar.Cell) error {
	if d.worker == nil {
		return nil
	}
	d.worker.Run(ctx, cell)
	return nil
}

func New(cfg Config) (*App, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("db path is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PollDelay <= 0 {
		cfg.PollDelay = 50 * time.Millisecond
	}
	if cfg.IdempotencyRemote == nil {
		cfg.IdempotencyRemote = firebaseidempotency.NewMemoryRemote(true)
	}

	db, err := sql.Open("sqlite", sqliteDSN(cfg.DBPath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	baseStore, err := basestore.New(db, nil)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	counterStore, err := counter.New(baseStore)
	if err != nil {
		_ = baseStore.Close()
		return nil, err
	}

	idempotencyStore, err := firebaseidempotency.New(baseStore)
	if err != nil {
		_ = baseStore.Close()
		return nil, err
	}

	for _, check := range cfg.StartupComponentChecks {
		if check == nil {
			continue
		}
		if err := check(baseStore); err != nil {
			_ = baseStore.Close()
			return nil, fmt.Errorf("startup component check failed: %w", err)
		}
	}

	registry := cellar.NewMemoryRegistry()
	if err := runtime.RegisterJSON(registry, handlers.HandlerExampleIncrement, handlers.IncrementHandler{Counter: counterStore, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := runtime.RegisterJSON(registry, handlers.HandlerExampleFanout, handlers.FanoutHandler{Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := runtime.RegisterJSON(registry, firebaseidempotency.HandlerPending, firebaseidempotency.PendingHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := runtime.RegisterJSON(registry, firebaseidempotency.HandlerPush, firebaseidempotency.PushHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := runtime.RegisterJSON(registry, firebaseidempotency.HandlerCheck, firebaseidempotency.CheckHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, RetryDelay: 80 * time.Millisecond, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	registry.Freeze()

	worker := cellar.NewWorker(registry, cellar.NewStoreResultApplier(baseStore.CellarStore()))
	scheduler := cellar.NewScheduler(baseStore.CellarStore(), runtimeDispatcher{worker: worker}, 1, cfg.PollDelay)

	incrementPayload, err := cellar.JSONCodec[handlers.IncrementPayload]().Marshal(handlers.IncrementPayload{
		Counter: counter.DefaultCounter,
		Delta:   1,
	})
	if err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	listener := examplelistener.NewProducer(baseStore.CellarStore(), handlers.HandlerExampleIncrement, incrementPayload, nil, cfg.Logger)
	if !cfg.EnableExampleListener {
		listener = nil
	}

	return &App{
		logger:           cfg.Logger,
		baseStore:        baseStore,
		counterStore:     counterStore,
		idempotencyStore: idempotencyStore,
		registry:         registry,
		worker:           worker,
		scheduler:        scheduler,
		exampleListener:  listener,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if a.baseStore == nil {
		return fmt.Errorf("app is not initialised")
	}

	if err := a.baseStore.CellarStore().Recover(); err != nil {
		return fmt.Errorf("cellar recover: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	a.runCancel = cancel
	defer func() {
		cancel()
		a.runCancel = nil
	}()

	go a.scheduler.Run(runCtx)

	if a.exampleListener != nil {
		if err := a.exampleListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start example listener: %w", err)
		}
	}

	<-ctx.Done()
	return nil
}

func (a *App) Close() error {
	if a.runCancel != nil {
		a.runCancel()
	}
	if a.baseStore == nil {
		return nil
	}
	return a.baseStore.Close()
}

func (a *App) AddCell(request cellar.CellRequest) error {
	_, err := a.baseStore.CellarStore().Add([]cellar.CellRequest{request})
	return err
}

func (a *App) CounterValue(ctx context.Context, counterName string) (int64, error) {
	return a.counterStore.Value(ctx, counterName)
}

func (a *App) IdempotencyState(ctx context.Context, listener, eventKey string) (firebaseidempotency.State, bool, error) {
	return a.idempotencyStore.CurrentState(ctx, listener, eventKey)
}

func (a *App) ForceIdempotencyState(ctx context.Context, listener, eventKey string, state firebaseidempotency.State) error {
	return a.idempotencyStore.ForceState(ctx, listener, eventKey, state)
}

func (a *App) CellarStore() cellar.Store {
	if a.baseStore == nil {
		return nil
	}
	return a.baseStore.CellarStore()
}

func (a *App) Worker() *cellar.Worker {
	return a.worker
}

func sqliteDSN(path string) string {
	return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}
