package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"cellar/pkg/cellar"
	publicsqlite "cellar/pkg/sqlite"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/counter"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/handlers"
	completedpolllistener "last_orders/internal/lastorders/listeners/completedpolls"
	eventvenuelistener "last_orders/internal/lastorders/listeners/eventvenues"
	examplelistener "last_orders/internal/lastorders/listeners/example"
	newpolllistener "last_orders/internal/lastorders/listeners/newpolls"

	"cloud.google.com/go/firestore"

	_ "modernc.org/sqlite"
)

type Config struct {
	DBPath                 string
	PollDelay              time.Duration
	Logger                 *slog.Logger
	FirestoreProjectID     string
	EnableFirestore        bool
	IdempotencyRemote      firebaseidempotency.Remote
	EnableExampleListener  bool
	EventReevaluateEvery   time.Duration
	StartupComponentChecks []func(*basestore.Store) error
}

type App struct {
	logger                *slog.Logger
	baseStore             *basestore.Store
	cellarStore           cellar.Store
	counterStore          *counter.Store
	idempotencyStore      *firebaseidempotency.Store
	recurrenceService     *recurrence.Service
	firestoreClient       *firestore.Client
	cellarRuntime         *cellar.Cellar
	exampleListener       *examplelistener.Producer
	eventVenueListener    *eventvenuelistener.Listener
	newPollListener       *newpolllistener.Listener
	completedPollListener *completedpolllistener.Listener
	runCancel             context.CancelFunc
	runDone               chan struct{}
	runMu                 sync.Mutex
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
	if cfg.EnableFirestore && cfg.FirestoreProjectID == "" {
		cfg.FirestoreProjectID = "last-orders-emulator"
	}

	db, err := sql.Open("sqlite", sqliteDSN(cfg.DBPath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	baseStore, err := basestore.New(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	cellarStore, err := publicsqlite.NewStore(baseStore.DB(), nil)
	if err != nil {
		_ = baseStore.Close()
		return nil, fmt.Errorf("init cellar store: %w", err)
	}

	counterStore, err := counter.New(baseStore)
	if err != nil {
		_ = baseStore.Close()
		return nil, err
	}

	var firestoreClient *firestore.Client
	var recurrenceService *recurrence.Service
	if cfg.EnableFirestore {
		firestoreClient, err = firestore.NewClient(context.Background(), cfg.FirestoreProjectID)
		if err != nil {
			_ = baseStore.Close()
			return nil, fmt.Errorf("init firestore client: %w", err)
		}

		recurrenceService, err = recurrence.NewService(firestoreClient, cfg.Logger)
		if err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
	}

	if cfg.IdempotencyRemote == nil {
		if firestoreClient != nil {
			cfg.IdempotencyRemote, err = firebaseidempotency.NewFirestoreRemote(firestoreClient, "listener_state", "last_orders")
			if err != nil {
				_ = firestoreClient.Close()
				_ = baseStore.Close()
				return nil, err
			}
		} else {
			cfg.IdempotencyRemote = firebaseidempotency.NewInMemoryRemoteStandIn(true)
		}
	}

	idempotencyStore, err := firebaseidempotency.New(baseStore)
	if err != nil {
		if firestoreClient != nil {
			_ = firestoreClient.Close()
		}
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

	cellarRuntime := cellar.New(cellarStore, cellar.Config{PollDelay: cfg.PollDelay})
	if err := cellarRuntime.Register(handlers.HandlerExampleIncrement, handlers.IncrementHandler{Counter: counterStore, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(handlers.HandlerExampleFanout, handlers.FanoutHandler{Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(handlers.HandlerNewPoll, handlers.NewPollHandler{Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(handlers.HandlerCompletedPoll, handlers.CompletedPollHandler{Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerPending, firebaseidempotency.PendingHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerPush, firebaseidempotency.PushHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerCheck, firebaseidempotency.CheckHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, RetryDelay: 80 * time.Millisecond, Logger: cfg.Logger}); err != nil {
		if firestoreClient != nil {
			_ = firestoreClient.Close()
		}
		_ = baseStore.Close()
		return nil, err
	}
	if recurrenceService != nil {
		if err := cellarRuntime.Register(handlers.HandlerStaleEvent, handlers.StaleEventHandler{Service: recurrenceService, Logger: cfg.Logger}); err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
		if err := cellarRuntime.Register(handlers.HandlerCreateEventPoll, handlers.CreateEventPollHandler{Service: recurrenceService, Logger: cfg.Logger}); err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
	}
	incrementPayload, err := cellar.JSONCodec[handlers.IncrementPayload]().Marshal(handlers.IncrementPayload{
		Counter: counter.DefaultCounter,
		Delta:   1,
	})
	if err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	listener := examplelistener.NewProducer(cellarStore, handlers.HandlerExampleIncrement, incrementPayload, nil, cfg.Logger)
	if !cfg.EnableExampleListener {
		listener = nil
	}

	var eventVenueListener *eventvenuelistener.Listener
	var newPollListener *newpolllistener.Listener
	var completedPollListener *completedpolllistener.Listener
	if recurrenceService != nil {
		eventVenueListener, err = eventvenuelistener.New(eventvenuelistener.Config{
			Store:              cellarStore,
			Service:            recurrenceService,
			ReevaluateInterval: cfg.EventReevaluateEvery,
			Logger:             cfg.Logger,
		})
		if err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}

		newPollListener, err = newpolllistener.New(newpolllistener.Config{
			Client: firestoreClient,
			Store:  cellarStore,
			Logger: cfg.Logger,
		})
		if err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}

		completedPollListener, err = completedpolllistener.New(completedpolllistener.Config{
			Client: firestoreClient,
			Store:  cellarStore,
			Logger: cfg.Logger,
		})
		if err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
	}

	return &App{
		logger:                cfg.Logger,
		baseStore:             baseStore,
		cellarStore:           cellarStore,
		counterStore:          counterStore,
		idempotencyStore:      idempotencyStore,
		recurrenceService:     recurrenceService,
		firestoreClient:       firestoreClient,
		cellarRuntime:         cellarRuntime,
		exampleListener:       listener,
		eventVenueListener:    eventVenueListener,
		newPollListener:       newPollListener,
		completedPollListener: completedPollListener,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if a.baseStore == nil {
		return fmt.Errorf("app is not initialised")
	}

	runCtx, cancel := context.WithCancel(ctx)
	a.runMu.Lock()
	a.runCancel = cancel
	a.runDone = make(chan struct{})
	runDone := a.runDone
	a.runMu.Unlock()
	defer func() {
		cancel()
		_ = a.cellarRuntime.Stop()
		<-runDone
		a.runMu.Lock()
		a.runCancel = nil
		a.runDone = nil
		a.runMu.Unlock()
	}()

	cellarErr := make(chan error, 1)
	go func() {
		defer close(runDone)
		cellarErr <- a.cellarRuntime.Start(runCtx)
	}()

	if a.exampleListener != nil {
		if err := a.exampleListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start example listener: %w", err)
		}
	}

	if a.eventVenueListener != nil {
		if err := a.eventVenueListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start event venue listener: %w", err)
		}
	}

	if a.newPollListener != nil {
		if err := a.newPollListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start new poll listener: %w", err)
		}
	}

	if a.completedPollListener != nil {
		if err := a.completedPollListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start completed poll listener: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-cellarErr:
		if err != nil {
			return fmt.Errorf("run cellar: %w", err)
		}
	}
	return nil
}

func (a *App) Close() error {
	a.runMu.Lock()
	runCancel := a.runCancel
	runDone := a.runDone
	a.runMu.Unlock()
	if runCancel != nil {
		runCancel()
		_ = a.cellarRuntime.Stop()
		if runDone != nil {
			<-runDone
		}
	}
	if a.baseStore == nil {
		return nil
	}
	if a.firestoreClient != nil {
		_ = a.firestoreClient.Close()
	}
	return a.baseStore.Close()
}

func (a *App) AddCell(request cellar.CellRequest) error {
	_, err := a.cellarStore.Add([]cellar.CellRequest{request})
	return err
}

func (a *App) CounterValue(ctx context.Context, counterName string) (int64, error) {
	return a.counterStore.Value(ctx, counterName)
}

func (a *App) IdempotencyState(ctx context.Context, listener, eventKey string) (firebaseidempotency.State, bool, error) {
	return a.idempotencyStore.CurrentState(ctx, listener, eventKey)
}

func (a *App) CellarStore() cellar.Store {
	return a.cellarStore
}

func sqliteDSN(path string) string {
	return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}
