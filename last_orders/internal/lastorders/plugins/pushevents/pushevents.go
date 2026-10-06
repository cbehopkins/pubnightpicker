// Package pushevents sends chat and diagnostic Web Push notifications, mirroring the
// Python notifier's recipient rules and Firestore history.
package pushevents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/pushsources"
	"last_orders/internal/lastorders/plugins/push"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerChatPush          cellar.HandlerName = "pushevents.chat_push"
	HandlerChatProcessed     cellar.HandlerName = "pushevents.chat_processed"
	HandlerPushTest          cellar.HandlerName = "pushevents.push_test"
	HandlerPushTestCompleted cellar.HandlerName = "pushevents.push_test_completed"

	shortTTL       = time.Hour
	chatBodyLength = 100
)

// Profiles selects endpoints and reads per-user preferences from the projection.
type Profiles interface {
	GetEligiblePushEndpoints(ctx context.Context, selector notificationprofile.Selector) ([]notificationprofile.Endpoint, error)
	Preferences(ctx context.Context, userID string) (notificationprofile.UserPreferences, error)
}

// Pusher commits push population and reports delivery outcomes.
type Pusher interface {
	BaseURL() string
	Populate(ctx context.Context, notification push.Notification, endpoints []notificationprofile.Endpoint, then ...cellar.Step) (cellar.Complete, error)
	Accepted(ctx context.Context, notificationID string) (int, error)
}

type chatPayload struct {
	EventType string  `json:"eventType"`
	MessageID string  `json:"messageId"`
	PollID    *string `json:"pollId"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	URL       string  `json:"url"`
	Tag       string  `json:"tag"`
	SentAt    string  `json:"sentAt"`
}

type chatProcessed struct {
	MessageID string `json:"message_id"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}

// ChatPushHandler mirrors Python's process_chat_message_push.
type ChatPushHandler struct {
	Source   pushsources.Source
	Profiles Profiles
	Push     Pusher
	Now      func() time.Time
}

func (h ChatPushHandler) Handle(ctx context.Context, message truths.ChatMessagePosted) cellar.Result {
	if message.MessageID == "" {
		return cellar.Complete{}
	}
	history, err := h.Source.ChatHistory(ctx, message.MessageID)
	if err != nil {
		return cellar.ErrorResult{Message: "read chat push history", Err: err}
	}
	if history.Processed {
		return cellar.Complete{}
	}

	scopeType, scopeID := "global", "main"
	kind := notificationprofile.KindGlobalChat
	if message.ScopeType == "event" {
		scopeType, kind = "event", notificationprofile.KindEventChat
		if message.ScopeID != "" {
			scopeID = message.ScopeID
		}
	}
	endpoints, err := h.Profiles.GetEligiblePushEndpoints(ctx, notificationprofile.Selector{Kind: kind})
	if err != nil {
		return cellar.ErrorResult{Message: "select chat push endpoints", Err: err}
	}
	endpoints, err = h.recipients(ctx, scopeType, scopeID, message.AuthorUserID, history, endpoints)
	if err != nil {
		return cellar.ErrorResult{Message: "select chat push recipients", Err: err}
	}

	now := clock(h.Now)
	payload := chatPayload{
		EventType: "chat_message_sent_global",
		MessageID: message.MessageID,
		Title:     senderName(message.SenderName) + " in Global Chat",
		Body:      truncate(message.Text, chatBodyLength),
		URL:       h.Push.BaseURL() + "/chat",
		Tag:       "chat:main",
		SentAt:    now.UTC().Format(time.RFC3339Nano),
	}
	if scopeType == "event" {
		payload.EventType = "chat_message_sent_event"
		payload.PollID = &scopeID
		payload.Title = senderName(message.SenderName) + " in Event Chat"
		payload.URL = h.Push.BaseURL() + "/chat/event/" + scopeID
		payload.Tag = "chat:" + scopeID
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return cellar.ErrorResult{Message: "encode chat push payload", Err: err}
	}
	// Python's chat topic exceeds the 32-character Web Push limit, so use the safe helper.
	result, err := h.Push.Populate(ctx, push.Notification{
		ID:        "chat:" + message.MessageID,
		Message:   encoded,
		Topic:     push.Topic("chat-" + payload.Tag),
		ExpiresAt: now.Add(shortTTL),
	}, endpoints, cellar.Step{HandlerName: HandlerChatProcessed, Payload: chatProcessed{
		MessageID: message.MessageID, ScopeType: scopeType, ScopeID: scopeID,
	}})
	if err != nil {
		return cellar.ErrorResult{Message: "populate chat push", Err: err}
	}
	return result
}

func (h ChatPushHandler) recipients(ctx context.Context, scopeType, scopeID, author string, history pushsources.ChatHistory, endpoints []notificationprofile.Endpoint) ([]notificationprofile.Endpoint, error) {
	var members map[string]bool
	if scopeType == "event" {
		attendees, err := h.Source.AttendeeUserIDs(ctx, scopeID)
		if err != nil {
			return nil, err
		}
		participants, err := h.Source.EventChatParticipantUserIDs(ctx, scopeID)
		if err != nil {
			return nil, err
		}
		members = map[string]bool{}
		for _, userID := range append(attendees, participants...) {
			members[userID] = true
		}
	}

	muted := map[string]bool{}
	selected := make([]notificationprofile.Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.UserID == author || slices.Contains(history.DeliveredEndpoints, EndpointHash(endpoint)) {
			continue
		}
		if members != nil {
			if !members[endpoint.UserID] {
				continue
			}
			isMuted, checked := muted[endpoint.UserID]
			if !checked {
				preferences, err := h.Profiles.Preferences(ctx, endpoint.UserID)
				if err != nil {
					return nil, err
				}
				isMuted = preferences.MutedFor(scopeID)
				muted[endpoint.UserID] = isMuted
			}
			if isMuted {
				continue
			}
		}
		selected = append(selected, endpoint)
	}
	return selected, nil
}

// EndpointHash mirrors Python's _endpoint_hash for chat_push_actions.delivered_endpoints.
func EndpointHash(endpoint notificationprofile.Endpoint) string {
	sum := sha256.Sum256([]byte(endpoint.UserID + "__" + endpoint.URL))
	return hex.EncodeToString(sum[:])[:32]
}

func senderName(name string) string {
	if name == "" {
		return "Someone"
	}
	return name
}

// truncate mirrors Python's text[:100], which counts characters rather than bytes.
func truncate(text string, length int) string {
	runes := []rune(text)
	if len(runes) <= length {
		return text
	}
	return string(runes[:length])
}

// ChatProcessedHandler records that every delivery for a message has finished.
type ChatProcessedHandler struct {
	Source pushsources.Source
}

func (h ChatProcessedHandler) Handle(ctx context.Context, processed chatProcessed) cellar.Result {
	if err := h.Source.MarkChatProcessed(ctx, processed.MessageID, processed.ScopeType, processed.ScopeID); err != nil {
		return cellar.ErrorResult{Message: "mark chat push processed", Err: err}
	}
	return cellar.Complete{}
}

type diagnosticPayload struct {
	EventType      string          `json:"eventType"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	URL            string          `json:"url"`
	Tag            string          `json:"tag"`
	RequestedValue json.RawMessage `json:"requestedValue"`
	SentAt         string          `json:"sentAt"`
}

type pushTestCompleted struct {
	NotificationID string          `json:"notification_id"`
	UserID         string          `json:"user_id"`
	Value          json.RawMessage `json:"value"`
}

// PushTestHandler mirrors Python's NotificationPushTestHandler for one requested user.
type PushTestHandler struct {
	Source   pushsources.Source
	Profiles Profiles
	Push     Pusher
	Now      func() time.Time
}

func (h PushTestHandler) Handle(ctx context.Context, request truths.PushTestRequested) cellar.Result {
	if request.UserID == "" {
		return cellar.Complete{}
	}
	acked, found, err := h.Source.PushTestAck(ctx, request.UserID)
	if err != nil {
		return cellar.ErrorResult{Message: "read push test ack", Err: err}
	}
	if found && sameJSON(acked, request.Value) {
		if err := h.Source.DeletePushTestRequest(ctx, request.UserID); err != nil {
			return cellar.ErrorResult{Message: "clear push test request", Err: err}
		}
		return cellar.Complete{}
	}

	endpoints, err := h.Profiles.GetEligiblePushEndpoints(ctx, notificationprofile.Selector{
		Kind: notificationprofile.KindDiagnostic, UserIDs: []string{request.UserID},
	})
	if err != nil {
		return cellar.ErrorResult{Message: "select push test endpoints", Err: err}
	}
	now := clock(h.Now)
	encoded, err := json.Marshal(diagnosticPayload{
		EventType:      "diagnostic_push_test",
		Title:          "Push diagnostics",
		Body:           "This is a test push notification from admin diagnostics.",
		URL:            h.Push.BaseURL() + "/diagnostics",
		Tag:            "push-diagnostic:" + request.UserID,
		RequestedValue: request.Value,
		SentAt:         now.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return cellar.ErrorResult{Message: "encode push test payload", Err: err}
	}
	notificationID := "push-test:" + request.UserID + ":" + string(request.Value)
	result, err := h.Push.Populate(ctx, push.Notification{
		ID:        notificationID,
		Message:   encoded,
		Topic:     push.Topic("diag-" + request.UserID),
		ExpiresAt: now.Add(shortTTL),
	}, endpoints, cellar.Step{HandlerName: HandlerPushTestCompleted, Payload: pushTestCompleted{
		NotificationID: notificationID, UserID: request.UserID, Value: request.Value,
	}})
	if err != nil {
		return cellar.ErrorResult{Message: "populate push test", Err: err}
	}
	return result
}

// PushTestCompletedHandler acknowledges a push test only if some endpoint was accepted or silenced.
type PushTestCompletedHandler struct {
	Source pushsources.Source
	Push   Pusher
}

func (h PushTestCompletedHandler) Handle(ctx context.Context, completed pushTestCompleted) cellar.Result {
	accepted, err := h.Push.Accepted(ctx, completed.NotificationID)
	if err != nil {
		return cellar.ErrorResult{Message: "count accepted push test deliveries", Err: err}
	}
	if accepted > 0 {
		value, err := firestoreValue(completed.Value)
		if err != nil {
			return cellar.ErrorResult{Message: "decode push test value", Err: err}
		}
		if err := h.Source.AckPushTest(ctx, completed.UserID, value); err != nil {
			return cellar.ErrorResult{Message: "acknowledge push test", Err: err}
		}
	}
	if err := h.Source.DeletePushTestRequest(ctx, completed.UserID); err != nil {
		return cellar.ErrorResult{Message: "clear push test request", Err: err}
	}
	return cellar.Complete{}
}

// sameJSON compares a stored Firestore value with a JSON value, ignoring numeric
// representation: Firestore returns integers where JSON decoding yields floats.
func sameJSON(stored any, value json.RawMessage) bool {
	encoded, err := json.Marshal(stored)
	if err != nil {
		return false
	}
	var left, right any
	if json.Unmarshal(encoded, &left) != nil || json.Unmarshal(value, &right) != nil {
		return false
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

// firestoreValue decodes JSON, keeping integral numbers as int64 so acks match requests.
func firestoreValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return normaliseNumbers(value), nil
}

func normaliseNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		float, _ := typed.Float64()
		return float
	case map[string]any:
		for key, item := range typed {
			typed[key] = normaliseNumbers(item)
		}
	case []any:
		for index, item := range typed {
			typed[index] = normaliseNumbers(item)
		}
	}
	return value
}

func clock(now func() time.Time) time.Time {
	if now == nil {
		return time.Now()
	}
	return now()
}
