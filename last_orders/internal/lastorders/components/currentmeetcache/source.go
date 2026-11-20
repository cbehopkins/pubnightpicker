package currentmeetcache

import (
	"context"
	"fmt"

	"last_orders/internal/lastorders/components/meetcache"

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

func (s *firestoreSource) Watch(ctx context.Context, groupID, date string) (Stream, error) {
	if groupID == "" || date == "" {
		return nil, fmt.Errorf("group ID and current date are required")
	}
	query := s.client.Collection("polls").
		Where("groupId", "==", groupID).
		Where("completed", "==", true).
		Where("date", ">=", date).
		OrderBy("date", firestore.Asc).
		OrderBy(firestore.DocumentID, firestore.Asc)
	return &firestoreStream{iterator: query.Snapshots(ctx)}, nil
}

type firestoreStream struct {
	iterator *firestore.QuerySnapshotIterator
}

func (s *firestoreStream) Next() (QuerySnapshot, error) {
	snapshot, err := s.iterator.Next()
	if err != nil {
		return QuerySnapshot{}, err
	}
	documents, err := snapshot.Documents.GetAll()
	if err != nil {
		return QuerySnapshot{}, err
	}
	result := QuerySnapshot{Documents: make([]meetcache.Document, 0, len(documents)), ReadTime: snapshot.ReadTime}
	for _, document := range documents {
		result.Documents = append(result.Documents, meetcache.Document{
			ID: document.Ref.ID, Exists: true, Data: document.Data(), Version: document.UpdateTime,
		})
	}
	return result, nil
}

func (s *firestoreStream) Stop() {
	s.iterator.Stop()
}
