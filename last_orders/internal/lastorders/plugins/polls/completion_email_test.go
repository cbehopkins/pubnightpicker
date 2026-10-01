package polls

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/completionactions/completionactionstest"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/truths"
)

type venueMap map[string]venuecache.VenueProjection

func (v venueMap) Get(_ context.Context, id string) (venuecache.VenueProjection, error) {
	venue, ok := v[id]
	if !ok {
		return venuecache.VenueProjection{}, venuecache.ErrNotFound
	}
	return venue, nil
}

func render(content string, variables map[string]any) string {
	for name, value := range variables {
		content = strings.ReplaceAll(content, "{{"+name+"}}", fmt.Sprint(value))
	}
	return content
}

func TestCompletionContentMatchesPythonPubLayout(t *testing.T) {
	venue := venuecache.VenueProjection{Name: "Red Lion", Website: "https://lion.test", Address: "1 High St", Map: "https://map.test"}
	restaurant := venuecache.VenueProjection{Name: "Bistro", VenueType: "restaurant", Website: "https://bistro.test"}

	content := newCompletionContent(venue, &restaurant, "2026-10-02", "18:30", false, true)
	variables := map[string]any{"uid": "u1"}
	for name, value := range content.Variables {
		variables[name] = value
	}

	want := completionBase +
		"\nBefore the pub we are meeting at Bistro. We will be meeting there at 18:30." +
		"\nPub Web Site https://bistro.test\n" +
		"\n\nThis week on 2026-10-02 we will be visiting Red Lion" +
		"\nPub Web Site https://lion.test\n" +
		"\n1 High St\n" +
		"\n\nMap to pub https://map.test" +
		"\n\nUnsubscribe at https://pubnightpicker.web.app/preferences/u1"
	want = strings.Replace(want, "Pub Web Site https://bistro.test", "Restaurant Web Site https://bistro.test", 1)
	if got := render(content.Text, variables); got != want {
		t.Fatalf("text =\n%s\nwant\n%s", got, want)
	}
	if got := render(content.Subject, variables); got != "Pub Night @ Red Lion" {
		t.Errorf("subject = %q", got)
	}
}

func TestCompletionContentMarksReschedulesAndNonPubVenues(t *testing.T) {
	content := newCompletionContent(venuecache.VenueProjection{Name: "Gig", VenueType: "event"}, nil, "2026-10-02", "", true, false)
	text := render(content.Text, content.Variables)
	if !strings.HasPrefix(text, "This week's event has been rescheduled\n\nEvery week") || !strings.Contains(text, "we will be attending Gig.") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "Unsubscribe") {
		t.Error("mailing-list content must not contain an unsubscribe link")
	}
	if got := render(content.Subject, content.Variables); got != "Pub Night @ RESCHEDULED::Gig" {
		t.Errorf("subject = %q", got)
	}
}

func completedHandler(actions completionactions.Store, recipients []notificationprofile.EmailRecipient) PollCompletedEmailHandler {
	return PollCompletedEmailHandler{
		Actions: actions,
		Venues:  venueMap{"pub-1": {ID: "pub-1", Name: "Red Lion"}},
		Recipients: recipientSourceFunc(func(context.Context, notificationprofile.EmailKind) ([]notificationprofile.EmailRecipient, error) {
			return recipients, nil
		}),
	}
}

var completedPoll = truths.PollObservedPayload{PollID: "poll-1", SelectedVenueID: "pub-1", PollDate: "2026-10-02"}

func setupRequest(t *testing.T, cell cellar.CellRequest) durableemail.SendRequest {
	t.Helper()
	if cell.Steps[0].HandlerName != durableemail.HandlerSetup {
		t.Fatalf("first step = %q, want durable setup", cell.Steps[0].HandlerName)
	}
	var request durableemail.SendRequest
	if err := json.Unmarshal(cell.Steps[0].Payload, &request); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	return request
}

func lastStep(cell cellar.CellRequest) cellar.CellStep {
	return cell.Steps[len(cell.Steps)-1]
}

func TestPollCompletedEmailSendsMailingListAndPersonalThenMarks(t *testing.T) {
	handler := completedHandler(completionactionstest.New(), []notificationprofile.EmailRecipient{{UserID: "u1", Email: "u1@example.com"}})

	result, ok := handler.Handle(context.Background(), completedPoll).(cellar.Complete)
	if !ok || len(result.NewCells) != 2 {
		t.Fatalf("result = %#v, want two action cells", result)
	}

	mailingList := setupRequest(t, result.NewCells[0])
	if len(mailingList.Recipients) != 1 || mailingList.Recipients[0].Email != pollCompletedMailingList {
		t.Errorf("mailing-list recipients = %+v", mailingList.Recipients)
	}
	if mailingList.IdempotencyToken != "poll-completed:poll-1:email:pub-1" || strings.Contains(mailingList.Text, "Unsubscribe") {
		t.Errorf("mailing-list request = %+v", mailingList)
	}

	personal := setupRequest(t, result.NewCells[1])
	if len(personal.Recipients) != 1 || personal.Recipients[0].Variables["uid"] != "u1" || !strings.Contains(personal.Text, "{{uid}}") {
		t.Errorf("personal request = %+v", personal)
	}
	if strings.Contains(personal.Subject, "RESCHEDULED") {
		t.Errorf("subject = %q, want an initial completion", personal.Subject)
	}

	for _, cell := range result.NewCells {
		if step := lastStep(cell); step.HandlerName != HandlerCompletionMarked {
			t.Errorf("last step = %q, want the completion marker after Post", step.HandlerName)
		}
	}
}

func TestPollCompletedEmailUsesPythonRescheduleAndDedupeRules(t *testing.T) {
	actions := completionactionstest.New()
	ctx := context.Background()
	_ = actions.Mark(ctx, completionactions.CompletionCollection, "poll-1", completionactions.ActionEmail, "pub-1")
	_ = actions.Mark(ctx, completionactions.CompletionCollection, "poll-1", completionactions.ActionPersonalEmail, "pub-0")
	handler := completedHandler(actions, []notificationprofile.EmailRecipient{{UserID: "u1", Email: "u1@example.com"}})

	result := handler.Handle(ctx, completedPoll).(cellar.Complete)
	if len(result.NewCells) != 1 {
		t.Fatalf("new cells = %d, want only the unrecorded personal action", len(result.NewCells))
	}
	if request := setupRequest(t, result.NewCells[0]); request.Subject != "Pub Night @ RESCHEDULED::{{venue_name}}" {
		t.Errorf("subject = %q, want a reschedule", request.Subject)
	}

	_ = actions.Mark(ctx, completionactions.CompletionCollection, "poll-1", completionactions.ActionPersonalEmail, "pub-1")
	if result := handler.Handle(ctx, completedPoll).(cellar.Complete); len(result.NewCells) != 0 {
		t.Fatalf("new cells = %d, want none once every action is recorded", len(result.NewCells))
	}
}

func TestPollCompletedEmailMarksPersonalActionWithoutRecipients(t *testing.T) {
	handler := completedHandler(completionactionstest.New(), nil)
	result := handler.Handle(context.Background(), completedPoll).(cellar.Complete)
	if len(result.NewCells) != 2 {
		t.Fatalf("new cells = %d, want two", len(result.NewCells))
	}
	personal := result.NewCells[1]
	if len(personal.Steps) != 1 || personal.Steps[0].HandlerName != HandlerCompletionMarked {
		t.Fatalf("personal steps = %+v, want marker only", personal.Steps)
	}
}

func TestPollCompletedEmailRetriesUnknownVenue(t *testing.T) {
	handler := completedHandler(completionactionstest.New(), nil)
	payload := completedPoll
	payload.SelectedVenueID = "missing"
	if _, ok := handler.Handle(context.Background(), payload).(cellar.Retry); !ok {
		t.Fatal("unknown venue should retry")
	}
}

func TestCompletionMarkedHandlerRecordsAction(t *testing.T) {
	actions := completionactionstest.New()
	mark := completionMark{PollID: "poll-1", Action: completionactions.ActionEmail, Key: "pub-1"}
	if _, ok := (CompletionMarkedHandler{Actions: actions}).Handle(context.Background(), mark).(cellar.Complete); !ok {
		t.Fatal("marker did not complete")
	}
	record, _ := actions.Get(context.Background(), completionactions.CompletionCollection, "poll-1")
	if record.NeedsAction(completionactions.ActionEmail, "pub-1") {
		t.Fatal("marker did not record the action")
	}
}
