package idempotency

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Deduper decides if a logical event delivery should be processed.
type Deduper interface {
	FirstDelivery(ctx context.Context, key string) (bool, error)
}

// NoopDeduper permits all deliveries.
type NoopDeduper struct{}

func (NoopDeduper) FirstDelivery(ctx context.Context, key string) (bool, error) {
	_ = ctx
	_ = key
	return true, nil
}

// MemoryDeduper keeps state in-process for prototype verification.
type MemoryDeduper struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewMemoryDeduper() *MemoryDeduper {
	return &MemoryDeduper{seen: map[string]struct{}{}}
}

func (d *MemoryDeduper) FirstDelivery(ctx context.Context, key string) (bool, error) {
	_ = ctx
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[key]; ok {
		return false, nil
	}
	d.seen[key] = struct{}{}
	return true, nil
}

// FirestoreDeduper stores listener-owned idempotency state in a dedicated namespace.
// This writes only under collectionRoot/namespace/events and never touches historic app data.
type FirestoreDeduper struct {
	client         *firestore.Client
	collectionRoot string
	namespace      string
}

func NewFirestoreDeduper(client *firestore.Client, collectionRoot, namespace string) *FirestoreDeduper {
	if collectionRoot == "" {
		collectionRoot = "listener_state"
	}
	if namespace == "" {
		namespace = "last_orders"
	}
	return &FirestoreDeduper{client: client, collectionRoot: collectionRoot, namespace: namespace}
}

func (d *FirestoreDeduper) FirstDelivery(ctx context.Context, key string) (bool, error) {
	if d.client == nil {
		return false, fmt.Errorf("firestore client is nil")
	}

	docID := base64.RawURLEncoding.EncodeToString([]byte(key))
	doc := d.client.Collection(d.collectionRoot).Doc(d.namespace).Collection("events").Doc(docID)
	_, err := doc.Create(ctx, map[string]any{
		"deliveryKey": key,
		"firstSeenAt": firestore.ServerTimestamp,
		"ttlAt":       time.Now().UTC().Add(14 * 24 * time.Hour),
	})
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.AlreadyExists {
		return false, nil
	}
	return false, err
}
