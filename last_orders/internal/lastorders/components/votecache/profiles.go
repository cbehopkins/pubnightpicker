package votecache

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

type ProfileReader struct {
	client *firestore.Client
}

type VoterProfile struct {
	ID       string
	Name     *string
	PhotoURL *string
}

func NewFirestoreProfileReader(client *firestore.Client) (*ProfileReader, error) {
	if client == nil {
		return nil, fmt.Errorf("Firestore client is required")
	}
	return &ProfileReader{client: client}, nil
}

func (r *ProfileReader) VisibleProfiles(ctx context.Context, userIDs []string) map[string]VoterProfile {
	visible := make(map[string]VoterProfile)
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
		data := document.Data()
		preference, present := data["votesVisible"]
		if present && preference != nil && preference != true {
			continue
		}
		profile := VoterProfile{ID: document.Ref.ID}
		if name, ok := data["name"].(string); ok {
			profile.Name = &name
		}
		if photoURL, ok := data["customPhotoUrl"].(string); ok && photoURL != "" {
			profile.PhotoURL = &photoURL
		} else if photoURL, ok := data["photoUrl"].(string); ok {
			profile.PhotoURL = &photoURL
		}
		visible[profile.ID] = profile
	}
	return visible
}
