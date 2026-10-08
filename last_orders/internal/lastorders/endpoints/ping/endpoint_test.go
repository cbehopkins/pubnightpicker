package ping

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last_orders/internal/lastorders/components/firebaseauth"
)

const requestID = "bca1207e-0519-4512-b9b5-c1b8a1d6fd00"

type verifier struct{}

func (verifier) Verify(context.Context, string) (firebaseauth.Principal, error) {
	return firebaseauth.Principal{UID: "verified-user"}, nil
}

func TestPing(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType string
		status                  int
	}{
		{name: "success", body: `{"request_id":"` + requestID + `"}`, contentType: "application/json", status: 200},
		{name: "charset", body: `{"request_id":"` + requestID + `"}`, contentType: "application/json; charset=utf-8", status: 200},
		{name: "forged identity", body: `{"request_id":"` + requestID + `","uid":"forged-user"}`, contentType: "application/json", status: 400},
		{name: "missing id", body: `{}`, contentType: "application/json", status: 400},
		{name: "bad id", body: `{"request_id":"bad"}`, contentType: "application/json", status: 400},
		{name: "malformed", body: `{`, contentType: "application/json", status: 400},
		{name: "null", body: `null`, contentType: "application/json", status: 400},
		{name: "trailing JSON", body: `{"request_id":"` + requestID + `"} {}`, contentType: "application/json", status: 400},
		{name: "trailing junk", body: `{"request_id":"` + requestID + `"} x`, contentType: "application/json", status: 400},
		{name: "oversized", body: strings.Repeat(" ", 1025), contentType: "application/json", status: 413},
		{name: "wrong media", body: `{}`, contentType: "text/plain", status: 415},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := firebaseauth.Middleware(verifier{}, &Endpoint{Logger: slog.New(slog.NewJSONHandler(&output, nil))})
			request := httptest.NewRequest(http.MethodPost, "/api/ping", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer token-never-logged")
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.status == 200 {
				if !strings.Contains(output.String(), `"uid":"verified-user"`) || !strings.Contains(response.Body.String(), `"uid":"verified-user"`) || !strings.Contains(response.Body.String(), requestID) {
					t.Fatalf("missing verified identity/correlation: log=%s body=%s", &output, response.Body)
				}
			} else if output.Len() != 0 {
				t.Fatal("invalid ping was logged as successful")
			}
			if strings.Contains(output.String(), "token-never-logged") || strings.Contains(response.Body.String(), "token-never-logged") {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestPingRequiresPrincipal(t *testing.T) {
	response := httptest.NewRecorder()
	(&Endpoint{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ping", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}
