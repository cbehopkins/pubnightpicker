// Package pushtest observes notification_req/push_test and emits PushTestRequested Truths.
package pushtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/pushsources"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/truths"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ListenerPushTestRequested = "PushTestRequested"

	requestCollection = "notification_req"
	watchRetryDelay   = 5 * time.Second
)

// SnapshotStream yields successive states of the push_test request document.
type SnapshotStream interface {
	Next() (map[string]any, error)
	Stop()
}

type Source interface {
	Watch(context.Context) (SnapshotStream, error)
}

type Listener struct {
	source Source
	store  cellar.Store
	logger *slog.Logger
	lifecycle.Controller
}

func New(source Source, store cellar.Store, logger *slog.Logger) (*Listener, error) {
	if source == nil {
		return nil, fmt.Errorf("push test source is required")
	}
	if store == nil {
		return nil, fmt.Errorf("cellar store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Listener{source: source, store: store, logger: logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx); err != nil {
			l.logger.Error("push test watch failed", "err", err)
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
		requests, err := stream.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) || errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}
		for userID, value := range requests {
			if userID != "" {
				l.createTruth(userID, value)
			}
		}
	}
}

func (l *Listener) createTruth(userID string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		l.logger.Warn("skip unencodable push test request", "user_id", userID, "err", err)
		return
	}
	envelope, err := truths.NewEnvelope(truths.PushTestRequestedFanout, truths.PushTestRequested{UserID: userID, Value: encoded})
	if err != nil {
		l.logger.Error("marshal push test truth", "user_id", userID, "err", err)
		return
	}
	request, err := firebaseidempotency.NewCellRequest(ListenerPushTestRequested, userID+":"+string(encoded), envelope)
	if err != nil {
		l.logger.Error("build push test idempotency cell", "user_id", userID, "err", err)
		return
	}
	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil && !errors.Is(err, cellar.ErrCellAlreadyExists) {
		l.logger.Error("create push test truth cell", "user_id", userID, "err", err)
	}
}

type FirestoreSource struct {
	client *firestore.Client
}

func NewFirestoreSource(client *firestore.Client) (*FirestoreSource, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreSource{client: client}, nil
}

func (s *FirestoreSource) Watch(ctx context.Context) (SnapshotStream, error) {
	return &firestoreStream{iterator: s.client.Collection(requestCollection).Doc(pushsources.PushTestDocument).Snapshots(ctx)}, nil
}

type firestoreStream struct {
	iterator *firestore.DocumentSnapshotIterator
}

func (s *firestoreStream) Next() (map[string]any, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		return nil, err
	}
	if !snapshot.Exists() {
		return map[string]any{}, nil
	}
	return snapshot.Data(), nil
}

func (s *firestoreStream) Stop() {
	s.iterator.Stop()
}
