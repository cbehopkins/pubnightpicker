package eventvenues

import (
	"context"
	"fmt"

	"last_orders/internal/lastorders/components/recurrence"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

const pubCollection = "pubs"

// ErrStreamDone is returned by ChangeStream.Next when the stream ends cleanly.
var ErrStreamDone = iterator.Done

type ChangeKind int

const (
	ChangeAdded ChangeKind = iota
	ChangeModified
	ChangeRemoved
)

type Change struct {
	Kind  ChangeKind
	Venue recurrence.EventVenue
}

type ChangeStream interface {
	Next() ([]Change, error)
	Stop()
}

// Source supplies event venues, both as a live stream and as a point-in-time list
// for the periodic re-evaluation Timer.
type Source interface {
	Watch(context.Context) (ChangeStream, error)
	ListEventVenues(context.Context) ([]recurrence.EventVenue, error)
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

func (s *FirestoreSource) eventVenueQuery() firestore.Query {
	return s.client.Collection(pubCollection).Where("venueType", "==", "event")
}

func (s *FirestoreSource) Watch(ctx context.Context) (ChangeStream, error) {
	return &firestoreChangeStream{iterator: s.eventVenueQuery().Snapshots(ctx)}, nil
}

func (s *FirestoreSource) ListEventVenues(ctx context.Context) ([]recurrence.EventVenue, error) {
	iter := s.eventVenueQuery().Documents(ctx)
	defer iter.Stop()

	venues := make([]recurrence.EventVenue, 0, 32)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return venues, nil
		}
		if err != nil {
			return nil, err
		}
		venues = append(venues, recurrence.EventVenueFrom(doc))
	}
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
		changes = append(changes, Change{Kind: kind, Venue: recurrence.EventVenueFrom(change.Doc)})
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
