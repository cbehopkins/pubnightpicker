package sweego

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordedRequest struct {
	method      string
	path        string
	apiKey      string
	accept      string
	contentType string
	body        []byte
}

func newRecordingClient(t *testing.T) (*Client, *recordedRequest) {
	t.Helper()
	recorded := &recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.method = r.Method
		recorded.path = r.URL.EscapedPath()
		recorded.apiKey = r.Header.Get("Api-Key")
		recorded.accept = r.Header.Get("Accept")
		recorded.contentType = r.Header.Get("Content-Type")
		recorded.body, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Sweego-Test", "present")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"state":true}`)
	}))
	t.Cleanup(server.Close)
	return NewClient(server.URL, "test-token", 5*time.Second), recorded
}

func TestClientDoSendsAuthenticatedJSONAndPreservesResponse(t *testing.T) {
	client, recorded := newRecordingClient(t)

	result, err := client.Do(context.Background(), http.MethodPost, "/resource", map[string]string{"name": "value"})
	if err != nil {
		t.Fatal(err)
	}

	if recorded.method != http.MethodPost || recorded.path != "/resource" {
		t.Fatalf("unexpected request: %s %s", recorded.method, recorded.path)
	}
	if recorded.apiKey != "test-token" || recorded.accept != "application/json" || recorded.contentType != "application/json" {
		t.Fatalf("unexpected headers: Api-Key=%q Accept=%q Content-Type=%q", recorded.apiKey, recorded.accept, recorded.contentType)
	}
	if string(recorded.body) != `{"name":"value"}` {
		t.Fatalf("unexpected body: %s", recorded.body)
	}
	if result.Status != http.StatusAccepted || result.Headers.Get("X-Sweego-Test") != "present" || string(result.Body) != `{"state":true}` {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestClientDoWithNilPayloadSendsNoBodyOrContentType(t *testing.T) {
	client, recorded := newRecordingClient(t)

	if _, err := client.Do(context.Background(), http.MethodGet, "/resource", nil); err != nil {
		t.Fatal(err)
	}
	if len(recorded.body) != 0 || recorded.contentType != "" {
		t.Fatalf("nil payload sent body %q with Content-Type %q", recorded.body, recorded.contentType)
	}
}

func TestClientDoReportsMarshalAndRequestErrors(t *testing.T) {
	client, _ := newRecordingClient(t)

	if _, err := client.Do(context.Background(), http.MethodPost, "/resource", make(chan int)); err == nil || !strings.Contains(err.Error(), "marshal request") {
		t.Fatalf("expected marshal error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Do(ctx, http.MethodGet, "/resource", nil); err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("expected request error, got %v", err)
	}
}

func TestAPIWrapperRoutesAndBodies(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		call       func(context.Context, *Client) error
		assertBody func(*testing.T, []byte)
	}{
		{
			name: "send email", method: http.MethodPost, path: "/send",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SendEmail(ctx, SendEmailRequest{Channel: "email", MessageTxt: "hello", CampaignType: "transac", DryRun: true})
				return err
			},
			assertBody: assertJSONFields(map[string]any{"channel": "email", "message-txt": "hello", "campaign-type": "transac", "dry-run": true}),
		},
		{
			name: "send bulk email", method: http.MethodPost, path: "/send/bulk/email",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.SendBulkEmail(ctx, BulkEmailRequest{Channel: "email", TemplateID: "tpl-1", DryRun: true, Recipients: []BulkRecipient{{Email: "alice@example.com", Variables: map[string]any{"name": "Alice"}}}})
				return err
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				var value map[string]any
				if err := json.Unmarshal(body, &value); err != nil {
					t.Fatal(err)
				}
				if value["template-id"] != "tpl-1" || value["dry-run"] != true {
					t.Fatalf("unexpected bulk fields: %v", value)
				}
				recipients := value["recipients"].([]any)
				variables := recipients[0].(map[string]any)["variables"].(map[string]any)
				if variables["name"] != "Alice" {
					t.Fatalf("unexpected recipient variables: %v", variables)
				}
			},
		},
		{
			name: "create template", method: http.MethodPost, path: "/clients/client%2Fid/channels/email/templates",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.CreateTemplate(ctx, "client/id", CreateTemplateRequest{Name: "invite", Template: "body"})
				return err
			},
			assertBody: assertJSONFields(map[string]any{"name": "invite", "template": "body"}),
		},
		{
			name: "update template", method: http.MethodPost, path: "/clients/client%2Fid/channels/email/templates/template%2Fid",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.UpdateTemplate(ctx, "client/id", "template/id", UpdateTemplateRequest{Name: "invite", Template: "body", TemplateType: "email"})
				return err
			},
			assertBody: assertJSONFields(map[string]any{"name": "invite", "template": "body", "template_type": "email"}),
		},
		{
			name: "get template", method: http.MethodGet, path: "/clients/client%2Fid/channels/email/templates/template%2Fid",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetTemplate(ctx, "client/id", "template/id")
				return err
			},
		},
		{
			name: "delete template", method: http.MethodDelete, path: "/clients/client%2Fid/channels/email/templates/template%2Fid",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.DeleteTemplate(ctx, "client/id", "template/id")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, recorded := newRecordingClient(t)
			if err := test.call(context.Background(), client); err != nil {
				t.Fatal(err)
			}
			if recorded.method != test.method || recorded.path != test.path {
				t.Fatalf("got %s %s, want %s %s", recorded.method, recorded.path, test.method, test.path)
			}
			if test.assertBody == nil {
				if len(recorded.body) != 0 {
					t.Fatalf("expected no body, got %s", recorded.body)
				}
				return
			}
			test.assertBody(t, recorded.body)
		})
	}
}

func assertJSONFields(want map[string]any) func(*testing.T, []byte) {
	return func(t *testing.T, body []byte) {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		for key, value := range want {
			if got[key] != value {
				t.Fatalf("field %q = %v, want %v; body=%v", key, got[key], value, got)
			}
		}
	}
}

func TestTemplateUUIDUsesVerifiedTopLevelField(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{name: "top-level uuid", body: `{"uuid":"tpl-1"}`, want: "tpl-1"},
		{name: "empty uuid", body: `{"uuid":""}`, wantErr: "no template uuid found"},
		{name: "nested alternative", body: `{"data":{"uuid_template":"tpl-1"}}`, wantErr: "no template uuid found"},
		{name: "generic id", body: `{"id":"tpl-2"}`, wantErr: "no template uuid found"},
		{name: "non-string uuid", body: `{"uuid":42}`, wantErr: "decode template response JSON"},
		{name: "malformed JSON", body: `{`, wantErr: "decode template response JSON"},
		{name: "uuid absent", body: `{"state":true}`, wantErr: "no template uuid found"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TemplateUUID([]byte(test.body))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("got %q, %v; want error containing %q", got, err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
}
