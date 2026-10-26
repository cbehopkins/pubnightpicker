package venuecache

import (
	"context"
	"fmt"
	"sort"

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

type Writer interface {
	Create(context.Context, map[string]any) (Document, error)
	Patch(context.Context, string, map[string]any) (Document, error)
	SoftDelete(context.Context, string) (Document, error)
	CanManage(context.Context, string) (bool, error)
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

func (s *FirestoreSource) Create(ctx context.Context, fields map[string]any) (Document, error) {
	ref := s.client.Collection(collection).NewDoc()
	if _, err := ref.Set(ctx, fields); err != nil {
		return Document{}, err
	}
	doc, err := ref.Get(ctx)
	if err != nil {
		return Document{}, err
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, nil
}

func (s *FirestoreSource) Patch(ctx context.Context, venueID string, fields map[string]any) (Document, error) {
	ref := s.client.Collection(collection).Doc(venueID)
	if len(fields) == 0 {
		return Document{}, fmt.Errorf("venue patch is empty")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	updates := make([]firestore.Update, 0, len(keys))
	for _, key := range keys {
		updates = append(updates, firestore.Update{Path: key, Value: fields[key]})
	}
	if _, err := ref.Update(ctx, updates); err != nil {
		if status.Code(err) == codes.NotFound {
			return Document{}, ErrNotFound
		}
		return Document{}, err
	}
	doc, err := ref.Get(ctx)
	if err != nil {
		return Document{}, err
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, nil
}

func (s *FirestoreSource) SoftDelete(ctx context.Context, venueID string) (Document, error) {
	ref := s.client.Collection(collection).Doc(venueID)
	if _, err := ref.Update(ctx, []firestore.Update{{Path: "deleted_on", Value: firestore.ServerTimestamp}}); err != nil {
		if status.Code(err) == codes.NotFound {
			return Document{}, ErrNotFound
		}
		return Document{}, err
	}
	doc, err := ref.Get(ctx)
	if err != nil {
		return Document{}, err
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, nil
}

func (s *FirestoreSource) CanManage(ctx context.Context, uid string) (bool, error) {
	if uid == "" {
		return false, nil
	}
	for _, role := range []string{"canManagePubs", "admin"} {
		doc, err := s.client.Collection("roles").Doc(role).Get(ctx)
		if status.Code(err) == codes.NotFound {
			continue
		}
		if err != nil {
			return false, err
		}
		if value, exists := doc.Data()[uid]; exists && value == true {
			return true, nil
		}
	}
	return false, nil
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
