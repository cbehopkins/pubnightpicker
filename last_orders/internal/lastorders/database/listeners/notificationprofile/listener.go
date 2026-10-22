package notificationprofile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/database/listeners/projectionwatch"
)

const defaultRetryDelay = 5 * time.Second

var ErrMalformedDocument = errors.New("malformed notification profile document")

var errPersistence = errors.New("persist notification profile projection")

// Listener keeps the notification profile projection converged with Firebase.
// It emits no Truths; it only maintains derived state.
type Listener struct {
	service    *notificationprofile.Service
	store      *notificationprofile.Store
	logger     *slog.Logger
	retryDelay time.Duration
	ready      chan struct{}
	failed     chan error
	unloaded   atomic.Int32
	lifecycle.Controller
}

// New constructs a new notification profile Listener.
// see CDD 0009-notification-profile.md for design details.
func New(service *notificationprofile.Service, store *notificationprofile.Store, logger *slog.Logger) (*Listener, error) {
	if service == nil {
		return nil, errors.New("notification profile service is required")
	}
	if store == nil {
		return nil, errors.New("notification profile store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	listener := &Listener{service: service, store: store, logger: logger, retryDelay: defaultRetryDelay, ready: make(chan struct{}), failed: make(chan error, 1)}
	listener.unloaded.Store(2)
	return listener, nil
}

// Ready closes once both streams have applied their first full snapshot.
func (l *Listener) Ready() <-chan struct{} {
	return l.ready
}

func (l *Listener) Errors() <-chan error {
	return l.failed
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

// watch runs the user and endpoint streams together because the Controller owns a
// single worker.
func (l *Listener) watch(ctx context.Context) {
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		l.watchStream(ctx, "user", l.service.SourceWatchUsers, l.applyUsers, l.streamLoaded())
	}()
	go func() {
		defer group.Done()
		l.watchStream(ctx, "endpoint", l.service.SourceWatchEndpoints, l.applyEndpoints, l.streamLoaded())
	}()
	group.Wait()
}

// streamLoaded returns a marker that counts one stream's first snapshot once.
func (l *Listener) streamLoaded() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if l.unloaded.Add(-1) == 0 {
				close(l.ready)
			}
		})
	}
}

type openStream func(context.Context) (notificationprofile.ChangeStream, error)

type applyBatch func(context.Context, []notificationprofile.Change, bool) error

func (l *Listener) watchStream(ctx context.Context, name string, open openStream, apply applyBatch, loaded func()) {
	openTyped := func(ctx context.Context) (projectionwatch.Stream[notificationprofile.Change], error) {
		return open(ctx)
	}
	err := projectionwatch.Run(ctx, projectionwatch.Config[notificationprofile.Change]{
		Name:            name,
		Open:            openTyped,
		Apply:           projectionwatch.ApplyBatch[notificationprofile.Change](apply),
		BaselineApplied: loaded,
		RetryDelay:      l.retryDelay,
		Logger:          l.logger,
	})
	if err != nil {
		select {
		case l.failed <- fmt.Errorf("%s stream: %w", name, err):
		default:
		}
	}
}

func (l *Listener) applyUsers(ctx context.Context, changes []notificationprofile.Change, baseline bool) error {
	mutations := make([]notificationprofile.UserPreferencesMutation, 0, len(changes))
	for _, change := range changes {
		mutation := notificationprofile.UserPreferencesMutation{UserID: change.Doc.ID}
		if change.Kind != notificationprofile.ChangeRemoved {
			preferences, err := notificationprofile.PreferencesFromDocument(change.Doc)
			if err != nil {
				if baseline {
					return fmt.Errorf("%w: user %q: %v", ErrMalformedDocument, change.Doc.ID, err)
				}
				l.logger.Warn("skip malformed notification profile document",
					"doc_id", change.Doc.ID, "err", err)
				continue
			}
			mutation.Preferences = &preferences
		}
		mutations = append(mutations, mutation)
	}
	if err := l.store.ApplyPreferences(ctx, mutations, baseline); err != nil {
		return fmt.Errorf("%w: %w", errPersistence, err)
	}
	return nil
}

func (l *Listener) applyEndpoints(ctx context.Context, changes []notificationprofile.Change, baseline bool) error {
	mutations := make([]notificationprofile.EndpointMutation, 0, len(changes))
	for _, change := range changes {
		mutation := notificationprofile.EndpointMutation{UserID: change.Doc.UserID, EndpointID: change.Doc.ID}
		if change.Kind != notificationprofile.ChangeRemoved {
			endpoint, err := notificationprofile.EndpointFromDocument(change.Doc)
			if err != nil {
				if baseline {
					return fmt.Errorf("%w: endpoint %q for user %q: %v", ErrMalformedDocument, change.Doc.ID, change.Doc.UserID, err)
				}
				l.logger.Warn("skip malformed notification profile document",
					"doc_id", change.Doc.ID, "user_id", change.Doc.UserID, "err", err)
				continue
			}
			mutation.Endpoint = &endpoint
		}
		mutations = append(mutations, mutation)
	}
	if err := l.store.ApplyEndpoints(ctx, mutations, baseline); err != nil {
		return fmt.Errorf("%w: %w", errPersistence, err)
	}
	return nil
}
