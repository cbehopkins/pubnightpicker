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
	"last_orders/internal/lastorders/components/diagnosticsconfig"
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

func TestPollPushBudgetDefersEndpointsAndKeepsActionPending(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "push-budget.db")
	logs := &syncBuffer{}
	actions := completionactionstest.New()
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.PushDailyLimit = 1
	cfg.CompletionActions = actions
	cfg.NotificationProfileSource = &notificationprofiletest.Source{
		UserChanges:    []notificationprofile.Change{userDocument("alice", map[string]any{"webPushEnabled": true})},
		EndpointChange: []notificationprofile.Change{endpointDocument("alice", "phone"), endpointDocument("alice", "laptop")},
	}
	application := newApp(t, cfg)
	defer application.Close()
	if err := enqueueNewPoll(t, application, "push-budget"); err != nil {
		t.Fatal(err)
	}
	db := openSQLite(t, dbPath)
	var accepted, pending int
	runUntil(t, application, func() bool {
		if err := db.QueryRow(`SELECT COUNT(*) FROM push_deliveries WHERE state = 'Accepted'`).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM push_deliveries WHERE state = 'Pending' AND attempts = 0`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		return accepted == 1 && pending == 1
	})
	if accepted != 1 || pending != 1 || strings.Count(logs.String(), `"msg":"dummy push sent"`) != 1 {
		t.Fatalf("accepted %d, untouched pending %d; logs %s", accepted, pending, logs.String())
	}
	record, err := actions.Get(context.Background(), completionactions.OpenCollection, "push-budget")
	if err != nil {
		t.Fatal(err)
	}
	if !record.NeedsAction(completionactions.ActionPush, "push-budget") {
		t.Fatal("marked push action before all endpoints became terminal")
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

func TestSharedConfigurationSilencesPollChatAndDiagnosticPush(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "silenced-push.db")
	logs := &syncBuffer{}
	actions := completionactionstest.New()
	pushSources := pushsourcestest.New()
	chatSource := chatmessagestest.New()
	chatSource.Documents = []chatmessages.Document{{ID: "silenced-message", Data: map[string]any{"uid": "author", "displayName": "Ann", "text": "hello"}}}
	pushTestSource := pushtesttest.New()
	pushTestSource.Requests["alice"] = "silenced-check"
	cfg := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.CompletionActions = actions
	cfg.PushSources = pushSources
	cfg.ChatMessageSource = chatSource
	cfg.PushTestSource = pushTestSource
	cfg.DiagnosticsSource = func(ctx context.Context, update func(diagnosticsconfig.Settings)) error {
		update(diagnosticsconfig.Settings{SilenceNotifications: true})
		<-ctx.Done()
		return ctx.Err()
	}
	cfg.NotificationProfileSource = &notificationprofiletest.Source{
		UserChanges:    []notificationprofile.Change{userDocument("alice", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"globalChat": true}})},
		EndpointChange: []notificationprofile.Change{endpointDocument("alice", "phone")},
	}
	application := newApp(t, cfg)
	defer application.Close()
	if err := enqueueNewPoll(t, application, "silenced-poll"); err != nil {
		t.Fatal(err)
	}
	runUntil(t, application, func() bool {
		record, err := actions.Get(context.Background(), completionactions.OpenCollection, "silenced-poll")
		if err != nil {
			t.Fatal(err)
		}
		value, acknowledged := pushSources.Ack("alice")
		return !record.NeedsAction(completionactions.ActionPush, "silenced-poll") && pushSources.Processed("silenced-message") && acknowledged && value == "silenced-check"
	})
	db := openSQLite(t, dbPath)
	var accepted, attempts int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(attempts), 0) FROM push_deliveries WHERE state = 'Accepted'`).Scan(&accepted, &attempts); err != nil {
		t.Fatal(err)
	}
	if accepted != 3 || attempts != 0 {
		t.Fatalf("handled = %d, attempts = %d; logs: %s", accepted, attempts, logs.String())
	}
	if strings.Contains(logs.String(), `"msg":"dummy push sent"`) || strings.Count(logs.String(), `"msg":"push silenced; handled without sending"`) != 3 {
		t.Fatalf("push was not suppressed: %s", logs.String())
	}
}

func TestChatExceptionRetainsPreferencesAndAuthorExclusion(t *testing.T) {
	logs := &syncBuffer{}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "chat-exception.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.DiagnosticsSource = func(ctx context.Context, update func(diagnosticsconfig.Settings)) error {
		update(diagnosticsconfig.Settings{SilenceNotifications: true, KeepChatNotificationsWhenSilenced: true})
		<-ctx.Done()
		return ctx.Err()
	}
	source := pushsourcestest.New()
	cfg.PushSources = source
	chatSource := chatmessagestest.New()
	chatSource.Documents = []chatmessages.Document{{ID: "live-chat", Data: map[string]any{"uid": "author", "text": "hello"}}}
	cfg.ChatMessageSource = chatSource
	cfg.NotificationProfileSource = &notificationprofiletest.Source{
		UserChanges: []notificationprofile.Change{
			userDocument("author", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"globalChat": true}}),
			userDocument("reader", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"globalChat": true}}),
			userDocument("muted", map[string]any{"webPushEnabled": true, "pushPreferences": map[string]any{"globalChat": false}}),
		},
		EndpointChange: []notificationprofile.Change{endpointDocument("author", "author-phone"), endpointDocument("reader", "reader-phone"), endpointDocument("muted", "muted-phone")},
	}
	application := newApp(t, cfg)
	defer application.Close()
	runUntil(t, application, func() bool { return source.Processed("live-chat") })
	if strings.Count(logs.String(), `"msg":"dummy push sent"`) != 1 || !strings.Contains(logs.String(), "reader-phone") || strings.Contains(logs.String(), "author-phone") || strings.Contains(logs.String(), "muted-phone") {
		t.Fatalf("unexpected chat recipients: %s", logs.String())
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
