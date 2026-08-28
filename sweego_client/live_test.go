package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"sweego_client/sweego"
)

// These tests exercise the real Sweego API and are skipped unless
// SWEEGO_LIVE_TESTS is set. They exist to re-check the provider behaviour
// recorded in the README, including the negative findings: if Sweego ever
// starts accepting plain-text templates, TestLivePlainTextTemplateCannotSend
// fails and the documented conclusion needs revisiting.
//
// Sweego caps the number of stored templates (observed: 5 on this plan, with a
// 429 beyond it), so every template created here is deleted on cleanup.
//
// Required: SWEEGO_LIVE_TESTS, SWEEGO_TOKEN, SWEEGO_PROVIDER, SWEEGO_CLIENT_UUID,
// SWEEGO_LIVE_FROM, SWEEGO_LIVE_TO.
// Optional: SWEEGO_LIVE_UI_TEMPLATE_UUID, a template built in the Sweego UI.
//
// Every send uses dry-run, so no email is delivered.

const livePlainTextTemplate = "Hello {{name}}\n\nYour pub night is on {{date}}.\n"

type liveEnv struct {
	client     *sweego.Client
	clientUUID string
	provider   string
	from       sweego.EmailAddress
	to         sweego.BulkRecipient
}

func requireLive(t *testing.T) liveEnv {
	t.Helper()
	if os.Getenv("SWEEGO_LIVE_TESTS") == "" {
		t.Skip("set SWEEGO_LIVE_TESTS to run tests against the real Sweego API")
	}

	values := map[string]string{}
	for _, name := range []string{"SWEEGO_TOKEN", "SWEEGO_PROVIDER", "SWEEGO_CLIENT_UUID", "SWEEGO_LIVE_FROM", "SWEEGO_LIVE_TO"} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			t.Fatalf("%s is required when SWEEGO_LIVE_TESTS is set", name)
		}
		values[name] = value
	}

	from, err := parseAddress(values["SWEEGO_LIVE_FROM"])
	if err != nil {
		t.Fatalf("invalid SWEEGO_LIVE_FROM: %v", err)
	}
	to, err := parseAddress(values["SWEEGO_LIVE_TO"])
	if err != nil {
		t.Fatalf("invalid SWEEGO_LIVE_TO: %v", err)
	}

	baseURL := strings.TrimSpace(os.Getenv("SWEEGO_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return liveEnv{
		client:     sweego.NewClient(strings.TrimRight(baseURL, "/"), values["SWEEGO_TOKEN"], 30*time.Second),
		clientUUID: values["SWEEGO_CLIENT_UUID"],
		provider:   values["SWEEGO_PROVIDER"],
		from:       from,
		to: sweego.BulkRecipient{
			Email:     to.Email,
			Name:      to.Name,
			Variables: map[string]any{"name": "Live Test", "date": "Friday"},
		},
	}
}

// dryRunSend returns the HTTP status of a dry-run bulk send using the supplied
// content source.
func (env liveEnv) dryRunSend(t *testing.T, templateID, messageTxt string) (int, []byte) {
	t.Helper()
	response, err := env.client.SendBulkEmail(context.Background(), sweego.BulkEmailRequest{
		Channel:      "email",
		From:         env.from,
		Provider:     env.provider,
		Subject:      "Live behaviour check",
		Recipients:   []sweego.BulkRecipient{env.to},
		MessageTxt:   messageTxt,
		CampaignType: "transac",
		TemplateID:   templateID,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("bulk send request failed: %v", err)
	}
	return response.Status, response.Body
}

func uploadLiveTemplate(t *testing.T, env liveEnv, label, content string) string {
	t.Helper()
	name := fmt.Sprintf("live-%s-%d", label, time.Now().UnixNano())
	response, err := env.client.CreateTemplate(context.Background(), env.clientUUID, sweego.CreateTemplateRequest{
		Name:     name,
		Template: content,
	})
	if err != nil {
		t.Fatalf("create template request failed: %v", err)
	}
	if response.Status != 201 {
		t.Fatalf("expected 201 Created, got %d: %s", response.Status, response.Body)
	}
	uuid, err := sweego.TemplateUUID(response.Body)
	if err != nil {
		t.Fatalf("no template uuid in response %s: %v", response.Body, err)
	}

	// Sweego caps stored templates per plan, so leaving these behind wedges
	// later runs with a 429.
	t.Cleanup(func() {
		deleted, err := env.client.DeleteTemplate(context.Background(), env.clientUUID, uuid)
		if err != nil {
			t.Logf("cleanup: delete template %s failed: %v", uuid, err)
			return
		}
		// 404 means a test deleted it already.
		if deleted.Status == 404 {
			return
		}
		if deleted.Status < 200 || deleted.Status >= 300 {
			t.Logf("cleanup: delete template %s returned %d: %s", uuid, deleted.Status, deleted.Body)
		}
	})

	return uuid
}

func readLiveFixture(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func TestLiveTemplateUploadStoresPlainTextUnchanged(t *testing.T) {
	env := requireLive(t)
	uuid := uploadLiveTemplate(t, env, "plaintext", livePlainTextTemplate)

	response, err := env.client.GetTemplate(context.Background(), env.clientUUID, uuid)
	if err != nil {
		t.Fatalf("get template request failed: %v", err)
	}
	if response.Status != 200 {
		t.Fatalf("expected 200, got %d: %s", response.Status, response.Body)
	}

	var stored struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal(response.Body, &stored); err != nil {
		t.Fatalf("decode stored template: %v", err)
	}
	if stored.Template != livePlainTextTemplate {
		t.Fatalf("Sweego altered the stored template.\n want: %q\n  got: %q", livePlainTextTemplate, stored.Template)
	}
}

// The load-bearing negative finding: plain text is stored happily but cannot be
// rendered at send time, because Sweego's template field expects a serialised
// visual-editor document.
func TestLivePlainTextTemplateCannotSend(t *testing.T) {
	env := requireLive(t)
	uuid := uploadLiveTemplate(t, env, "plaintext", livePlainTextTemplate)

	status, body := env.dryRunSend(t, uuid, "")
	if status != 500 {
		t.Fatalf("Sweego no longer rejects a plain-text template (got %d: %s).\n"+
			"If this is now a success, plain-text templates may be supported and the README conclusion is stale.", status, body)
	}
}

// Raw HTML fails the same way, which shows the constraint is the document
// format rather than plain text specifically.
func TestLiveRawHTMLTemplateCannotSend(t *testing.T) {
	env := requireLive(t)
	uuid := uploadLiveTemplate(t, env, "rawhtml", readLiveFixture(t, "template.html"))

	status, body := env.dryRunSend(t, uuid, "")
	if status != 500 {
		t.Fatalf("expected 500 for a raw HTML template, got %d: %s", status, body)
	}
}

// Templating working end to end: an editor-document template uploaded from a
// file sends successfully.
func TestLiveEditorDocumentTemplateSends(t *testing.T) {
	env := requireLive(t)
	uuid := uploadLiveTemplate(t, env, "htmldoc", readLiveFixture(t, "template_document.json"))

	if status, body := env.dryRunSend(t, uuid, ""); status != 200 {
		t.Fatalf("expected 200 for an editor-document template, got %d: %s", status, body)
	}
}

// Isolates the cause: the same request succeeds when the only change is a
// template UUID pointing at a UI-built editor document.
func TestLiveUIBuiltTemplateSends(t *testing.T) {
	env := requireLive(t)
	uiTemplate := strings.TrimSpace(os.Getenv("SWEEGO_LIVE_UI_TEMPLATE_UUID"))
	if uiTemplate == "" {
		t.Skip("set SWEEGO_LIVE_UI_TEMPLATE_UUID to a template built in the Sweego UI")
	}

	if status, body := env.dryRunSend(t, uiTemplate, ""); status != 200 {
		t.Fatalf("expected 200 for a UI-built template, got %d: %s", status, body)
	}
}

// The route that actually delivers text/plain.
func TestLiveMessageTxtSends(t *testing.T) {
	env := requireLive(t)

	if status, body := env.dryRunSend(t, "", livePlainTextTemplate); status != 200 {
		t.Fatalf("expected 200 for a message-txt send, got %d: %s", status, body)
	}
}

// Templates are capped per plan, so the client must be able to remove them.
func TestLiveTemplateDelete(t *testing.T) {
	env := requireLive(t)
	uuid := uploadLiveTemplate(t, env, "deleteprobe", livePlainTextTemplate)

	deleted, err := env.client.DeleteTemplate(context.Background(), env.clientUUID, uuid)
	if err != nil {
		t.Fatalf("delete template request failed: %v", err)
	}
	if deleted.Status < 200 || deleted.Status >= 300 {
		t.Fatalf("expected a 2xx delete, got %d: %s", deleted.Status, deleted.Body)
	}

	after, err := env.client.GetTemplate(context.Background(), env.clientUUID, uuid)
	if err != nil {
		t.Fatalf("get template request failed: %v", err)
	}
	if after.Status != 404 {
		t.Fatalf("expected 404 after delete, got %d: %s", after.Status, after.Body)
	}
}

// Sweego requires a subject even when a template supplies the content.
func TestLiveSubjectIsRequired(t *testing.T) {
	env := requireLive(t)

	response, err := env.client.SendBulkEmail(context.Background(), sweego.BulkEmailRequest{
		Channel:      "email",
		From:         env.from,
		Provider:     env.provider,
		Recipients:   []sweego.BulkRecipient{env.to},
		MessageTxt:   livePlainTextTemplate,
		CampaignType: "transac",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("bulk send request failed: %v", err)
	}
	if response.Status != 422 {
		t.Fatalf("expected 422 for a missing subject, got %d: %s", response.Status, response.Body)
	}
}
