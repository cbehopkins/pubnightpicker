package firebaseauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type verifierFunc func(context.Context, string) (Principal, error)

func (verify verifierFunc) Verify(ctx context.Context, token string) (Principal, error) {
	return verify(ctx, token)
}

func TestMiddleware(t *testing.T) {
	tests := []struct {
		name   string
		header string
		uid    string
		err    error
		status int
		code   string
	}{
		{name: "valid", header: "Bearer good", uid: "verified-user", status: 200},
		{name: "case insensitive scheme", header: "bearer good", uid: "verified-user", status: 200},
		{name: "missing", status: 401, code: "unauthenticated"},
		{name: "empty", header: "Bearer", status: 401, code: "unauthenticated"},
		{name: "wrong scheme", header: "Basic good", status: 401, code: "unauthenticated"},
		{name: "extra token", header: "Bearer good extra", status: 401, code: "unauthenticated"},
		{name: "refused", header: "Bearer bad", err: ErrUnauthenticated, status: 401, code: "unauthenticated"},
		{name: "empty principal", header: "Bearer good", status: 401, code: "unauthenticated"},
		{name: "outage", header: "Bearer good", err: errors.New("private infrastructure details"), status: 503, code: "auth_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			verifier := verifierFunc(func(ctx context.Context, token string) (Principal, error) {
				if token != "good" && token != "bad" {
					t.Fatalf("unexpected token %q", token)
				}
				return Principal{UID: test.uid}, test.err
			})
			handler := Middleware(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				principal, ok := FromContext(r.Context())
				if !ok || principal.UID != "verified-user" {
					t.Fatalf("untrusted principal: %+v", principal)
				}
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodPost, "/api/ping", strings.NewReader(`{"uid":"forged-user"}`))
			if test.header != "" {
				request.Header.Set("Authorization", test.header)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || called != (test.status == 200) {
				t.Fatalf("status=%d handlerCalled=%v body=%s", response.Code, called, response.Body.String())
			}
			if test.code != "" && !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("missing error code: %s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private infrastructure details") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("error details leaked or cache enabled")
			}
		})
	}
}

func TestMiddlewareRejectsDuplicateAuthorization(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/ping", nil)
	request.Header.Add("Authorization", "Bearer good")
	request.Header.Add("Authorization", "Bearer other")
	response := httptest.NewRecorder()
	Middleware(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	})).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestMiddlewareIsolatesPrincipals(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("principal exists outside an authenticated request")
	}
	handler := Middleware(verifierFunc(func(ctx context.Context, token string) (Principal, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("verification has no deadline")
		}
		return Principal{UID: token}, nil
	}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := FromContext(r.Context())
		_, _ = io.WriteString(w, principal.UID)
	}))
	var requests sync.WaitGroup
	for index := range 20 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			uid := fmt.Sprintf("user-%d", index)
			request := httptest.NewRequest(http.MethodPost, "/api/ping", nil)
			request.Header.Set("Authorization", "Bearer "+uid)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Body.String() != uid {
				t.Errorf("principal crossed request boundary: wanted %s, got %s", uid, response.Body.String())
			}
		}()
	}
	requests.Wait()
}
