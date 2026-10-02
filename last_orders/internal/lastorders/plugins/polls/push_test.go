package polls

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/completionactions/completionactionstest"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/plugins/push"
	"last_orders/internal/lastorders/truths"
)

type endpointSourceFunc func(notificationprofile.Selector) []notificationprofile.Endpoint

func (f endpointSourceFunc) GetEligiblePushEndpoints(_ context.Context, selector notificationprofile.Selector) ([]notificationprofile.Endpoint, error) {
	return f(selector), nil
}

type recordingPopulator struct {
	notification push.Notification
	endpoints    []notificationprofile.Endpoint
	then         []cellar.CellStep
	calls        int
}

func (p *recordingPopulator) BaseURL() string { return "https://app.test" }

func (p *recordingPopulator) Populate(_ context.Context, notification push.Notification, endpoints []notificationprofile.Endpoint, then ...cellar.Step) (cellar.Complete, error) {
	p.calls++
	p.notification, p.endpoints = notification, endpoints
	sequence, err := cellar.NewSequence(then...)
	if err != nil {
		return cellar.Complete{}, err
	}
	request, err := sequence.CellRequest()
	if err != nil {
		return cellar.Complete{}, err
	}
	p.then = request.Steps
	return cellar.Complete{}, nil
}

func (p *recordingPopulator) payload(t *testing.T) pushPayload {
	t.Helper()
	var payload pushPayload
	if err := json.Unmarshal(p.notification.Message, &payload); err != nil {
		t.Fatalf("decode push payload: %v", err)
	}
	return payload
}

var pushNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestPollOpenedPushMatchesPythonPayload(t *testing.T) {
	var selected notificationprofile.Kind
	populator := &recordingPopulator{}
	handler := PollOpenedPushHandler{
		Actions: completionactionstest.New(),
		Endpoints: endpointSourceFunc(func(selector notificationprofile.Selector) []notificationprofile.Endpoint {
			selected = selector.Kind
			return []notificationprofile.Endpoint{{UserID: "u1", EndpointID: "e1"}}
		}),
		Push: populator,
		Now:  func() time.Time { return pushNow },
	}

	handler.Handle(context.Background(), truths.PollObservedPayload{PollID: "poll-1", PollDate: "2026-10-02"})

	if selected != notificationprofile.KindPollOpens || len(populator.endpoints) != 1 {
		t.Fatalf("selector = %q, endpoints = %+v", selected, populator.endpoints)
	}
	want := pushPayload{
		EventType: "poll_opened",
		PollID:    "poll-1",
		Title:     "Pub Night voting opened",
		Body:      "Voting has opened for this week's pub night. Tap to open the active polls page.",
		URL:       "https://app.test/active_polls",
		Tag:       "poll-open:poll-1",
		SentAt:    "2026-10-01T12:00:00Z",
	}
	if got := populator.payload(t); got != want {
		t.Fatalf("payload = %+v, want %+v", got, want)
	}
	if populator.notification.ID != "poll-opened:poll-1" || populator.notification.Topic != "poll-1" {
		t.Errorf("notification = %+v", populator.notification)
	}
	if !populator.notification.ExpiresAt.Equal(pushNow.Add(12 * time.Hour)) {
		t.Errorf("expires at %v", populator.notification.ExpiresAt)
	}
	if len(populator.then) != 1 {
		t.Fatalf("follow-up steps = %+v", populator.then)
	}
	assertOpenMark(t, populator.then[0], completionactions.ActionPush)
}

func TestPollOpenedPushSkipsRecordedAction(t *testing.T) {
	actions := completionactionstest.New()
	_ = actions.Mark(context.Background(), completionactions.OpenCollection, "poll-1", completionactions.ActionPush, "poll-1")
	populator := &recordingPopulator{}
	handler := PollOpenedPushHandler{Actions: actions, Push: populator}
	handler.Handle(context.Background(), truths.PollObservedPayload{PollID: "poll-1"})
	if populator.calls != 0 {
		t.Fatal("an already actioned open push was populated again")
	}
}

func completedPushHandler(actions completionactions.Store, populator *recordingPopulator) PollCompletedPushHandler {
	return PollCompletedPushHandler{
		Actions: actions,
		Venues:  venueMap{"pub-1": {Name: "Red Lion"}, "rest-1": {Name: "Bistro"}},
		Endpoints: endpointSourceFunc(func(notificationprofile.Selector) []notificationprofile.Endpoint {
			return []notificationprofile.Endpoint{{UserID: "u1", EndpointID: "e1"}}
		}),
		Push: populator,
		Now:  func() time.Time { return pushNow },
	}
}

func TestPollCompletedPushMatchesPythonPayload(t *testing.T) {
	populator := &recordingPopulator{}
	payload := truths.PollObservedPayload{
		PollID: "poll-1", SelectedVenueID: "pub-1", PollDate: "2026-10-02",
		SelectedRestaurantID: "rest-1", SelectedRestaurantTime: "18:30",
	}
	completedPushHandler(completionactionstest.New(), populator).Handle(context.Background(), payload)

	got := populator.payload(t)
	if got.EventType != "poll_completed" || got.Title != "Pub Night @ Red Lion" {
		t.Errorf("event/title = %q / %q", got.EventType, got.Title)
	}
	if want := "2026-10-02: Red Lion. Pre-pub meal at Bistro. Meet at 18:30."; got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
	if got.URL != "https://app.test/current_events" || got.Tag != "poll-complete:poll-1" {
		t.Errorf("url/tag = %q / %q", got.URL, got.Tag)
	}
	var mark completionMark
	if err := json.Unmarshal(populator.then[0].Payload, &mark); err != nil {
		t.Fatal(err)
	}
	if mark.Collection != completionactions.CompletionCollection || mark.Action != completionactions.ActionPush || mark.Key != "pub-1:rest-1:18:30" {
		t.Errorf("mark = %+v", mark)
	}
}

func TestPollCompletedPushUsesPythonRescheduleAndDedupeRules(t *testing.T) {
	actions := completionactionstest.New()
	ctx := context.Background()
	_ = actions.Mark(ctx, completionactions.CompletionCollection, "poll-1", completionactions.ActionPush, "pub-0")
	populator := &recordingPopulator{}
	handler := completedPushHandler(actions, populator)

	handler.Handle(ctx, completedPoll)
	if got := populator.payload(t); got.EventType != "poll_rescheduled" || got.Title != "Pub Night rescheduled: Red Lion" {
		t.Fatalf("payload = %+v, want a reschedule", got)
	}

	_ = actions.Mark(ctx, completionactions.CompletionCollection, "poll-1", completionactions.ActionPush, "pub-1")
	populator.calls = 0
	handler.Handle(ctx, completedPoll)
	if populator.calls != 0 {
		t.Fatal("an already actioned configuration was populated again")
	}
}

func TestPollCompletedPushRetriesUnknownVenue(t *testing.T) {
	payload := completedPoll
	payload.SelectedVenueID = "missing"
	if _, ok := completedPushHandler(completionactionstest.New(), &recordingPopulator{}).Handle(context.Background(), payload).(cellar.Retry); !ok {
		t.Fatal("unknown venue should retry")
	}
}

type completersFunc func() []string

func (f completersFunc) CanCompletePollUserIDs(context.Context) ([]string, error) { return f(), nil }

func TestManualCompletionPushTargetsCompletersOnly(t *testing.T) {
	var selected notificationprofile.Selector
	populator := &recordingPopulator{}
	handler := ManualCompletionPushHandler{
		Completers: completersFunc(func() []string { return []string{"admin"} }),
		Endpoints: endpointSourceFunc(func(selector notificationprofile.Selector) []notificationprofile.Endpoint {
			selected = selector
			return []notificationprofile.Endpoint{{UserID: "admin", EndpointID: "e1"}}
		}),
		Push: populator,
		Now:  func() time.Time { return pushNow },
	}

	handler.Handle(context.Background(), truths.PollManualCompletionRequired{PollID: "poll-1", PollDate: "2026-10-02"})

	if selected.Kind != notificationprofile.KindPollCompletes || len(selected.UserIDs) != 1 || selected.UserIDs[0] != "admin" {
		t.Fatalf("selector = %+v", selected)
	}
	want := pushPayload{
		EventType: "poll_manual_completion_required",
		PollID:    "poll-1",
		Title:     "Poll needs manual completion",
		Body:      "2026-10-02: auto-complete could not choose a clear winner. Tap to complete the poll.",
		URL:       "https://app.test/active_polls",
		Tag:       "poll-manual-complete:poll-1",
		SentAt:    "2026-10-01T12:00:00Z",
	}
	if got := populator.payload(t); got != want {
		t.Fatalf("payload = %+v, want %+v", got, want)
	}
	if populator.notification.Topic != "manual-complete-poll-1" {
		t.Errorf("topic = %q", populator.notification.Topic)
	}
}

func TestManualCompletionPushWithoutCompletersSendsNothing(t *testing.T) {
	populator := &recordingPopulator{}
	handler := ManualCompletionPushHandler{
		Completers: completersFunc(func() []string { return nil }),
		Endpoints: endpointSourceFunc(func(notificationprofile.Selector) []notificationprofile.Endpoint {
			t.Fatal("an empty audience would select every user")
			return nil
		}),
		Push: populator,
	}
	handler.Handle(context.Background(), truths.PollManualCompletionRequired{PollID: "poll-1"})
	if populator.calls != 0 {
		t.Fatal("push populated without completers")
	}
}
