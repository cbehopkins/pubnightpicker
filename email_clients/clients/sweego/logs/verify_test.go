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

	"email_clients/clients"
	"email_clients/clients/sweego"
)

func TestVerifyBuildsQueryAndReturnsPMUID(t *testing.T) {
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
			{EmailTo: "Alice <alice@example.com>", SwgUID: "uid-1", Headers: map[string]any{"x-pubnight-message-id": "pn-1"}},
		}}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)

	verifier := NewVerifier(NewClient(sweego.NewClient(server.URL, "token", time.Second)), 5*time.Minute)
	result, err := verifier.Verify(context.Background(), clients.VerifyRequest{
		CorrelationID: "pn-1",
		Recipient:     "alice@example.com",
		SentAt:        time.Date(2026, 9, 1, 0, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest != (Request{Channel: "email", StartDate: "2026-08-31", EndDate: "2026-09-01", SearchWord: "alice@example.com", Size: 500}) {
		t.Fatalf("query = %#v", gotRequest)
	}
	if !result.Found || result.PMUID != "uid-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestVerifyReturnsNotFoundWithoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":[{"headers":{"x-pubnight-message-id":"other"}}]}`)
	}))
	t.Cleanup(server.Close)
	verifier := NewVerifier(NewClient(sweego.NewClient(server.URL, "token", time.Second)), time.Minute)

	result, err := verifier.Verify(context.Background(), clients.VerifyRequest{
		CorrelationID: "pn-1", Recipient: "alice@example.com", SentAt: time.Now(),
	})
	if err != nil || result.Found || result.PMUID != "" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestVerifyReportsProviderAndDecodeFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{name: "non-2xx", statusCode: http.StatusForbidden, body: `{"detail":"forbidden"}`, wantError: "non-2xx status: 403"},
		{name: "malformed JSON", statusCode: http.StatusOK, body: `{`, wantError: "decode logs response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			verifier := NewVerifier(NewClient(sweego.NewClient(server.URL, "token", time.Second)), time.Minute)

			result, err := verifier.Verify(context.Background(), clients.VerifyRequest{SentAt: time.Now()})
			if err == nil || !strings.Contains(err.Error(), test.wantError) || result.Found {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestVerifyReportsTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := sweego.NewClient(server.URL, "token", time.Second)
	server.Close()

	result, err := NewVerifier(NewClient(client), time.Minute).Verify(context.Background(), clients.VerifyRequest{SentAt: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "request failed") || result.Found {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}
