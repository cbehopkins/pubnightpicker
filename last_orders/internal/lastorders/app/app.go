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
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/recurrence"
	venuecache "last_orders/internal/lastorders/components/venuecache"
	autocompletelistener "last_orders/internal/lastorders/database/listeners/autocomplete"
	completedpolllistener "last_orders/internal/lastorders/database/listeners/completedpolls"
	eventvenuelistener "last_orders/internal/lastorders/database/listeners/eventvenues"
	newpolllistener "last_orders/internal/lastorders/database/listeners/newpolls"
	notificationprofilelistener "last_orders/internal/lastorders/database/listeners/notificationprofile"
	venuecachelistener "last_orders/internal/lastorders/database/listeners/venuecache"
	logendpoint "last_orders/internal/lastorders/endpoints/log"
	autocompleteplugin "last_orders/internal/lastorders/plugins/autocomplete"
	"last_orders/internal/lastorders/plugins/polls"
	recurrenceplugin "last_orders/internal/lastorders/plugins/recurrence"
	autocompletesvc "last_orders/internal/lastorders/services/autocomplete"
	logsvc "last_orders/internal/lastorders/services/log"
	"last_orders/internal/lastorders/truths"

	"cloud.google.com/go/firestore"

	_ "modernc.org/sqlite"
)

// RecurrenceService is the recurrence surface the application requires. It is the
// union of what app.New and its downstream consumers need, and is satisfied by
// *recurrence.Service.
type RecurrenceService interface {
	Location() *time.Location
	Today() time.Time
	AdvanceStaleEvent(ctx context.Context, eventID string) error
	CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error
}

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

	// External collaborators. Each is optional; when nil it is built from the
	// Firestore client, and construction fails if Firestore is disabled.
	RecurrenceService         RecurrenceService
	VenueSource               venuecache.Source
	NotificationProfileSource notificationprofile.Source
	EventVenueSource          eventvenuelistener.Source
	NewPollSource             newpolllistener.Source
	CompletedPollSource       completedpolllistener.Source
	AutocompleteSource        autocompletesvc.Source
}

type App struct {
	logger                      *slog.Logger
	baseStore                   *basestore.Store
	cellarStore                 cellar.Store
	idempotencyStore            *firebaseidempotency.Store
	recurrenceService           RecurrenceService
	venueCacheStore             *venuecache.Store
	venueCacheService           *venuecache.Service
	notificationProfileStore    *notificationprofile.Store
	notificationProfileService  *notificationprofile.Service
	firestoreClient             *firestore.Client
	cellarRuntime               *cellar.Cellar
	eventVenueListener          *eventvenuelistener.Listener
	newPollListener             *newpolllistener.Listener
	completedPollListener       *completedpolllistener.Listener
	venueCacheListener          *venuecachelistener.Listener
	notificationProfileListener *notificationprofilelistener.Listener
	httpServer                  *http.Server
	httpListener                net.Listener
	runCancel                   context.CancelFunc
	runDone                     chan struct{}
	runMu                       sync.Mutex
}

func New(cfg Config) (application *App, err error) {
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
	cleanup := newCleanupStack()
	cleanup.Add(db.Close)
	defer func() {
		if application == nil {
			cleanup.Run()
		}
	}()

	baseStore, err := basestore.New(db)
	if err != nil {
		return nil, err
	}
	// Replace the previous cleanup stack with a new one for the baseStore and subsequent resources.
	cleanup = newCleanupStack()
	cleanup.Add(baseStore.Close)

	cellarStore, err := publicsqlite.NewStore(baseStore.DB(), nil)
	if err != nil {
		return nil, fmt.Errorf("init cellar store: %w", err)
	}

	var firestoreClient *firestore.Client
	if cfg.EnableFirestore {
		firestoreClient, err = firestore.NewClient(context.Background(), cfg.FirestoreProjectID)
		if err != nil {
			return nil, fmt.Errorf("init firestore client: %w", err)
		}
		cleanup.Add(firestoreClient.Close)
	}

	// Projection stores are SQLite-backed and always constructible; only their
	// upstream sources need resolving.
	venueCacheStore, err := venuecache.New(baseStore)
	if err != nil {
		return nil, fmt.Errorf("init venue cache store: %w", err)
	}
	notificationProfileStore, err := notificationprofile.New(baseStore)
	if err != nil {
		return nil, fmt.Errorf("init notification profile store: %w", err)
	}

	venueSource := cfg.VenueSource
	if venueSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("venue source is required: supply Config.VenueSource or enable firestore")
		}
		venueSource, err = venuecache.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}
	venueCacheService, err := venuecache.NewService(venueCacheStore, venueSource, cfg.Logger)
	if err != nil {
		return nil, err
	}

	notificationProfileSource := cfg.NotificationProfileSource
	if notificationProfileSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("notification profile source is required: supply Config.NotificationProfileSource or enable firestore")
		}
		notificationProfileSource, err = notificationprofile.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}
	notificationProfileService, err := notificationprofile.NewService(notificationProfileStore, notificationProfileSource, cfg.Logger)
	if err != nil {
		return nil, err
	}

	recurrenceService := cfg.RecurrenceService
	if recurrenceService == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("recurrence service is required: supply Config.RecurrenceService or enable firestore")
		}
		concrete, err := recurrence.NewService(firestoreClient, cfg.Logger, venueCacheService)
		if err != nil || concrete == nil {
			return nil, fmt.Errorf("init recurrence service: %w", err)
		}
		recurrenceService = concrete
	}

	autocompleteSource := cfg.AutocompleteSource
	if autocompleteSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("autocomplete source is required: supply Config.AutocompleteSource or enable firestore")
		}
		autocompleteSource, err = autocompletesvc.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	eventVenueSource := cfg.EventVenueSource
	if eventVenueSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("event venue source is required: supply Config.EventVenueSource or enable firestore")
		}
		eventVenueSource, err = eventvenuelistener.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	newPollSource := cfg.NewPollSource
	if newPollSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("new poll source is required: supply Config.NewPollSource or enable firestore")
		}
		newPollSource, err = newpolllistener.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	completedPollSource := cfg.CompletedPollSource
	if completedPollSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("completed poll source is required: supply Config.CompletedPollSource or enable firestore")
		}
		completedPollSource, err = completedpolllistener.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	if cfg.IdempotencyRemote == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("idempotency remote is required: enable firestore or supply Config.IdempotencyRemote")
		}
		cfg.IdempotencyRemote, err = firebaseidempotency.NewFirestoreRemote(firestoreClient, "listener_state", "last_orders")
		if err != nil {
			return nil, err
		}
	}

	idempotencyStore, err := firebaseidempotency.New(baseStore)
	if err != nil {
		return nil, err
	}
	localIdempotencyStore, err := idempotency.New(baseStore)
	if err != nil {
		return nil, err
	}

	for _, check := range cfg.StartupComponentChecks {
		if check == nil {
			continue
		}
		if err := check(baseStore); err != nil {
			return nil, fmt.Errorf("startup component check failed: %w", err)
		}
	}

	truths.PollOpenedRegistry.Register(polls.HandlerPollOpened)
	truths.PollCompletedRegistry.Register(polls.HandlerPollCompleted)
	truths.EventVenueObservedRegistry.Register(recurrenceplugin.HandlerEvaluateEventVenue)
	truths.StaleEventRegistry.Register(recurrenceplugin.HandlerStaleEvent)
	truths.CreateEventPollRegistry.Register(recurrenceplugin.HandlerCreateEventPoll)
	truths.LogMessageRegistry.Register(logsvc.HandlerLogMessage)
	autocompleteplugin.Register()

	cellarRuntime := cellar.New(cellarStore, cellar.Config{PollDelay: cfg.PollDelay})
	if err := registerTruthFanouts(cellarRuntime); err != nil {
		return nil, err
	}
	// FIXME this should probably be in a table
	if err := cellarRuntime.Register(polls.HandlerPollOpened, polls.PollOpenedHandler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerPollCompleted, polls.PollCompletedHandler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerCheck, firebaseidempotency.CheckHandler{Store: idempotencyStore, Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerPopulateRemote, firebaseidempotency.PopulateRemoteHandler{Remote: cfg.IdempotencyRemote, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(firebaseidempotency.HandlerEmitTruth, firebaseidempotency.EmitTruthHandler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(idempotency.HandlerCheck, idempotency.CheckHandler{Store: localIdempotencyStore, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(logsvc.HandlerLogMessage, logsvc.Handler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(autocompletesvc.HandlerDiscovery, autocompletesvc.DiscoveryHandler{Source: autocompleteSource, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(autocompletesvc.HandlerCandidate, autocompletesvc.CandidateHandler{Source: autocompleteSource, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(autocompletesvc.HandlerClose, autocompletesvc.CloseHandler{Source: autocompleteSource, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(autocompletesvc.HandlerAmbiguous, autocompletesvc.AmbiguousHandler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(recurrenceplugin.HandlerEvaluateEventVenue, recurrenceplugin.EvaluateEventVenueHandler{Store: cellarStore, Location: recurrenceService.Location(), Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(recurrenceplugin.HandlerStaleEvent, recurrenceplugin.StaleEventHandler{Service: recurrenceService, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(recurrenceplugin.HandlerCreateEventPoll, recurrenceplugin.CreateEventPollHandler{Service: recurrenceService, Logger: cfg.Logger}); err != nil {
		return nil, err
	}

	autoCompleteListener, err := autocompletelistener.New(cellarStore, cfg.Logger)
	if err != nil {
		return nil, err
	}
	autoCompleteTimer, err := cellar.NewTimer(autocompletelistener.TimerName, cellar.TimerConfig{Mode: cellar.TimerDailyCalendar, DailyAt: "16:00", Location: "Europe/London"}, autoCompleteListener.RunOnce)
	if err != nil {
		return nil, err
	}
	if err := autoCompleteTimer.Register(cellarRuntime); err != nil {
		return nil, err
	}
	if _, err := autoCompleteTimer.Schedule(cellarRuntime); err != nil && !errors.Is(err, cellar.ErrTimerAlreadyExists) {
		return nil, err
	}

	eventVenueListener, err := eventvenuelistener.New(eventvenuelistener.Config{
		Store:              cellarStore,
		Clock:              recurrenceService,
		Source:             eventVenueSource,
		VenueCache:         venueCacheService,
		ReevaluateInterval: cfg.EventReevaluateEvery,
		Logger:             cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	reevaluateTimer, err := cellar.NewTimer(eventvenuelistener.TimerName, cellar.TimerConfig{
		Interval: eventVenueListener.Interval(),
		Mode:     cellar.TimerFixedRate,
	}, eventVenueListener.ReevaluateOnce)
	if err != nil {
		return nil, err
	}
	if err := reevaluateTimer.Register(cellarRuntime); err != nil {
		return nil, err
	}
	if _, err := reevaluateTimer.Schedule(cellarRuntime); err != nil && !errors.Is(err, cellar.ErrTimerAlreadyExists) {
		return nil, err
	}

	newPollListener, err := newpolllistener.New(newpolllistener.Config{
		Source: newPollSource,
		Store:  cellarStore,
		Logger: cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	completedPollListener, err := completedpolllistener.New(completedpolllistener.Config{
		Source: completedPollSource,
		Store:  cellarStore,
		Logger: cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	venueCacheListener, err := venuecachelistener.New(venueCacheService, venueCacheStore, cfg.Logger)
	if err != nil {
		return nil, err
	}

	notificationProfileListener, err := notificationprofilelistener.New(notificationProfileService, notificationProfileStore, cfg.Logger)
	if err != nil {
		return nil, err
	}

	application = &App{
		logger:                      cfg.Logger,
		baseStore:                   baseStore,
		cellarStore:                 cellarStore,
		idempotencyStore:            idempotencyStore,
		recurrenceService:           recurrenceService,
		venueCacheStore:             venueCacheStore,
		venueCacheService:           venueCacheService,
		notificationProfileStore:    notificationProfileStore,
		notificationProfileService:  notificationProfileService,
		firestoreClient:             firestoreClient,
		cellarRuntime:               cellarRuntime,
		eventVenueListener:          eventVenueListener,
		newPollListener:             newPollListener,
		completedPollListener:       completedPollListener,
		venueCacheListener:          venueCacheListener,
		notificationProfileListener: notificationProfileListener,
	}

	if cfg.HTTPAddr != "" {
		listener, err := net.Listen("tcp", cfg.HTTPAddr)
		if err != nil {
			return nil, fmt.Errorf("listen on %q: %w", cfg.HTTPAddr, err)
		}
		mux := http.NewServeMux()
		mux.Handle("POST /log", &logendpoint.Endpoint{Cells: application, Logger: cfg.Logger})
		application.httpListener = listener
		application.httpServer = &http.Server{Handler: mux}
		cleanup.Add(listener.Close)
	}

	cleanup = nil
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
		a.closeListeners()
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

	if a.venueCacheListener != nil {
		if err := a.venueCacheListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start venue cache listener: %w", err)
		}
	}

	if a.notificationProfileListener != nil {
		if err := a.notificationProfileListener.Start(runCtx); err != nil {
			cancel()
			return fmt.Errorf("start notification profile listener: %w", err)
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
		a.logger.Info("run context cancelled, shutting down")
		return nil
	case err := <-cellarErr:
		// The scheduler stopping ends the whole application, so say so on the way out
		// rather than only via the process exit code.
		if err != nil {
			a.logger.Error("cellar scheduler stopped, shutting down", "err", err)
			return fmt.Errorf("run cellar: %w", err)
		}
		a.logger.Warn("cellar scheduler stopped without error, shutting down")
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
	} else {
		a.closeListeners()
	}
	if a.baseStore == nil {
		return nil
	}
	var closeErrs []error
	if a.firestoreClient != nil {
		closeErrs = append(closeErrs, a.firestoreClient.Close())
	}
	closeErrs = append(closeErrs, a.baseStore.Close())
	return errors.Join(closeErrs...)
}

func (a *App) closeListeners() error {
	var closeErrs []error
	if a.eventVenueListener != nil {
		closeErrs = append(closeErrs, a.eventVenueListener.Close())
	}
	if a.newPollListener != nil {
		closeErrs = append(closeErrs, a.newPollListener.Close())
	}
	if a.completedPollListener != nil {
		closeErrs = append(closeErrs, a.completedPollListener.Close())
	}
	if a.venueCacheListener != nil {
		closeErrs = append(closeErrs, a.venueCacheListener.Close())
	}
	if a.notificationProfileListener != nil {
		closeErrs = append(closeErrs, a.notificationProfileListener.Close())
	}
	return errors.Join(closeErrs...)
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

// NotificationProfile returns the projection service, or nil when Firestore is disabled.
func (a *App) NotificationProfile() *notificationprofile.Service {
	return a.notificationProfileService
}

// sqliteDSN configures the shared database. _txlock=immediate takes the write lock at
// BEGIN: without it a deferred transaction that upgrades from read to write fails with
// SQLITE_BUSY_SNAPSHOT straight away, which busy_timeout does not retry.
func sqliteDSN(path string) string {
	return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

// registerTruthFanouts registers every Truth's native Cellar Fanout with the
// runtime. It must run after every Plugin has declared its handlers via the
// truths package's per-Truth Registry.Register.
func registerTruthFanouts(cellarRuntime *cellar.Cellar) error {
	registrars := []func(*cellar.Cellar) error{
		registerFanout[truths.PollObservedPayload](truths.PollOpenedRegistry),
		registerFanout[truths.PollObservedPayload](truths.PollCompletedRegistry),
		registerFanout[truths.EventVenueObserved](truths.EventVenueObservedRegistry),
		registerFanout[truths.StaleEvent](truths.StaleEventRegistry),
		registerFanout[truths.CreateEventPoll](truths.CreateEventPollRegistry),
		registerFanout[truths.LogMessage](truths.LogMessageRegistry),
		registerFanout[truths.DailyPollAutoCompleteDue](truths.DailyPollAutoCompleteDueRegistry),
		registerFanout[truths.PollAutoCompletionDue](truths.PollAutoCompletionDueRegistry),
	}
	for _, registrar := range registrars {
		if err := registrar(cellarRuntime); err != nil {
			return err
		}
	}
	return nil
}

func registerFanout[T any](registry *truths.Registry[T]) func(*cellar.Cellar) error {
	return func(cellarRuntime *cellar.Cellar) error {
		fanout, err := registry.Fanout()
		if err != nil {
			return err
		}
		return fanout.Register(cellarRuntime)
	}
}

type cleanupStack []func() error

func newCleanupStack() cleanupStack {
	return make(cleanupStack, 0, 4)
}

func (s *cleanupStack) Add(cleanup func() error) {
	*s = append(*s, cleanup)
}

func (s *cleanupStack) Run() {
	for index := len(*s) - 1; index >= 0; index-- {
		_ = (*s)[index]()
	}
}
