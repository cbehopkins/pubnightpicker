package mailtrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"email_clients/clients"

	sdk "github.com/mailtrap/mailtrap-go"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, options ...sdk.Option) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options = append(options, sdk.WithHTTPClient(&http.Client{Timeout: time.Second}))
	for _, host := range []sdk.Host{sdk.HostSend, sdk.HostBulk, sdk.HostSandbox, sdk.HostGeneral} {
		options = append(options, sdk.WithBaseURL(host, server.URL))
	}
	client, err := sdk.NewClient("test-token", options...)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testEmail(count int) clients.Email {
	recipients := make([]clients.Recipient, count)
	for index := range recipients {
		recipients[index] = clients.Recipient{Address: clients.Address{Email: "same@example.com", Name: "Guest"}}
	}
	return clients.Email{
		From: clients.Address{Email: "sender@example.com", Name: "Sender"},
		To:   recipients, Subject: "Hello", Text: "Plain text",
	}
}

func TestSendRoutingAndOneRequest(t *testing.T) {
	for _, count := range []int{1, 2, 500} {
		t.Run(stringCount(count), func(t *testing.T) {
			var calls atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("request = %s, auth = %q", r.Method, r.Header.Get("Authorization"))
				}
				if count == 1 {
					if r.URL.Path != "/api/send" {
						t.Errorf("path = %s", r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(sdk.SendResponse{Success: true, MessageIDs: []string{"uid-0"}})
					return
				}
				if r.URL.Path != "/api/batch" {
					t.Errorf("path = %s", r.URL.Path)
				}
				var request sdk.BatchSendRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Requests) != count {
					t.Errorf("batch request count = %d, err = %v", len(request.Requests), err)
				}
				response := sdk.BatchSendResponse{Success: true}
				for index, message := range request.Requests {
					if len(message.To) != 1 || len(message.Cc)+len(message.Bcc) != 0 {
						t.Errorf("recipient privacy = %+v", message)
					}
					response.Responses = append(response.Responses, sdk.BatchSendResponseItem{Success: true, MessageIDs: []string{"uid-" + stringCount(index)}})
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			result, err := client.Send(context.Background(), testEmail(count))
			if err != nil || len(result.Recipients) != count || calls.Load() != 1 {
				t.Fatalf("result = %+v, err = %v, calls = %d", result, err, calls.Load())
			}
			for index, recipient := range result.Recipients {
				if recipient.PMUID != "uid-"+stringCount(index) {
					t.Errorf("result[%d] = %q", index, recipient.PMUID)
				}
			}
		})
	}
}

func stringCount(value int) string {
	return fmt.Sprint(value)
}

func TestSendPersonalisationAndImmutableOptions(t *testing.T) {
	var captured sdk.BatchSendRequest
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`)
	})
	email := testEmail(2)
	email.Subject = "Hi {{name}}"
	email.Text = "{{ name }}: {{event}} & <literal>"
	email.Variables = map[string]any{"name": "Guest", "event": "Friday"}
	email.To[0].Variables = map[string]any{"name": "Alice"}
	email.Headers = map[string]string{strings.ToLower(clients.CorrelationHeader): "pn-1", "X-Custom": "value"}
	before := maps.Clone(email.Variables)
	configured := client.WithSendOptions(SendOptions{Category: "Pub notification"})
	if _, err := configured.Send(context.Background(), email); err != nil {
		t.Fatal(err)
	}
	if client.sendOptions.Category != "" || !reflect.DeepEqual(email.Variables, before) {
		t.Fatal("configuration or variables mutated")
	}
	for index, name := range []string{"Alice", "Guest"} {
		message := captured.Requests[index]
		if message.Subject != "Hi "+name || message.Text != name+": Friday & <literal>" || message.Category != "Pub notification" {
			t.Errorf("message = %+v", message)
		}
		if !reflect.DeepEqual(message.Headers, email.Headers) || message.CustomVariables[CorrelationVariable] != "pn-1" || len(message.CustomVariables) != 1 {
			t.Errorf("metadata = %+v", message)
		}
	}
}

func TestSendHostedTemplates(t *testing.T) {
	for _, count := range []int{1, 2} {
		var requests []sdk.SendRequest
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if count == 1 {
				var request sdk.SendRequest
				_ = json.NewDecoder(r.Body).Decode(&request)
				requests = []sdk.SendRequest{request}
				_, _ = io.WriteString(w, `{"success":true,"message_ids":["a"]}`)
			} else {
				var request sdk.BatchSendRequest
				_ = json.NewDecoder(r.Body).Decode(&request)
				requests = request.Requests
				_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`)
			}
		})
		email := testEmail(count)
		email.Subject, email.Text, email.TemplateID = "", "", "template-uuid"
		email.Variables = map[string]any{"name": "Guest", "nested": map[string]any{"value": 1}}
		email.To[0].Variables = map[string]any{"name": "Alice"}
		if _, err := client.Send(context.Background(), email); err != nil {
			t.Fatal(err)
		}
		if requests[0].TemplateUUID != "template-uuid" || requests[0].TemplateVariables["name"] != "Alice" || requests[0].Text != "" || requests[0].Subject != "" {
			t.Fatalf("template request = %+v", requests[0])
		}
	}
}

func TestSendSuppressionRouting(t *testing.T) {
	for _, count := range []int{1, 2} {
		for _, hosted := range []bool{false, true} {
			var suppressed atomic.Bool
			var callbackCalls, liveCalls, sandboxCalls int
			var requests []sdk.SendRequest
			var capturedCategory string
			handler := func(sandbox bool) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					path := "/api/send"
					if count > 1 {
						path = "/api/batch"
					}
					if sandbox {
						sandboxCalls++
						path += "/123"
					} else {
						liveCalls++
					}
					if r.URL.Path != path {
						t.Errorf("path = %s, want %s", r.URL.Path, path)
					}
					if count == 1 {
						var request sdk.SendRequest
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Error(err)
						}
						requests = []sdk.SendRequest{request}
						_, _ = io.WriteString(w, `{"success":true,"message_ids":["uid"]}`)
					} else {
						var request sdk.BatchSendRequest
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Error(err)
						}
						requests = request.Requests
						_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`)
					}
					capturedCategory = requests[0].Category
				}
			}
			client := newTestClient(t, handler(false))
			client.SetDryRunCallback(func() bool {
				callbackCalls++
				return suppressed.Load()
			})
			sandbox := newTestClient(t, handler(true), sdk.WithSandbox(true), sdk.WithSandboxID(123))
			configured, err := client.WithSandboxClient(sandbox.sdk)
			if err != nil {
				t.Fatal(err)
			}
			configured = configured.WithSendOptions(SendOptions{Category: "Training"})
			email := testEmail(count)
			email.Subject = "Hi {{name}}"
			email.Variables = map[string]any{"name": "Guest", clients.NotificationPrefixVariable: "wrong"}
			email.To[0].Variables = map[string]any{"name": "Alice", clients.NotificationPrefixVariable: "override"}
			email.Headers = map[string]string{clients.CorrelationHeader: "pn-1"}
			if hosted {
				email.Subject, email.Text, email.TemplateID = "", "", "template"
			}
			for index, mode := range []bool{false, true, false} {
				suppressed.Store(mode)
				result, err := configured.Send(context.Background(), email)
				if err != nil || len(result.Recipients) != count {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
				if callbackCalls != index+1 || capturedCategory != "Training" {
					t.Fatalf("callbackCalls=%d category=%s", callbackCalls, capturedCategory)
				}
				prefix := ""
				if mode {
					prefix = clients.DryRunSubjectPrefix
				}
				for recipientIndex, request := range requests {
					if hosted {
						if request.Subject != "" || request.TemplateVariables[clients.NotificationPrefixVariable] != prefix {
							t.Fatalf("hosted request = %+v", request)
						}
					} else {
						name := "Guest"
						if recipientIndex == 0 {
							name = "Alice"
						}
						if request.Subject != prefix+"Hi "+name {
							t.Fatalf("subject = %q", request.Subject)
						}
					}
					if request.Headers[clients.CorrelationHeader] != "pn-1" || (mode && request.Headers[clients.DryRunHeader] != "true") || (!mode && request.Headers[clients.DryRunHeader] != "") {
						t.Fatalf("headers = %v", request.Headers)
					}
				}
			}
			if liveCalls != 2 || sandboxCalls != 1 || client.sandboxSDK != nil || email.Headers[clients.DryRunHeader] != "" || email.To[0].Variables[clients.NotificationPrefixVariable] != "override" {
				t.Fatal("routing, clone isolation or input immutability failed")
			}
			configured.SetDryRunCallback(nil)
			if _, err := configured.Send(context.Background(), email); err != nil || liveCalls != 3 {
				t.Fatalf("nil callback: err=%v liveCalls=%d", err, liveCalls)
			}
		}
	}
}

func TestSendSuppressionNeverFallsBackToLive(t *testing.T) {
	var liveCalls, sandboxCalls atomic.Int32
	var callbackCalls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { liveCalls.Add(1) })
	client.SetDryRunCallback(func() bool {
		callbackCalls.Add(1)
		return true
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Send(ctx, testEmail(1)); !errors.Is(err, context.Canceled) || callbackCalls.Load() != 0 {
		t.Fatalf("cancelled send: err=%v callbacks=%d", err, callbackCalls.Load())
	}
	if _, err := client.Send(context.Background(), testEmail(1)); !errors.Is(err, ErrSandboxNotConfigured) || liveCalls.Load() != 0 {
		t.Fatalf("missing sandbox: err=%v calls=%d", err, liveCalls.Load())
	}
	if _, err := client.WithSandboxClient(nil); !errors.Is(err, ErrSandboxNotConfigured) {
		t.Fatal(err)
	}
	sandbox := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sandboxCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}, sdk.WithSandbox(true), sdk.WithSandboxID(123))
	configured, err := client.WithSandboxClient(sandbox.sdk)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{1, 2} {
		if _, err := configured.Send(context.Background(), testEmail(count)); err == nil || liveCalls.Load() != 0 {
			t.Fatalf("sandbox failure: err=%v liveCalls=%d", err, liveCalls.Load())
		}
	}
	for _, subject := range []string{"", " ", "{{name}}"} {
		email := testEmail(1)
		email.Subject = subject
		email.Variables = map[string]any{"name": ""}
		if _, err := configured.Send(context.Background(), email); err == nil {
			t.Fatalf("invalid subject %q accepted", subject)
		}
	}
	if sandboxCalls.Load() != 2 {
		t.Fatalf("invalid subjects contacted sandbox: %d requests", sandboxCalls.Load())
	}
}

func TestSendPreflightNeverContactsProvider(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, name := range []string{"zero", "oversized", "sender", "recipient", "empty subject", "empty text", "render", "empty rendered", "template subject", "template text", "empty template", "correlation", "whitespace correlation", "conflicting correlation", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			email := testEmail(2)
			ctx := context.Background()
			switch name {
			case "zero":
				email.To = nil
			case "oversized":
				email = testEmail(501)
			case "sender":
				email.From.Email = "not an address"
			case "recipient":
				email.To[1].Email = "not an address"
			case "empty subject":
				email.Subject = ""
			case "empty text":
				email.Text = ""
			case "render":
				email.Text = "{{name}}"
				email.To[0].Variables = map[string]any{"name": "Alice"}
			case "empty rendered":
				email.Subject = "{{name}}"
				email.Variables = map[string]any{"name": ""}
			case "template subject":
				email.TemplateID, email.Text = "template", ""
			case "template text":
				email.TemplateID, email.Subject = "template", ""
			case "empty template":
				email.TemplateID, email.Subject, email.Text = " ", "", ""
			case "correlation":
				email.Headers = map[string]string{clients.CorrelationHeader: ""}
			case "whitespace correlation":
				email.Headers = map[string]string{clients.CorrelationHeader: " "}
			case "conflicting correlation":
				email.Headers = map[string]string{clients.CorrelationHeader: "one", strings.ToLower(clients.CorrelationHeader): "two"}
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			_, err := client.Send(ctx, email)
			if err == nil || calls.Load() != 0 {
				t.Fatalf("err = %v, calls = %d", err, calls.Load())
			}
			if name == "zero" && !errors.Is(err, ErrNoRecipients) {
				t.Fatal(err)
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
	if _, err := NewClient(nil); err == nil {
		t.Fatal("nil SDK accepted")
	}
	if _, err := (*Client)(nil).Send(context.Background(), testEmail(1)); err == nil {
		t.Fatal("nil client accepted")
	}
}

func TestBatchPartialResults(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["accepted"]},{"success":false,"errors":["invalid recipient"]},{"success":true,"message_ids":[]}]}`)
	})
	result, err := client.Send(context.Background(), testEmail(3))
	var batchErr *BatchError
	if !errors.As(err, &batchErr) || !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("error = %v", err)
	}
	if len(result.Recipients) != 3 || result.Recipients[0].PMUID != "accepted" || result.Recipients[1].PMUID != "" || result.Recipients[2].PMUID != "" {
		t.Fatalf("partial result = %+v", result)
	}
	if len(batchErr.Refusals) != 1 || batchErr.Refusals[0].Index != 1 || len(batchErr.InvalidResults) != 1 || batchErr.InvalidResults[0].Index != 2 {
		t.Fatalf("diagnostics = %+v", batchErr)
	}
}

func TestBatchUnusableAndRefusedResponses(t *testing.T) {
	for _, body := range []string{
		`{`, `{"success":true}`, `{"success":true,"responses":[{"success":true,"message_ids":["a"]}]}`,
		`{"success":true,"responses":[{},{}]}`,
		`{"success":true,"responses":[{"success":true,"message_ids":["a","b"]},{}]}`,
		`{"success":false,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`,
		`{"success":true,"errors":["batch error"],"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`,
		`{"success":true,"responses":[{"success":true,"message_ids":["a"],"errors":["contradiction"]},{"success":false,"message_ids":["b"],"errors":["contradiction"]}]}`,
		`{"success":false,"responses":[{"success":false,"errors":["refused"]},{"success":false,"errors":["refused"]}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, body) })
			result, err := client.Send(context.Background(), testEmail(2))
			if err == nil || calls.Load() != 1 {
				t.Fatalf("result = %+v, err = %v, calls = %d", result, err, calls.Load())
			}
			if strings.Contains(body, `"batch error"`) || strings.Contains(body, `"message_ids":["b"]}]`) {
				if len(result.Recipients) != 2 || result.Recipients[0].PMUID != "a" || result.Recipients[1].PMUID != "b" {
					t.Fatalf("lost accepted IDs: %+v", result)
				}
			}
		})
	}
}

func TestSingleUnusableResponse(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"success":false,"message_ids":["a"]}`, `{"success":true,"message_ids":[""]}`, `{"success":true,"message_ids":["a","b"]}`} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		if result, err := client.Send(context.Background(), testEmail(1)); err == nil || len(result.Recipients) != 0 {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
	}
}

func TestSDKErrorsPreserveTypes(t *testing.T) {
	for _, status := range []int{401, 403, 422, 429, 500} {
		for _, count := range []int{1, 2} {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "10")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"success":false,"errors":["refused"]}`)
			})
			_, err := client.Send(context.Background(), testEmail(count))
			var providerErr *sdk.Error
			if !errors.As(err, &providerErr) || providerErr.StatusCode != status {
				t.Fatalf("SDK error = %v", err)
			}
			if status == 429 {
				var limited *sdk.RateLimitError
				if !errors.As(err, &limited) || limited.RetryAfter != 10*time.Second {
					t.Fatalf("rate limit = %v", err)
				}
			}
		}
	}
}

func TestSandboxRouting(t *testing.T) {
	for _, count := range []int{1, 2} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if count == 1 {
				if r.URL.Path != "/api/send/42" {
					t.Error(r.URL.Path)
				}
				_, _ = io.WriteString(w, `{"success":true,"message_ids":["a"]}`)
			} else {
				if r.URL.Path != "/api/batch/42" {
					t.Error(r.URL.Path)
				}
				_, _ = io.WriteString(w, `{"success":true,"responses":[{"success":true,"message_ids":["a"]},{"success":true,"message_ids":["b"]}]}`)
			}
		}, sdk.WithSandbox(true), sdk.WithSandboxID(42))
		if _, err := client.Send(context.Background(), testEmail(count)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTransportDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { <-release })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Send(ctx, testEmail(1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}
