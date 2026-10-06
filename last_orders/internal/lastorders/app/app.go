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
	"last_orders/internal/lastorders/components/apicors"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/diagnosticsconfig"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/idempotency"
	"last_orders/internal/lastorders/components/listenerscope"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/pushsources"
	"last_orders/internal/lastorders/components/ratelimit"
	"last_orders/internal/lastorders/components/recurrence"
	venuecache "last_orders/internal/lastorders/components/venuecache"
	autocompletelistener "last_orders/internal/lastorders/database/listeners/autocomplete"
	chatmessagelistener "last_orders/internal/lastorders/database/listeners/chatmessages"
	completedpolllistener "last_orders/internal/lastorders/database/listeners/completedpolls"
	eventvenuelistener "last_orders/internal/lastorders/database/listeners/eventvenues"
	newpolllistener "last_orders/internal/lastorders/database/listeners/newpolls"
	notificationmirrorlistener "last_orders/internal/lastorders/database/listeners/notificationmirror"
	notificationprofilelistener "last_orders/internal/lastorders/database/listeners/notificationprofile"
	pushtestlistener "last_orders/internal/lastorders/database/listeners/pushtest"
	testemaillistener "last_orders/internal/lastorders/database/listeners/testemail"
	venuecachelistener "last_orders/internal/lastorders/database/listeners/venuecache"
	emailhistoryendpoint "last_orders/internal/lastorders/endpoints/emailhistory"
	logendpoint "last_orders/internal/lastorders/endpoints/log"
	pingendpoint "last_orders/internal/lastorders/endpoints/ping"
	autocompleteplugin "last_orders/internal/lastorders/plugins/autocomplete"
	emailplugin "last_orders/internal/lastorders/plugins/email"
	"last_orders/internal/lastorders/plugins/polls"
	pushplugin "last_orders/internal/lastorders/plugins/push"
	"last_orders/internal/lastorders/plugins/pushevents"
	recurrenceplugin "last_orders/internal/lastorders/plugins/recurrence"
	testemailplugin "last_orders/internal/lastorders/plugins/testemail"
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
	DBPath             string
	PollDelay          time.Duration
	Logger             *slog.Logger
	FirestoreProjectID string
	EnableFirestore    bool
	// PollsSince (YYYY-MM-DD) is persisted in Firestore and may only move forward; empty uses the stored value.
	PollsSince        string
	IdempotencyRemote firebaseidempotency.Remote
	// CompletionActions is the durable completed-poll action history shared with Python.
	CompletionActions      completionactions.Store
	EventReevaluateEvery   time.Duration
	StartupComponentChecks []func(*basestore.Store) error
	// HTTPAddr is the address to serve HTTP endpoints on. An empty value disables HTTP entirely.
	HTTPAddr               string
	AuthProjectID          string
	AuthVerifier           firebaseauth.Verifier
	AllowAuthEmulator      bool
	AllowedAPIOrigins      []string
	AllowedAPIPreviewSites []string
	// EmailClient selects the provider used for durable email delivery.
	EmailClient emailplugin.ClientKind
	// Email contains provider credentials and options. EmailClient remains as a
	// compatibility shortcut for callers that only need to select a client kind.
	Email             emailplugin.Options
	DiagnosticsSource diagnosticsconfig.Source
	// Push configures Web Push delivery.
	Push            pushplugin.Options
	EmailDailyLimit int
	PushDailyLimit  int

	// External collaborators. Each is optional; when nil it is built from the
	// Firestore client, and construction fails if Firestore is disabled.
	RecurrenceService         RecurrenceService
	VenueSource               venuecache.Source
	NotificationProfileSource notificationprofile.Source
	EventVenueSource          eventvenuelistener.Source
	NewPollSource             newpolllistener.Source
	CompletedPollSource       completedpolllistener.Source
	AutocompleteSource        autocompletesvc.Source
	PushSources               pushsources.Source
	ChatMessageSource         chatmessagelistener.Source
	PushTestSource            pushtestlistener.Source
	NotificationMirrorSource  notificationmirrorlistener.Source
	TestEmailSource           testemaillistener.Source
	// TestEmailTokens limits diagnostics test emails; defaults to TestEmailDailyLimit per day.
	TestEmailTokens ratelimit.TokenSource
}

// TestEmailDailyLimit is the default number of diagnostics test emails per day.
const TestEmailDailyLimit = 10

const (
	EmailDailyLimit = 100
	PushDailyLimit  = 1000
)

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
	emailPlugin                 *emailplugin.Plugin
	diagnosticsConfiguration    *diagnosticsconfig.Service
	eventVenueListener          *eventvenuelistener.Listener
	newPollListener             *newpolllistener.Listener
	completedPollListener       *completedpolllistener.Listener
	chatMessageListener         *chatmessagelistener.Listener
	pushTestListener            *pushtestlistener.Listener
	testEmailListener           *testemaillistener.Listener
	notificationMirrorListener  *notificationmirrorlistener.Listener
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
	if cfg.EmailDailyLimit < 0 || cfg.PushDailyLimit < 0 {
		return nil, fmt.Errorf("daily send limits must be positive")
	}
	if cfg.EmailDailyLimit == 0 {
		cfg.EmailDailyLimit = EmailDailyLimit
	}
	if cfg.PushDailyLimit == 0 {
		cfg.PushDailyLimit = PushDailyLimit
	}
	if cfg.PollDelay <= 0 {
		cfg.PollDelay = 50 * time.Millisecond
	}
	if cfg.EnableFirestore && cfg.FirestoreProjectID == "" {
		cfg.FirestoreProjectID = "last-orders-emulator"
	}
	if cfg.HTTPAddr != "" {
		if err := firebaseauth.CheckEmulator(cfg.AllowAuthEmulator); err != nil {
			return nil, err
		}
		if cfg.AuthVerifier == nil {
			cfg.AuthVerifier, err = firebaseauth.New(context.Background(), cfg.AuthProjectID, cfg.AllowAuthEmulator)
			if err != nil {
				return nil, err
			}
		}
		if cfg.AllowedAPIOrigins == nil {
			cfg.AllowedAPIOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
		}
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
	emailOptions := cfg.Email
	if emailOptions.Client == "" {
		emailOptions.Client = cfg.EmailClient
	}
	if emailOptions.Logger == nil {
		emailOptions.Logger = cfg.Logger
	}
	diagnosticsSource := cfg.DiagnosticsSource
	if diagnosticsSource == nil && firestoreClient != nil {
		diagnosticsSource = diagnosticsconfig.FirestoreSource(firestoreClient)
	}
	var diagnosticsConfiguration *diagnosticsconfig.Service
	if diagnosticsSource != nil {
		diagnosticsConfiguration = diagnosticsconfig.New(diagnosticsSource, cfg.Logger)
		emailOptions.SilenceNotifications = diagnosticsConfiguration.Snapshot
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
	pushOptions := cfg.Push
	if pushOptions.Logger == nil {
		pushOptions.Logger = cfg.Logger
	}
	if diagnosticsConfiguration != nil {
		pushOptions.SilenceNotifications = diagnosticsConfiguration.Snapshot
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

	if emailOptions.Tokens == nil {
		emailOptions.Tokens, err = ratelimit.New("email.send", cfg.EmailDailyLimit, recurrenceService.Location(), func() {
			cfg.Logger.Warn("rate limit exhausted", "source", "email.send", "maximum", cfg.EmailDailyLimit)
		})
		if err != nil {
			return nil, err
		}
	}
	if pushOptions.Tokens == nil {
		pushOptions.Tokens, err = ratelimit.New("push.send", cfg.PushDailyLimit, recurrenceService.Location(), func() {
			cfg.Logger.Warn("rate limit exhausted", "source", "push.send", "maximum", cfg.PushDailyLimit)
		})
		if err != nil {
			return nil, err
		}
	}
	emailPlugin, err := emailplugin.New(baseStore.DB(), emailOptions)
	if err != nil {
		return nil, fmt.Errorf("init email plugin: %w", err)
	}
	pushPlugin, err := pushplugin.New(baseStore.DB(), notificationProfileService, pushOptions)
	if err != nil {
		return nil, fmt.Errorf("init push plugin: %w", err)
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

	var pollsSince string
	if cfg.NewPollSource == nil || cfg.CompletedPollSource == nil || cfg.ChatMessageSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("poll and chat message sources are required: supply them in Config or enable firestore")
		}
		scope, err := listenerscope.NewFirestoreStore(firestoreClient, "listener_state", "last_orders")
		if err != nil {
			return nil, err
		}
		resolveCtx, cancelResolve := context.WithTimeout(context.Background(), 30*time.Second)
		pollsSince, err = scope.ResolvePollsSince(resolveCtx, cfg.PollsSince)
		cancelResolve()
		if err != nil {
			return nil, fmt.Errorf("resolve polls since: %w", err)
		}
		cfg.Logger.Info("listener scope resolved", "polls_since", pollsSince)
	}

	newPollSource := cfg.NewPollSource
	if newPollSource == nil {
		newPollSource, err = newpolllistener.NewFirestoreSource(firestoreClient, pollsSince)
		if err != nil {
			return nil, err
		}
	}

	completedPollSource := cfg.CompletedPollSource
	if completedPollSource == nil {
		completedPollSource, err = completedpolllistener.NewFirestoreSource(firestoreClient, pollsSince)
		if err != nil {
			return nil, err
		}
	}

	pushSources := cfg.PushSources
	if pushSources == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("push sources are required: supply Config.PushSources or enable firestore")
		}
		pushSources, err = pushsources.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	chatMessageSource := cfg.ChatMessageSource
	if chatMessageSource == nil {
		messagesSince, err := time.ParseInLocation(time.DateOnly, pollsSince, recurrenceService.Location())
		if err != nil {
			return nil, fmt.Errorf("parse polls since: %w", err)
		}
		chatMessageSource, err = chatmessagelistener.NewFirestoreSource(firestoreClient, messagesSince)
		if err != nil {
			return nil, err
		}
	}

	pushTestSource := cfg.PushTestSource
	if pushTestSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("push test source is required: supply Config.PushTestSource or enable firestore")
		}
		pushTestSource, err = pushtestlistener.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	notificationMirrorSource := cfg.NotificationMirrorSource
	if notificationMirrorSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("notification mirror source is required: supply Config.NotificationMirrorSource or enable firestore")
		}
		notificationMirrorSource, err = notificationmirrorlistener.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	testEmailSource := cfg.TestEmailSource
	if testEmailSource == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("test email source is required: supply Config.TestEmailSource or enable firestore")
		}
		testEmailSource, err = testemaillistener.NewFirestoreSource(firestoreClient)
		if err != nil {
			return nil, err
		}
	}

	testEmailTokens := cfg.TestEmailTokens
	if testEmailTokens == nil {
		logger := cfg.Logger
		testEmailTokens, err = ratelimit.New("email.test", TestEmailDailyLimit, recurrenceService.Location(), func() {
			logger.Warn("test email daily limit exhausted", "source", "email.test", "limit", TestEmailDailyLimit)
		})
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

	if cfg.CompletionActions == nil {
		if firestoreClient == nil {
			return nil, fmt.Errorf("completion actions are required: enable firestore or supply Config.CompletionActions")
		}
		cfg.CompletionActions, err = completionactions.NewFirestoreStore(firestoreClient)
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
	truths.PollOpenedRegistry.Register(polls.HandlerPollOpenedEmail)
	truths.PollOpenedRegistry.Register(polls.HandlerPollOpenedPush)
	truths.PollCompletedRegistry.Register(polls.HandlerPollCompleted)
	truths.PollCompletedRegistry.Register(polls.HandlerPollCompletedEmail)
	truths.PollCompletedRegistry.Register(polls.HandlerPollCompletedPush)
	truths.PollManualCompletionRequiredRegistry.Register(polls.HandlerManualCompletionNeedPush)
	truths.ChatMessagePostedRegistry.Register(pushevents.HandlerChatPush)
	truths.PushTestRequestedRegistry.Register(pushevents.HandlerPushTest)
	truths.TestEmailRequestedRegistry.Register(testemailplugin.HandlerTestEmail)
	truths.EventVenueObservedRegistry.Register(recurrenceplugin.HandlerEvaluateEventVenue)
	truths.StaleEventRegistry.Register(recurrenceplugin.HandlerStaleEvent)
	truths.CreateEventPollRegistry.Register(recurrenceplugin.HandlerCreateEventPoll)
	truths.LogMessageRegistry.Register(logsvc.HandlerLogMessage)
	autocompleteplugin.Register()

	cellarRuntime := cellar.New(cellarStore, cellar.Config{PollDelay: cfg.PollDelay})
	if err := registerTruthFanouts(cellarRuntime); err != nil {
		return nil, err
	}
	if err := emailPlugin.Register(cellarRuntime); err != nil {
		return nil, fmt.Errorf("register email plugin: %w", err)
	}
	if err := pushPlugin.Register(cellarRuntime); err != nil {
		return nil, fmt.Errorf("register push plugin: %w", err)
	}
	// FIXME this should probably be in a table
	if err := cellarRuntime.Register(polls.HandlerPollOpened, polls.PollOpenedHandler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerPollOpenedEmail, polls.PollOpenedEmailHandler{
		Actions: cfg.CompletionActions, Recipients: notificationProfileService,
		BaseURL: pushPlugin.BaseURL(), Logger: cfg.Logger,
	}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerPollOpenedPush, polls.PollOpenedPushHandler{
		Actions:   cfg.CompletionActions,
		Endpoints: notificationProfileService,
		Push:      pushPlugin,
		Logger:    cfg.Logger,
	}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerPollCompleted, polls.PollCompletedHandler{Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerPollCompletedEmail, polls.PollCompletedEmailHandler{
		Actions:    cfg.CompletionActions,
		Venues:     venueCacheService,
		Recipients: notificationProfileService,
		BaseURL:    pushPlugin.BaseURL(),
		Logger:     cfg.Logger,
	}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerCompletionMarked, polls.CompletionMarkedHandler{Actions: cfg.CompletionActions}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerPollCompletedPush, polls.PollCompletedPushHandler{
		Actions:   cfg.CompletionActions,
		Venues:    venueCacheService,
		Endpoints: notificationProfileService,
		Push:      pushPlugin,
		Logger:    cfg.Logger,
	}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(polls.HandlerManualCompletionNeedPush, polls.ManualCompletionPushHandler{
		Completers: pushSources,
		Endpoints:  notificationProfileService,
		Push:       pushPlugin,
	}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(pushevents.HandlerChatPush, pushevents.ChatPushHandler{Source: pushSources, Profiles: notificationProfileService, Push: pushPlugin}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(pushevents.HandlerChatProcessed, pushevents.ChatProcessedHandler{Source: pushSources}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(pushevents.HandlerPushTest, pushevents.PushTestHandler{Source: pushSources, Profiles: notificationProfileService, Push: pushPlugin}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(pushevents.HandlerPushTestCompleted, pushevents.PushTestCompletedHandler{Source: pushSources, Push: pushPlugin}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(testemailplugin.HandlerTestEmail, testemailplugin.Handler{Tokens: testEmailTokens, Logger: cfg.Logger}); err != nil {
		return nil, err
	}
	if err := cellarRuntime.Register(testemailplugin.HandlerTestEmailAcked, testemailplugin.AckedHandler{Acks: testEmailSource}); err != nil {
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

	chatMessageListener, err := chatmessagelistener.New(chatMessageSource, cellarStore, cfg.Logger)
	if err != nil {
		return nil, err
	}

	pushTestListener, err := pushtestlistener.New(pushTestSource, cellarStore, cfg.Logger)
	if err != nil {
		return nil, err
	}

	testEmailListener, err := testemaillistener.New(testEmailSource, cellarStore, cfg.Logger)
	if err != nil {
		return nil, err
	}

	notificationMirrorListener, err := notificationmirrorlistener.New(notificationmirrorlistener.Config{
		Source: notificationMirrorSource,
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
		emailPlugin:                 emailPlugin,
		diagnosticsConfiguration:    diagnosticsConfiguration,
		eventVenueListener:          eventVenueListener,
		newPollListener:             newPollListener,
		completedPollListener:       completedPollListener,
		chatMessageListener:         chatMessageListener,
		pushTestListener:            pushTestListener,
		testEmailListener:           testEmailListener,
		notificationMirrorListener:  notificationMirrorListener,
		venueCacheListener:          venueCacheListener,
		notificationProfileListener: notificationProfileListener,
	}

	if cfg.HTTPAddr != "" {
		apiMux := http.NewServeMux()
		apiMux.Handle("POST /api/ping", &pingendpoint.Endpoint{Logger: cfg.Logger})
		apiMux.Handle("POST /api/email-history", &emailhistoryendpoint.Endpoint{Store: emailPlugin, Logger: cfg.Logger})
		apiHandler, err := apicors.Wrap(cfg.AllowedAPIOrigins, firebaseauth.Middleware(cfg.AuthVerifier, apiMux), cfg.AllowedAPIPreviewSites...)
		if err != nil {
			return nil, err
		}
		listener, err := net.Listen("tcp", cfg.HTTPAddr)
		if err != nil {
			return nil, fmt.Errorf("listen on %q: %w", cfg.HTTPAddr, err)
		}
		mux := http.NewServeMux()
		mux.Handle("POST /log", &logendpoint.Endpoint{Cells: application, Logger: cfg.Logger})
		mux.Handle("/api/", apiHandler)
		application.httpListener = listener
		application.httpServer = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
		cleanup.Add(listener.Close)
	}

	cleanup = nil
	return application, nil
}

func (a *App) Run(ctx context.Context) error {
	if a.baseStore == nil {
		return fmt.Errorf("app is not initialised")
	}
	// Must precede Cellar so interrupted submissions are verified before any resend.
	if err := a.emailPlugin.RecoverSubmissions(ctx); err != nil {
		return fmt.Errorf("recover email submissions: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	runDone := make(chan struct{})
	a.runMu.Lock()
	a.runCancel = cancel
	a.runDone = runDone
	a.runMu.Unlock()
	var cellarDone chan struct{}
	defer func() {
		a.shutdownHTTP()
		cancel()
		a.closeListeners()
		if cellarDone != nil {
			_ = a.cellarRuntime.Stop()
			<-cellarDone
		}
		a.runMu.Lock()
		a.runCancel = nil
		a.runDone = nil
		a.runMu.Unlock()
		close(runDone)
	}()

	if a.diagnosticsConfiguration != nil {
		if err := a.diagnosticsConfiguration.Start(runCtx); err != nil {
			return fmt.Errorf("start diagnostics configuration: %w", err)
		}
	}

	// Recipient and venue reads must see complete projections before any work runs.
	for _, projection := range []interface {
		Start(context.Context) error
		Ready() <-chan struct{}
	}{a.notificationProfileListener, a.venueCacheListener} {
		if err := projection.Start(runCtx); err != nil {
			return fmt.Errorf("start projection listener: %w", err)
		}
	}
	for _, ready := range []<-chan struct{}{a.notificationProfileListener.Ready(), a.venueCacheListener.Ready()} {
		select {
		case <-ready:
		case <-runCtx.Done():
			a.logger.Info("run context cancelled before projections were ready")
			return nil
		}
	}

	cellarErr := make(chan error, 1)
	cellarDone = make(chan struct{})
	go func() {
		defer close(cellarDone)
		cellarErr <- a.cellarRuntime.Start(runCtx)
	}()

	if a.eventVenueListener != nil {
		if err := a.eventVenueListener.Start(runCtx); err != nil {
			return fmt.Errorf("start event venue listener: %w", err)
		}
	}

	if a.newPollListener != nil {
		if err := a.newPollListener.Start(runCtx); err != nil {
			return fmt.Errorf("start new poll listener: %w", err)
		}
	}

	if a.completedPollListener != nil {
		if err := a.completedPollListener.Start(runCtx); err != nil {
			return fmt.Errorf("start completed poll listener: %w", err)
		}
	}

	if err := a.chatMessageListener.Start(runCtx); err != nil {
		return fmt.Errorf("start chat message listener: %w", err)
	}
	if err := a.pushTestListener.Start(runCtx); err != nil {
		return fmt.Errorf("start push test listener: %w", err)
	}
	if err := a.testEmailListener.Start(runCtx); err != nil {
		return fmt.Errorf("start test email listener: %w", err)
	}
	if err := a.notificationMirrorListener.Start(runCtx); err != nil {
		return fmt.Errorf("start notification mirror listener: %w", err)
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
	if a.diagnosticsConfiguration != nil {
		closeErrs = append(closeErrs, a.diagnosticsConfiguration.Close())
	}
	if a.eventVenueListener != nil {
		closeErrs = append(closeErrs, a.eventVenueListener.Close())
	}
	if a.newPollListener != nil {
		closeErrs = append(closeErrs, a.newPollListener.Close())
	}
	if a.completedPollListener != nil {
		closeErrs = append(closeErrs, a.completedPollListener.Close())
	}
	if a.chatMessageListener != nil {
		closeErrs = append(closeErrs, a.chatMessageListener.Close())
	}
	if a.pushTestListener != nil {
		closeErrs = append(closeErrs, a.pushTestListener.Close())
	}
	if a.testEmailListener != nil {
		closeErrs = append(closeErrs, a.testEmailListener.Close())
	}
	if a.notificationMirrorListener != nil {
		closeErrs = append(closeErrs, a.notificationMirrorListener.Close())
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
		registerFanout[truths.PollManualCompletionRequired](truths.PollManualCompletionRequiredRegistry),
		registerFanout[truths.ChatMessagePosted](truths.ChatMessagePostedRegistry),
		registerFanout[truths.PushTestRequested](truths.PushTestRequestedRegistry),
		registerFanout[truths.TestEmailRequested](truths.TestEmailRequestedRegistry),
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
