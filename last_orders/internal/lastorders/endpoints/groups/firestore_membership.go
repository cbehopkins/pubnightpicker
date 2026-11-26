package groups

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errGroupNotFound = errors.New("group not found")
var errPrivateGroup = errors.New("permission to join group is required")

type firestoreMembership struct {
	client     *firestore.Client
	invalidate func(string)
}

func NewFirestoreMembership(client *firestore.Client, invalidate func(string)) *firestoreMembership {
	return &firestoreMembership{client: client, invalidate: invalidate}
}

func (writer *firestoreMembership) EnsureMember(ctx context.Context, groupID, uid string) error {
	err := writer.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		reference := writer.client.Collection("groups").Doc(groupID)
		document, err := transaction.Get(reference)
		if status.Code(err) == codes.NotFound {
			return errGroupNotFound
		}
		if err != nil {
			return err
		}
		data := document.Data()
		if data["visibility"] != "public" {
			if data["visibility"] != "private" {
				return errPrivateGroup
			}
			admins, err := transaction.Get(writer.client.Collection("roles").Doc("admin"))
			if status.Code(err) == codes.NotFound {
				return errPrivateGroup
			}
			if err != nil {
				return err
			}
			if allowed, _ := admins.Data()[uid].(bool); !allowed {
				return errPrivateGroup
			}
		}
		if raw, exists := data["members"]; exists {
			members, valid := raw.(map[string]any)
			if !valid {
				return fmt.Errorf("invalid Group membership representation")
			}
			if member, _ := members[uid].(bool); member {
				return nil
			}
		}
		return transaction.Update(reference, []firestore.Update{{FieldPath: firestore.FieldPath{"members", uid}, Value: true}})
	})
	if err == nil {
		writer.invalidate(groupID)
	}
	return err
}
