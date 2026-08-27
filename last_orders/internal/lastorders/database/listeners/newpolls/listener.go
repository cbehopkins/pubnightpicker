package newpolls

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/plugins/polls"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ListenerNewPoll = "NewPoll"

	pollCollection  = "polls"
	watchRetryDelay = 5 * time.Second
)

type Config struct {
	Client *firestore.Client
	Store  cellar.Store
	Logger *slog.Logger
}

// Listener observes poll documents as they are added to Firestore.
type Listener struct {
	client *firestore.Client
	store  cellar.Store
	logger *slog.Logger
}

func New(cfg Config) (*Listener, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("cellar store is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Listener{client: cfg.Client, store: cfg.Store, logger: cfg.Logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	go l.watch(ctx)
	return nil
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
	iter := l.client.Collection(pollCollection).Snapshots(ctx)
	defer iter.Stop()

	for {
		snapshot, err := iter.Next()
		if err != nil {
			if err == iterator.Done || err == context.Canceled || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}

		for _, change := range snapshot.Changes {
			if change.Kind != firestore.DocumentAdded {
				continue
			}
			l.createFact(change.Doc.Ref.ID)
		}
	}
}

func (l *Listener) createFact(pollID string) {
	targetPayload, err := cellar.JSONCodec[polls.PollObservedPayload]().Marshal(polls.PollObservedPayload{PollID: pollID})
	if err != nil {
		l.logger.Error("marshal new poll payload", "poll_id", pollID, "err", err)
		return
	}

	request, err := firebaseidempotency.NewCellRequest(ListenerNewPoll, pollID, facts.Fact{Name: polls.FactNewPoll, Payload: targetPayload})
	if err != nil {
		l.logger.Error("build new poll idempotency cell", "poll_id", pollID, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
		l.logger.Error("create new poll fact cell", "poll_id", pollID, "err", err)
	}
}
