package votecache

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

const collection = "votes"

type firestoreSource struct {
	client *firestore.Client
}

func NewFirestoreSource(client *firestore.Client) (Source, error) {
	if client == nil {
		return nil, fmt.Errorf("Firestore client is required")
	}
	return &firestoreSource{client: client}, nil
}

func (s *firestoreSource) Watch(ctx context.Context, meetID string) (Stream, error) {
	if meetID == "" {
		return nil, fmt.Errorf("Meet ID is required")
	}
	iterator := s.client.Collection(collection).Doc(meetID).Snapshots(ctx)
	return &firestoreStream{iterator: iterator}, nil
}

type firestoreStream struct {
	iterator *firestore.DocumentSnapshotIterator
}

func (s *firestoreStream) Next() (Document, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		return Document{}, err
	}
	if !snapshot.Exists() {
		return Document{ID: snapshot.Ref.ID}, nil
	}
	return Document{ID: snapshot.Ref.ID, Exists: true, Data: snapshot.Data(), Version: snapshot.UpdateTime}, nil
}

func (s *firestoreStream) Stop() {
	s.iterator.Stop()
}
