package testemail

import (
	"testing"

	"last_orders/internal/lastorders/truths"
)

func TestRequest(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]any
		want   truths.TestEmailRequested
		wantOK bool
	}{
		{
			name:   "missing ack prefers notification email",
			data:   map[string]any{"testEmailReq": "r1", "notificationEmail": " n@example.com ", "email": "e@example.com"},
			want:   truths.TestEmailRequested{UserID: "u1", RequestID: "r1", Email: "n@example.com"},
			wantOK: true,
		},
		{
			name:   "different ack falls back to email",
			data:   map[string]any{"testEmailReq": "r2", "testEmailAck": "r1", "notificationEmail": " ", "email": "e@example.com"},
			want:   truths.TestEmailRequested{UserID: "u1", RequestID: "r2", Email: "e@example.com"},
			wantOK: true,
		},
		{
			name:   "no address still requested",
			data:   map[string]any{"testEmailReq": "r1"},
			want:   truths.TestEmailRequested{UserID: "u1", RequestID: "r1"},
			wantOK: true,
		},
		{name: "matching ack", data: map[string]any{"testEmailReq": "r1", "testEmailAck": "r1", "email": "e@example.com"}},
		{name: "empty request", data: map[string]any{"testEmailReq": "", "email": "e@example.com"}},
		{name: "non-string request", data: map[string]any{"testEmailReq": 7, "email": "e@example.com"}},
		{name: "no request", data: map[string]any{"email": "e@example.com"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Request(Document{ID: "u1", Data: test.data})
			if ok != test.wantOK || got != test.want {
				t.Fatalf("Request() = %+v, %v; want %+v, %v", got, ok, test.want, test.wantOK)
			}
		})
	}
}
