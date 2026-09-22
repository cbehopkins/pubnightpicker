package autocomplete

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Document struct {
	ID   string
	Data map[string]any
}

// AuditEntry records an automated poll completion.
type AuditEntry struct {
	PollID          string
	PollDate        string
	SelectedVenueID string
}

// Source is the poll state the auto-completion handlers read and write.
type Source interface {
	ListOpenPollsOn(ctx context.Context, date string) ([]Document, error)
	GetPoll(ctx context.Context, pollID string) (Document, error)
	GetVotes(ctx context.Context, pollID string) (Document, bool, error)
	GetVenue(ctx context.Context, venueID string) (Document, bool, error)
	// CompletePoll conditionally completes a poll, reporting whether this call
	// was the one that completed it. It must be atomic.
	CompletePoll(ctx context.Context, pollID, selectedVenueID string) (bool, error)
	WriteCompletionAudit(ctx context.Context, entry AuditEntry) error
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

func (s *FirestoreSource) ListOpenPollsOn(ctx context.Context, date string) ([]Document, error) {
	docs, err := s.client.Collection("polls").Where("completed", "==", false).Where("date", "==", date).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	polls := make([]Document, 0, len(docs))
	for _, doc := range docs {
		if doc == nil || doc.Ref == nil {
			return nil, fmt.Errorf("poll document is nil")
		}
		polls = append(polls, Document{ID: doc.Ref.ID, Data: doc.Data()})
	}
	return polls, nil
}

func (s *FirestoreSource) GetPoll(ctx context.Context, pollID string) (Document, error) {
	doc, err := s.client.Collection("polls").Doc(pollID).Get(ctx)
	if err != nil {
		return Document{}, err
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, nil
}

func (s *FirestoreSource) GetVotes(ctx context.Context, pollID string) (Document, bool, error) {
	doc, err := s.client.Collection("votes").Doc(pollID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound || doc == nil || !doc.Exists() {
			return Document{}, false, nil
		}
		return Document{}, false, err
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, true, nil
}

func (s *FirestoreSource) GetVenue(ctx context.Context, venueID string) (Document, bool, error) {
	doc, err := s.client.Collection("pubs").Doc(venueID).Get(ctx)
	if err != nil {
		// Firestore reports a missing document as NotFound; that is an answer, not a failure.
		if status.Code(err) == codes.NotFound {
			return Document{}, false, nil
		}
		return Document{}, false, err
	}
	if !doc.Exists() {
		return Document{}, false, nil
	}
	return Document{ID: doc.Ref.ID, Data: doc.Data()}, true, nil
}

func (s *FirestoreSource) CompletePoll(ctx context.Context, pollID, selectedVenueID string) (bool, error) {
	pollRef := s.client.Collection("polls").Doc(pollID)
	completed := false
	err := s.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		doc, err := transaction.Get(pollRef)
		if err != nil {
			return err
		}
		if current, _ := doc.Data()["completed"].(bool); current {
			return nil
		}
		if err := transaction.Set(pollRef, map[string]any{"completed": true, "selected": selectedVenueID}, firestore.MergeAll); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return completed, nil
}

func (s *FirestoreSource) WriteCompletionAudit(ctx context.Context, entry AuditEntry) error {
	auditID := fmt.Sprintf("%s_complete_%d", entry.PollID, time.Now().UTC().UnixNano())
	_, err := s.client.Collection("poll_action_audit").Doc(auditID).Set(ctx, map[string]any{
		"actionType": "complete", "actorUid": "backend:auto", "pollId": entry.PollID,
		"pollDate": entry.PollDate, "selectedVenueId": entry.SelectedVenueID, "at": firestore.ServerTimestamp,
	})
	return err
}
