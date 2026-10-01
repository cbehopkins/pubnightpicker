package dummy

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
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
	client := NewClient(func(emailAddress, message string, headers map[string]string) (Response, error) {
		calls = append(calls, callbackCall{email: emailAddress, message: message, headers: maps.Clone(headers)})
		headers["changed"] = emailAddress
		return Response{}, nil
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
	client := NewClient(func(_ string, message string, _ map[string]string) (Response, error) {
		messages = append(messages, message)
		return Response{}, nil
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
	client := NewClient(func(_ string, _ string, _ map[string]string) (Response, error) {
		called = true
		return Response{}, nil
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
	client := NewClient(func(emailAddress, _ string, _ map[string]string) (Response, error) {
		recipients = append(recipients, emailAddress)
		if emailAddress == "bob@example.com" {
			return Response{}, callbackErr
		}
		return Response{}, nil
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

func TestSendRefusalAbortsRemainingRecipients(t *testing.T) {
	var recipients []string
	client := NewClient(func(emailAddress, _ string, _ map[string]string) (Response, error) {
		recipients = append(recipients, emailAddress)
		if emailAddress == "bob@example.com" {
			return Response{Status: 429, Body: " rate limited "}, nil
		}
		return Response{}, nil
	})

	result, err := client.Send(context.Background(), clients.Email{To: []clients.Recipient{
		{Address: clients.Address{Email: "alice@example.com"}},
		{Address: clients.Address{Email: "bob@example.com"}},
		{Address: clients.Address{Email: "carol@example.com"}},
	}})
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("send error = %v, want *RefusedError", err)
	}
	if refused.Recipient != "bob@example.com" || refused.Status != 429 || refused.Body != " rate limited " {
		t.Fatalf("RefusedError = %+v", refused)
	}
	if want := "dummy email service refused \"bob@example.com\": HTTP 429: rate limited"; refused.Error() != want {
		t.Fatalf("Error() = %q, want %q", refused.Error(), want)
	}
	if len(result.Recipients) != 0 {
		t.Fatalf("result = %#v, want empty on refusal", result)
	}
	if want := []string{"alice@example.com", "bob@example.com"}; !reflect.DeepEqual(recipients, want) {
		t.Fatalf("callback recipients = %v, want %v", recipients, want)
	}
}

func TestSendTreatsSuccessStatusesAsAccepted(t *testing.T) {
	for _, status := range []int{0, 200, 202, 299} {
		client := NewClient(func(string, string, map[string]string) (Response, error) {
			return Response{Status: status}, nil
		})
		result, err := client.Send(context.Background(), clients.Email{
			To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}},
		})
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if len(result.Recipients) != 1 || result.Recipients[0].PMUID == "" {
			t.Fatalf("status %d: result = %#v", status, result)
		}
	}
}

func TestSendUsesCallbackPMUIDWhenSupplied(t *testing.T) {
	client := NewClient(func(emailAddress, _ string, _ map[string]string) (Response, error) {
		if emailAddress == "alice@example.com" {
			return Response{PMUID: "provider-1"}, nil
		}
		return Response{}, nil
	})

	result, err := client.Send(context.Background(), clients.Email{
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com"}},
			{Address: clients.Address{Email: "bob@example.com"}},
		},
		Headers: map[string]string{clients.CorrelationHeader: "pn-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recipients[0].PMUID != "provider-1" {
		t.Fatalf("PMUID = %q, want the callback value", result.Recipients[0].PMUID)
	}
	if !strings.HasPrefix(result.Recipients[1].PMUID, "dummy-") {
		t.Fatalf("PMUID = %q, want a generated fallback", result.Recipients[1].PMUID)
	}

	verified, err := client.Verify(context.Background(), clients.VerifyRequest{CorrelationID: "pn-1", Recipient: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Found || verified.PMUID != "provider-1" {
		t.Fatalf("Verify = %+v, want the callback PMUID", verified)
	}
}

func TestVerifyIgnoresRefusedRecipients(t *testing.T) {
	client := NewClient(func(emailAddress, _ string, _ map[string]string) (Response, error) {
		if emailAddress == "bob@example.com" {
			return Response{Status: 403, Body: "sender blocked"}, nil
		}
		return Response{}, nil
	})
	if _, err := client.Send(context.Background(), clients.Email{
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com"}},
			{Address: clients.Address{Email: "bob@example.com"}},
		},
		Headers: map[string]string{clients.CorrelationHeader: "pn-1"},
	}); err == nil {
		t.Fatal("expected a refusal error")
	}

	verified, err := client.Verify(context.Background(), clients.VerifyRequest{CorrelationID: "pn-1", Recipient: "bob@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Found {
		t.Fatalf("Verify = %+v, want not found for a refused recipient", verified)
	}
}

func TestSendValidatesClientAndEmail(t *testing.T) {
	if _, err := NewClient(nil).Send(context.Background(), clients.Email{}); !errors.Is(err, ErrNilCallback) {
		t.Fatalf("nil callback error = %v", err)
	}
	client := NewClient(func(string, string, map[string]string) (Response, error) { return Response{}, nil })
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

func TestVerifyFindsRecipientAfterSuccessfulSend(t *testing.T) {
	client := NewClient(func(string, string, map[string]string) (Response, error) { return Response{}, nil })
	email := clients.Email{
		To:      []clients.Recipient{{Address: clients.Address{Email: "Alice@Example.com"}}},
		Headers: map[string]string{clients.CorrelationHeader: "pn-1"},
	}

	result, err := client.Send(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}

	verified, err := client.Verify(context.Background(), clients.VerifyRequest{CorrelationID: "pn-1", Recipient: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Found || verified.PMUID != result.Recipients[0].PMUID {
		t.Fatalf("Verify = %+v, want found with PMUID %q", verified, result.Recipients[0].PMUID)
	}
}

func TestVerifyReturnsNotFoundForUnmatchedRequest(t *testing.T) {
	client := NewClient(func(string, string, map[string]string) (Response, error) { return Response{}, nil })
	if _, err := client.Send(context.Background(), clients.Email{
		To:      []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}},
		Headers: map[string]string{clients.CorrelationHeader: "pn-1"},
	}); err != nil {
		t.Fatal(err)
	}

	tests := []clients.VerifyRequest{
		{CorrelationID: "pn-2", Recipient: "alice@example.com"},
		{CorrelationID: "pn-1", Recipient: "bob@example.com"},
		{CorrelationID: "", Recipient: "alice@example.com"},
	}
	for _, request := range tests {
		verified, err := client.Verify(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if verified.Found {
			t.Fatalf("Verify(%+v) = %+v, want not found", request, verified)
		}
	}
}

func TestVerifyIgnoresRecipientsFromFailedCallbacks(t *testing.T) {
	callbackErr := errors.New("recording failed")
	client := NewClient(func(emailAddress, _ string, _ map[string]string) (Response, error) {
		if emailAddress == "bob@example.com" {
			return Response{}, callbackErr
		}
		return Response{}, nil
	})
	email := clients.Email{
		To: []clients.Recipient{
			{Address: clients.Address{Email: "alice@example.com"}},
			{Address: clients.Address{Email: "bob@example.com"}},
		},
		Headers: map[string]string{clients.CorrelationHeader: "pn-1"},
	}
	if _, err := client.Send(context.Background(), email); !errors.Is(err, callbackErr) {
		t.Fatalf("send error = %v", err)
	}

	verified, err := client.Verify(context.Background(), clients.VerifyRequest{CorrelationID: "pn-1", Recipient: "bob@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Found {
		t.Fatalf("Verify = %+v, want not found for failed recipient", verified)
	}

	verified, err = client.Verify(context.Background(), clients.VerifyRequest{CorrelationID: "pn-1", Recipient: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Found {
		t.Fatal("expected the recipient preceding the failure to be recorded")
	}
}

func TestVerifyRequiresCorrelationHeaderOnRecord(t *testing.T) {
	client := NewClient(func(string, string, map[string]string) (Response, error) { return Response{}, nil })
	if _, err := client.Send(context.Background(), clients.Email{
		To: []clients.Recipient{{Address: clients.Address{Email: "alice@example.com"}}},
	}); err != nil {
		t.Fatal(err)
	}

	verified, err := client.Verify(context.Background(), clients.VerifyRequest{CorrelationID: "", Recipient: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Found {
		t.Fatalf("Verify = %+v, want not found when no correlation header was sent", verified)
	}
}
