package main

import (
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

	"sweego_client/sweego"
)

// bulkTemplateServer answers the bulk send and log endpoints and records both
// the decoded bulk request and the total number of requests received.
func bulkTemplateServer(t *testing.T, status int, sendBody string) (*sweego.Client, *map[string]any, *atomic.Int32) {
	t.Helper()
	var body map[string]any
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/send/bulk/email":
			_ = json.Unmarshal(raw, &body)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, sendBody)
		default:
			_, _ = io.WriteString(w, `{"nb_result":0,"result":[],"state":true}`)
		}
	}))
	t.Cleanup(server.Close)
	return sweego.NewClient(server.URL, "test-token", 5*time.Second), &body, &calls
}

const twoTargetDocument = `{
  "template": "tpl-123",
  "subject": "Pub night",
  "from": "Me <sender@example.com>",
  "targets": [
    {"dest": "alice@example.com", "vars": {"name": "Alice", "date": "Friday"}},
    {"dest": "Bob <bob@example.com>", "vars": {"name": "Bob"}}
  ]
}`

func quickRecoveryArgs(path string) []string {
	return []string{"--attempts", "1", "--retry-delay", "0s", path}
}

func TestBulkSendDocumentMapsTemplateOntoSweegoRequest(t *testing.T) {
	client, body, _ := bulkTemplateServer(t, http.StatusOK, `{"transaction_id":"T1","swg_uids":{"alice@example.com":"U1"}}`)
	path := writeTempFile(t, "targets.json", twoTargetDocument)

	if err := runBulkSendDocument(quickRecoveryArgs(path), client, "example.com"); err != nil {
		t.Fatal(err)
	}

	if (*body)["template-id"] != "tpl-123" {
		t.Fatalf("expected the document uuid in template-id, got %v", (*body)["template-id"])
	}
	if _, present := (*body)["message-txt"]; present {
		t.Fatal("message-txt must not be set on a template send")
	}
	if (*body)["subject"] != "Pub night" {
		t.Fatalf("unexpected subject: %v", (*body)["subject"])
	}

	recipients, ok := (*body)["recipients"].([]any)
	if !ok || len(recipients) != 2 {
		t.Fatalf("expected two recipients, got %v", (*body)["recipients"])
	}
	first := recipients[0].(map[string]any)
	if first["email"] != "alice@example.com" {
		t.Fatalf("dest was not mapped to email: %v", first["email"])
	}
	variables, ok := first["variables"].(map[string]any)
	if !ok || variables["name"] != "Alice" || variables["date"] != "Friday" {
		t.Fatalf("per-recipient vars were not mapped to variables: %v", first["variables"])
	}
	second := recipients[1].(map[string]any)
	if second["email"] != "bob@example.com" || second["name"] != "Bob" {
		t.Fatalf("display name was not preserved: %v", second)
	}
}

func TestBulkSendDocumentWithSingleTarget(t *testing.T) {
	client, body, _ := bulkTemplateServer(t, http.StatusOK, `{"transaction_id":"T1"}`)
	path := writeTempFile(t, "targets.json", `{"template":"tpl-123","subject":"S","from":"sender@example.com","targets":[{"dest":"alice@example.com"}]}`)

	if err := runBulkSendDocument(quickRecoveryArgs(path), client, "example.com"); err != nil {
		t.Fatal(err)
	}
	if recipients := (*body)["recipients"].([]any); len(recipients) != 1 {
		t.Fatalf("expected one recipient, got %d", len(recipients))
	}
}

// The plain-text route: "body" names a text file whose contents become
// message-txt, with no template involved.
func TestBulkSendDocumentWithBodyFileSendsMessageTxt(t *testing.T) {
	client, body, _ := bulkTemplateServer(t, http.StatusOK, `{"transaction_id":"T1"}`)
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "body.txt")
	if err := os.WriteFile(bodyPath, []byte(rawTemplateSource), 0o600); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(docPath, []byte(`{"body":"body.txt","subject":"S","from":"sender@example.com","targets":[{"dest":"alice@example.com","vars":{"name":"Alice"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runBulkSendDocument(quickRecoveryArgs(docPath), client, "example.com"); err != nil {
		t.Fatal(err)
	}

	if (*body)["message-txt"] != rawTemplateSource {
		t.Fatalf("body file was not passed through unchanged: %q", (*body)["message-txt"])
	}
	if _, present := (*body)["template-id"]; present {
		t.Fatal("template-id must not be set when a body file is used")
	}
	first := (*body)["recipients"].([]any)[0].(map[string]any)
	if first["variables"].(map[string]any)["name"] != "Alice" {
		t.Fatalf("per-recipient vars lost on the body path: %v", first)
	}
}

func TestBulkSendDocumentWithNoTargetsMakesNoRequest(t *testing.T) {
	client, _, calls := bulkTemplateServer(t, http.StatusOK, `{}`)
	path := writeTempFile(t, "targets.json", `{"template":"tpl-123","subject":"S","from":"sender@example.com","targets":[]}`)

	if err := runBulkSendDocument(quickRecoveryArgs(path), client, "example.com"); err != nil {
		t.Fatalf("an empty target list is a no-op, not an error: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected no HTTP calls, got %d", calls.Load())
	}
}

func TestBulkSendDocumentRejectsInvalidDocuments(t *testing.T) {
	cases := map[string]struct {
		document string
		contains string
	}{
		"malformed JSON":       {`{"template":`, "decode targets JSON"},
		"no content source":    {`{"subject":"S","from":"sender@example.com","targets":[{"dest":"alice@example.com"}]}`, `one of "template"`},
		"both content sources": {`{"template":"tpl-123","body":"body.txt","subject":"S","from":"s@example.com","targets":[]}`, "mutually exclusive"},
		"missing subject":      {`{"template":"tpl-123","from":"sender@example.com","targets":[]}`, `"subject" is required`},
		"missing body file":    {`{"body":"absent.txt","subject":"S","from":"s@example.com","targets":[]}`, "read body file"},
		"invalid dest":         {`{"template":"tpl-123","subject":"S","from":"sender@example.com","targets":[{"dest":"not-an-email"}]}`, "invalid target 0 dest"},
		"missing sender":       {`{"template":"tpl-123","subject":"S","targets":[{"dest":"alice@example.com"}]}`, "a sender is required"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			client, _, calls := bulkTemplateServer(t, http.StatusOK, `{}`)
			path := writeTempFile(t, "targets.json", testCase.document)

			err := runBulkSendDocument(quickRecoveryArgs(path), client, "example.com")
			if err == nil || !strings.Contains(err.Error(), testCase.contains) {
				t.Fatalf("expected an error containing %q, got %v", testCase.contains, err)
			}
			if calls.Load() != 0 {
				t.Fatal("validation must fail before any HTTP call")
			}
		})
	}
}

func TestBulkSendDocumentReportsAPIFailure(t *testing.T) {
	client, _, _ := bulkTemplateServer(t, http.StatusBadRequest, `{"error":["unknown template"]}`)
	path := writeTempFile(t, "targets.json", twoTargetDocument)

	err := runBulkSendDocument(quickRecoveryArgs(path), client, "example.com")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected a non-2xx error, got %v", err)
	}
}

func TestBulkSendDocumentFromFlagOverridesDocument(t *testing.T) {
	client, body, _ := bulkTemplateServer(t, http.StatusOK, `{"transaction_id":"T1"}`)
	path := writeTempFile(t, "targets.json", twoTargetDocument)

	args := append([]string{"--from", "override@example.com"}, quickRecoveryArgs(path)...)
	if err := runBulkSendDocument(args, client, "example.com"); err != nil {
		t.Fatal(err)
	}
	from := (*body)["from"].(map[string]any)
	if from["email"] != "override@example.com" {
		t.Fatalf("unexpected sender: %v", from)
	}
}

func TestBulkSendDocumentAcceptsTrailingFlags(t *testing.T) {
	client, body, _ := bulkTemplateServer(t, http.StatusOK, `{"transaction_id":"T1"}`)
	path := writeTempFile(t, "targets.json", twoTargetDocument)

	args := []string{path, "--dry-run", "--attempts", "1", "--retry-delay", "0s"}
	if err := runBulkSendDocument(args, client, "example.com"); err != nil {
		t.Fatal(err)
	}
	if (*body)["dry-run"] != true {
		t.Fatal("flags after the positional argument were ignored")
	}
}

// The hyphenated names come from Sweego's documented request sample; snake case
// variants are silently ignored by the provider.
func TestBulkEmailRequestUsesDocumentedFieldNames(t *testing.T) {
	raw, err := json.Marshal(sweego.BulkEmailRequest{
		Channel:    "email",
		TemplateID: "tpl-123",
		DryRun:     true,
		Recipients: []sweego.BulkRecipient{{Email: "alice@example.com", Variables: map[string]any{"name": "Alice"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"template-id":"tpl-123"`, `"dry-run":true`, `"variables":{"name":"Alice"}`} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("expected %s in %s", expected, raw)
		}
	}
}
