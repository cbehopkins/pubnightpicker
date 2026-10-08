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
	"sync"
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
		"dry-run": true, "subject": clients.DryRunSubjectPrefix + "Subject", "template-id": "tpl-1", "message-txt": "Text",
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
	for _, recipient := range []map[string]any{alice, bob} {
		if recipient["variables"].(map[string]any)[clients.NotificationPrefixVariable] != clients.DryRunSubjectPrefix {
			t.Fatalf("template prefix = %v", recipient)
		}
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

func TestSendSuppressionCallback(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			var suppressed atomic.Bool
			var callbackCalls int
			var captured map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/send"
				if bulk {
					wantPath = "/send/bulk/email"
				}
				if r.URL.Path != wantPath {
					t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
				}
				captured = nil
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Error(err)
				}
				_, _ = io.WriteString(w, `{"swg_uids":{"alice@example.com":"uid-a","bob@example.com":"uid-b"}}`)
			}))
			t.Cleanup(server.Close)
			client := NewClient(server.URL, "token", time.Second)
			client.SetDryRunCallback(func() bool {
				callbackCalls++
				return suppressed.Load()
			})
			client = client.WithSendOptions(SendOptions{DryRun: explicit})
			email := clients.Email{Subject: "Hello", Text: "Text", Headers: map[string]string{clients.CorrelationHeader: "pn-1"}, To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}}}
			if bulk {
				email.To = append(email.To, clients.Recipient{Address: clients.Address{Email: "bob@example.com"}})
			}
			for index, mode := range []bool{false, true, false} {
				suppressed.Store(mode)
				if _, err := client.Send(context.Background(), email); err != nil {
					t.Fatal(err)
				}
				dryRun, _ := captured["dry-run"].(bool)
				wantMode := explicit || mode
				wantSubject := "Hello"
				if wantMode {
					wantSubject = clients.DryRunSubjectPrefix + wantSubject
				}
				if dryRun != wantMode || captured["subject"] != wantSubject || callbackCalls != index+1 {
					t.Fatalf("mode=%v subject=%v callbackCalls=%d", dryRun, captured["subject"], callbackCalls)
				}
				headers := captured["headers"].(map[string]any)
				if wantMode && headers[clients.DryRunHeader] != "true" {
					t.Fatalf("headers = %v", headers)
				}
			}
			if email.Subject != "Hello" || email.Headers[clients.DryRunHeader] != "" || client.sendOptions.DryRun != explicit {
				t.Fatal("caller input or client options mutated")
			}
			client.SetDryRunCallback(nil)
			if _, err := client.Send(context.Background(), email); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSendConcurrentSuppressionSnapshots(t *testing.T) {
	var suppressed atomic.Bool
	var callbackCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request sendEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		wantSubject, wantHeader := "Hello", ""
		if request.DryRun {
			wantSubject, wantHeader = clients.DryRunSubjectPrefix+"Hello", "true"
		}
		if request.Subject != wantSubject || request.Headers[clients.DryRunHeader] != wantHeader {
			t.Errorf("inconsistent snapshot: %+v", request)
		}
		_, _ = io.WriteString(w, `{"swg_uids":{"alice@example.com":"uid"}}`)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "token", 5*time.Second)
	client.SetDryRunCallback(func() bool {
		callbackCalls.Add(1)
		return suppressed.Load()
	})
	email := clients.Email{Subject: "Hello", Headers: map[string]string{clients.CorrelationHeader: "pn-1"}, To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}}}
	var workers sync.WaitGroup
	for index := range 20 {
		workers.Go(func() {
			suppressed.Store(index%2 == 0)
			if _, err := client.Send(context.Background(), email); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if callbackCalls.Load() != 20 || client.sendOptions.DryRun || email.Subject != "Hello" || email.Headers[clients.DryRunHeader] != "" {
		t.Fatal("callback count or immutable client/input contract failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Send(ctx, email); !errors.Is(err, context.Canceled) || callbackCalls.Load() != 20 {
		t.Fatalf("cancelled send: err=%v callbacks=%d", err, callbackCalls.Load())
	}
}
