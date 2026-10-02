package pushevents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/pushsources"
	"last_orders/internal/lastorders/components/pushsources/pushsourcestest"
	"last_orders/internal/lastorders/plugins/push"
	"last_orders/internal/lastorders/truths"
)

type fakeProfiles struct {
	endpoints   []notificationprofile.Endpoint
	muted       map[string][]string
	selector    notificationprofile.Selector
	preferences int
}

func (p *fakeProfiles) GetEligiblePushEndpoints(_ context.Context, selector notificationprofile.Selector) ([]notificationprofile.Endpoint, error) {
	p.selector = selector
	return p.endpoints, nil
}

func (p *fakeProfiles) Preferences(_ context.Context, userID string) (notificationprofile.UserPreferences, error) {
	p.preferences++
	return notificationprofile.UserPreferences{UserID: userID, EventChatMutedPollIDs: p.muted[userID]}, nil
}

type fakePusher struct {
	notification push.Notification
	endpoints    []notificationprofile.Endpoint
	then         []cellar.Step
	accepted     int
}

func (p *fakePusher) BaseURL() string { return "https://app.test" }

func (p *fakePusher) Populate(_ context.Context, notification push.Notification, endpoints []notificationprofile.Endpoint, then ...cellar.Step) (cellar.Complete, error) {
	p.notification, p.endpoints, p.then = notification, endpoints, then
	return cellar.Complete{}, nil
}

func (p *fakePusher) Accepted(context.Context, string) (int, error) { return p.accepted, nil }

func endpoint(userID string) notificationprofile.Endpoint {
	return notificationprofile.Endpoint{UserID: userID, EndpointID: "e", URL: "https://push.test/" + userID}
}

func userIDs(endpoints []notificationprofile.Endpoint) []string {
	ids := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ids = append(ids, endpoint.UserID)
	}
	return ids
}

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestEventChatFiltersMembersMutedAuthorAndDeliveredEndpoints(t *testing.T) {
	source := pushsourcestest.New()
	source.Attendees["poll-1"] = []string{"attendee", "muted", "author"}
	source.Participants["poll-1"] = []string{"participant", "delivered"}
	source.Chats["msg-1"] = pushsources.ChatHistory{Exists: true, DeliveredEndpoints: []string{EndpointHash(endpoint("delivered"))}}
	profiles := &fakeProfiles{
		endpoints: []notificationprofile.Endpoint{
			endpoint("attendee"), endpoint("participant"), endpoint("muted"),
			endpoint("author"), endpoint("outsider"), endpoint("delivered"),
		},
		muted: map[string][]string{"muted": {"poll-1"}},
	}
	pusher := &fakePusher{}
	handler := ChatPushHandler{Source: source, Profiles: profiles, Push: pusher, Now: func() time.Time { return now }}

	handler.Handle(context.Background(), truths.ChatMessagePosted{
		MessageID: "msg-1", ScopeType: "event", ScopeID: "poll-1", AuthorUserID: "author", SenderName: "Ann", Text: "hello",
	})

	if profiles.selector.Kind != notificationprofile.KindEventChat {
		t.Errorf("kind = %q", profiles.selector.Kind)
	}
	if got := userIDs(pusher.endpoints); len(got) != 2 || got[0] != "attendee" || got[1] != "participant" {
		t.Fatalf("recipients = %v, want attendee and participant", got)
	}
	var payload chatPayload
	if err := json.Unmarshal(pusher.notification.Message, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.EventType != "chat_message_sent_event" || payload.Title != "Ann in Event Chat" || payload.URL != "https://app.test/chat/event/poll-1" || payload.Tag != "chat:poll-1" || payload.PollID == nil || *payload.PollID != "poll-1" {
		t.Errorf("payload = %+v", payload)
	}
	if len(pusher.notification.Topic) > 32 || !pusher.notification.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("notification = %+v", pusher.notification)
	}
	if len(pusher.then) != 1 || pusher.then[0].HandlerName != HandlerChatProcessed {
		t.Errorf("follow-up = %+v", pusher.then)
	}
}

func TestGlobalChatMatchesPythonPayload(t *testing.T) {
	pusher := &fakePusher{}
	handler := ChatPushHandler{
		Source:   pushsourcestest.New(),
		Profiles: &fakeProfiles{endpoints: []notificationprofile.Endpoint{endpoint("reader"), endpoint("author")}},
		Push:     pusher,
		Now:      func() time.Time { return now },
	}
	long := ""
	for range 120 {
		long += "é"
	}
	handler.Handle(context.Background(), truths.ChatMessagePosted{MessageID: "msg-1", AuthorUserID: "author", Text: long})

	var raw map[string]any
	if err := json.Unmarshal(pusher.notification.Message, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["eventType"] != "chat_message_sent_global" || raw["title"] != "Someone in Global Chat" || raw["tag"] != "chat:main" || raw["url"] != "https://app.test/chat" {
		t.Errorf("payload = %v", raw)
	}
	if pollID, present := raw["pollId"]; !present || pollID != nil {
		t.Errorf("pollId = %v, want explicit null", pollID)
	}
	if body := raw["body"].(string); len([]rune(body)) != 100 {
		t.Errorf("body length = %d characters, want 100", len([]rune(body)))
	}
	if got := userIDs(pusher.endpoints); len(got) != 1 || got[0] != "reader" {
		t.Errorf("recipients = %v", got)
	}
}

func TestProcessedChatIsSkipped(t *testing.T) {
	source := pushsourcestest.New()
	_ = source.MarkChatProcessed(context.Background(), "msg-1", "global", "main")
	pusher := &fakePusher{}
	ChatPushHandler{Source: source, Profiles: &fakeProfiles{}, Push: pusher}.Handle(context.Background(), truths.ChatMessagePosted{MessageID: "msg-1"})
	if pusher.notification.ID != "" {
		t.Fatal("an already processed chat message was populated")
	}
}

func TestPushTestTargetsRequesterAndAcknowledgesOnlyAcceptedDeliveries(t *testing.T) {
	source := pushsourcestest.New()
	profiles := &fakeProfiles{endpoints: []notificationprofile.Endpoint{endpoint("admin")}}
	pusher := &fakePusher{}
	request := truths.PushTestRequested{UserID: "admin", Value: json.RawMessage(`1727780000000`)}
	PushTestHandler{Source: source, Profiles: profiles, Push: pusher, Now: func() time.Time { return now }}.Handle(context.Background(), request)

	if profiles.selector.Kind != notificationprofile.KindDiagnostic || len(profiles.selector.UserIDs) != 1 || profiles.selector.UserIDs[0] != "admin" {
		t.Fatalf("selector = %+v", profiles.selector)
	}
	var payload diagnosticPayload
	if err := json.Unmarshal(pusher.notification.Message, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.EventType != "diagnostic_push_test" || string(payload.RequestedValue) != "1727780000000" || payload.Tag != "push-diagnostic:admin" {
		t.Errorf("payload = %+v", payload)
	}

	var completed pushTestCompleted
	encoded, _ := json.Marshal(pusher.then[0].Payload)
	if err := json.Unmarshal(encoded, &completed); err != nil {
		t.Fatal(err)
	}
	finish := PushTestCompletedHandler{Source: source, Push: pusher}
	finish.Handle(context.Background(), completed)
	if _, acked := source.Ack("admin"); acked || len(source.Deleted) != 1 {
		t.Fatalf("ack written without an accepted delivery, deleted = %v", source.Deleted)
	}

	pusher.accepted = 1
	finish.Handle(context.Background(), completed)
	if value, acked := source.Ack("admin"); !acked || value != int64(1727780000000) {
		t.Fatalf("ack = %#v, %t; want the integer request value", value, acked)
	}
}

func TestDuplicatePushTestRequestIsConsumed(t *testing.T) {
	source := pushsourcestest.New()
	_ = source.AckPushTest(context.Background(), "admin", int64(5))
	pusher := &fakePusher{}
	PushTestHandler{Source: source, Profiles: &fakeProfiles{}, Push: pusher}.Handle(context.Background(),
		truths.PushTestRequested{UserID: "admin", Value: json.RawMessage(`5`)})
	if pusher.notification.ID != "" || len(source.Deleted) != 1 {
		t.Fatalf("duplicate request sent or not cleared: id=%q deleted=%v", pusher.notification.ID, source.Deleted)
	}
}
