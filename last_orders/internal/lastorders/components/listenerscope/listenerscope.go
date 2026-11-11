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
	doc   *firestore.DocumentRef
	polls *firestore.CollectionRef
}

func NewFirestoreStore(client *firestore.Client, collectionRoot, namespace string) (*FirestoreStore, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreStore{
		doc:   client.Collection(collectionRoot).Doc(namespace),
		polls: client.Collection("polls"),
	}, nil
}

// ResolvePollsSince returns the effective cutoff (YYYY-MM-DD). An uninitialised
// store uses the oldest poll date, or fallbackDate when no polls exist.
func (s *FirestoreStore) ResolvePollsSince(ctx context.Context, requested, fallbackDate string) (string, error) {
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
	if requested == "" {
		if stored != "" {
			return stored, nil
		}
		oldestPollDate, err := s.oldestPollDate(ctx)
		if err != nil {
			return "", err
		}
		requested, err = initialPollsSince(oldestPollDate, fallbackDate)
		if err != nil {
			return "", err
		}
	}
	switch {
	case requested == stored:
		return stored, nil
	case stored != "" && requested < stored:
		return "", fmt.Errorf("polls-since %s would move the stored cutoff %s backwards", requested, stored)
	}
	if _, err := s.doc.Set(ctx, map[string]any{pollsSinceField: requested}, firestore.MergeAll); err != nil {
		return "", fmt.Errorf("write %s: %w", pollsSinceField, err)
	}
	return requested, nil
}

func (s *FirestoreStore) oldestPollDate(ctx context.Context) (string, error) {
	documents, err := s.polls.OrderBy("date", firestore.Asc).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		return "", fmt.Errorf("read oldest poll date: %w", err)
	}
	if len(documents) == 0 {
		return "", nil
	}
	value, err := documents[0].DataAt("date")
	if err != nil {
		return "", fmt.Errorf("read oldest poll date: %w", err)
	}
	date, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("oldest poll date is not a string")
	}
	return date, nil
}

func initialPollsSince(oldestPollDate, fallbackDate string) (string, error) {
	date := oldestPollDate
	if date == "" {
		date = fallbackDate
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return "", fmt.Errorf("initial polls-since date %q must be YYYY-MM-DD: %w", date, err)
	}
	return date, nil
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
