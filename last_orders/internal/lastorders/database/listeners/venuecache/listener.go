package venuecache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/database/listeners/projectionwatch"
)

const retryDelay = 5 * time.Second

var ErrMalformedDocument = errors.New("malformed venue cache document")

var errPersistence = errors.New("persist venue cache projection")

type Listener struct {
	service   *venuecache.Service
	store     *venuecache.Store
	hub       *sse.Hub
	logger    *slog.Logger
	ready     chan struct{}
	failed    chan error
	readyOnce sync.Once
	lifecycle.Controller
}

func New(service *venuecache.Service, store *venuecache.Store, logger *slog.Logger, hubs ...*sse.Hub) (*Listener, error) {
	if service == nil {
		return nil, errors.New("venue cache service is required")
	}
	if store == nil {
		return nil, errors.New("venue cache store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	listener := &Listener{service: service, store: store, logger: logger, ready: make(chan struct{}), failed: make(chan error, 1)}
	if len(hubs) > 0 {
		listener.hub = hubs[0]
	}
	return listener, nil
}

// Ready closes once the first full venue snapshot has been applied.
func (l *Listener) Ready() <-chan struct{} {
	return l.ready
}

func (l *Listener) Errors() <-chan error {
	return l.failed
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	err := projectionwatch.Run(ctx, projectionwatch.Config[venuecache.Change]{
		Name: "venue cache",
		Open: func(ctx context.Context) (projectionwatch.Stream[venuecache.Change], error) {
			return l.service.SourceWatch(ctx)
		},
		Apply:      projectionwatch.ApplyBatch[venuecache.Change](l.applyBatch),
		RetryDelay: retryDelay,
		Logger:     l.logger,
		BaselineApplied: func() {
			l.readyOnce.Do(func() { close(l.ready) })
		},
	})
	if err != nil {
		l.failed <- err
	}
}

func (l *Listener) applyBatch(ctx context.Context, changes []venuecache.Change, baseline bool) error {
	mutations := make([]venuecache.Mutation, 0, len(changes))
	for _, change := range changes {
		mutation := venuecache.Mutation{ID: change.Doc.ID}
		if change.Kind != venuecache.ChangeRemoved {
			projection, err := venuecache.ProjectionFromDocument(change.Doc)
			if err != nil {
				if baseline {
					return fmt.Errorf("malformed venue %q in baseline: %w", change.Doc.ID, ErrMalformedDocument)
				}
				l.logger.Warn("skip malformed venue cache document", "venue_id", change.Doc.ID, "err", err)
				continue
			}
			if projection.DeletedOn != "" {
				mutations = append(mutations, mutation)
				continue
			}
			mutation.Projection = &projection
		}
		mutations = append(mutations, mutation)
	}
	if err := l.store.ApplyBatch(ctx, mutations, baseline); err != nil {
		return errors.Join(errPersistence, err)
	}
	if l.hub != nil && len(changes) > 0 {
		snapshot, err := l.service.ListSnapshot(ctx)
		if err != nil {
			return errors.Join(errPersistence, err)
		}
		l.hub.PublishChanged("venues:all", snapshot.State.Sequence)
	}
	return nil
}

func (l *Listener) apply(ctx context.Context, change venuecache.Change) error {
	return l.applyBatch(ctx, []venuecache.Change{change}, false)
}
