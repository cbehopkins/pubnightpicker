package polls

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/completionactions/completionactionstest"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/truths"
)

type recipientSourceFunc func(context.Context, notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error)

func (f recipientSourceFunc) GetEligibleEmailRecipients(ctx context.Context, kind notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error) {
	return f(ctx, kind)
}

func TestPollOpenedEmailCreatesOneBatchedSend(t *testing.T) {
	var requestedKind notificationprofile.EmailKind
	handler := PollOpenedEmailHandler{Actions: completionactionstest.New(), BaseURL: "https://pubnightpicker.web.app", Recipients: recipientSourceFunc(func(_ context.Context, kind notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error) {
		requestedKind = kind
		return []notificationprofile.EmailRecipient{
			{UserID: "alice", Email: "alice@example.com"},
			{UserID: "bob", Email: "bob@example.com"},
		}, nil
	})}

	result, ok := handler.Handle(context.Background(), truths.PollObservedPayload{PollID: "poll-1"}).(cellar.Complete)
	if !ok {
		t.Fatal("handler did not complete")
	}
	if requestedKind != notificationprofile.EmailPollOpens {
		t.Errorf("recipient kind = %q, want %q", requestedKind, notificationprofile.EmailPollOpens)
	}
	if len(result.NewCells) != 1 {
		t.Fatalf("new cells = %d, want one batched send", len(result.NewCells))
	}

	steps := result.NewCells[0].Steps
	if len(steps) == 0 || steps[0].HandlerName != durableemail.HandlerSetup {
		t.Fatalf("send steps = %+v, want a durable email sequence", steps)
	}
	var request durableemail.SendRequest
	if err := json.Unmarshal(steps[0].Payload, &request); err != nil {
		t.Fatalf("decode setup payload: %v", err)
	}
	if request.IdempotencyToken != "poll-opened:poll-1" {
		t.Errorf("token = %q", request.IdempotencyToken)
	}
	if request.SenderEmail != pollEmailSenderEmail || request.SenderName != pollEmailSenderName {
		t.Errorf("sender = %q <%q>", request.SenderName, request.SenderEmail)
	}
	wantBody := `Voting has opened for this week's pub night.
Please visit https://pubnightpicker.web.app/active_polls
to participate in the voting.
`
	if request.Subject != pollOpenedSubject || request.Text != wantBody {
		t.Errorf("subject/body = %q / %q", request.Subject, request.Text)
	}
	if len(request.Recipients) != 2 || request.Recipients[0].Email != "alice@example.com" || request.Recipients[1].Email != "bob@example.com" {
		t.Errorf("recipients = %+v", request.Recipients)
	}
	assertOpenMark(t, steps[len(steps)-1], completionactions.ActionEmail)
}

func assertOpenMark(t *testing.T, step cellar.CellStep, action completionactions.ActionType) {
	t.Helper()
	if step.HandlerName != HandlerCompletionMarked {
		t.Fatalf("last step = %q, want the open_actions marker", step.HandlerName)
	}
	var mark completionMark
	if err := json.Unmarshal(step.Payload, &mark); err != nil {
		t.Fatal(err)
	}
	want := completionMark{Collection: completionactions.OpenCollection, PollID: "poll-1", Action: action, Key: "poll-1"}
	if mark != want {
		t.Fatalf("mark = %+v, want %+v", mark, want)
	}
}

func TestPollOpenedEmailSkipsRecordedAction(t *testing.T) {
	actions := completionactionstest.New()
	_ = actions.Mark(context.Background(), completionactions.OpenCollection, "poll-1", completionactions.ActionEmail, "poll-1")
	handler := PollOpenedEmailHandler{Actions: actions, Recipients: recipientSourceFunc(func(context.Context, notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error) {
		t.Fatal("recipients selected for an already actioned poll")
		return nil, nil
	})}
	if result := handler.Handle(context.Background(), truths.PollObservedPayload{PollID: "poll-1"}).(cellar.Complete); len(result.NewCells) != 0 {
		t.Fatalf("new cells = %d, want none", len(result.NewCells))
	}
}

func TestPollOpenedEmailMarksActionWithoutRecipients(t *testing.T) {
	handler := PollOpenedEmailHandler{Actions: completionactionstest.New(), Recipients: recipientSourceFunc(func(context.Context, notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error) {
		return nil, nil
	})}
	result, ok := handler.Handle(context.Background(), truths.PollObservedPayload{PollID: "poll-1"}).(cellar.Complete)
	if !ok {
		t.Fatal("handler did not complete")
	}
	if len(result.NewCells) != 1 || len(result.NewCells[0].Steps) != 1 {
		t.Fatalf("new cells = %+v, want one marker-only cell", result.NewCells)
	}
	assertOpenMark(t, result.NewCells[0].Steps[0], completionactions.ActionEmail)
}

func TestPollOpenedEmailReportsRecipientLookupFailure(t *testing.T) {
	handler := PollOpenedEmailHandler{Actions: completionactionstest.New(), Recipients: recipientSourceFunc(func(context.Context, notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error) {
		return nil, errors.New("projection unavailable")
	})}
	if _, ok := handler.Handle(context.Background(), truths.PollObservedPayload{PollID: "poll-1"}).(cellar.ErrorResult); !ok {
		t.Fatal("handler did not report the lookup failure")
	}
}
