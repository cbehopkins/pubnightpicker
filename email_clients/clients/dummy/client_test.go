package dummy

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"testing"

	"email_clients/clients"
)

func TestSendEmailInvokesCallbackForEachRecipient(t *testing.T) {
	type callbackCall struct {
		email   string
		message string
		headers map[string]string
	}

	var calls []callbackCall
	client := NewClient(func(emailAddress, message string, headers map[string]string) error {
		calls = append(calls, callbackCall{email: emailAddress, message: message, headers: maps.Clone(headers)})
		headers["changed"] = emailAddress
		return nil
	})
	req := clients.SendEmailRequest{
		Recipients: []clients.EmailAddress{
			{Email: "alice@example.com"},
			{Email: "bob@example.com"},
		},
		MessageTxt: "Hello",
		Headers:    map[string]string{"X-Message-ID": "message-1"},
	}

	result, err := client.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != http.StatusOK || result.Headers != nil || result.Body != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got, want := len(calls), 2; got != want {
		t.Fatalf("got %d callback calls, want %d", got, want)
	}
	if got, want := []string{calls[0].email, calls[1].email}, []string{"alice@example.com", "bob@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order = %v, want %v", got, want)
	}
	for _, call := range calls {
		if call.message != "Hello" || call.headers["X-Message-ID"] != "message-1" {
			t.Fatalf("unexpected callback values: %+v", call)
		}
	}
	if _, ok := calls[1].headers["changed"]; ok {
		t.Fatalf("header mutation leaked between callbacks: %v", calls[1].headers)
	}
	if _, ok := req.Headers["changed"]; ok {
		t.Fatalf("header mutation changed request: %v", req.Headers)
	}
}

func TestSendEmailStopsAtFirstCallbackError(t *testing.T) {
	callbackErr := errors.New("recording failed")
	var recipients []string
	client := NewClient(func(emailAddress, _ string, _ map[string]string) error {
		recipients = append(recipients, emailAddress)
		if emailAddress == "bob@example.com" {
			return callbackErr
		}
		return nil
	})

	_, err := client.SendEmail(context.Background(), clients.SendEmailRequest{Recipients: []clients.EmailAddress{
		{Email: "alice@example.com"},
		{Email: "bob@example.com"},
		{Email: "carol@example.com"},
	}})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("got error %v, want wrapped callback error", err)
	}
	if got, want := recipients, []string{"alice@example.com", "bob@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callback recipients = %v, want %v", got, want)
	}
}

func TestSendEmailRejectsNilCallback(t *testing.T) {
	_, err := NewClient(nil).SendEmail(context.Background(), clients.SendEmailRequest{})
	if !errors.Is(err, ErrNilCallback) {
		t.Fatalf("got error %v, want %v", err, ErrNilCallback)
	}
}

func TestSendEmailHonoursCancelledContext(t *testing.T) {
	called := false
	client := NewClient(func(_ string, _ string, _ map[string]string) error {
		called = true
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.SendEmail(ctx, clients.SendEmailRequest{Recipients: []clients.EmailAddress{{Email: "alice@example.com"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got error %v, want %v", err, context.Canceled)
	}
	if called {
		t.Fatal("callback called with cancelled context")
	}
}

func TestSendBulkEmailInvokesCallbackForEachRecipient(t *testing.T) {
	type callbackCall struct {
		email   string
		message string
		headers map[string]string
	}

	var calls []callbackCall
	client := NewClient(func(emailAddress, message string, headers map[string]string) error {
		calls = append(calls, callbackCall{email: emailAddress, message: message, headers: maps.Clone(headers)})
		headers["changed"] = emailAddress
		return nil
	})
	req := clients.BulkEmailRequest{
		Recipients: []clients.BulkRecipient{
			{Email: "alice@example.com", Variables: map[string]any{"name": "Alice"}},
			{Email: "bob@example.com", Variables: map[string]any{"name": "Bob"}},
		},
		MessageTxt: "Hello {{ name }}",
		Headers:    map[string]string{"X-Message-ID": "message-1"},
	}

	result, err := client.SendBulkEmail(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != http.StatusOK || result.Headers != nil || result.Body != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got, want := len(calls), 2; got != want {
		t.Fatalf("got %d callback calls, want %d", got, want)
	}
	if got, want := []string{calls[0].email, calls[1].email}, []string{"alice@example.com", "bob@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order = %v, want %v", got, want)
	}
	for _, call := range calls {
		if call.message != "Hello {{ name }}" || call.headers["X-Message-ID"] != "message-1" {
			t.Fatalf("unexpected callback values: %+v", call)
		}
	}
	if _, ok := req.Headers["changed"]; ok {
		t.Fatalf("header mutation changed request: %v", req.Headers)
	}
}

func TestSendBulkEmailRendersTemplateForEachRecipient(t *testing.T) {
	var messages []string
	client := NewClient(func(_ string, message string, _ map[string]string) error {
		messages = append(messages, message)
		return nil
	})
	if err := client.AddTemplate("greeting", "Hello {{.name}}"); err != nil {
		t.Fatal(err)
	}

	_, err := client.SendBulkEmail(context.Background(), clients.BulkEmailRequest{
		Recipients: []clients.BulkRecipient{
			{Email: "alice@example.com", Variables: map[string]any{"name": "Alice"}},
			{Email: "bob@example.com", Variables: map[string]any{"name": "Bob"}},
		},
		TemplateID: "greeting",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Hello Alice", "Hello Bob"}; !reflect.DeepEqual(messages, want) {
		t.Fatalf("callback messages = %v, want %v", messages, want)
	}
}

func TestSendBulkEmailRejectsMissingTemplate(t *testing.T) {
	called := false
	client := NewClient(func(_ string, _ string, _ map[string]string) error {
		called = true
		return nil
	})

	_, err := client.SendBulkEmail(context.Background(), clients.BulkEmailRequest{
		Recipients: []clients.BulkRecipient{{Email: "alice@example.com"}},
		TemplateID: "missing",
	})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("got error %v, want %v", err, ErrTemplateNotFound)
	}
	if called {
		t.Fatal("callback called for missing template")
	}
}

func TestSendBulkEmailRendersAllRecipientsBeforeCallbacks(t *testing.T) {
	called := false
	client := NewClient(func(_ string, _ string, _ map[string]string) error {
		called = true
		return nil
	})
	if err := client.AddTemplate("greeting", "Hello {{.name}}"); err != nil {
		t.Fatal(err)
	}

	_, err := client.SendBulkEmail(context.Background(), clients.BulkEmailRequest{
		Recipients: []clients.BulkRecipient{
			{Email: "alice@example.com", Variables: map[string]any{"name": "Alice"}},
			{Email: "bob@example.com"},
		},
		TemplateID: "greeting",
	})
	if err == nil {
		t.Fatal("expected missing variable error")
	}
	if called {
		t.Fatal("callback called before all recipients rendered successfully")
	}
}

func TestAddTemplateRejectsInvalidDefinitions(t *testing.T) {
	client := NewClient(nil)
	if err := client.AddTemplate("", "Hello"); !errors.Is(err, ErrTemplateNameEmpty) {
		t.Fatalf("empty name error = %v, want %v", err, ErrTemplateNameEmpty)
	}
	if err := client.AddTemplate("broken", "{{"); err == nil {
		t.Fatal("expected template parse error")
	}
	if err := client.AddTemplate("greeting", "Hello"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddTemplate("greeting", "Goodbye"); !errors.Is(err, ErrTemplateExists) {
		t.Fatalf("duplicate name error = %v, want %v", err, ErrTemplateExists)
	}
}
