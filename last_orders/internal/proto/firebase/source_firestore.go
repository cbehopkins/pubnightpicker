package firebase

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/firestore"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type FirestoreSourceConfig struct {
	PollCollection string
	MinDate        string
	EmitInitial    bool
}

// FirestoreSource listens to poll-related firestore changes in read-only mode.
type FirestoreSource struct {
	client *firestore.Client
	cfg    FirestoreSourceConfig
	logger *slog.Logger
}

func NewFirestoreSource(client *firestore.Client, cfg FirestoreSourceConfig, logger *slog.Logger) *FirestoreSource {
	if cfg.PollCollection == "" {
		cfg.PollCollection = "polls"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FirestoreSource{client: client, cfg: cfg, logger: logger}
}

func (s *FirestoreSource) Run(ctxDone <-chan struct{}, out chan<- Event) error {
	if s.client == nil {
		return fmt.Errorf("firestore client is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-ctxDone
		cancel()
	}()

	base := s.client.Collection(s.cfg.PollCollection).Query
	if s.cfg.MinDate != "" {
		base = base.Where("date", ">=", s.cfg.MinDate)
	}

	openPollQuery := base.Where("completed", "==", false)
	completedPollQuery := base.Where("completed", "==", true)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return s.watchQuery(groupCtx, openPollQuery, out, EventPollCreated, EventPollModified, EventPollDeleted)
	})
	group.Go(func() error {
		return s.watchQuery(groupCtx, completedPollQuery, out, EventPollCompleted, EventPollCompleted, EventPollDeleted)
	})

	return group.Wait()
}

func (s *FirestoreSource) watchQuery(
	ctx context.Context,
	query firestore.Query,
	out chan<- Event,
	addedEventType string,
	modifiedEventType string,
	removedEventType string,
) error {
	iter := query.Snapshots(ctx)
	defer iter.Stop()

	firstSnapshot := true
	for {
		snapshot, err := iter.Next()
		if err != nil {
			if err == iterator.Done || status.Code(err) == codes.Canceled || status.Code(err) == codes.DeadlineExceeded || err == context.Canceled {
				return nil
			}
			return err
		}

		if firstSnapshot && !s.cfg.EmitInitial {
			firstSnapshot = false
			continue
		}
		firstSnapshot = false

		for _, change := range snapshot.Changes {
			eventType := ""
			switch change.Kind {
			case firestore.DocumentAdded:
				eventType = addedEventType
			case firestore.DocumentModified:
				eventType = modifiedEventType
			case firestore.DocumentRemoved:
				eventType = removedEventType
			default:
				continue
			}
			if eventType == "" {
				continue
			}

			raw := change.Doc.Data()
			event := Event{
				Type:        eventType,
				PollID:      change.Doc.Ref.ID,
				ObservedAt:  snapshot.ReadTime,
				DeliveryKey: fmt.Sprintf("%s:%s:%d", eventType, change.Doc.Ref.ID, snapshot.ReadTime.UnixNano()),
				Raw:         raw,
			}

			s.logger.Info("firebase event observed",
				"event", event.Type,
				"poll_id", event.PollID,
				"delivery_key", event.DeliveryKey,
			)

			select {
			case <-ctx.Done():
				return nil
			case out <- event:
			}
		}
	}
}
