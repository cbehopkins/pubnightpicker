package mailtrapcli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"email_clients/clients/mailtrap"

	sdk "github.com/mailtrap/mailtrap-go"
)

func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MAILTRAP_TOKEN", "test-token")
	t.Setenv("MAILTRAP_USE_SANDBOX", "")
	t.Setenv("MAILTRAP_SANDBOX_ID", "")
}

func testFactory(t *testing.T, handler http.HandlerFunc) clientFactory {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return func(cfg config) (*mailtrap.Client, error) {
		options := []sdk.Option{sdk.WithHTTPClient(&http.Client{Timeout: time.Second}), sdk.WithSandbox(cfg.Sandbox)}
		if cfg.SandboxID != 0 {
			options = append(options, sdk.WithSandboxID(cfg.SandboxID))
		}
		for _, host := range []sdk.Host{sdk.HostSend, sdk.HostSandbox, sdk.HostGeneral} {
			options = append(options, sdk.WithBaseURL(host, server.URL))
		}
		client, err := sdk.NewClient(cfg.Token, options...)
		if err != nil {
			return nil, err
		}
		return mailtrap.NewClient(client)
	}
}

func TestHelpWithoutCredentials(t *testing.T) {
	t.Setenv("MAILTRAP_TOKEN", "")
	for _, args := range [][]string{{"--help"}, {"help"}, {"send", "--help"}, {"batch-send-json", "--help"}, {"verify", "--help"}} {
		var out, errOut bytes.Buffer
		factory := func(config) (*mailtrap.Client, error) { t.Fatal("help created a client"); return nil, nil }
		if code := run(args, &out, &errOut, factory); code != 0 {
			t.Fatalf("args = %v, code = %d, stderr = %s", args, code, errOut.String())
		}
	}
}

func TestConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, token, sandbox, id string
		valid                    bool
	}{
		{"production", " token ", "", "", true},
		{"sandbox", "token", "true", "42", true},
		{"ignored ID", "token", "false", "42", true},
		{"missing token", " ", "", "", false},
		{"bad boolean", "token", "maybe", "", false},
		{"missing ID", "token", "true", "", false},
		{"bad ID", "token", "true", "abc", false},
		{"zero ID", "token", "true", "0", false},
		{"negative ID", "token", "true", "-1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MAILTRAP_TOKEN", test.token)
			t.Setenv("MAILTRAP_USE_SANDBOX", test.sandbox)
			t.Setenv("MAILTRAP_SANDBOX_ID", test.id)
			cfg, err := loadConfigFromEnv()
			if (err == nil) != test.valid {
				t.Fatalf("cfg = %+v, err = %v", cfg, err)
			}
			if err == nil {
				if cfg.Token != "token" {
					t.Fatalf("token not trimmed")
				}
				if _, err := newClient(cfg); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSendAndPartialBatchOutput(t *testing.T) {
	testEnv(t)
	var captured sdk.BatchSendRequest
	var calls atomic.Int32
	factory := testFactory(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/batch" {
			t.Error(r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["accepted"]},{"success":false,"errors":["refused"]}]}`)
	})
	var out, errOut bytes.Buffer
	code := run([]string{"send", "--from", "Sender <sender@example.com>", "--to", "Alice <alice@example.com>,bob@example.com", "--subject", "Hi {{name}}", "--text", "Hello {{name}}", "--variables", `{"name":"Guest"}`, "--category", "Pub notification", "--message-id", "pn-test"}, &out, &errOut, factory)
	if code != 1 || calls.Load() != 1 || !strings.Contains(out.String(), "PMUID: accepted") || !strings.Contains(out.String(), "no confirmed PMUID") || !strings.Contains(errOut.String(), "recipient 1") {
		t.Fatalf("code = %d, calls = %d, stdout = %s, stderr = %s", code, calls.Load(), out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Correlation ID: pn-test") || !strings.Contains(out.String(), "Sent at:") {
		t.Fatal(out.String())
	}
	if captured.Requests[0].Category != "Pub notification" || captured.Requests[0].From.Name != "Sender" || captured.Requests[0].To[0].Name != "Alice" || captured.Requests[0].Text != "Hello Guest" {
		t.Fatalf("request = %+v", captured)
	}
}

func TestSendSingleAndSandbox(t *testing.T) {
	testEnv(t)
	t.Setenv("MAILTRAP_USE_SANDBOX", "true")
	t.Setenv("MAILTRAP_SANDBOX_ID", "42")
	factory := testFactory(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/send/42" {
			t.Error(r.URL.Path)
		}
		var request sdk.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.CustomVariables[mailtrap.CorrelationVariable] == "" {
			t.Error("missing generated correlation")
		}
		_, _ = io.WriteString(w, `{"success":true,"message_ids":["captured"]}`)
	})
	var out, errOut bytes.Buffer
	if code := run([]string{"send", "--from", "sender@example.com", "--to", "alice@example.com", "--subject", "Subject", "--text", "Body"}, &out, &errOut, factory); code != 0 || !strings.Contains(out.String(), "Sandbox mode") {
		t.Fatalf("code = %d, out = %s, err = %s", code, out.String(), errOut.String())
	}
}

func writeDocument(t *testing.T, document string) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "body.txt"), []byte("Hello {{ name }} on {{date}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "targets.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validDocument = `{"from":"Sender <sender@example.com>","subject":"Hello {{name}}","body":"body.txt","variables":{"date":"Friday","name":"Guest"},"targets":[{"dest":"Alice <alice@example.com>","vars":{"name":"Alice"}},{"dest":"bob@example.com","vars":{"name":"Bob"}}]}`

func TestBatchDocumentRelativeBodyAndVariables(t *testing.T) {
	testEnv(t)
	path := writeDocument(t, validDocument)
	var captured sdk.BatchSendRequest
	factory := testFactory(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/batch" {
			t.Error(r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`)
	})
	var out, errOut bytes.Buffer
	code := run([]string{"batch-send-json", "--category", "Test email", "--from", "Override <override@example.com>", path}, &out, &errOut, factory)
	if code != 0 {
		t.Fatalf("code = %d, err = %s", code, errOut.String())
	}
	for index, name := range []string{"Alice", "Bob"} {
		request := captured.Requests[index]
		if request.Text != "Hello "+name+" on Friday\n" || request.Subject != "Hello "+name || request.Category != "Test email" || request.From.Email != "override@example.com" {
			t.Errorf("request = %+v", request)
		}
	}
}

func TestCheckedInBatchExample(t *testing.T) {
	testEnv(t)
	var captured sdk.BatchSendRequest
	factory := testFactory(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`)
	})
	var out, errOut bytes.Buffer
	path := filepath.Join("..", "..", "examples", "mailtrap_bulk_request.json")
	if code := run([]string{"batch-send-json", path}, &out, &errOut, factory); code != 0 {
		t.Fatalf("code = %d, err = %s", code, errOut.String())
	}
	if len(captured.Requests) != 2 {
		t.Fatalf("example message count = %d", len(captured.Requests))
	}
	for index, name := range []string{"Alice", "Bob"} {
		request := captured.Requests[index]
		if request.Subject != "Pub night for "+name || !strings.Contains(request.Text, "Hello "+name+"\n") || !strings.Contains(request.Text, "Friday") {
			t.Errorf("example message = %+v", request)
		}
	}
}

func TestBatchDocumentValidation(t *testing.T) {
	for _, document := range []string{
		`{`, `null`, validDocument + `{}`, validDocument + " garbage",
		strings.Replace(validDocument, `"body":"body.txt"`, `"body":"body.txt","unknown":1`, 1),
		strings.Replace(validDocument, `"body":"body.txt"`, `"body":"body.txt","template":"uuid"`, 1),
		strings.Replace(validDocument, `"body":"body.txt",`, "", 1),
		strings.Replace(validDocument, `"body":"body.txt"`, `"body":"missing.txt"`, 1),
		strings.Replace(validDocument, `"subject":"Hello {{name}}"`, `"subject":""`, 1),
		strings.Replace(validDocument, `"body":"body.txt"`, `"template":"uuid"`, 1),
		strings.Replace(validDocument, "alice@example.com", "invalid", 1),
		strings.Replace(validDocument, `"vars":{"name":"Alice"}`, `"vars":{"name":"Alice"},"unknown":true`, 1),
	} {
		path := writeDocument(t, document)
		if _, err := loadBatchDocument(path, ""); err == nil {
			t.Errorf("accepted invalid document: %s", document)
		}
	}
	path := writeDocument(t, `{"from":"sender@example.com","template":"uuid","variables":{"date":"Friday"},"targets":[{"dest":"alice@example.com","vars":{"name":"Alice"}}]}`)
	if email, err := loadBatchDocument(path, ""); err != nil || email.TemplateID != "uuid" || email.Text != "" || email.Subject != "" {
		t.Fatalf("email = %+v, err = %v", email, err)
	}
}

func TestEmptyTargetsNeverCreatesClient(t *testing.T) {
	t.Setenv("MAILTRAP_TOKEN", "")
	path := writeDocument(t, `{"from":"sender@example.com","body":"body.txt","subject":"Hello","targets":[]}`)
	factory := func(config) (*mailtrap.Client, error) { t.Fatal("empty targets created client"); return nil, nil }
	var out, errOut bytes.Buffer
	if code := run([]string{"batch-send-json", path}, &out, &errOut, factory); code != 0 || !strings.Contains(out.String(), "nothing to send") {
		t.Fatalf("code = %d, err = %s", code, errOut.String())
	}
}

func TestVerifyFoundAbsentAndFailure(t *testing.T) {
	testEnv(t)
	for _, test := range []struct {
		body         string
		status, code int
	}{
		{`{"messages":[{"message_id":"verified","to":"alice@example.com","custom_variables":{"correlation_id":"pn-1"}}]}`, 200, 0},
		{`{"messages":[]}`, 200, 1},
		{`{"error":"forbidden"}`, 403, 1},
	} {
		factory := testFactory(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.status)
			_, _ = io.WriteString(w, test.body)
		})
		var out, errOut bytes.Buffer
		code := run([]string{"verify", "--to", "alice@example.com", "--message-id", "pn-1", "--sent-at", "2026-10-02T12:00:00Z"}, &out, &errOut, factory)
		if code != test.code {
			t.Fatalf("code = %d, out = %s, err = %s", code, out.String(), errOut.String())
		}
		if code == 0 && !strings.Contains(out.String(), "PMUID: verified") {
			t.Fatal(out.String())
		}
	}
}

func TestCLIInvalidCommandsNeverSend(t *testing.T) {
	testEnv(t)
	var calls atomic.Int32
	factory := testFactory(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, args := range [][]string{
		nil, {"unknown"}, {"send"}, {"send", "--unknown"}, {"send", "unexpected"},
		{"send", "--from", "sender@example.com", "--to", "invalid"},
		{"send", "--from", "sender@example.com", "--to", "alice@example.com", "--subject", "Subject", "--text", "Body", "--variables", "[]"},
		{"send", "--from", "sender@example.com", "--to", "alice@example.com", "--subject", "Subject", "--template-id", "uuid"},
		{"batch-send-json"}, {"verify"}, {"verify", "--to", "alice@example.com", "--message-id", "pn-1"},
		{"verify", "--to", "alice@example.com", "--message-id", "pn-1", "--sent-at", "2026-10-02T12:00:00Z", "--verify-tolerance", "-1m"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut, factory); code == 0 || calls.Load() != 0 {
			t.Fatalf("args = %v, code = %d, calls = %d", args, code, calls.Load())
		}
	}
	t.Setenv("MAILTRAP_USE_SANDBOX", "true")
	t.Setenv("MAILTRAP_SANDBOX_ID", "42")
	var out, errOut bytes.Buffer
	if code := run([]string{"verify", "--to", "alice@example.com", "--message-id", "pn-1", "--sent-at", "2026-10-02T12:00:00Z"}, &out, &errOut, factory); code == 0 || calls.Load() != 0 || !strings.Contains(errOut.String(), "not available in sandbox") {
		t.Fatalf("code = %d, calls = %d, err = %s", code, calls.Load(), errOut.String())
	}
}
