package newpolls

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/truths"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ListenerPollOpened = "PollOpened"

	pollCollection  = "polls"
	watchRetryDelay = 5 * time.Second
)

type Config struct {
	Source Source
	Store  cellar.Store
	Logger *slog.Logger
}

// Listener observes poll documents as they are added to Firestore.
type Listener struct {
	source Source
	store  cellar.Store
	logger *slog.Logger
	lifecycle.Controller
}

// New constructs a new newpolls Listener.
// FIXME - there are no tests for this...
func New(cfg Config) (*Listener, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("poll source is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("cellar store is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Listener{source: cfg.Source, store: cfg.Store, logger: cfg.Logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx); err != nil {
			l.logger.Error("new poll watch failed", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(watchRetryDelay):
			}
		}
	}
}

func (l *Listener) watchOnce(ctx context.Context) error {
	stream, err := l.source.Watch(ctx)
	if err != nil {
		return err
	}
	defer stream.Stop()

	for {
		changes, err := stream.Next()
		if err != nil {
			if errors.Is(err, ErrStreamDone) || errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}

		for _, change := range changes {
			if change.Kind != ChangeAdded {
				continue
			}
			l.createTruth(change.Doc.ID)
		}
	}
}

func (l *Listener) createTruth(pollID string) {
	envelope, err := truths.NewEnvelope(truths.PollOpenedFanout, truths.PollObservedPayload{PollID: pollID})
	if err != nil {
		l.logger.Error("marshal new poll payload", "poll_id", pollID, "err", err)
		return
	}

	request, err := firebaseidempotency.NewCellRequest(ListenerPollOpened, pollID, envelope)
	if err != nil {
		l.logger.Error("build new poll idempotency cell", "poll_id", pollID, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
		l.logger.Error("create new poll truth cell", "poll_id", pollID, "err", err)
	}
}
