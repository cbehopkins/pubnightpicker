package accountcleanup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"last_orders/internal/lastorders/components/firebaseauth"
)

type cleanerStub struct {
	uid    string
	result Result
	err    error
}

func (c *cleanerStub) RemoveUser(_ context.Context, uid string) (Result, error) {
	c.uid = uid
	return c.result, c.err
}

type verifierStub struct{}

func (verifierStub) Verify(context.Context, string) (firebaseauth.Principal, error) {
	return firebaseauth.Principal{UID: "current-user"}, nil
}

func TestCleanupUsesVerifiedPrincipalAndReturnsCountOnly(t *testing.T) {
	cleaner := &cleanerStub{result: Result{UpdatedDocuments: 2}}
	endpoint := &Endpoint{Cleaner: cleaner, Auth: verifierStub{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/account/votes/cleanup", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if cleaner.uid != "current-user" {
		t.Fatalf("cleaned uid=%q, want verified principal", cleaner.uid)
	}
	if response.Body.String() != "{\"updatedDocuments\":2}\n" {
		t.Fatalf("body=%s, want count-only response", response.Body.String())
	}
}

func TestAttendanceCleanupUsesVerifiedPrincipal(t *testing.T) {
	cleaner := &cleanerStub{result: Result{UpdatedDocuments: 3}}
	endpoint := &Endpoint{AttendanceCleaner: cleaner, Auth: verifierStub{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/account/attendance/cleanup", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK || cleaner.uid != "current-user" || response.Body.String() != "{\"updatedDocuments\":3}\n" {
		t.Fatalf("status=%d uid=%q body=%s", response.Code, cleaner.uid, response.Body.String())
	}
}
