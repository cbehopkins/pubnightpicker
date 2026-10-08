package completionactions

// Package completionactions reads and records poll notification actions in Firestore
// open_actions and comp_actions, sharing the Python service's history and semantics.

import (
	"context"
	"fmt"
	"slices"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const collection = "comp_actions"

// Collection names a Firestore action history collection.
type Collection string

const (
	OpenCollection       Collection = "open_actions"
	CompletionCollection Collection = collection
)

// ActionType names a field of an action history document.
type ActionType string

const (
	ActionEmail         ActionType = "email"
	ActionPersonalEmail ActionType = "pemail"
	ActionPush          ActionType = "push"
)

// Key mirrors Python's PushDedupeKeys.complete_key.
func Key(pubID, restaurantID, restaurantTime string) string {
	if restaurantID == "" && restaurantTime == "" {
		return pubID
	}
	return pubID + ":" + restaurantID + ":" + restaurantTime
}

// Record is one poll's actioned configuration keys by action type.
type Record map[ActionType][]string

// NeedsAction reports whether action has not yet been recorded for key.
func (r Record) NeedsAction(action ActionType, key string) bool {
	return !slices.Contains(r[action], key)
}

// PreviouslyActioned reports whether action has run for any configuration of the poll.
func (r Record) PreviouslyActioned(action ActionType) bool {
	_, ok := r[action]
	return ok
}

// Store is the authoritative, durable poll action history.
type Store interface {
	Get(ctx context.Context, collection Collection, pollID string) (Record, error)
	Mark(ctx context.Context, collection Collection, pollID string, action ActionType, key string) error
}

type FirestoreStore struct {
	client *firestore.Client
}

func NewFirestoreStore(client *firestore.Client) (*FirestoreStore, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreStore{client: client}, nil
}

func (s *FirestoreStore) Get(ctx context.Context, collection Collection, pollID string) (Record, error) {
	snapshot, err := s.client.Collection(string(collection)).Doc(pollID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	record := Record{}
	for field, raw := range snapshot.Data() {
		values, ok := raw.([]any)
		if !ok {
			continue
		}
		keys := make([]string, 0, len(values))
		for _, value := range values {
			if key, ok := value.(string); ok {
				keys = append(keys, key)
			}
		}
		record[ActionType(field)] = keys
	}
	return record, nil
}

func (s *FirestoreStore) Mark(ctx context.Context, collection Collection, pollID string, action ActionType, key string) error {
	_, err := s.client.Collection(string(collection)).Doc(pollID).Set(ctx, map[string]any{
		string(action): firestore.ArrayUnion(key),
	}, firestore.MergeAll)
	return err
}
