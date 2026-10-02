package notificationprofile

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

type ChangeKind int

const (
	ChangeAdded ChangeKind = iota
	ChangeModified
	ChangeRemoved
)

type Change struct {
	Kind ChangeKind
	Doc  Document
}

type ChangeStream interface {
	Next() ([]Change, error)
	Stop()
}

// Source is the authoritative Firebase data behind the projection.
type Source interface {
	WatchUsers(context.Context) (ChangeStream, error)
	WatchEndpoints(context.Context) (ChangeStream, error)
	DeactivateEndpoint(ctx context.Context, userID, endpointID string) error
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

func (s *FirestoreSource) WatchUsers(ctx context.Context) (ChangeStream, error) {
	iter := s.client.Collection(usersCollection).Snapshots(ctx)
	return &firestoreChangeStream{iterator: iter, withParent: false}, nil
}

func (s *FirestoreSource) WatchEndpoints(ctx context.Context) (ChangeStream, error) {
	iter := s.client.CollectionGroup(endpointsCollection).Snapshots(ctx)
	return &firestoreChangeStream{iterator: iter, withParent: true}, nil
}

func (s *FirestoreSource) DeactivateEndpoint(ctx context.Context, userID, endpointID string) error {
	if userID == "" || endpointID == "" {
		return fmt.Errorf("user ID and endpoint ID are required")
	}
	_, err := s.client.Collection(usersCollection).Doc(userID).
		Collection(endpointsCollection).Doc(endpointID).
		Update(ctx, []firestore.Update{
			{Path: "active", Value: false},
			{Path: "disabledAt", Value: firestore.ServerTimestamp},
			{Path: "lastSeenAt", Value: firestore.ServerTimestamp},
		})
	return err
}

type firestoreChangeStream struct {
	iterator   *firestore.QuerySnapshotIterator
	withParent bool
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
		doc := Document{ID: change.Doc.Ref.ID, Data: change.Doc.Data()}
		if s.withParent {
			if change.Doc.Ref.Parent == nil || change.Doc.Ref.Parent.Parent == nil {
				continue
			}
			doc.UserID = change.Doc.Ref.Parent.Parent.ID
		}
		changes = append(changes, Change{Kind: kind, Doc: doc})
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
