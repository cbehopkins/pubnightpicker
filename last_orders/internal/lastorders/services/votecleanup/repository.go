package votecleanup

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	googleiterator "google.golang.org/api/iterator"
)

type firestoreRepository struct {
	client *firestore.Client
}

func (c *firestoreRepository) RemoveUser(ctx context.Context, targetUID string) (Result, error) {
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

func (c *firestoreRepository) removeUserFromDocument(ctx context.Context, ref *firestore.DocumentRef, targetUID string) (bool, error) {
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
