package firebaseidempotency

import (
	"context"
	"encoding/base64"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FirestoreRemote uses Cloud Firestore as the external Firebase idempotency surface.
//
// It writes only to a dedicated namespace and can be pointed at the Firestore emulator
// by setting FIRESTORE_EMULATOR_HOST in the process environment.
type FirestoreRemote struct {
	client         *firestore.Client
	collectionRoot string
	namespace      string
}

func NewFirestoreRemote(client *firestore.Client, collectionRoot, namespace string) (*FirestoreRemote, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	if collectionRoot == "" {
		collectionRoot = "listener_state"
	}
	if namespace == "" {
		namespace = "last_orders"
	}

	return &FirestoreRemote{
		client:         client,
		collectionRoot: collectionRoot,
		namespace:      namespace,
	}, nil
}

func (r *FirestoreRemote) CreateKey(ctx context.Context, listener, eventKey, owner string) (bool, bool, error) {
	doc := r.docRef(listener, eventKey)
	_, err := doc.Create(ctx, map[string]any{
		"listener":  listener,
		"eventKey":  eventKey,
		"owner":     owner,
		"createdAt": firestore.ServerTimestamp,
	})
	if err == nil {
		return false, true, nil
	}
	if status.Code(err) == codes.AlreadyExists {
		snap, getErr := doc.Get(ctx)
		if getErr != nil {
			return true, false, getErr
		}
		existingOwner, _ := snap.Data()["owner"].(string)
		return true, existingOwner == owner, nil
	}
	return false, false, err
}

func (r *FirestoreRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	_, err := r.docRef(listener, eventKey).Get(ctx)
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	return false, err
}

func (r *FirestoreRemote) docRef(listener, eventKey string) *firestore.DocumentRef {
	compositeKey := listener + "::" + eventKey
	docID := base64.RawURLEncoding.EncodeToString([]byte(compositeKey))
	return r.client.Collection(r.collectionRoot).Doc(r.namespace).Collection("events").Doc(docID)
}
