package landlordinternal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	durableemail "durable_email"
)

type progressReaderFunc func(context.Context, durableemail.ProgressFilter) ([]durableemail.ProgressRow, error)

func (f progressReaderFunc) QueryProgress(ctx context.Context, filter durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
	return f(ctx, filter)
}

func TestEmailHistoryHandlerRequiresServiceToken(t *testing.T) {
	called := false
	handler := EmailHistoryHandler{
		Token: "a sufficiently long service token",
		Store: progressReaderFunc(func(context.Context, durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
			called = true
			return nil, nil
		}),
	}
	for _, test := range []struct {
		name          string
		authorization string
		method        string
		wantStatus    int
	}{
		{name: "missing token", method: http.MethodPost, wantStatus: http.StatusUnauthorized},
		{name: "wrong token", authorization: "Bearer wrong", method: http.MethodPost, wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", authorization: "Basic a sufficiently long service token", method: http.MethodPost, wantStatus: http.StatusUnauthorized},
		{name: "unsupported method", authorization: "Bearer a sufficiently long service token", method: http.MethodGet, wantStatus: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			called = false
			request := httptest.NewRequest(test.method, EmailHistoryPath, strings.NewReader(`{"user_id":"user-1"}`))
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d, want %d", response.Code, test.wantStatus)
			}
			if called {
				t.Fatal("unauthorized or unsupported request reached the progress store")
			}
		})
	}
}

func TestEmailHistoryHandlerConstrainsOwnerQuery(t *testing.T) {
	createdAt := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	var gotFilter durableemail.ProgressFilter
	handler := EmailHistoryHandler{
		Token: "a sufficiently long service token",
		Store: progressReaderFunc(func(_ context.Context, filter durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
			gotFilter = filter
			return []durableemail.ProgressRow{{Subject: "owned", CreatedAt: &createdAt}}, nil
		}),
	}
	request := httptest.NewRequest(http.MethodPost, EmailHistoryPath, strings.NewReader(`{"user_id":"verified-user"}`))
	request.Header.Set("Authorization", "Bearer a sufficiently long service token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if gotFilter.UserID != "verified-user" || !gotFilter.LatestFirst || gotFilter.Limit != 20 {
		t.Fatalf("query filter=%+v, want owner filter, latest first, limit 20", gotFilter)
	}
	var body historyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Subject != "owned" {
		t.Fatalf("response entries=%+v", body.Entries)
	}
}

func TestEmailHistoryHandlerReturnsUnavailableOnStoreFailure(t *testing.T) {
	handler := EmailHistoryHandler{
		Token: "a sufficiently long service token",
		Store: progressReaderFunc(func(context.Context, durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
			return nil, errors.New("database unavailable")
		}),
	}
	request := httptest.NewRequest(http.MethodPost, EmailHistoryPath, strings.NewReader(`{"user_id":"verified-user"}`))
	request.Header.Set("Authorization", "Bearer a sufficiently long service token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), http.StatusServiceUnavailable)
	}
}
