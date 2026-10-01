package app_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"last_orders/internal/lastorders/components/completionactions"
	"last_orders/internal/lastorders/components/completionactions/completionactionstest"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/notificationprofile/notificationprofiletest"
	"last_orders/internal/lastorders/components/pushsources/pushsourcestest"
	"last_orders/internal/lastorders/database/listeners/chatmessages"
	"last_orders/internal/lastorders/database/listeners/chatmessages/chatmessagestest"
	"last_orders/internal/lastorders/database/listeners/notificationmirror"
	"last_orders/internal/lastorders/database/listeners/notificationmirror/notificationmirrortest"
	"last_orders/internal/lastorders/database/listeners/pushtest/pushtesttest"
)

func TestPollOpenedTruthPushesToEligibleEndpointsThenRecordsAction(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "poll-opened-push.db")
	logs := &syncBuffer{}
	actions := completionactionstest.New()
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.CompletionActions = actions
	cfg.NotificationProfileSource = &notificationprofiletest.Source{
		UserChanges: []notificationprofile.Change{
			userDocument("alice", map[string]any{"webPushEnabled": true}),
			userDocument("bob", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"pollOpens": false}}),
		},
		EndpointChange: []notificationprofile.Change{
			endpointDocument("alice", "phone"),
			endpointDocument("alice", "laptop"),
			endpointDocument("bob", "phone"),
		},
	}
	a := newApp(t, cfg)
	defer a.Close()

	if err := enqueueNewPoll(t, a, "poll-push"); err != nil {
		t.Fatalf("enqueue new poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		record, err := actions.Get(context.Background(), completionactions.OpenCollection, "poll-push")
		if err != nil {
			t.Fatalf("read open actions: %v", err)
		}
		if !record.NeedsAction(completionactions.ActionPush, "poll-push") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("open actions = %+v, want push recorded", record)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}

	logged := logs.String()
	if pushes := strings.Count(logged, `"msg":"dummy push sent"`); pushes != 2 {
		t.Errorf("dummy pushes = %d, want alice's two endpoints only", pushes)
	}
	for _, want := range []string{"https://push.test/phone", "https://push.test/laptop", `\"eventType\":\"poll_opened\"`} {
		if !strings.Contains(logged, want) {
			t.Errorf("logs missing %q", want)
		}
	}
}

func TestNotificationPingMirrorsRequestIntoAck(t *testing.T) {
	t.Parallel()

	mirrorSource := notificationmirrortest.New()
	mirrorSource.Requests = []notificationmirror.Document{{
		ID:   "diagnostics",
		Data: map[string]any{"manual": int64(123)},
	}}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "notification-ping.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.NotificationMirrorSource = mirrorSource
	a := newApp(t, cfg)
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if mirrorSource.Ack("diagnostics")["manual"] == int64(123) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for notification ACK")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}
}

func TestChatMessageListenerFiltersRecipientsAndMarksProcessed(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "chat-push.db")
	logs := &syncBuffer{}
	pushSources := pushsourcestest.New()
	chatSource := chatmessagestest.New()
	chatSource.Documents = []chatmessages.Document{{
		ID:   "message-1",
		Data: map[string]any{"uid": "author", "displayName": "Ann", "text": "hello"},
	}}
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.PushSources = pushSources
	cfg.ChatMessageSource = chatSource
	cfg.NotificationProfileSource = &notificationprofiletest.Source{
		UserChanges: []notificationprofile.Change{
			userDocument("author", map[string]any{"webPushEnabled": true}),
			userDocument("bob", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"globalChat": true}}),
			userDocument("muted", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"globalChat": false}}),
		},
		EndpointChange: []notificationprofile.Change{
			endpointDocument("author", "author-phone"),
			endpointDocument("bob", "bob-phone"),
			endpointDocument("muted", "muted-phone"),
		},
	}
	a := newApp(t, cfg)
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for !pushSources.Processed("message-1") {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for chat message to be marked processed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}

	if pushes := strings.Count(logs.String(), `"msg":"dummy push sent"`); pushes != 1 {
		t.Fatalf("dummy pushes = %d, want only bob's endpoint", pushes)
	}
	if !strings.Contains(logs.String(), "https://push.test/bob-phone") || strings.Contains(logs.String(), "muted-phone") {
		t.Fatalf("chat push recipient filtering was wrong: %s", logs.String())
	}
}

func TestPushTestListenerAcknowledgesAndDeletesRequest(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "push-test.db")
	logs := &syncBuffer{}
	pushSources := pushsourcestest.New()
	pushTestSource := pushtesttest.New()
	pushTestSource.Requests["alice"] = "check-1"
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.PushSources = pushSources
	cfg.PushTestSource = pushTestSource
	cfg.NotificationProfileSource = &notificationprofiletest.Source{
		UserChanges:    []notificationprofile.Change{userDocument("alice", map[string]any{"webPushEnabled": true})},
		EndpointChange: []notificationprofile.Change{endpointDocument("alice", "alice-phone")},
	}
	a := newApp(t, cfg)
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		value, acknowledged := pushSources.Ack("alice")
		if acknowledged && value == "check-1" && len(pushSources.Deleted) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("push test did not complete: ack=%v deleted=%v", value, pushSources.Deleted)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run app: %v", err)
	}
	if !strings.Contains(logs.String(), `\"eventType\":\"diagnostic_push_test\"`) {
		t.Fatalf("diagnostic payload missing: %s", logs.String())
	}
}

func endpointDocument(userID, endpointID string) notificationprofile.Change {
	return notificationprofile.Change{Kind: notificationprofile.ChangeAdded, Doc: notificationprofile.Document{
		ID:     endpointID,
		UserID: userID,
		Data: map[string]any{
			"endpoint": "https://push.test/" + endpointID,
			"p256dh":   "p256dh",
			"auth":     "auth",
			"active":   true,
		},
	}}
}
