package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sweego_client/sweego"
)

func TestVerifyMessageBuildsQueryAndFindsMatchingRecord(t *testing.T) {
	var gotRequest Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/logs/" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}
		response := Response{Result: []Record{
			{EmailTo: "ignored@example.com", Headers: map[string]any{"x-pubnight-message-id": "other"}},
			{EmailTo: "Alice <alice@example.com>", TransactionID: "transaction-1", SwgUID: "uid-1", Status: "delivered", Headers: map[string]any{"x-pubnight-message-id": "pn-1"}},
		}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)

	client := NewClient(sweego.NewClient(server.URL, "token", time.Second))
	sentAt := time.Date(2026, 9, 1, 0, 2, 0, 0, time.UTC)
	result := NewVerifier(client, 5*time.Minute).VerifyMessage(context.Background(), "pn-1", "alice@example.com", sentAt)

	if gotRequest != (Request{Channel: "email", StartDate: "2026-08-31", EndDate: "2026-09-01", SearchWord: "alice@example.com", Size: 500}) {
		t.Fatalf("unexpected query: %#v", gotRequest)
	}
	if result.Status != VerificationFound || result.CorrelationID != "pn-1" || result.Recipient != "Alice <alice@example.com>" || result.TransactionID != "transaction-1" || result.SwgUID != "uid-1" || result.EmailStatus != "delivered" || result.Err != nil {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func TestVerifyMessageClassifiesResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus VerificationStatus
		wantErr    string
	}{
		{name: "no matching record", statusCode: http.StatusOK, body: `{"result":[{"headers":{"x-pubnight-message-id":"other"}}]}`, wantStatus: VerificationNotFound},
		{name: "non-2xx", statusCode: http.StatusForbidden, body: `{"detail":"forbidden"}`, wantStatus: VerificationQueryError, wantErr: "non-2xx status: 403"},
		{name: "malformed JSON", statusCode: http.StatusOK, body: `{`, wantStatus: VerificationQueryError, wantErr: "decode logs response"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			verifier := NewVerifier(NewClient(sweego.NewClient(server.URL, "token", time.Second)), time.Minute)

			result := verifier.VerifyMessage(context.Background(), "pn-1", "alice@example.com", time.Now())
			if result.Status != test.wantStatus || result.CorrelationID != "pn-1" || result.Recipient != "alice@example.com" {
				t.Fatalf("unexpected verification result: %#v", result)
			}
			if test.wantErr == "" && result.Err != nil {
				t.Fatalf("unexpected error: %v", result.Err)
			}
			if test.wantErr != "" && (result.Err == nil || !strings.Contains(result.Err.Error(), test.wantErr)) {
				t.Fatalf("error = %v, want substring %q", result.Err, test.wantErr)
			}
		})
	}
}

func TestVerifyMessageReportsTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := sweego.NewClient(server.URL, "token", time.Second)
	server.Close()

	result := NewVerifier(NewClient(client), time.Minute).VerifyMessage(context.Background(), "pn-1", "alice@example.com", time.Now())
	if result.Status != VerificationQueryError || result.Err == nil || !strings.Contains(result.Err.Error(), "request failed") {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}
