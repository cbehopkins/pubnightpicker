package truths

import (
	"encoding/json"

	"cellar/pkg/cellar"
)

const (
	PollManualCompletionRequiredFanout cellar.HandlerName = "truths.poll_manual_completion_required"
	ChatMessagePostedFanout            cellar.HandlerName = "truths.chat_message_posted"
	PushTestRequestedFanout            cellar.HandlerName = "truths.push_test_requested"
)

var (
	PollManualCompletionRequiredRegistry = NewRegistry[PollManualCompletionRequired](PollManualCompletionRequiredFanout)
	ChatMessagePostedRegistry            = NewRegistry[ChatMessagePosted](ChatMessagePostedFanout)
	PushTestRequestedRegistry            = NewRegistry[PushTestRequested](PushTestRequestedFanout)
)

// PollManualCompletionRequired states that automatic completion could not choose a winner.
type PollManualCompletionRequired struct {
	PollID   string `json:"poll_id"`
	PollDate string `json:"poll_date"`
	Reason   string `json:"reason"`
}

// ChatMessagePosted is the evidence observed for a chat message document.
type ChatMessagePosted struct {
	MessageID    string `json:"message_id"`
	ScopeType    string `json:"scope_type"`
	ScopeID      string `json:"scope_id"`
	AuthorUserID string `json:"author_user_id"`
	SenderName   string `json:"sender_name"`
	Text         string `json:"text"`
}

// PushTestRequested is one user's entry in notification_req/push_test.
type PushTestRequested struct {
	UserID string          `json:"user_id"`
	Value  json.RawMessage `json:"value"`
}
