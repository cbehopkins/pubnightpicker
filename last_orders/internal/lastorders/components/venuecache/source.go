package venuecache

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ChangeKind int

const (
	ChangeAdded ChangeKind = iota
	ChangeModified
	ChangeRemoved
)

type Document struct {
	ID   string
	Data map[string]any
}

type Change struct {
	Kind ChangeKind
	Doc  Document
}

type ChangeStream interface {
	Next() ([]Change, error)
	Stop()
}

type Source interface {
	Get(context.Context, string) (Document, error)
	ListEventVenues(context.Context) ([]Document, error)
	Watch(context.Context) (ChangeStream, error)
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

func (s *FirestoreSource) Get(ctx context.Context, venueID string) (Document, error) {
	doc, err := s.client.Collection(collection).Doc(venueID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return Document{}, ErrNotFound
		}
		return Document{}, err
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, nil
}

func (s *FirestoreSource) ListEventVenues(ctx context.Context) ([]Document, error) {
	docs, err := s.client.Collection(collection).Where("venueType", "==", "event").Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	venues := make([]Document, 0, len(docs))
	for _, doc := range docs {
		venues = append(venues, Document{ID: doc.Ref.ID, Data: doc.Data()})
	}
	return venues, nil
}

func (s *FirestoreSource) Watch(ctx context.Context) (ChangeStream, error) {
	return &firestoreChangeStream{iterator: s.client.Collection(collection).Snapshots(ctx)}, nil
}

type firestoreChangeStream struct {
	iterator *firestore.QuerySnapshotIterator
}

func (s *firestoreChangeStream) Next() ([]Change, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		if err == iterator.Done {
			return nil, err
		}
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
