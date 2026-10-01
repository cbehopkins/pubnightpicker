package venuecache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/database/listeners/lifecycle"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const retryDelay = 5 * time.Second

var ErrMalformedDocument = errors.New("malformed venue cache document")

type Listener struct {
	service   *venuecache.Service
	store     *venuecache.Store
	logger    *slog.Logger
	ready     chan struct{}
	readyOnce sync.Once
	lifecycle.Controller
}

func New(service *venuecache.Service, store *venuecache.Store, logger *slog.Logger) (*Listener, error) {
	if service == nil {
		return nil, errors.New("venue cache service is required")
	}
	if store == nil {
		return nil, errors.New("venue cache store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Listener{service: service, store: store, logger: logger, ready: make(chan struct{})}, nil
}

// Ready closes once the first full venue snapshot has been applied.
func (l *Listener) Ready() <-chan struct{} {
	return l.ready
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx); err != nil {
			l.logger.Error("venue cache watch failed", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(retryDelay):
			}
		}
	}
}

func (l *Listener) watchOnce(ctx context.Context) error {
	stream, err := l.service.SourceWatch(ctx)
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
			if err := l.apply(ctx, change); err != nil {
				if errors.Is(err, ErrMalformedDocument) {
					l.logger.Warn("skip malformed venue cache document", "venue_id", change.Doc.ID, "err", err)
					continue
				}
				return err
			}
		}
		l.readyOnce.Do(func() { close(l.ready) })
	}
}

func (l *Listener) apply(ctx context.Context, change venuecache.Change) error {
	if change.Kind == venuecache.ChangeRemoved {
		return l.store.Delete(ctx, change.Doc.ID)
	}
	projection, err := venuecache.ProjectionFromDocument(change.Doc)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedDocument, err)
	}
	return l.store.Put(ctx, projection)
}
