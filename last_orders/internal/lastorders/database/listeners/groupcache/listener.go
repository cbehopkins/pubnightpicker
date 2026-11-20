package groupcache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	component "last_orders/internal/lastorders/components/groupcache"
	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/database/listeners/projectionwatch"
)

const retryDelay = 5 * time.Second

var ErrNoGroups = errors.New("authoritative Groups collection is empty")
var ErrMalformedDocument = errors.New("malformed Group document")

type Listener struct {
	service       *component.Service
	store         *component.Store
	logger        *slog.Logger
	hub           *sse.Hub
	authorization AuthorizationInvalidator
	ready         chan struct{}
	failed        chan error
	readyOnce     sync.Once
	lifecycle.Controller
}

type AuthorizationInvalidator interface {
	InvalidateGroup(string)
	InvalidateAllGroups()
}

func New(service *component.Service, store *component.Store, logger *slog.Logger, hubs ...*sse.Hub) (*Listener, error) {
	if service == nil {
		return nil, errors.New("group cache service is required")
	}
	if store == nil {
		return nil, errors.New("group cache store is required")
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

func (l *Listener) Ready() <-chan struct{} {
	return l.ready
}

func (l *Listener) Errors() <-chan error {
	return l.failed
}

func (l *Listener) SetAuthorizationInvalidator(invalidator AuthorizationInvalidator) {
	l.authorization = invalidator
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	err := projectionwatch.Run(ctx, projectionwatch.Config[component.Change]{
		Name: "Group cache",
		Open: func(ctx context.Context) (projectionwatch.Stream[component.Change], error) {
			return l.service.Watch(ctx)
		},
		Apply:      projectionwatch.ApplyBatch[component.Change](l.applyBatch),
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

func (l *Listener) applyBatch(ctx context.Context, changes []component.Change, baseline bool) error {
	if baseline && l.authorization != nil {
		l.authorization.InvalidateAllGroups()
	}
	mutations := make([]component.Mutation, 0, len(changes))
	changedIDs := make([]component.GroupID, 0, len(changes))
	for _, change := range changes {
		changedIDs = append(changedIDs, component.GroupID(change.Doc.ID))
		mutation := component.Mutation{ID: component.GroupID(change.Doc.ID)}
		if change.Kind != component.ChangeRemoved {
			group, err := component.GroupFromDocument(change.Doc)
			if err != nil {
				if baseline {
					return fmt.Errorf("%w: %w", ErrMalformedDocument, err)
				}
				l.logger.Warn("skip malformed Group cache document", "group_id", change.Doc.ID, "err", err)
				continue
			}
			mutation.Group = &group
		}
		mutations = append(mutations, mutation)
	}
	if l.authorization != nil {
		if !baseline {
			for _, groupID := range changedIDs {
				l.authorization.InvalidateGroup(string(groupID))
			}
		}
	}
	if baseline && len(mutations) == 0 {
		return ErrNoGroups
	}
	if err := l.store.ApplyBatch(ctx, mutations, baseline); err != nil {
		if baseline {
			return fmt.Errorf("validate and persist Group baseline: %w", err)
		}
		return fmt.Errorf("persist Group projection changes: %w", err)
	}
	if !baseline && l.hub != nil {
		for _, mutation := range mutations {
			l.hub.InvalidateScope("current-meets-group:" + string(mutation.ID))
			l.hub.InvalidateScope("open-meets-group:" + string(mutation.ID))
		}
	}
	return nil
}
