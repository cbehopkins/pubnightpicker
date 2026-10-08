// Package testemail observes users/{uid}.testEmailReq and emits TestEmailRequested Truths.
package testemail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/truths"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ListenerTestEmailRequested = "TestEmailRequested"

	usersCollection = "users"
	RequestField    = "testEmailReq"
	AckField        = "testEmailAck"
	watchRetryDelay = 5 * time.Second
)

type Document struct {
	ID   string
	Data map[string]any
}

type ChangeStream interface {
	Next() ([]Document, error)
	Stop()
}

// Source streams added and modified users that have a test email request, and
// records the acknowledgement once the request has been handled.
type Source interface {
	Watch(context.Context) (ChangeStream, error)
	AckTestEmail(ctx context.Context, userID, requestID string) error
}

type Listener struct {
	source Source
	store  cellar.Store
	logger *slog.Logger
	lifecycle.Controller
}

func New(source Source, store cellar.Store, logger *slog.Logger) (*Listener, error) {
	if source == nil {
		return nil, fmt.Errorf("test email source is required")
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
			l.logger.Error("test email watch failed", "err", err)
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
			if request, ok := Request(document); ok {
				l.createTruth(request)
			}
		}
	}
}

// Request reports the pending test email request for a user document, if any.
func Request(document Document) (truths.TestEmailRequested, bool) {
	requestID, _ := document.Data[RequestField].(string)
	if requestID == "" {
		return truths.TestEmailRequested{}, false
	}
	if ack, _ := document.Data[AckField].(string); ack == requestID {
		return truths.TestEmailRequested{}, false
	}
	return truths.TestEmailRequested{
		UserID:    document.ID,
		RequestID: requestID,
		Email:     resolveEmail(document.Data),
	}, true
}

func resolveEmail(data map[string]any) string {
	for _, field := range []string{"notificationEmail", "email"} {
		if value, _ := data[field].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (l *Listener) createTruth(request truths.TestEmailRequested) {
	envelope, err := truths.NewEnvelope(truths.TestEmailRequestedFanout, request)
	if err != nil {
		l.logger.Error("marshal test email truth", "user_id", request.UserID, "err", err)
		return
	}
	cell, err := firebaseidempotency.NewCellRequest(ListenerTestEmailRequested, request.UserID+":"+request.RequestID, envelope)
	if err != nil {
		l.logger.Error("build test email idempotency cell", "user_id", request.UserID, "err", err)
		return
	}
	if _, err := l.store.Add([]cellar.CellRequest{cell}); err != nil && !errors.Is(err, cellar.ErrCellAlreadyExists) {
		l.logger.Error("create test email truth cell", "user_id", request.UserID, "err", err)
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
	query := s.client.Collection(usersCollection).Where(RequestField, "!=", "")
	return &firestoreStream{iterator: query.Snapshots(ctx)}, nil
}

func (s *FirestoreSource) AckTestEmail(ctx context.Context, userID, requestID string) error {
	_, err := s.client.Collection(usersCollection).Doc(userID).Set(ctx, map[string]any{AckField: requestID}, firestore.MergeAll)
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
