// Package chatmessages observes chat messages and emits ChatMessagePosted Truths.
package chatmessages

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

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ListenerChatMessagePosted = "ChatMessagePosted"

	messagesCollection = "messages"
	watchRetryDelay    = 5 * time.Second
)

type Document struct {
	ID   string
	Data map[string]any
}

type ChangeStream interface {
	Next() ([]Document, error)
	Stop()
}

// Source streams added and modified message documents.
type Source interface {
	Watch(context.Context) (ChangeStream, error)
}

type Listener struct {
	source Source
	store  cellar.Store
	logger *slog.Logger
	lifecycle.Controller
}

func New(source Source, store cellar.Store, logger *slog.Logger) (*Listener, error) {
	if source == nil {
		return nil, fmt.Errorf("chat message source is required")
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
			l.logger.Error("chat message watch failed", "err", err)
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
			l.createTruth(Message(document))
		}
	}
}

// Message mirrors Python's chat_push field fallbacks.
func Message(document Document) truths.ChatMessagePosted {
	text, ok := document.Data["text"].(string)
	if !ok {
		text, _ = document.Data["message"].(string)
	}
	sender, _ := document.Data["displayName"].(string)
	if sender == "" {
		sender, _ = document.Data["name"].(string)
	}
	scopeType, _ := document.Data["scopeType"].(string)
	scopeID, _ := document.Data["scopeId"].(string)
	author, _ := document.Data["uid"].(string)
	return truths.ChatMessagePosted{
		MessageID:    document.ID,
		ScopeType:    scopeType,
		ScopeID:      scopeID,
		AuthorUserID: author,
		SenderName:   sender,
		Text:         text,
	}
}

func (l *Listener) createTruth(message truths.ChatMessagePosted) {
	envelope, err := truths.NewEnvelope(truths.ChatMessagePostedFanout, message)
	if err != nil {
		l.logger.Error("marshal chat message truth", "message_id", message.MessageID, "err", err)
		return
	}
	request, err := firebaseidempotency.NewCellRequest(ListenerChatMessagePosted, message.MessageID, envelope)
	if err != nil {
		l.logger.Error("build chat message idempotency cell", "message_id", message.MessageID, "err", err)
		return
	}
	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil && !errors.Is(err, cellar.ErrCellAlreadyExists) {
		l.logger.Error("create chat message truth cell", "message_id", message.MessageID, "err", err)
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
	return &firestoreStream{iterator: s.client.Collection(messagesCollection).Snapshots(ctx)}, nil
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
		// Python reacts to added and modified messages; processed history suppresses repeats.
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
