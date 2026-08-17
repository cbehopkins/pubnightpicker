package completedpolls

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/handlers"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ListenerCompletedPoll = "CompletedPoll"

	pollCollection  = "polls"
	watchRetryDelay = 5 * time.Second
)

type Config struct {
	Client *firestore.Client
	Store  cellar.Store
	Logger *slog.Logger
}

// Listener observes completed poll documents in Firestore.
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
			l.logger.Error("completed poll watch failed", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(watchRetryDelay):
			}
		}
	}
}

func (l *Listener) watchOnce(ctx context.Context) error {
	query := l.client.Collection(pollCollection).Where("completed", "==", true)
	iter := query.Snapshots(ctx)
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
			if change.Kind != firestore.DocumentAdded && change.Kind != firestore.DocumentModified {
				continue
			}
			// FIXME - if we use the schema we might be able to get this type safe earlier
			selectedVenueID, _ := change.Doc.Data()["selected"].(string)
			selectedRestaurantID, _ := change.Doc.Data()["restaurant_id"].(string)
			selectedRestaurantTime, _ := change.Doc.Data()["restaurant_time"].(string)
			l.createFact(change.Doc.Ref.ID, selectedVenueID, changeKind(change.Kind), selectedRestaurantID, selectedRestaurantTime)
		}
	}
}

type eventIdentity struct {
	PollID          string `json:"poll_id"`
	SelectedVenueID string `json:"selected_venue_id"`
}

func (l *Listener) createFact(pollID, selectedVenueID, kind, selectedRestaurantID, selectedRestaurantTime string) {
	targetPayload, err := cellar.JSONCodec[handlers.PollObservedPayload]().Marshal(handlers.PollObservedPayload{
		PollID:                 pollID,
		ChangeKind:             kind,
		SelectedRestaurantID:   selectedRestaurantID,
		SelectedRestaurantTime: selectedRestaurantTime,
	})
	if err != nil {
		l.logger.Error("marshal completed poll payload", "poll_id", pollID, "err", err)
		return
	}

	pendingPayload, err := cellar.JSONCodec[firebaseidempotency.PendingPayload]().Marshal(firebaseidempotency.PendingPayload{
		Listener: ListenerCompletedPoll,
		EventKey: completedEventKey(pollID, selectedVenueID, selectedRestaurantID, selectedRestaurantTime),
		Fanout: []firebaseidempotency.FanoutTarget{{
			HandlerName: handlers.HandlerCompletedPoll,
			Payload:     targetPayload,
		}},
	})
	if err != nil {
		l.logger.Error("marshal completed poll pending payload", "poll_id", pollID, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{{
		HandlerName: firebaseidempotency.HandlerPending,
		Payload:     pendingPayload,
	}}); err != nil {
		l.logger.Error("create completed poll fact cell", "poll_id", pollID, "err", err)
	}
}

func completedEventKey(pollID, selectedVenueID, selectedRestaurantID, selectedRestaurantTime string) string {
	var normalizedRestaurantID, normalizedRestaurantTime string
	if selectedRestaurantID != "" {
		normalizedRestaurantID = ":" + selectedRestaurantID
	} else {
		normalizedRestaurantID = ""
	}

	if selectedRestaurantTime != "" {
		normalizedRestaurantTime = ":" + selectedRestaurantTime
	} else {
		normalizedRestaurantTime = ""
	}

	return pollID + ":" + selectedVenueID + normalizedRestaurantID + normalizedRestaurantTime
}

func changeKind(kind firestore.DocumentChangeKind) string {
	switch kind {
	case firestore.DocumentAdded:
		return "added"
	case firestore.DocumentModified:
		return "modified"
	default:
		return "unknown"
	}
}
