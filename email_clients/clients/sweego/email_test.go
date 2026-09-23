package sweego

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"email_clients/clients"
)

func TestSendSelectsEndpointAndMakesOneRequest(t *testing.T) {
	tests := []struct {
		name  string
		email clients.Email
		path  string
	}{
		{
			name:  "plain single recipient",
			email: clients.Email{To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}}},
			path:  "/send",
		},
		{
			name:  "templated single recipient",
			email: clients.Email{TemplateID: "tpl-1", To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}}},
			path:  "/send/bulk/email",
		},
		{
			name: "multiple recipients",
			email: clients.Email{To: []clients.Recipient{
				{Address: clients.Address{Email: "alice@example.com"}},
				{Address: clients.Address{Email: "bob@example.com"}},
			}},
			path: "/send/bulk/email",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != test.path {
					t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, test.path)
				}
				ids := make(map[string]string, len(test.email.To))
				for index, recipient := range test.email.To {
					ids[recipient.Email] = "uid-" + string(rune('1'+index))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"swg_uids": ids})
			}))
			t.Cleanup(server.Close)

			result, err := NewClient(server.URL, "token", time.Second).Send(context.Background(), test.email)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("provider requests = %d, want 1", calls.Load())
			}
			if len(result.Recipients) != len(test.email.To) {
				t.Fatalf("recipient results = %d, want %d", len(result.Recipients), len(test.email.To))
			}
		})
	}
}

func TestSendMapsBulkRequestAndPMUIDs(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = io.WriteString(w, `{"transaction_id":"tx-1","swg_uids":{"alice@example.com":"uid-a","bob@example.com":"uid-b"}}`)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "token", time.Second).WithSendOptions(SendOptions{
		Provider: "sender.example", CampaignType: "transac", DryRun: true,
	})
	result, err := client.Send(context.Background(), clients.Email{
		From:       clients.Address{Email: "sender@example.com", Name: "Sender"},
		Subject:    "Subject",
		TemplateID: "tpl-1",
		Text:       "Text",
		Variables:  map[string]any{"event": "Pub", "name": "Common"},
		Headers:    map[string]string{PubnightMessageIDHeader: "pn-1", "X-Custom": "value"},
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com", Name: "Alice"}, Variables: map[string]any{"name": "Alice"}},
			{Address: clients.Address{Email: "bob@example.com"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result, (clients.SendResult{Recipients: []clients.RecipientResult{{PMUID: "uid-a"}, {PMUID: "uid-b"}}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
	for key, want := range map[string]any{
		"channel": "email", "provider": "sender.example", "campaign-type": "transac",
		"dry-run": true, "subject": "Subject", "template-id": "tpl-1", "message-txt": "Text",
	} {
		if body[key] != want {
			t.Fatalf("field %q = %v, want %v", key, body[key], want)
		}
	}
	headers := body["headers"].(map[string]any)
	if headers[PubnightMessageIDHeader] != "pn-1" || headers["X-Custom"] != "value" {
		t.Fatalf("headers = %v", headers)
	}
	recipients := body["recipients"].([]any)
	alice := recipients[0].(map[string]any)
	bob := recipients[1].(map[string]any)
	if alice["name"] != "Alice" || alice["variables"].(map[string]any)["name"] != "Alice" || alice["variables"].(map[string]any)["event"] != "Pub" {
		t.Fatalf("Alice mapping = %v", alice)
	}
	if bob["variables"].(map[string]any)["name"] != "Common" || bob["variables"].(map[string]any)["event"] != "Pub" {
		t.Fatalf("Bob mapping = %v", bob)
	}
}

func TestSendRejectsInvalidOrUnusableResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Query().Get("case") {
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"detail":"invalid"}`)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL+"?case=provider", "token", time.Second)
	_, err := client.Send(context.Background(), clients.Email{To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("provider error = %v", err)
	}

	before := calls.Load()
	_, err = client.Send(context.Background(), clients.Email{})
	if !errors.Is(err, ErrNoRecipients) || calls.Load() != before {
		t.Fatalf("empty send error = %v, calls = %d", err, calls.Load())
	}
}

func TestParsePMUIDsRejectsMalformedAndMissingIdentifiers(t *testing.T) {
	for _, body := range []string{`{`, `{"state":true}`} {
		if _, err := parsePMUIDs([]byte(body)); err == nil {
			t.Fatalf("parsePMUIDs(%q) succeeded", body)
		}
	}
}
