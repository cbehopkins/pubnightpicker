package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"cellar/pkg/cellar"
	publicsqlite "cellar/pkg/sqlite"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	completedpolllistener "last_orders/internal/lastorders/database/listeners/completedpolls"
	eventvenuelistener "last_orders/internal/lastorders/database/listeners/eventvenues"
	newpolllistener "last_orders/internal/lastorders/database/listeners/newpolls"
	logendpoint "last_orders/internal/lastorders/endpoints/log"
	"last_orders/internal/lastorders/plugins/polls"
	recurrenceplugin "last_orders/internal/lastorders/plugins/recurrence"
	logsvc "last_orders/internal/lastorders/services/log"

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
	EventReevaluateEvery   time.Duration
	StartupComponentChecks []func(*basestore.Store) error
	// HTTPAddr is the address to serve HTTP endpoints on. An empty value disables HTTP entirely.
	HTTPAddr string
}

type App struct {
	logger                *slog.Logger
	baseStore             *basestore.Store
	cellarStore           cellar.Store
	idempotencyStore      *firebaseidempotency.Store
	recurrenceService     *recurrence.Service
	firestoreClient       *firestore.Client
	cellarRuntime         *cellar.Cellar
	eventVenueListener    *eventvenuelistener.Listener
	newPollListener       *newpolllistener.Listener
	completedPollListener *completedpolllistener.Listener
	httpServer            *http.Server
	httpListener          net.Listener
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
		if firestoreClient == nil {
			_ = baseStore.Close()
			return nil, fmt.Errorf("idempotency remote is required: enable firestore or supply Config.IdempotencyRemote")
		}
		cfg.IdempotencyRemote, err = firebaseidempotency.NewFirestoreRemote(firestoreClient, "listener_state", "last_orders")
		if err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
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

	factRegistry := facts.NewRegistry()
	factRegistry.Register(polls.FactNewPoll, polls.HandlerNewPoll)
	factRegistry.Register(polls.FactCompletedPoll, polls.HandlerCompletedPoll)
	factRegistry.Register(recurrenceplugin.FactStaleEvent, recurrenceplugin.HandlerStaleEvent)
	factRegistry.Register(recurrenceplugin.FactCreateEventPoll, recurrenceplugin.HandlerCreateEventPoll)
	factRegistry.Register(logsvc.FactLogMessage, logsvc.HandlerLogMessage)

	factFanout, err := facts.Fanout(factRegistry)
	if err != nil {
		_ = baseStore.Close()
		return nil, err
	}

	cellarRuntime := cellar.New(cellarStore, cellar.Config{PollDelay: cfg.PollDelay})
	if err := factFanout.Register(cellarRuntime); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerNewPoll, polls.NewPollHandler{Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerCompletedPoll, polls.CompletedPollHandler{Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerCheck, firebaseidempotency.CheckHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerPopulateRemote, firebaseidempotency.PopulateRemoteHandler{Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerEmitFact, firebaseidempotency.EmitFactHandler{Logger: cfg.Logger}); err != nil {
		if firestoreClient != nil {
			_ = firestoreClient.Close()
		}
		_ = baseStore.Close()
		return nil, err
	}
	if err := cellarRuntime.Register(logsvc.HandlerLogMessage, logsvc.Handler{Logger: cfg.Logger}); err != nil {
		if firestoreClient != nil {
			_ = firestoreClient.Close()
		}
		_ = baseStore.Close()
		return nil, err
	}
	if recurrenceService != nil {
		if err := cellarRuntime.Register(recurrenceplugin.HandlerStaleEvent, recurrenceplugin.StaleEventHandler{Service: recurrenceService, Logger: cfg.Logger}); err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
		if err := cellarRuntime.Register(recurrenceplugin.HandlerCreateEventPoll, recurrenceplugin.CreateEventPollHandler{Service: recurrenceService, Logger: cfg.Logger}); err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
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

		reevaluateTimer, err := cellar.NewTimer(eventvenuelistener.TimerName, cellar.TimerConfig{
			Interval: eventVenueListener.Interval(),
			Mode:     cellar.TimerFixedRate,
		}, eventVenueListener.ReevaluateOnce)
		if err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
		if err := reevaluateTimer.Register(cellarRuntime); err != nil {
			_ = firestoreClient.Close()
			_ = baseStore.Close()
			return nil, err
		}
		if _, err := reevaluateTimer.Schedule(cellarRuntime); err != nil && !errors.Is(err, cellar.ErrTimerAlreadyExists) {
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

	application := &App{
		logger:                cfg.Logger,
		baseStore:             baseStore,
		cellarStore:           cellarStore,
		idempotencyStore:      idempotencyStore,
		recurrenceService:     recurrenceService,
		firestoreClient:       firestoreClient,
		cellarRuntime:         cellarRuntime,
		eventVenueListener:    eventVenueListener,
		newPollListener:       newPollListener,
		completedPollListener: completedPollListener,
	}

	if cfg.HTTPAddr != "" {
		listener, err := net.Listen("tcp", cfg.HTTPAddr)
		if err != nil {
			if firestoreClient != nil {
				_ = firestoreClient.Close()
			}
			_ = baseStore.Close()
			return nil, fmt.Errorf("listen on %q: %w", cfg.HTTPAddr, err)
		}
		mux := http.NewServeMux()
		mux.Handle("POST /log", &logendpoint.Endpoint{Cells: application, Logger: cfg.Logger})
		application.httpListener = listener
		application.httpServer = &http.Server{Handler: mux}
	}

	return application, nil
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
		a.shutdownHTTP()
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

	if a.httpServer != nil {
		go func() {
			if err := a.httpServer.Serve(a.httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.logger.Error("http server stopped unexpectedly", "err", err)
			}
		}()
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
	a.shutdownHTTP()
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

// shutdownHTTP stops accepting new HTTP requests before Cellar is drained and
// stopped, per docs/adr/0007-lifecycle.md. It is safe to call more than once and
// when HTTP is disabled.
func (a *App) shutdownHTTP() {
	if a.httpServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.httpServer.Shutdown(ctx)
}

// HTTPAddr returns the bound address of the HTTP server, or "" if HTTP is disabled.
func (a *App) HTTPAddr() string {
	if a.httpListener == nil {
		return ""
	}
	return a.httpListener.Addr().String()
}

func (a *App) AddCell(request cellar.CellRequest) error {
	_, err := a.cellarStore.Add([]cellar.CellRequest{request})
	return err
}

// IdempotencyClaimed reports whether an identity has already been claimed.
func (a *App) IdempotencyClaimed(ctx context.Context, listener, eventKey string) (bool, error) {
	return a.idempotencyStore.Exists(ctx, listener, eventKey)
}

func (a *App) CellarStore() cellar.Store {
	return a.cellarStore
}

func sqliteDSN(path string) string {
	return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}
