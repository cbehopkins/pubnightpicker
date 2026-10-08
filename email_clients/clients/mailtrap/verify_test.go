package mailtrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"email_clients/clients"

	sdk "github.com/mailtrap/mailtrap-go"
)

func verifyRequest() clients.VerifyRequest {
	return clients.VerifyRequest{CorrelationID: "pn-1", Recipient: "alice@example.com", SentAt: time.Date(2026, 10, 2, 12, 30, 0, 0, time.FixedZone("local", 3600))}
}

func TestVerifyPaginationAndExactMatching(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		query := r.URL.Query()
		if r.URL.Path != "/api/email_logs" || r.Method != http.MethodGet || query.Get("filters[to][operator]") != "ci_equal" || query.Get("filters[to][value]") != "alice@example.com" {
			t.Errorf("logs request = %s %s", r.Method, r.URL)
		}
		if query.Get("filters[sent_after]") != "2026-10-02T11:25:00Z" || query.Get("filters[sent_before]") != "2026-10-02T11:35:00Z" {
			t.Errorf("time window = %v", query)
		}
		switch query.Get("search_after") {
		case "":
			_, _ = io.WriteString(w, `{"messages":[{"message_id":"other","to":"alice@example.com","custom_variables":{"correlation_id":"unrelated"}},{"message_id":"wrong-to","to":"bob@example.com","custom_variables":{"correlation_id":"pn-1"}},{"message_id":"wrong-type","to":"alice@example.com","custom_variables":{"correlation_id":1}}],"next_page_cursor":"next"}`)
		case "next":
			_, _ = io.WriteString(w, `{"messages":[{"message_id":"accepted","to":"ALICE@example.com","custom_variables":{"correlation_id":"pn-1"}}],"next_page_cursor":null}`)
		default:
			t.Errorf("unexpected cursor: %v", query)
		}
	})
	result, err := NewVerifier(client, 5*time.Minute).Verify(context.Background(), verifyRequest())
	if err != nil || !result.Found || result.PMUID != "accepted" || calls.Load() != 2 {
		t.Fatalf("result = %+v, err = %v, calls = %d", result, err, calls.Load())
	}
}

func TestVerifyAbsentInvalidAndAmbiguous(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantError  error
		found      bool
	}{
		{"absent", `{"messages":[],"next_page_cursor":null}`, nil, false},
		{"duplicate same ID", `{"messages":[{"message_id":"a","to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}},{"message_id":"a","to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}}]}`, nil, true},
		{"ambiguous", `{"messages":[{"message_id":"a","to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}},{"message_id":"b","to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}}]}`, ErrAmbiguousVerification, false},
		{"missing ID", `{"messages":[{"to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}}]}`, ErrInvalidResponse, false},
		{"null", `{"messages":[null]}`, ErrInvalidResponse, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, test.body) })
			result, err := NewVerifier(client, time.Minute).Verify(context.Background(), verifyRequest())
			if !errors.Is(err, test.wantError) || result.Found != test.found {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
		})
	}
}

func TestVerifyErrorsAreNotAbsence(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search_after") == "" {
			_, _ = io.WriteString(w, `{"messages":[{"message_id":"a","to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}}],"next_page_cursor":"next"}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"forbidden"}`)
	})
	result, err := NewVerifier(client, time.Minute).Verify(context.Background(), verifyRequest())
	var forbidden *sdk.ForbiddenError
	if !errors.As(err, &forbidden) || result.Found {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	broken := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{`) })
	if _, err := NewVerifier(broken, time.Minute).Verify(context.Background(), verifyRequest()); err == nil {
		t.Fatal("malformed logs accepted")
	}
}

func TestVerifyPreflight(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, name := range []string{"correlation", "recipient", "time", "tolerance", "cancelled", "nil client", "nil verifier"} {
		t.Run(name, func(t *testing.T) {
			request := verifyRequest()
			verifier := NewVerifier(client, time.Minute)
			ctx := context.Background()
			switch name {
			case "correlation":
				request.CorrelationID = ""
			case "recipient":
				request.Recipient = "invalid"
			case "time":
				request.SentAt = time.Time{}
			case "tolerance":
				verifier.Tolerance = -time.Minute
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "nil client":
				verifier.Client = nil
			case "nil verifier":
				verifier = nil
			}
			if _, err := verifier.Verify(ctx, request); err == nil || calls.Load() != 0 {
				t.Fatalf("err = %v, calls = %d", err, calls.Load())
			}
		})
	}
}

func TestSendThenVerifyRoundTrip(t *testing.T) {
	var metadata map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/send" {
			var request sdk.SendRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			metadata = request.CustomVariables
			_, _ = io.WriteString(w, `{"success":true,"message_ids":["round-trip"]}`)
			return
		}
		_ = json.NewEncoder(w).Encode(sdk.EmailLogsList{Messages: []*sdk.EmailLogMessage{{MessageID: "round-trip", To: "alice@example.com", CustomVariables: metadata}}})
	})
	email := testEmail(1)
	email.To[0].Email = "alice@example.com"
	email.Headers = map[string]string{clients.CorrelationHeader: "pn-1"}
	sent, err := client.Send(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := NewVerifier(client, time.Minute).Verify(context.Background(), verifyRequest())
	if err != nil || !verified.Found || verified.PMUID != sent.Recipients[0].PMUID {
		t.Fatalf("verified = %+v, err = %v", verified, err)
	}
}
