// Package notificationmirror mirrors notification requests into acknowledgements.
package notificationmirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"last_orders/internal/lastorders/database/listeners/lifecycle"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	requestCollection = "notification_req"
	ackCollection     = "notification_ack"
	pushTestDocument  = "push_test"
	watchRetryDelay   = 5 * time.Second
)

type Document struct {
	ID   string
	Data map[string]any
}

type ChangeStream interface {
	Next() ([]Document, error)
	Stop()
}

// Source watches requests and mirrors one request document into its ACK.
type Source interface {
	Watch(context.Context) (ChangeStream, error)
	Mirror(context.Context, Document) error
}

type Listener struct {
	source Source
	logger *slog.Logger
	lifecycle.Controller
}

type Config struct {
	Source Source
	Logger *slog.Logger
}

func New(cfg Config) (*Listener, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("notification mirror source is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Listener{source: cfg.Source, logger: cfg.Logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
}

func (l *Listener) watch(ctx context.Context) {
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx); err != nil {
			l.logger.Error("notification mirror watch failed", "err", err)
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
		documents, err := stream.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) || errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}
		for _, document := range documents {
			if document.ID == pushTestDocument {
				continue
			}
			if err := l.source.Mirror(ctx, document); err != nil {
				return fmt.Errorf("mirror notification %s: %w", document.ID, err)
			}
		}
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

func (s *FirestoreSource) Watch(ctx context.Context) (ChangeStream, error) {
	return &firestoreStream{iterator: s.client.Collection(requestCollection).Snapshots(ctx)}, nil
}

func (s *FirestoreSource) Mirror(ctx context.Context, request Document) error {
	ackDocument := s.client.Collection(ackCollection).Doc(request.ID)
	ackSnapshot, err := ackDocument.Get(ctx)
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	ackPayload := map[string]any{}
	if err == nil {
		if data := ackSnapshot.Data(); data != nil {
			ackPayload = data
		}
	}
	patch := make(map[string]any)
	for key, value := range request.Data {
		if existing, ok := ackPayload[key]; !ok || !reflect.DeepEqual(existing, value) {
			patch[key] = value
		}
	}
	if len(patch) == 0 {
		return nil
	}
	_, err = ackDocument.Set(ctx, patch, firestore.MergeAll)
	return err
}

type firestoreStream struct {
	iterator *firestore.QuerySnapshotIterator
}

func (s *firestoreStream) Next() ([]Document, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		return nil, err
	}
	documents := make([]Document, 0, len(snapshot.Changes))
	for _, change := range snapshot.Changes {
		if change.Kind == firestore.DocumentRemoved {
			continue
		}
		documents = append(documents, Document{ID: change.Doc.Ref.ID, Data: change.Doc.Data()})
	}
	return documents, nil
}

func (s *firestoreStream) Stop() {
	s.iterator.Stop()
}
