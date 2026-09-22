package completedpolls

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
	ListenerPollCompleted = "PollCompleted"

	pollCollection  = "polls"
	watchRetryDelay = 5 * time.Second
)

type Config struct {
	Source Source
	Store  cellar.Store
	Logger *slog.Logger
}

type Listener struct {
	source Source
	store  cellar.Store
	logger *slog.Logger
	lifecycle.Controller
}

func New(cfg Config) (*Listener, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("completed poll source is required")
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
			l.logger.Error("completed poll watch failed", "err", err)
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
			if change.Kind != ChangeAdded && change.Kind != ChangeModified {
				continue
			}
			// FIXME - if we use the schema we might be able to get this type safe earlier
			selectedVenueID, _ := change.Doc.Data["selected"].(string)
			selectedRestaurantID, _ := change.Doc.Data["restaurant_id"].(string)
			selectedRestaurantTime, _ := change.Doc.Data["restaurant_time"].(string)
			l.createTruth(change.Doc.ID, selectedVenueID, changeKind(change.Kind), selectedRestaurantID, selectedRestaurantTime)
		}
	}
}

type eventIdentity struct {
	PollID          string `json:"poll_id"`
	SelectedVenueID string `json:"selected_venue_id"`
}

func (l *Listener) createTruth(pollID, selectedVenueID, kind, selectedRestaurantID, selectedRestaurantTime string) {
	envelope, err := truths.NewEnvelope(truths.PollCompletedFanout, truths.PollObservedPayload{
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
		ListenerPollCompleted,
		completedEventKey(pollID, selectedVenueID, selectedRestaurantID, selectedRestaurantTime),
		envelope,
	)
	if err != nil {
		l.logger.Error("build completed poll idempotency cell", "poll_id", pollID, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
		l.logger.Error("create completed poll truth cell", "poll_id", pollID, "err", err)
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

func changeKind(kind ChangeKind) string {
	switch kind {
	case ChangeAdded:
		return "added"
	case ChangeModified:
		return "modified"
	default:
		return "unknown"
	}
}
