package groupcache

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

type firestoreSource struct {
	client *firestore.Client
}

func NewFirestoreSource(client *firestore.Client) (Source, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &firestoreSource{client: client}, nil
}

func (s *firestoreSource) Watch(ctx context.Context) (ChangeStream, error) {
	return &firestoreChangeStream{iterator: s.client.Collection("groups").Snapshots(ctx)}, nil
}

type firestoreChangeStream struct {
	iterator *firestore.QuerySnapshotIterator
}

func (s *firestoreChangeStream) Next() ([]Change, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		return nil, err
	}
	changes := make([]Change, 0, len(snapshot.Changes))
	for _, change := range snapshot.Changes {
		kind, ok := firestoreChangeKind(change.Kind)
		if !ok {
			continue
		}
		changes = append(changes, Change{Kind: kind, Doc: Document{ID: change.Doc.Ref.ID, Data: change.Doc.Data()}})
	}
	return changes, nil
}

func (s *firestoreChangeStream) Stop() {
	s.iterator.Stop()
}

func firestoreChangeKind(kind firestore.DocumentChangeKind) (ChangeKind, bool) {
	switch kind {
	case firestore.DocumentAdded:
		return ChangeAdded, true
	case firestore.DocumentModified:
		return ChangeModified, true
	case firestore.DocumentRemoved:
		return ChangeRemoved, true
	default:
		return 0, false
	}
}
