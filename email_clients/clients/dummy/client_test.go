package dummy

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"testing"

	"email_clients/clients"
)

func TestSendInvokesCallbacksAndReturnsDistinctPMUIDs(t *testing.T) {
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
	email := clients.Email{
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com"}},
			{Address: clients.Address{Email: "bob@example.com"}},
		},
		Text:    "Hello",
		Headers: map[string]string{"X-Message-ID": "message-1"},
	}

	result, err := client.Send(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []string{calls[0].email, calls[1].email}, []string{"alice@example.com", "bob@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order = %v, want %v", got, want)
	}
	for _, call := range calls {
		if call.message != "Hello" || call.headers["X-Message-ID"] != "message-1" {
			t.Fatalf("callback = %+v", call)
		}
	}
	if _, ok := calls[1].headers["changed"]; ok {
		t.Fatalf("header mutation leaked between callbacks: %v", calls[1].headers)
	}
	if _, ok := email.Headers["changed"]; ok {
		t.Fatalf("header mutation changed email: %v", email.Headers)
	}
	if len(result.Recipients) != 2 || result.Recipients[0].PMUID == "" || result.Recipients[1].PMUID == "" || result.Recipients[0].PMUID == result.Recipients[1].PMUID {
		t.Fatalf("invalid PMUID results: %#v", result)
	}
}

func TestSendRendersTemplateWithMergedVariables(t *testing.T) {
	var messages []string
	client := NewClient(func(_ string, message string, _ map[string]string) error {
		messages = append(messages, message)
		return nil
	})
	if err := client.AddTemplate("greeting", "Hello {{.name}} at {{.place}}"); err != nil {
		t.Fatal(err)
	}

	_, err := client.Send(context.Background(), clients.Email{
		TemplateID: "greeting",
		Variables:  map[string]any{"name": "Guest", "place": "The Crown"},
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com"}, Variables: map[string]any{"name": "Alice"}},
			{Address: clients.Address{Email: "bob@example.com"}, Variables: map[string]any{"name": "Bob"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Hello Alice at The Crown", "Hello Bob at The Crown"}; !reflect.DeepEqual(messages, want) {
		t.Fatalf("messages = %v, want %v", messages, want)
	}
}

func TestSendRendersAllRecipientsBeforeCallbacks(t *testing.T) {
	called := false
	client := NewClient(func(_ string, _ string, _ map[string]string) error {
		called = true
		return nil
	})
	if err := client.AddTemplate("greeting", "Hello {{.name}}"); err != nil {
		t.Fatal(err)
	}

	_, err := client.Send(context.Background(), clients.Email{
		TemplateID: "greeting",
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com"}, Variables: map[string]any{"name": "Alice"}},
			{Address: clients.Address{Email: "bob@example.com"}},
		},
	})
	if err == nil || called {
		t.Fatalf("error = %v, callback called = %t", err, called)
	}
}

func TestSendPropagatesErrorsAndCancellation(t *testing.T) {
	callbackErr := errors.New("recording failed")
	var recipients []string
	client := NewClient(func(emailAddress, _ string, _ map[string]string) error {
		recipients = append(recipients, emailAddress)
		if emailAddress == "bob@example.com" {
			return callbackErr
		}
		return nil
	})
	email := clients.Email{To: []clients.Recipient{
		{Address: clients.Address{Email: "alice@example.com"}},
		{Address: clients.Address{Email: "bob@example.com"}},
		{Address: clients.Address{Email: "carol@example.com"}},
	}}
	if _, err := client.Send(context.Background(), email); !errors.Is(err, callbackErr) {
		t.Fatalf("callback error = %v", err)
	}
	if want := []string{"alice@example.com", "bob@example.com"}; !reflect.DeepEqual(recipients, want) {
		t.Fatalf("callback recipients = %v, want %v", recipients, want)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Send(ctx, clients.Email{To: email.To[:1]}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled send error = %v", err)
	}
}

func TestSendValidatesClientAndEmail(t *testing.T) {
	if _, err := NewClient(nil).Send(context.Background(), clients.Email{}); !errors.Is(err, ErrNilCallback) {
		t.Fatalf("nil callback error = %v", err)
	}
	client := NewClient(func(string, string, map[string]string) error { return nil })
	if _, err := client.Send(context.Background(), clients.Email{}); !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("empty email error = %v", err)
	}
	if _, err := client.Send(context.Background(), clients.Email{
		TemplateID: "missing",
		To:         []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}},
	}); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("missing template error = %v", err)
	}
}

func TestAddTemplateRejectsInvalidDefinitions(t *testing.T) {
	client := NewClient(nil)
	if err := client.AddTemplate("", "Hello"); !errors.Is(err, ErrTemplateNameEmpty) {
		t.Fatalf("empty name error = %v", err)
	}
	if err := client.AddTemplate("broken", "{{"); err == nil {
		t.Fatal("expected template parse error")
	}
	if err := client.AddTemplate("greeting", "Hello"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddTemplate("greeting", "Goodbye"); !errors.Is(err, ErrTemplateExists) {
		t.Fatalf("duplicate name error = %v", err)
	}
}
