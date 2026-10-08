package firebaseauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/endpoints/ping"
)

func TestFirebaseAuthEmulatorPing(t *testing.T) {
	host := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	projectID := os.Getenv("FIREBASE_AUTH_PROJECT_ID")
	if host == "" || projectID == "" {
		t.Skip("requires FIREBASE_AUTH_EMULATOR_HOST and FIREBASE_AUTH_PROJECT_ID for a running local Auth emulator")
	}
	verifier, err := firebaseauth.New(context.Background(), projectID, true)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post("http://"+host+"/identitytoolkit.googleapis.com/v1/accounts:signUp?key="+url.QueryEscape(projectID), "application/json", strings.NewReader(`{"returnSecureToken":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var account struct {
		IDToken string `json:"idToken"`
		LocalID string `json:"localId"`
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("emulator sign-up returned %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&account); err != nil {
		t.Fatal(err)
	}
	if account.IDToken == "" || account.LocalID == "" {
		t.Fatal("emulator did not return an ID token and UID")
	}
	t.Cleanup(func() {
		deleted, err := client.Post("http://"+host+"/identitytoolkit.googleapis.com/v1/accounts:delete?key="+url.QueryEscape(projectID), "application/json", strings.NewReader(`{"idToken":"`+account.IDToken+`"}`))
		if err == nil {
			deleted.Body.Close()
		}
	})
	var output bytes.Buffer
	handler := firebaseauth.Middleware(verifier, &ping.Endpoint{Logger: slog.New(slog.NewJSONHandler(&output, nil))})
	server := httptest.NewServer(handler)
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/ping", strings.NewReader(`{"request_id":"bca1207e-0519-4512-b9b5-c1b8a1d6fd00"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+account.IDToken)
	result, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK || !strings.Contains(string(body), account.LocalID) || !strings.Contains(output.String(), account.LocalID) {
		t.Fatalf("ping status=%d body=%s", result.StatusCode, body)
	}
	if strings.Contains(output.String(), account.IDToken) {
		t.Fatal("token leaked into log")
	}
}
