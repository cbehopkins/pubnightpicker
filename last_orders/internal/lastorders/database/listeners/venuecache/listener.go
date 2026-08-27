package venuecache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"last_orders/internal/lastorders/components/venuecache"
)

const retryDelay = 5 * time.Second

type Listener struct {
	service *venuecache.Service
	store   *venuecache.Store
	logger  *slog.Logger
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
	return &Listener{service: service, store: store, logger: logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	go l.watch(ctx)
	return nil
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
				l.logger.Error("apply venue cache change failed", "venue_id", change.Doc.ID, "err", err)
			}
		}
	}
}

func (l *Listener) apply(ctx context.Context, change venuecache.Change) error {
	if change.Kind == venuecache.ChangeRemoved {
		return l.store.Delete(ctx, change.Doc.ID)
	}
	projection, err := venuecache.ProjectionFromDocument(change.Doc)
	if err != nil {
		return err
	}
	return l.store.Put(ctx, projection)
}
