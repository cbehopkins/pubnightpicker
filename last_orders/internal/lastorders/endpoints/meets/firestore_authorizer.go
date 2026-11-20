package meets

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type firestoreGroupAuthorizer struct {
	client *firestore.Client
}

func NewFirestoreGroupAuthorizer(client *firestore.Client) (GroupAuthorizer, error) {
	if client == nil {
		return nil, fmt.Errorf("Firestore client is required")
	}
	return &firestoreGroupAuthorizer{client: client}, nil
}

func (a *firestoreGroupAuthorizer) CanRead(ctx context.Context, uid, groupID string) (bool, error) {
	group, err := a.client.Collection("groups").Doc(groupID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	visibility, _ := group.Data()["visibility"].(string)
	if visibility == "public" {
		return true, nil
	}
	members, ok := group.Data()["members"].(map[string]any)
	if ok && isTrue(members[uid]) {
		return true, nil
	}
	admins, err := a.client.Collection("roles").Doc("admin").Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isTrue(admins.Data()[uid]), nil
}

func isTrue(value any) bool {
	allowed, ok := value.(bool)
	return ok && allowed
}
