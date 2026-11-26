package groups

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"last_orders/internal/lastorders/components/firebaseauth"
)

type verifierStub struct{}

func (verifierStub) Verify(context.Context, string) (firebaseauth.Principal, error) {
	return firebaseauth.Principal{UID: "caller"}, nil
}

func TestSelfJoinRequiresAuthentication(t *testing.T) {
	endpoint := &Endpoint{Auth: verifierStub{}}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodPut, "/api/groups/public-group/members/caller", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("self-join status=%d, want HTTP 401", response.Code)
	}
}

func TestSelfJoinRejectsNestedGroupPaths(t *testing.T) {
	endpoint := &Endpoint{Auth: verifierStub{}, Membership: membershipStub(func(context.Context, string, string) error {
		t.Fatal("invalid Group path reached membership writer")
		return nil
	})}
	mux := http.NewServeMux()
	endpoint.Register(mux)
	request := httptest.NewRequest(http.MethodPut, "/api/groups/outer%2Fnested%2Fgroup/members/caller", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("nested Group path status=%d, want HTTP 400", response.Code)
	}
}

type membershipStub func(context.Context, string, string) error

func (ensure membershipStub) EnsureMember(ctx context.Context, groupID, uid string) error {
	return ensure(ctx, groupID, uid)
}

func TestSelfJoinUsesVerifiedCallerAndMapsFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		user string
		err  error
		want int
	}{
		{name: "join", user: "caller", want: http.StatusNoContent},
		{name: "other user", user: "other", want: http.StatusForbidden},
		{name: "private", user: "caller", err: errPrivateGroup, want: http.StatusForbidden},
		{name: "missing", user: "caller", err: errGroupNotFound, want: http.StatusNotFound},
		{name: "unavailable", user: "caller", err: errors.New("offline"), want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			endpoint := &Endpoint{Auth: verifierStub{}, Membership: membershipStub(func(_ context.Context, groupID, uid string) error {
				called = true
				if groupID != "public-group" || uid != "caller" {
					t.Fatalf("unexpected membership target: %s/%s", groupID, uid)
				}
				return test.err
			})}
			mux := http.NewServeMux()
			endpoint.Register(mux)
			request := httptest.NewRequest(http.MethodPut, "/api/groups/public-group/members/"+test.user, nil)
			request.Header.Set("Authorization", "Bearer token")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.want || called != (test.user == "caller") {
				t.Fatalf("status=%d called=%t, want status=%d", response.Code, called, test.want)
			}
		})
	}
}
