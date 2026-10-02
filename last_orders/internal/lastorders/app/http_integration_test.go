package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
)

type testAuthVerifier struct{}

func (testAuthVerifier) Verify(ctx context.Context, token string) (firebaseauth.Principal, error) {
	if token != "valid-token" {
		return firebaseauth.Principal{}, firebaseauth.ErrUnauthenticated
	}
	return firebaseauth.Principal{UID: "verified-user"}, nil
}

func TestHTTPAuthEmulatorGuard(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	cfg := testConfig(t, t.TempDir()+"/auth-guard.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.HTTPAddr = "127.0.0.1:0"
	if _, err := app.New(cfg); err == nil {
		t.Fatal("HTTP enabled with implicit emulator acceptance")
	}
	cfg.AllowAuthEmulator = true
	cfg.AuthVerifier = nil
	if _, err := app.New(cfg); err == nil {
		t.Fatal("HTTP auth emulator enabled without a concrete project")
	}
	cfg.HTTPAddr = ""
	application, err := app.New(cfg)
	if err != nil {
		t.Fatalf("disabled HTTP must not require Auth configuration: %v", err)
	}
	application.Close()
}

func TestHTTPInvalidAPIOrigin(t *testing.T) {
	cfg := testConfig(t, t.TempDir()+"/invalid-origin.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.AllowedAPIOrigins = []string{"*"}
	if _, err := app.New(cfg); err == nil {
		t.Fatal("accepted wildcard CORS origin")
	}
}

func TestHTTPInvalidPreviewSite(t *testing.T) {
	cfg := testConfig(t, t.TempDir()+"/invalid-preview.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.AllowedAPIPreviewSites = []string{"*.web.app"}
	if _, err := app.New(cfg); err == nil {
		t.Fatal("accepted wildcard preview site")
	}
}

func TestAuthenticatedEmailHistoryEndToEnd(t *testing.T) {
	dbPath := t.TempDir() + "/email-history.db"
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.AllowedAPIOrigins = []string{"http://localhost:3000"}
	application, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	db := openSQLite(t, dbPath)
	for index := range 24 {
		token := fmt.Sprintf("send-%02d", index)
		if _, err := db.Exec(`INSERT INTO email_requests VALUES (?, ?, '', '', ?, '', '', '{}', '{}')`, token, token, token); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO email_progress (idempotency_token, recipient, recipient_name, state, variables) VALUES (?, 'shared@example.com', '', 'Pending', '{}')`, token); err != nil {
			t.Fatal(err)
		}
		if index == 23 {
			continue
		}
		uid := "verified-user"
		if index == 22 {
			uid = "another-user"
		}
		if _, err := db.Exec(`INSERT INTO email_ownership VALUES (?, ?, 'shared@example.com', ?)`, uid, token, time.Date(2026, 10, 1, index, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}
	for _, token := range []string{"", "bad-token", "valid-token"} {
		request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddr()+"/api/email-history", strings.NewReader(`{"request_id":"bca1207e-0519-4512-b9b5-c1b8a1d6fd00"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if token != "valid-token" {
			response.Body.Close()
			if response.StatusCode != 401 {
				t.Fatalf("unauthenticated status = %d", response.StatusCode)
			}
			continue
		}
		var body struct {
			UID     string `json:"uid"`
			Entries []struct {
				Subject string `json:"subject"`
			} `json:"entries"`
		}
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || body.UID != "verified-user" || len(body.Entries) != 20 {
			t.Fatalf("status=%d body=%+v", response.StatusCode, body)
		}
		for index, entry := range body.Entries {
			if entry.Subject != fmt.Sprintf("send-%02d", 21-index) {
				t.Fatalf("entry %d = %+v", index, entry)
			}
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app did not stop")
	}
}

func TestAuthenticatedPingEndToEnd(t *testing.T) {
	var output syncBuffer
	cfg := testConfig(t, t.TempDir()+"/ping.db", firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(&output, nil))
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.AllowedAPIOrigins = []string{"http://localhost:3000", "https://ampubnight.org", "https://pubnightpicker.web.app"}
	cfg.AllowedAPIPreviewSites = []string{"pubnightpicker"}
	application, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}
	requestID := "bca1207e-0519-4512-b9b5-c1b8a1d6fd00"
	previewOrigin := "https://pubnightpicker--pr141-bug-test-build-ie5jywxw.web.app"
	successfulPings := 0
	for _, test := range []struct {
		method, token, origin string
		status                int
	}{
		{method: "OPTIONS", status: 204},
		{method: "POST", status: 401},
		{method: "POST", token: "bad-token", status: 401},
		{method: "POST", token: "valid-token", status: 200},
		{method: "OPTIONS", origin: previewOrigin, status: 204},
		{method: "POST", origin: previewOrigin, status: 401},
		{method: "POST", token: "bad-token", origin: previewOrigin, status: 401},
		{method: "POST", token: "valid-token", origin: previewOrigin, status: 200},
		{method: "POST", token: "valid-token", origin: "https://ampubnight.org", status: 200},
		{method: "POST", token: "valid-token", origin: "https://pubnightpicker.web.app", status: 200},
		{method: "POST", token: "valid-token", origin: "https://other-project--preview.web.app", status: 403},
		{method: "OPTIONS", origin: previewOrigin + ".evil.example", status: 403},
	} {
		request, err := http.NewRequest(test.method, "http://"+application.HTTPAddr()+"/api/ping", strings.NewReader(`{"request_id":"`+requestID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		origin := test.origin
		if origin == "" {
			origin = "http://localhost:3000"
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Access-Control-Request-Method", "POST")
		request.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if response.StatusCode != 204 {
			err = json.NewDecoder(response.Body).Decode(&body)
		}
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		allowedOrigin := origin
		if test.status == http.StatusForbidden {
			allowedOrigin = ""
		}
		if response.StatusCode != test.status || response.Header.Get("Access-Control-Allow-Origin") != allowedOrigin {
			t.Fatalf("status=%d headers=%v", response.StatusCode, response.Header)
		}
		if test.status == http.StatusOK {
			successfulPings++
		}
		if test.status == 200 && (body["uid"] != "verified-user" || body["request_id"] != requestID) {
			t.Fatalf("unexpected response: %v", body)
		}
	}
	if strings.Count(output.String(), "authenticated API ping") != successfulPings || strings.Contains(output.String(), "valid-token") {
		t.Fatalf("unexpected ping logs: %s", output.String())
	}
	if work := activeWorkCells(t, application.CellarStore()); len(work) != 0 {
		t.Fatalf("ping created application work: %v", work)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app did not stop")
	}
}

func TestLogEndpointEndToEnd(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/http-log.db"
	var output syncBuffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = logger
	cfg.HTTPAddr = "127.0.0.1:0"

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.Close()

	if a.HTTPAddr() == "" {
		t.Fatal("expected HTTP to be enabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- a.Run(ctx) }()

	postLog(t, a.HTTPAddr(), "evt-1", "hello world")
	waitForLogLine(t, &output, "hello world")

	// A duplicate event_id must be suppressed by idempotency, not logged twice.
	postLog(t, a.HTTPAddr(), "evt-1", "hello world")
	time.Sleep(100 * time.Millisecond)
	if count := strings.Count(output.String(), "hello world"); count != 1 {
		t.Fatalf("expected duplicate event_id to be suppressed, got %d log entries", count)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

func postLog(t *testing.T, addr, eventID, message string) {
	t.Helper()
	body := `{"event_id":"` + eventID + `","message":"` + message + `"}`
	resp, err := http.Post("http://"+addr+"/log", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post /log: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
}

func waitForLogLine(t *testing.T, output *syncBuffer, substr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), substr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log line containing %q, got: %s", substr, output.String())
}
