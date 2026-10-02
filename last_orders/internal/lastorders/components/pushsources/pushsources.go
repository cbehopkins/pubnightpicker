// Package pushsources reads and writes the Firestore data, outside the notification
// projection, that chat, manual-completion, and diagnostic pushes need.
package pushsources

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	rolesCollection      = "roles"
	canCompletePollRole  = "canCompletePoll"
	attendanceCollection = "attendance"
	messagesCollection   = "messages"
	chatActions          = "chat_push_actions"
	requestCollection    = "notification_req"
	ackCollection        = "notification_ack"
	// PushTestDocument is the request/ack document shared with the React diagnostics page.
	PushTestDocument = "push_test"
)

// ChatHistory is a message's chat_push_actions state.
type ChatHistory struct {
	Exists             bool
	Processed          bool
	DeliveredEndpoints []string
}

// Source is the Firestore surface used by the remaining push notifications.
type Source interface {
	CanCompletePollUserIDs(ctx context.Context) ([]string, error)
	AttendeeUserIDs(ctx context.Context, pollID string) ([]string, error)
	EventChatParticipantUserIDs(ctx context.Context, pollID string) ([]string, error)
	ChatHistory(ctx context.Context, messageID string) (ChatHistory, error)
	MarkChatProcessed(ctx context.Context, messageID, scopeType, scopeID string) error
	PushTestAck(ctx context.Context, userID string) (any, bool, error)
	AckPushTest(ctx context.Context, userID string, value any) error
	DeletePushTestRequest(ctx context.Context, userID string) error
}

type FirestoreSource struct {
	client *firestore.Client
}

func NewFirestoreSource(client *firestore.Client) (*FirestoreSource, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreSource{client: client}, nil
}

func (s *FirestoreSource) document(ctx context.Context, collection, id string) (map[string]any, bool, error) {
	snapshot, err := s.client.Collection(collection).Doc(id).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return snapshot.Data(), true, nil
}

// CanCompletePollUserIDs mirrors Python's role check: keys of roles/canCompletePoll with a true value.
func (s *FirestoreSource) CanCompletePollUserIDs(ctx context.Context) ([]string, error) {
	data, _, err := s.document(ctx, rolesCollection, canCompletePollRole)
	if err != nil {
		return nil, err
	}
	userIDs := []string{}
	for userID, granted := range data {
		if ok, _ := granted.(bool); ok && userID != "" {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs, nil
}

// AttendeeUserIDs unions every venue's canCome list in attendance/{pollID}.
func (s *FirestoreSource) AttendeeUserIDs(ctx context.Context, pollID string) ([]string, error) {
	data, _, err := s.document(ctx, attendanceCollection, pollID)
	if err != nil {
		return nil, err
	}
	userIDs := []string{}
	for _, venue := range data {
		fields, _ := venue.(map[string]any)
		userIDs = append(userIDs, Strings(fields["canCome"])...)
	}
	return userIDs, nil
}

func (s *FirestoreSource) EventChatParticipantUserIDs(ctx context.Context, pollID string) ([]string, error) {
	if pollID == "" {
		return nil, nil
	}
	documents, err := s.client.Collection(messagesCollection).
		Where("scopeType", "==", "event").
		Where("scopeId", "==", pollID).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(documents))
	for _, document := range documents {
		if userID, ok := document.Data()["uid"].(string); ok && userID != "" {
			userIDs = append(userIDs, userID)
		}
	}
	return userIDs, nil
}

func (s *FirestoreSource) ChatHistory(ctx context.Context, messageID string) (ChatHistory, error) {
	data, exists, err := s.document(ctx, chatActions, messageID)
	if err != nil {
		return ChatHistory{}, err
	}
	processed, _ := data["processed"].(bool)
	return ChatHistory{Exists: exists, Processed: processed, DeliveredEndpoints: Strings(data["delivered_endpoints"])}, nil
}

// MarkChatProcessed mirrors Python's _mark_chat_message_processed.
func (s *FirestoreSource) MarkChatProcessed(ctx context.Context, messageID, scopeType, scopeID string) error {
	history, err := s.ChatHistory(ctx, messageID)
	if err != nil {
		return err
	}
	update := map[string]any{"processed": true, "processedAt": firestore.ServerTimestamp}
	if !history.Exists {
		update["scopeType"] = scopeType
		update["scopeId"] = scopeID
		update["createdAt"] = firestore.ServerTimestamp
	}
	_, err = s.client.Collection(chatActions).Doc(messageID).Set(ctx, update, firestore.MergeAll)
	return err
}

func (s *FirestoreSource) PushTestAck(ctx context.Context, userID string) (any, bool, error) {
	data, _, err := s.document(ctx, ackCollection, PushTestDocument)
	if err != nil {
		return nil, false, err
	}
	value, ok := data[userID]
	return value, ok, nil
}

func (s *FirestoreSource) AckPushTest(ctx context.Context, userID string, value any) error {
	_, err := s.client.Collection(ackCollection).Doc(PushTestDocument).Set(ctx, map[string]any{userID: value}, firestore.MergeAll)
	return err
}

func (s *FirestoreSource) DeletePushTestRequest(ctx context.Context, userID string) error {
	_, err := s.client.Collection(requestCollection).Doc(PushTestDocument).Set(ctx, map[string]any{userID: firestore.Delete}, firestore.MergeAll)
	return err
}

// Strings keeps the string entries of a Firestore array value.
func Strings(raw any) []string {
	values, _ := raw.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
