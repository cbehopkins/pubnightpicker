package testemail

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"last_orders/internal/lastorders/truths"
)

type tokens struct {
	wait  int
	calls int
}

func (t *tokens) Acquire() int {
	t.calls++
	return t.wait
}

func (t *tokens) AcquireN(count int) (int, error) {
	return t.Acquire(), nil
}

type acks struct {
	err  error
	user string
	req  string
}

func (a *acks) AckTestEmail(_ context.Context, userID, requestID string) error {
	a.user, a.req = userID, requestID
	return a.err
}

func assertAckStep(t *testing.T, step cellar.CellStep) {
	t.Helper()
	if step.HandlerName != HandlerTestEmailAcked {
		t.Fatalf("last step = %q, want ack", step.HandlerName)
	}
	var payload ackPayload
	if err := json.Unmarshal(step.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload != (ackPayload{UserID: "u1", RequestID: "r1"}) {
		t.Fatalf("ack payload = %+v", payload)
	}
}

func TestHandlerSendsThenAcks(t *testing.T) {
	limiter := &tokens{}
	result, ok := Handler{Tokens: limiter}.Handle(context.Background(), truths.TestEmailRequested{UserID: "u1", RequestID: "r1", Email: "a@example.com"}).(cellar.Complete)
	if !ok || len(result.NewCells) != 1 {
		t.Fatalf("result = %+v, want one send cell", result)
	}
	steps := result.NewCells[0].Steps
	if len(steps) < 2 || steps[0].HandlerName != durableemail.HandlerSetup {
		t.Fatalf("steps = %+v, want durable send sequence", steps)
	}
	var request durableemail.SendRequest
	if err := json.Unmarshal(steps[0].Payload, &request); err != nil {
		t.Fatal(err)
	}
	if request.IdempotencyToken != "test-email:u1:r1" || request.Subject != subject || request.Text != body {
		t.Errorf("request = %+v", request)
	}
	if len(request.Recipients) != 1 || request.Recipients[0].Email != "a@example.com" || request.Recipients[0].UserID != "u1" {
		t.Errorf("recipients = %+v", request.Recipients)
	}
	assertAckStep(t, steps[len(steps)-1])
	if limiter.calls != 1 {
		t.Errorf("token acquisitions = %d, want 1", limiter.calls)
	}
}

func TestHandlerAcksWithoutSendingWhenNoAddress(t *testing.T) {
	limiter := &tokens{}
	result, ok := Handler{Tokens: limiter}.Handle(context.Background(), truths.TestEmailRequested{UserID: "u1", RequestID: "r1"}).(cellar.Complete)
	if !ok || len(result.NewCells) != 1 || len(result.NewCells[0].Steps) != 1 {
		t.Fatalf("result = %+v, want ack-only cell", result)
	}
	assertAckStep(t, result.NewCells[0].Steps[0])
	if limiter.calls != 0 {
		t.Errorf("token acquisitions = %d, want 0", limiter.calls)
	}
}

func TestHandlerDropsWhenRateLimited(t *testing.T) {
	result, ok := Handler{Tokens: &tokens{wait: 60}}.Handle(context.Background(), truths.TestEmailRequested{UserID: "u1", RequestID: "r1", Email: "a@example.com"}).(cellar.Complete)
	if !ok || len(result.NewCells) != 0 {
		t.Fatalf("result = %+v, want completion with no cells", result)
	}
}

func TestAckedHandlerWritesAck(t *testing.T) {
	writer := &acks{}
	if _, ok := (AckedHandler{Acks: writer}).Handle(context.Background(), ackPayload{UserID: "u1", RequestID: "r1"}).(cellar.Complete); !ok {
		t.Fatal("ack handler did not complete")
	}
	if writer.user != "u1" || writer.req != "r1" {
		t.Fatalf("ack = %q/%q", writer.user, writer.req)
	}

	writer.err = errors.New("boom")
	if _, ok := (AckedHandler{Acks: writer}).Handle(context.Background(), ackPayload{UserID: "u1", RequestID: "r1"}).(cellar.ErrorResult); !ok {
		t.Fatal("ack failure must be an error result")
	}
}
