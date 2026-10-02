package emailhistory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	durableemail "durable_email"
	"last_orders/internal/lastorders/components/firebaseauth"
)

const requestID = "bca1207e-0519-4512-b9b5-c1b8a1d6fd00"

type verifier struct{}

func (verifier) Verify(context.Context, string) (firebaseauth.Principal, error) {
	return firebaseauth.Principal{UID: "verified-user"}, nil
}

type reader struct {
	filter durableemail.ProgressFilter
	calls  int
	fail   bool
}

func (store *reader) QueryProgress(ctx context.Context, filter durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
	store.filter = filter
	store.calls++
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing query deadline")
	}
	if store.fail {
		return nil, errors.New("private database details")
	}
	created := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	return []durableemail.ProgressRow{{Subject: "Hello", Recipient: "me@example.com", State: durableemail.StatePending, CreatedAt: &created, PMUID: "private-provider-id", IdempotencyToken: "private-token"}}, nil
}

func TestHistoryRequestBoundary(t *testing.T) {
	for _, test := range []struct {
		name, body, media string
		status            int
		fail              bool
	}{
		{"success", `{"request_id":"` + requestID + `"}`, "application/json", 200, false},
		{"forged UID", `{"request_id":"` + requestID + `","uid":"someone-else"}`, "application/json", 400, false},
		{"forged recipient", `{"request_id":"` + requestID + `","recipient":"someone@example.com"}`, "application/json", 400, false},
		{"missing correlation", `{}`, "application/json", 400, false},
		{"null", `null`, "application/json", 400, false},
		{"trailing JSON", `{"request_id":"` + requestID + `"} {}`, "application/json", 400, false},
		{"too large", strings.Repeat(" ", 1025), "application/json", 413, false},
		{"wrong media", `{}`, "text/plain", 415, false},
		{"database error", `{"request_id":"` + requestID + `"}`, "application/json", 503, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &reader{fail: test.fail}
			handler := firebaseauth.Middleware(verifier{}, &Endpoint{Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			request := httptest.NewRequest(http.MethodPost, "/api/email-history", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer token")
			request.Header.Set("Content-Type", test.media)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if test.status == 200 || test.fail {
				if !reflect.DeepEqual(store.filter, durableemail.ProgressFilter{UserID: "verified-user", LatestFirst: true, Limit: 20}) {
					t.Fatalf("filter=%+v", store.filter)
				}
			} else if store.calls != 0 {
				t.Fatal("invalid request queried history")
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatal("internal data exposed")
			}
			if test.status == 200 {
				var body struct {
					UID, RequestID string
					Entries        []entry
				}
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(payload["uid"], &body.UID); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(payload["request_id"], &body.RequestID); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(payload["entries"], &body.Entries); err != nil {
					t.Fatal(err)
				}
				if body.UID != "verified-user" || body.RequestID != requestID || len(body.Entries) != 1 || body.Entries[0].State != "Pending" {
					t.Fatalf("body=%+v", body)
				}
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
			}
		})
	}
}

func TestHistoryRequiresAuthentication(t *testing.T) {
	store := &reader{}
	response := httptest.NewRecorder()
	(&Endpoint{Store: store}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/email-history", nil))
	if response.Code != 401 || store.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, store.calls)
	}
}
