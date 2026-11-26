package attendancecleanup

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"last_orders/internal/lastorders/services/votecleanup"
)

type Result = votecleanup.Result

type FirestoreCleaner struct {
	client *firestore.Client
}

func NewFirestoreCleaner(client *firestore.Client) (*FirestoreCleaner, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreCleaner{client: client}, nil
}

func (cleaner *FirestoreCleaner) PreviewUser(ctx context.Context, targetUID string) ([]string, error) {
	if targetUID == "" {
		return nil, fmt.Errorf("target uid is required")
	}
	var paths []string
	documents := cleaner.client.Collection("attendance").Documents(ctx)
	defer documents.Stop()
	for {
		document, err := documents.Next()
		if err == iterator.Done {
			return paths, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list attendance documents: %w", err)
		}
		if len(attendanceUpdates(document.Data(), targetUID)) > 0 {
			paths = append(paths, document.Ref.Path)
		}
	}
}

func (cleaner *FirestoreCleaner) RemoveUser(ctx context.Context, targetUID string) (Result, error) {
	if targetUID == "" {
		return Result{}, fmt.Errorf("target uid is required")
	}
	result := Result{}
	documents := cleaner.client.Collection("attendance").Documents(ctx)
	defer documents.Stop()
	for {
		document, err := documents.Next()
		if err == iterator.Done {
			return result, nil
		}
		if err != nil {
			return result, fmt.Errorf("list attendance documents: %w", err)
		}
		updated, err := cleaner.removeUserFromDocument(ctx, document.Ref, targetUID)
		if err != nil {
			return result, fmt.Errorf("clean attendance document %q: %w", document.Ref.ID, err)
		}
		if updated {
			result.UpdatedDocuments++
		}
	}
}

func (cleaner *FirestoreCleaner) removeUserFromDocument(ctx context.Context, ref *firestore.DocumentRef, targetUID string) (bool, error) {
	updated := false
	err := cleaner.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		snapshot, err := transaction.Get(ref)
		if status.Code(err) == codes.NotFound {
			updated = false
			return nil
		}
		if err != nil {
			return err
		}
		updates := attendanceUpdates(snapshot.Data(), targetUID)
		if len(updates) == 0 {
			updated = false
			return nil
		}
		if err := transaction.Update(ref, updates); err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}

func attendanceUpdates(data map[string]any, targetUID string) []firestore.Update {
	updates := make([]firestore.Update, 0)
	for venueID, rawEntry := range data {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		changed := false
		for _, field := range []string{"canCome", "cannotCome"} {
			filtered, removed := removeUID(entry[field], targetUID)
			if removed {
				entry[field] = filtered
				changed = true
			}
		}
		if eta, ok := entry["eta"].(map[string]any); ok {
			if _, exists := eta[targetUID]; exists {
				delete(eta, targetUID)
				changed = true
			}
		}
		if changed {
			updates = append(updates, firestore.Update{FieldPath: firestore.FieldPath{venueID}, Value: entry})
		}
	}
	return updates
}

func removeUID(value any, targetUID string) (any, bool) {
	switch values := value.(type) {
	case []any:
		filtered := make([]any, 0, len(values))
		for _, item := range values {
			uid, isString := item.(string)
			if !isString || uid != targetUID {
				filtered = append(filtered, item)
			}
		}
		return filtered, len(filtered) != len(values)
	case []string:
		filtered := make([]string, 0, len(values))
		for _, uid := range values {
			if uid != targetUID {
				filtered = append(filtered, uid)
			}
		}
		return filtered, len(filtered) != len(values)
	default:
		return value, false
	}
}
