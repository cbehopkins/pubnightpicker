package votecleanup

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	googleiterator "google.golang.org/api/iterator"
)

type Result struct {
	UpdatedDocuments int `json:"updatedDocuments"`
}

type FirestoreCleaner struct {
	client *firestore.Client
}

func NewFirestoreCleaner(client *firestore.Client) (*FirestoreCleaner, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreCleaner{client: client}, nil
}

func (c *FirestoreCleaner) RemoveUser(ctx context.Context, targetUID string) (Result, error) {
	if targetUID == "" {
		return Result{}, fmt.Errorf("target uid is required")
	}

	result := Result{}
	docs := c.client.Collection("votes").Documents(ctx)
	defer docs.Stop()
	for {
		doc, err := docs.Next()
		if err == googleiterator.Done {
			break
		}
		if err != nil {
			return result, fmt.Errorf("list vote documents: %w", err)
		}

		updated, err := c.removeUserFromDocument(ctx, doc.Ref, targetUID)
		if err != nil {
			return result, fmt.Errorf("clean vote document %q: %w", doc.Ref.ID, err)
		}
		if updated {
			result.UpdatedDocuments++
		}
	}
	return result, nil
}

func (c *FirestoreCleaner) removeUserFromDocument(ctx context.Context, ref *firestore.DocumentRef, targetUID string) (bool, error) {
	updated := false
	err := c.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snapshot, err := tx.Get(ref)
		if err != nil {
			return err
		}
		updates := make([]firestore.Update, 0)
		for field, rawVoters := range snapshot.Data() {
			voters, changed := removeUID(rawVoters, targetUID)
			if changed {
				updates = append(updates, firestore.Update{Path: field, Value: voters})
			}
		}
		if len(updates) == 0 {
			updated = false
			return nil
		}
		if err := tx.Update(ref, updates); err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}

func removeUID(rawVoters any, targetUID string) (any, bool) {
	switch voters := rawVoters.(type) {
	case []any:
		filtered := make([]any, 0, len(voters))
		changed := false
		for _, voter := range voters {
			if voter == targetUID {
				changed = true
				continue
			}
			filtered = append(filtered, voter)
		}
		if changed {
			return filtered, true
		}
	case []string:
		filtered := make([]string, 0, len(voters))
		changed := false
		for _, voter := range voters {
			if voter == targetUID {
				changed = true
				continue
			}
			filtered = append(filtered, voter)
		}
		if changed {
			return filtered, true
		}
	}
	return rawVoters, false
}
