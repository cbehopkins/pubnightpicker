package notificationprofile

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/database/listeners/lifecycle"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultRetryDelay = 5 * time.Second

// Listener keeps the notification profile projection converged with Firebase.
// It emits no Truths; it only maintains derived state.
type Listener struct {
	service    *notificationprofile.Service
	store      *notificationprofile.Store
	logger     *slog.Logger
	retryDelay time.Duration
	lifecycle.Controller
}

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
	return &Listener{service: service, store: store, logger: logger, retryDelay: defaultRetryDelay}, nil
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
		l.watchStream(ctx, "user", l.service.SourceWatchUsers, l.applyUser)
	}()
	go func() {
		defer group.Done()
		l.watchStream(ctx, "endpoint", l.service.SourceWatchEndpoints, l.applyEndpoint)
	}()
	group.Wait()
}

type openStream func(context.Context) (notificationprofile.ChangeStream, error)

type applyChange func(context.Context, notificationprofile.Change) error

func (l *Listener) watchStream(ctx context.Context, name string, open openStream, apply applyChange) {
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx, open, apply); err != nil {
			l.logger.Error("notification profile watch failed", "stream", name, "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(l.retryDelay):
			}
		}
	}
}

// watchOnce establishes a stream and applies changes until it fails. Firestore
// replays a full snapshot on each new stream, which is what rebuilds a lost
// projection.
func (l *Listener) watchOnce(ctx context.Context, open openStream, apply applyChange) error {
	stream, err := open(ctx)
	if err != nil {
		return err
	}
	defer stream.Stop()
	for {
		changes, err := stream.Next()
		if err != nil {
			if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}
		for _, change := range changes {
			if err := apply(ctx, change); err != nil {
				l.logger.Error("apply notification profile change failed",
					"doc_id", change.Doc.ID, "user_id", change.Doc.UserID, "err", err)
			}
		}
	}
}

func (l *Listener) applyUser(ctx context.Context, change notificationprofile.Change) error {
	if change.Kind == notificationprofile.ChangeRemoved {
		return l.store.DeletePreferences(ctx, change.Doc.ID)
	}
	preferences, err := notificationprofile.PreferencesFromDocument(change.Doc)
	if err != nil {
		return err
	}
	return l.store.PutPreferences(ctx, preferences)
}

func (l *Listener) applyEndpoint(ctx context.Context, change notificationprofile.Change) error {
	if change.Kind == notificationprofile.ChangeRemoved {
		return l.store.DeleteEndpoint(ctx, change.Doc.UserID, change.Doc.ID)
	}
	endpoint, err := notificationprofile.EndpointFromDocument(change.Doc)
	if err != nil {
		return err
	}
	return l.store.PutEndpoint(ctx, endpoint)
}
