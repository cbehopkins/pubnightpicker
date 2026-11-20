package roleauthorization

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"last_orders/internal/lastorders/components/sse"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/database/listeners/projectionwatch"
)

const retryDelay = 5 * time.Second

type Stream interface {
	Next() ([]Change, error)
	Stop()
}

type Change struct {
	UIDs    []string
	Unknown bool
}

type Source interface {
	Watch(context.Context) (Stream, error)
}

type Listener struct {
	source     Source
	invalidate func()
	hub        *sse.Hub
	logger     *slog.Logger
	retryDelay time.Duration
	ready      chan struct{}
	failed     chan error
	readyOnce  sync.Once
	lifecycle.Controller
}

func New(source Source, invalidate func(), logger *slog.Logger, hubs ...*sse.Hub) (*Listener, error) {
	if source == nil {
		return nil, errors.New("role authorisation source is required")
	}
	if invalidate == nil {
		return nil, errors.New("role authorisation invalidator is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	listener := &Listener{
		source: source, invalidate: invalidate, logger: logger,
		retryDelay: retryDelay, ready: make(chan struct{}), failed: make(chan error, 1),
	}
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

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	err := projectionwatch.Run(ctx, projectionwatch.Config[Change]{
		Name: "Global administrator authorisation",
		Open: func(ctx context.Context) (projectionwatch.Stream[Change], error) {
			return l.source.Watch(ctx)
		},
		Apply: func(_ context.Context, changes []Change, baseline bool) error {
			if baseline {
				l.invalidate()
				if l.hub != nil {
					l.hub.InvalidateAllAuthenticated()
				}
				return nil
			}
			for _, change := range changes {
				l.invalidate()
				if l.hub == nil {
					continue
				}
				if change.Unknown {
					l.hub.InvalidateAllAuthenticated()
					continue
				}
				for _, uid := range change.UIDs {
					l.hub.InvalidateUID(uid)
				}
			}
			return nil
		},
		RetryDelay: l.retryDelay,
		Logger:     l.logger,
		BaselineApplied: func() {
			l.readyOnce.Do(func() { close(l.ready) })
		},
	})
	if err != nil {
		l.failed <- err
	}
}
