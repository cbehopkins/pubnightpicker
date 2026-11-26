package attendancecache

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

type Profile struct {
	ID       string
	Name     *string
	PhotoURL *string
}

type ProfileReader struct {
	client *firestore.Client
}

func NewFirestoreProfileReader(client *firestore.Client) (*ProfileReader, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &ProfileReader{client: client}, nil
}

func (r *ProfileReader) PublicProfiles(ctx context.Context, userIDs []string) (map[string]Profile, error) {
	profiles := make(map[string]Profile, len(userIDs))
	if len(userIDs) == 0 {
		return profiles, nil
	}
	refs := make([]*firestore.DocumentRef, len(userIDs))
	for index, userID := range userIDs {
		refs[index] = r.client.Collection("user-public").Doc(userID)
	}
	documents, err := r.client.GetAll(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("read public attendance profiles: %w", err)
	}
	for _, document := range documents {
		if !document.Exists() {
			continue
		}
		data := document.Data()
		profile := Profile{ID: document.Ref.ID}
		if name, ok := data["name"].(string); ok {
			profile.Name = &name
		}
		if photoURL, ok := data["customPhotoUrl"].(string); ok && photoURL != "" {
			profile.PhotoURL = &photoURL
		} else if photoURL, ok := data["photoUrl"].(string); ok {
			profile.PhotoURL = &photoURL
		}
		profiles[profile.ID] = profile
	}
	return profiles, nil
}
