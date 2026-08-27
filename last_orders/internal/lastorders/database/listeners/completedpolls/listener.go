package completedpolls

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/plugins/polls"

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

type Listener struct {
	client *firestore.Client
	store  cellar.Store
	logger *slog.Logger
	lifecycle.Controller
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
	return l.Controller.Start(ctx, l.watch)
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
	targetPayload, err := cellar.JSONCodec[polls.PollObservedPayload]().Marshal(polls.PollObservedPayload{
		PollID:                 pollID,
		ChangeKind:             kind,
		SelectedRestaurantID:   selectedRestaurantID,
		SelectedRestaurantTime: selectedRestaurantTime,
	})
	if err != nil {
		l.logger.Error("marshal completed poll payload", "poll_id", pollID, "err", err)
		return
	}

	request, err := firebaseidempotency.NewCellRequest(
		ListenerCompletedPoll,
		completedEventKey(pollID, selectedVenueID, selectedRestaurantID, selectedRestaurantTime),
		facts.Fact{Name: polls.FactCompletedPoll, Payload: targetPayload},
	)
	if err != nil {
		l.logger.Error("build completed poll idempotency cell", "poll_id", pollID, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
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
