package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sweego_client/sweego"
)

type capturedRequest struct {
	Method string
	Path   string
	APIKey string
	Body   map[string]any
	Raw    []byte
}

// templateServer records the last request and replies with the supplied status
// and body.
func templateServer(t *testing.T, status int, responseBody string) (*sweego.Client, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.APIKey = r.Header.Get("Api-Key")
		captured.Raw = raw
		_ = json.Unmarshal(raw, &captured.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, responseBody)
	}))
	t.Cleanup(server.Close)
	return sweego.NewClient(server.URL, "test-token", 5*time.Second), captured
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const rawTemplateSource = "Hello {{name}}\n\nA & B <not-html> \"quoted\"\n\nYour pub night is on {{date}}.\n"

func TestTemplateUploadSubmitsFileContentVerbatim(t *testing.T) {
	client, captured := templateServer(t, http.StatusCreated, `{"uuid":"tpl-123"}`)
	path := writeTempFile(t, "template.txt", rawTemplateSource)

	if err := runTemplateUpload([]string{path}, client, "client-uuid"); err != nil {
		t.Fatal(err)
	}

	if captured.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", captured.Method)
	}
	if captured.Path != "/clients/client-uuid/channels/email/templates" {
		t.Fatalf("unexpected path: %s", captured.Path)
	}
	if captured.APIKey != "test-token" {
		t.Fatalf("expected Api-Key auth, got %q", captured.APIKey)
	}
	if captured.Body["template"] != rawTemplateSource {
		t.Fatalf("template content was not passed through unchanged: %q", captured.Body["template"])
	}
	if captured.Body["name"] != "template.txt" {
		t.Fatalf("expected the file base name as the default name, got %v", captured.Body["name"])
	}
}

func TestTemplateUploadHonoursExplicitName(t *testing.T) {
	client, captured := templateServer(t, http.StatusOK, `{"uuid":"tpl-123"}`)
	path := writeTempFile(t, "template.txt", rawTemplateSource)

	if err := runTemplateUpload([]string{"--name", "pubnight-invite", path}, client, "client-uuid"); err != nil {
		t.Fatal(err)
	}
	if captured.Body["name"] != "pubnight-invite" {
		t.Fatalf("unexpected name: %v", captured.Body["name"])
	}
}

func TestTemplateUUIDReadsNestedIdentifiers(t *testing.T) {
	for _, body := range []string{
		`{"uuid":"tpl-123"}`,
		`{"data":{"uuid_template":"tpl-123"}}`,
		`{"result":[{"id":"tpl-123"}]}`,
	} {
		uuid, err := sweego.TemplateUUID([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if uuid != "tpl-123" {
			t.Fatalf("%s: got %q", body, uuid)
		}
	}
	if _, err := sweego.TemplateUUID([]byte(`{"state":true}`)); err == nil {
		t.Fatal("expected an error when no uuid is present")
	}
}

func TestTemplateUploadReportsAPIFailure(t *testing.T) {
	client, _ := templateServer(t, http.StatusUnprocessableEntity, `{"error":["bad template"]}`)
	path := writeTempFile(t, "template.txt", rawTemplateSource)

	err := runTemplateUpload([]string{path}, client, "client-uuid")
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("expected a non-2xx error, got %v", err)
	}
}

func TestTemplateUploadRejectsMissingFile(t *testing.T) {
	client, _ := templateServer(t, http.StatusOK, `{"uuid":"tpl-123"}`)

	err := runTemplateUpload([]string{filepath.Join(t.TempDir(), "absent.txt")}, client, "client-uuid")
	if err == nil || !strings.Contains(err.Error(), "read template file") {
		t.Fatalf("expected a read error, got %v", err)
	}
}

func TestTemplateUploadRequiresExactlyOneFile(t *testing.T) {
	client, _ := templateServer(t, http.StatusOK, `{"uuid":"tpl-123"}`)

	if err := runTemplateUpload(nil, client, "client-uuid"); err == nil {
		t.Fatal("expected an error when no file is supplied")
	}
}

func TestTemplateUpdateUsesTemplateUUIDInPath(t *testing.T) {
	client, captured := templateServer(t, http.StatusOK, `{"uuid":"tpl-123"}`)
	path := writeTempFile(t, "replacement.txt", "Replacement {{name}}\n")

	if err := runTemplateUpdate([]string{"tpl-123", path}, client, "client-uuid"); err != nil {
		t.Fatal(err)
	}

	if captured.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", captured.Method)
	}
	if captured.Path != "/clients/client-uuid/channels/email/templates/tpl-123" {
		t.Fatalf("unexpected path: %s", captured.Path)
	}
	if captured.Body["template"] != "Replacement {{name}}\n" {
		t.Fatalf("replacement content not submitted verbatim: %q", captured.Body["template"])
	}
	if captured.Body["template_type"] != "email" {
		t.Fatalf("expected template_type email, got %v", captured.Body["template_type"])
	}
}

func TestTemplateUpdateRejectsBlankUUID(t *testing.T) {
	client, _ := templateServer(t, http.StatusOK, `{"uuid":"tpl-123"}`)
	path := writeTempFile(t, "replacement.txt", "body")

	if err := runTemplateUpdate([]string{"   ", path}, client, "client-uuid"); err == nil {
		t.Fatal("expected an error for a blank template uuid")
	}
}

func TestTemplateUpdateReportsAPIFailure(t *testing.T) {
	client, _ := templateServer(t, http.StatusNotFound, `{"error":["unknown template"]}`)
	path := writeTempFile(t, "replacement.txt", "body")

	err := runTemplateUpdate([]string{"tpl-123", path}, client, "client-uuid")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a non-2xx error, got %v", err)
	}
}

func TestTemplateDeleteIssuesDeleteWithoutBody(t *testing.T) {
	client, captured := templateServer(t, http.StatusOK, `{"detail":"deleted"}`)

	if err := runTemplateDelete([]string{"tpl-123"}, client, "client-uuid"); err != nil {
		t.Fatal(err)
	}

	if captured.Method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", captured.Method)
	}
	if captured.Path != "/clients/client-uuid/channels/email/templates/tpl-123" {
		t.Fatalf("unexpected path: %s", captured.Path)
	}
	if len(captured.Raw) != 0 {
		t.Fatalf("expected no request body, got %q", captured.Raw)
	}
}

func TestTemplateDeleteReportsAPIFailure(t *testing.T) {
	client, _ := templateServer(t, http.StatusNotFound, `{"detail":"Cannot found given resource"}`)

	err := runTemplateDelete([]string{"tpl-123"}, client, "client-uuid")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a non-2xx error, got %v", err)
	}
}

func TestTemplateDeleteRequiresUUID(t *testing.T) {
	client, _ := templateServer(t, http.StatusOK, `{}`)

	if err := runTemplateDelete(nil, client, "client-uuid"); err == nil {
		t.Fatal("expected an error when no uuid is supplied")
	}
}
