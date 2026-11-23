package roleauthorization

import (
	"context"
	"fmt"
	"slices"

	"cloud.google.com/go/firestore"
)

type FirestoreSource struct {
	client *firestore.Client
}

func NewFirestoreSource(client *firestore.Client) (*FirestoreSource, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreSource{client: client}, nil
}

func (s *FirestoreSource) Watch(ctx context.Context) (Stream, error) {
	return &firestoreStream{iterator: s.client.Collection("roles").Doc("admin").Snapshots(ctx)}, nil
}

type firestoreStream struct {
	iterator *firestore.DocumentSnapshotIterator
	previous map[string]bool
	started  bool
}

func (s *firestoreStream) Next() ([]Change, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		return nil, err
	}
	current := make(map[string]bool)
	if snapshot.Exists() {
		for uid, value := range snapshot.Data() {
			allowed, ok := value.(bool)
			current[uid] = ok && allowed
		}
	}
	if !s.started {
		s.previous = current
		s.started = true
		return []Change{{Unknown: true}}, nil
	}
	changed := make(map[string]struct{})
	for uid, allowed := range current {
		if s.previous[uid] != allowed {
			changed[uid] = struct{}{}
		}
	}
	for uid, allowed := range s.previous {
		if allowed && !current[uid] {
			changed[uid] = struct{}{}
		}
	}
	s.previous = current
	uids := make([]string, 0, len(changed))
	for uid := range changed {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	return []Change{{UIDs: uids}}, nil
}

func (s *firestoreStream) Stop() {
	s.iterator.Stop()
}
