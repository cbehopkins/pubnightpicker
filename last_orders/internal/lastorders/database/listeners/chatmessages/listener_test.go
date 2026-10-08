package chatmessages

import (
	"testing"

	"last_orders/internal/lastorders/truths"
)

func TestMessageMirrorsPythonFieldFallbacks(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want truths.ChatMessagePosted
	}{
		{
			name: "preferred fields",
			data: map[string]any{"text": "hi", "message": "old", "displayName": "Ann", "name": "A", "scopeType": "event", "scopeId": "poll-1", "uid": "u1"},
			want: truths.ChatMessagePosted{MessageID: "m1", ScopeType: "event", ScopeID: "poll-1", AuthorUserID: "u1", SenderName: "Ann", Text: "hi"},
		},
		{
			name: "fallback fields",
			data: map[string]any{"message": "old", "displayName": "", "name": "A"},
			want: truths.ChatMessagePosted{MessageID: "m1", SenderName: "A", Text: "old"},
		},
		{
			name: "empty text is still preferred",
			data: map[string]any{"text": "", "message": "old"},
			want: truths.ChatMessagePosted{MessageID: "m1"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Message(Document{ID: "m1", Data: test.data}); got != test.want {
				t.Fatalf("Message() = %+v, want %+v", got, test.want)
			}
		})
	}
}
