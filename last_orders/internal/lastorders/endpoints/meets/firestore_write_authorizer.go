package meets

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WriteAuthorizer interface {
	CanWrite(context.Context, string, string) (bool, error)
}

type firestoreWriteAuthorizer struct {
	client *firestore.Client
}

func NewFirestoreWriteAuthorizer(client *firestore.Client) (WriteAuthorizer, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return firestoreWriteAuthorizer{client: client}, nil
}

func (a firestoreWriteAuthorizer) CanWrite(ctx context.Context, uid, permission string) (bool, error) {
	admin, err := a.hasRole(ctx, "admin", uid)
	if err != nil || admin {
		return admin, err
	}
	return a.hasRole(ctx, permission, uid)
}

func (a firestoreWriteAuthorizer) hasRole(ctx context.Context, role, uid string) (bool, error) {
	doc, err := a.client.Collection("roles").Doc(role).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	allowed, _ := doc.Data()[uid].(bool)
	return allowed, nil
}
