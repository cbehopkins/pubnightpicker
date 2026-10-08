// Package listenerscope persists in Firestore the date before which poll and chat
// listeners ignore documents, so the cutoff survives loss of the local database.
package listenerscope

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const pollsSinceField = "pollsSince"

type FirestoreStore struct {
	doc *firestore.DocumentRef
}

func NewFirestoreStore(client *firestore.Client, collectionRoot, namespace string) (*FirestoreStore, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreStore{doc: client.Collection(collectionRoot).Doc(namespace)}, nil
}

// ResolvePollsSince returns the effective cutoff (YYYY-MM-DD). A non-empty requested
// value is persisted; it may not move the stored cutoff backwards.
func (s *FirestoreStore) ResolvePollsSince(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		if _, err := time.Parse(time.DateOnly, requested); err != nil {
			return "", fmt.Errorf("polls-since %q must be YYYY-MM-DD: %w", requested, err)
		}
	}
	// No transaction: only one backend runs, and emulator transactions can block on stale locks.
	stored, err := s.stored(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case requested == "" && stored == "":
		return "", fmt.Errorf("no %s stored in Firestore: supply -polls-since", pollsSinceField)
	case requested == "" || requested == stored:
		return stored, nil
	case requested < stored:
		return "", fmt.Errorf("polls-since %s would move the stored cutoff %s backwards", requested, stored)
	}
	if _, err := s.doc.Set(ctx, map[string]any{pollsSinceField: requested}, firestore.MergeAll); err != nil {
		return "", fmt.Errorf("write %s: %w", pollsSinceField, err)
	}
	return requested, nil
}

func (s *FirestoreStore) stored(ctx context.Context) (string, error) {
	snapshot, err := s.doc.Get(ctx)
	if status.Code(err) == codes.NotFound {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", pollsSinceField, err)
	}
	raw, err := snapshot.DataAt(pollsSinceField)
	if err != nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("stored %s is not a string", pollsSinceField)
	}
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return "", fmt.Errorf("stored %s %q is not YYYY-MM-DD", pollsSinceField, value)
	}
	return value, nil
}
