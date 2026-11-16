package venuecache

import (
	"context"
	"fmt"
	"sort"
	"time"

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
	ID         string
	Data       map[string]any
	UpdateTime time.Time
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
	CanManage(context.Context, string) (bool, error)
}

type ConditionalWriter interface {
	PatchWithBaseline(context.Context, string, []FieldUpdate) (Document, []string, error)
}

type ConditionalDeleteWriter interface {
	SoftDeleteIfUnchanged(context.Context, string, time.Time) (Document, error)
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
	return Document{ID: doc.Ref.ID, Data: doc.Data(), UpdateTime: doc.UpdateTime}, nil
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
	return Document{ID: doc.Ref.ID, Data: doc.Data(), UpdateTime: doc.UpdateTime}, nil
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
	return Document{ID: doc.Ref.ID, Data: doc.Data(), UpdateTime: doc.UpdateTime}, nil
}

func (s *FirestoreSource) PatchWithBaseline(ctx context.Context, venueID string, fields []FieldUpdate) (Document, []string, error) {
	if len(fields) == 0 {
		return Document{}, nil, fmt.Errorf("venue patch is empty")
	}
	ref := s.client.Collection(collection).Doc(venueID)
	var result Document
	var conflicts []string
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return ErrNotFound
			}
			return err
		}
		data := snapshot.Data()
		projection, err := ProjectionFromDocument(Document{ID: snapshot.Ref.ID, Data: data})
		if err != nil {
			return err
		}
		currentValues := CanonicalAPIValues(projection)
		conflicts = nil
		for _, field := range fields {
			currentDigest, err := CanonicalValueDigest(currentValues[field.APIField])
			if err != nil {
				return err
			}
			submittedDigest, err := CanonicalValueDigest(field.CanonicalValue)
			if err != nil {
				return err
			}
			if currentDigest != field.BaselineDigest && currentDigest != submittedDigest {
				conflicts = append(conflicts, field.APIField)
			}
		}
		sort.Strings(conflicts)
		if len(conflicts) > 0 {
			result = Document{ID: snapshot.Ref.ID, Data: data, UpdateTime: snapshot.UpdateTime}
			return nil
		}
		updates := make([]firestore.Update, 0, len(fields))
		resultData := make(map[string]any, len(data)+len(fields))
		for key, value := range data {
			resultData[key] = value
		}
		for _, field := range fields {
			currentDigest, err := CanonicalValueDigest(currentValues[field.APIField])
			if err != nil {
				return err
			}
			submittedDigest, err := CanonicalValueDigest(field.CanonicalValue)
			if err != nil {
				return err
			}
			if currentDigest == submittedDigest {
				continue
			}
			updates = append(updates, firestore.Update{Path: field.FirestoreField, Value: field.Value})
			if field.Value == firestore.Delete {
				delete(resultData, field.FirestoreField)
			} else {
				resultData[field.FirestoreField] = field.Value
			}
		}
		result = Document{ID: snapshot.Ref.ID, Data: resultData, UpdateTime: snapshot.UpdateTime}
		if len(updates) == 0 {
			return nil
		}
		return tx.Update(ref, updates)
	})
	if status.Code(err) == codes.NotFound {
		return Document{}, nil, ErrNotFound
	}
	if err != nil {
		return Document{}, nil, err
	}
	if len(conflicts) > 0 {
		return result, conflicts, ErrFieldConflict
	}
	doc, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return Document{}, nil, ErrNotFound
		}
		return Document{}, nil, err
	}
	result = Document{ID: doc.Ref.ID, Data: doc.Data(), UpdateTime: doc.UpdateTime}
	return result, nil, nil
}

func (s *FirestoreSource) SoftDeleteIfUnchanged(ctx context.Context, venueID string, expectedUpdateTime time.Time) (Document, error) {
	ref := s.client.Collection(collection).Doc(venueID)
	var current Document
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return ErrNotFound
			}
			return err
		}
		current = Document{ID: snapshot.Ref.ID, Data: snapshot.Data(), UpdateTime: snapshot.UpdateTime}
		if !snapshot.UpdateTime.Equal(expectedUpdateTime) {
			return ErrVersionConflict
		}
		return tx.Update(ref, []firestore.Update{{Path: "deleted_on", Value: firestore.ServerTimestamp}})
	})
	if status.Code(err) == codes.NotFound {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return current, err
	}
	return Document{}, nil
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
		venues = append(venues, Document{ID: doc.Ref.ID, Data: doc.Data(), UpdateTime: doc.UpdateTime})
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
		changes = append(changes, Change{Kind: kind, Doc: Document{ID: change.Doc.Ref.ID, Data: change.Doc.Data(), UpdateTime: change.Doc.UpdateTime}})
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
