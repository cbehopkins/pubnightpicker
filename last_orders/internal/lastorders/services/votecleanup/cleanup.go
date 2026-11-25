package votecleanup

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

type Result struct {
	UpdatedDocuments int `json:"updatedDocuments"`
}

type FirestoreCleaner struct {
	repository repository
}

type repository interface {
	RemoveUser(context.Context, string) (Result, error)
	PreviewUser(context.Context, string) ([]string, error)
}

func NewFirestoreCleaner(client *firestore.Client) (*FirestoreCleaner, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreCleaner{repository: &firestoreRepository{client: client}}, nil
}

func (c *FirestoreCleaner) RemoveUser(ctx context.Context, targetUID string) (Result, error) {
	if targetUID == "" {
		return Result{}, fmt.Errorf("target uid is required")
	}
	return c.repository.RemoveUser(ctx, targetUID)
}

func (c *FirestoreCleaner) PreviewUser(ctx context.Context, targetUID string) ([]string, error) {
	if targetUID == "" {
		return nil, fmt.Errorf("target uid is required")
	}
	return c.repository.PreviewUser(ctx, targetUID)
}
