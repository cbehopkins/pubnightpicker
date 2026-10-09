package admindelete

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	requestCollection = "admin_delete_requests"
	killSwitchPath    = "config/admin_delete"
)

type Document struct {
	ID   string
	Data map[string]any
}

type ChangeStream interface {
	Next() ([]Document, error)
	Stop()
}

type Source interface {
	Watch(context.Context) (ChangeStream, error)
	WatchPause(context.Context) (PauseStream, error)
	ListEligible(context.Context, bool) ([]Document, error)
	IsPaused(context.Context) (bool, error)
	RequestStatus(context.Context, string) (string, error)
}

type PauseStream interface {
	Next() error
	Stop()
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

func (s *FirestoreSource) WatchPause(ctx context.Context) (PauseStream, error) {
	return &pauseStream{iterator: s.client.Doc(killSwitchPath).Snapshots(ctx)}, nil
}

func (s *FirestoreSource) ListEligible(ctx context.Context, realDelete bool) ([]Document, error) {
	statuses := []string{"pending"}
	if realDelete {
		statuses = append(statuses, "dry_run_validated")
	}
	iter := s.client.Collection(requestCollection).Where("status", "in", statuses).Documents(ctx)
	defer iter.Stop()

	var documents []Document
	for {
		snapshot, err := iter.Next()
		if err == iterator.Done {
			return documents, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list eligible admin-delete requests: %w", err)
		}
		documents = append(documents, Document{ID: snapshot.Ref.ID, Data: snapshot.Data()})
	}
}

func (s *FirestoreSource) IsPaused(ctx context.Context) (bool, error) {
	snapshot, err := s.client.Doc(killSwitchPath).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read admin-delete kill switch: %w", err)
	}
	value, exists := snapshot.Data()["paused"]
	if !exists {
		return false, nil
	}
	paused, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s.paused must be a boolean", killSwitchPath)
	}
	return paused, nil
}

func (s *FirestoreSource) RequestStatus(ctx context.Context, requestID string) (string, error) {
	snapshot, err := s.client.Collection(requestCollection).Doc(requestID).Get(ctx)
	if err != nil {
		return "", fmt.Errorf("read admin-delete request %q: %w", requestID, err)
	}
	value, exists := snapshot.Data()["status"]
	if !exists {
		return "", fmt.Errorf("admin-delete request %q has no status", requestID)
	}
	requestStatus, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("admin-delete request %q status must be a string", requestID)
	}
	return requestStatus, nil
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

type pauseStream struct {
	iterator *firestore.DocumentSnapshotIterator
}

func (s *pauseStream) Next() error {
	_, err := s.iterator.Next()
	return err
}

func (s *pauseStream) Stop() {
	s.iterator.Stop()
}
