package votecache

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

type ProfileReader struct {
	client *firestore.Client
}

func NewFirestoreProfileReader(client *firestore.Client) (*ProfileReader, error) {
	if client == nil {
		return nil, fmt.Errorf("Firestore client is required")
	}
	return &ProfileReader{client: client}, nil
}

func (r *ProfileReader) VisibleUIDs(ctx context.Context, userIDs []string) map[string]bool {
	visible := make(map[string]bool)
	if len(userIDs) == 0 {
		return visible
	}
	refs := make([]*firestore.DocumentRef, len(userIDs))
	for index, userID := range userIDs {
		refs[index] = r.client.Collection("user-public").Doc(userID)
	}
	documents, err := r.client.GetAll(ctx, refs)
	if err != nil {
		return visible
	}
	for _, document := range documents {
		if !document.Exists() {
			continue
		}
		preference, present := document.Data()["votesVisible"]
		if !present || preference == nil {
			visible[document.Ref.ID] = true
			continue
		}
		if preference, ok := preference.(bool); ok && preference {
			visible[document.Ref.ID] = true
		}
	}
	return visible
}
