package push

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/ratelimit"

	webpush "github.com/SherClockHolmes/webpush-go"
	_ "modernc.org/sqlite"
)

type txAdapter struct{ tx *sql.Tx }

func (a txAdapter) Exec(query string, args ...any) error {
	_, err := a.tx.Exec(query, args...)
	return err
}

func (a txAdapter) ExecContext(ctx context.Context, query string, args ...any) error {
	_, err := a.tx.ExecContext(ctx, query, args...)
	return err
}

func (a txAdapter) Query(string, ...any) (cellar.ApplicationRows, error) {
	return nil, errors.ErrUnsupported
}
func (a txAdapter) QueryContext(context.Context, string, ...any) (cellar.ApplicationRows, error) {
	return nil, errors.ErrUnsupported
}
func (a txAdapter) QueryRow(query string, args ...any) cellar.ApplicationRow {
	return a.tx.QueryRow(query, args...)
}
func (a txAdapter) QueryRowContext(ctx context.Context, query string, args ...any) cellar.ApplicationRow {
	return a.tx.QueryRowContext(ctx, query, args...)
}

func applyWork(t *testing.T, db *sql.DB, work []cellar.ApplicationWork) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, item := range work {
		if err := item(txAdapter{tx: tx}); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply work: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

type fakeSender struct {
	status int
	err    error
	sent   int
}

func (s *fakeSender) Send(context.Context, Subscription, []byte, time.Duration, string) (int, error) {
	s.sent++
	return s.status, s.err
}

type fakeInvalidator struct {
	invalidated []string
	err         error
}

func (i *fakeInvalidator) InvalidateEndpoint(_ context.Context, userID, endpointID string) error {
	i.invalidated = append(i.invalidated, userID+"/"+endpointID)
	return i.err
}

func newTestPlugin(t *testing.T, sender Sender, invalidator *fakeInvalidator) *Plugin {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "push.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tokens, err := ratelimit.New("push.send", 1000, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := New(db, invalidator, Options{Client: ClientDummy, Tokens: tokens, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("new plugin: %v", err)
	}
	plugin.sender = sender
	return plugin
}

var testEndpoints = []notificationprofile.Endpoint{
	{UserID: "alice", EndpointID: "a", URL: "https://push.test/a", P256DH: "p", Auth: "x"},
	{UserID: "bob", EndpointID: "b", URL: "https://push.test/b", P256DH: "p", Auth: "x"},
}

func populate(t *testing.T, plugin *Plugin, then ...cellar.Step) cellar.Complete {
	t.Helper()
	result, err := plugin.Populate(context.Background(), Notification{
		ID: "n1", Message: []byte(`{"title":"t"}`), Topic: "topic", ExpiresAt: time.Now().Add(time.Hour),
	}, testEndpoints, then...)
	if err != nil {
		t.Fatalf("populate: %v", err)
	}
	applyWork(t, plugin.db, result.ApplicationWork)
	return result
}

func deliveryState(t *testing.T, plugin *Plugin, userID string) string {
	t.Helper()
	var state string
	if err := plugin.db.QueryRow(`SELECT state FROM push_deliveries WHERE notification_id = 'n1' AND user_id = ?`, userID).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func firstDelivery(t *testing.T, result cellar.Complete) deliveryRequest {
	t.Helper()
	var request deliveryRequest
	if err := decode(result.NewCells[0].Steps[0].Payload, &request); err != nil {
		t.Fatalf("decode delivery: %v", err)
	}
	return request
}

func TestPopulateCreatesOneDeliveryPerEndpointAndAWaitingStep(t *testing.T) {
	plugin := newTestPlugin(t, &fakeSender{}, &fakeInvalidator{})
	then := cellar.Step{HandlerName: "test.after", Payload: struct{}{}}
	result := populate(t, plugin, then)

	if len(result.NewCells) != 3 {
		t.Fatalf("new cells = %d, want two deliveries and one waiter", len(result.NewCells))
	}
	for _, cell := range result.NewCells[:2] {
		if cell.Steps[0].HandlerName != HandlerDeliver {
			t.Errorf("delivery step = %q", cell.Steps[0].HandlerName)
		}
	}
	waiter := result.NewCells[2].Steps
	if len(waiter) != 2 || waiter[0].HandlerName != HandlerWait || waiter[1].HandlerName != "test.after" {
		t.Errorf("waiter steps = %+v", waiter)
	}
	if deliveryState(t, plugin, "alice") != StatePending || deliveryState(t, plugin, "bob") != StatePending {
		t.Error("population did not record pending deliveries")
	}

	if replay, err := plugin.Populate(context.Background(), Notification{ID: "n1"}, testEndpoints, then); err != nil || len(replay.NewCells) != 0 {
		t.Fatalf("replay = %+v, %v; want no duplicate work", replay, err)
	}
}

func TestPopulateWithoutEndpointsRunsFollowUpDirectly(t *testing.T) {
	plugin := newTestPlugin(t, &fakeSender{}, &fakeInvalidator{})
	result, err := plugin.Populate(context.Background(), Notification{ID: "n1"}, nil, cellar.Step{HandlerName: "test.after", Payload: struct{}{}})
	if err != nil {
		t.Fatalf("populate: %v", err)
	}
	if len(result.NewCells) != 1 || result.NewCells[0].Steps[0].HandlerName != "test.after" {
		t.Fatalf("new cells = %+v, want only the follow-up", result.NewCells)
	}
}

type deliveryTokens struct {
	wait  int
	calls int
}

func (source *deliveryTokens) Acquire() int                    { source.calls++; return source.wait }
func (source *deliveryTokens) AcquireN(count int) (int, error) { return source.Acquire(), nil }

func TestSuppressedPushIsHandledWithoutQuotaInvalidationOrReplay(t *testing.T) {
	sender := &fakeSender{status: http.StatusGone}
	invalidator := &fakeInvalidator{}
	plugin := newTestPlugin(t, sender, invalidator)
	tokens := &deliveryTokens{wait: 86400}
	plugin.tokens = tokens
	silenced := true
	plugin.silenceNotifications = func() (bool, bool) { return silenced, true }
	population := populate(t, plugin)
	for _, cell := range population.NewCells[:2] {
		var request deliveryRequest
		if err := decode(cell.Steps[0].Payload, &request); err != nil {
			t.Fatal(err)
		}
		result, ok := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Complete)
		if !ok {
			t.Fatal("suppressed delivery did not complete")
		}
		applyWork(t, plugin.db, result.ApplicationWork)
	}
	if sender.sent != 0 || tokens.calls != 0 || len(invalidator.invalidated) != 0 {
		t.Fatal("suppression invoked live side effects")
	}
	accepted, err := plugin.Accepted(context.Background(), "n1")
	if err != nil || accepted != 2 {
		t.Fatalf("handled deliveries = %d, %v", accepted, err)
	}
	if _, ok := (waitHandler{plugin: plugin}).Handle(context.Background(), waitRequest{NotificationID: "n1"}).(cellar.Complete); !ok {
		t.Fatal("suppressed deliveries blocked follow-up")
	}
	silenced = false
	request := firstDelivery(t, population)
	if _, ok := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Complete); !ok {
		t.Fatal("terminal delivery replayed")
	}
	if sender.sent != 0 || tokens.calls != 0 {
		t.Fatal("unsilencing replayed handled push")
	}
	var attempts int
	if err := plugin.db.QueryRow(`SELECT SUM(attempts) FROM push_deliveries`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("attempts = %d, %v", attempts, err)
	}
}

func TestPushUnknownConfigurationDefersAndResumesLive(t *testing.T) {
	sender := &fakeSender{status: http.StatusCreated}
	plugin := newTestPlugin(t, sender, &fakeInvalidator{})
	tokens := &deliveryTokens{}
	plugin.tokens = tokens
	known := false
	plugin.silenceNotifications = func() (bool, bool) { return false, known }
	request := firstDelivery(t, populate(t, plugin))
	request.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	result, ok := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Retry)
	if !ok || result.NotBefore == nil || result.NotBefore.After(request.ExpiresAt) || len(result.ApplicationWork) != 0 {
		t.Fatalf("unknown configuration result = %#v", result)
	}
	if sender.sent != 0 || tokens.calls != 0 || deliveryState(t, plugin, "alice") != StatePending {
		t.Fatal("unknown configuration submitted push")
	}
	known = true
	request.ExpiresAt = time.Now().Add(time.Hour)
	complete, ok := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Complete)
	if !ok {
		t.Fatal("live push did not complete")
	}
	applyWork(t, plugin.db, complete.ApplicationWork)
	if sender.sent != 1 || tokens.calls != 1 || deliveryState(t, plugin, "alice") != StateAccepted {
		t.Fatal("live sending did not resume")
	}
}

func TestDeliverRateLimitDefersWithoutAttemptOrInvalidation(t *testing.T) {
	for _, wait := range []int{30, 86400} {
		t.Run(time.Duration(wait).String(), func(t *testing.T) {
			sender := &fakeSender{status: http.StatusGone}
			invalidator := &fakeInvalidator{}
			plugin := newTestPlugin(t, sender, invalidator)
			source := &deliveryTokens{wait: wait}
			plugin.tokens = source
			request := firstDelivery(t, populate(t, plugin))
			before := time.Now()
			result := (deliverHandler{plugin: plugin}).Handle(context.Background(), request)
			retry, ok := result.(cellar.Retry)
			if !ok || retry.NotBefore == nil || !retry.NotBefore.After(before) || retry.NotBefore.After(request.ExpiresAt) || len(retry.ApplicationWork) != 0 {
				t.Fatalf("retry = %#v", result)
			}
			if wait == 86400 && !retry.NotBefore.Equal(request.ExpiresAt) {
				t.Fatal("retry not capped at expiry")
			}
			applyWork(t, plugin.db, retry.ApplicationWork)
			var attempts int
			if err := plugin.db.QueryRow(`SELECT attempts FROM push_deliveries WHERE notification_id = 'n1' AND user_id = 'alice'`).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if sender.sent != 0 || len(invalidator.invalidated) != 0 || attempts != 0 || source.calls != 1 || deliveryState(t, plugin, "alice") != StatePending {
				t.Fatalf("sent %d, invalidated %v, attempts %d, acquisitions %d", sender.sent, invalidator.invalidated, attempts, source.calls)
			}
			request.ExpiresAt = time.Now().Add(-time.Second)
			expired := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Complete)
			applyWork(t, plugin.db, expired.ApplicationWork)
			if source.calls != 1 || deliveryState(t, plugin, "alice") != StateExpired {
				t.Fatal("expiry acquired tokens or did not finish")
			}
		})
	}
}

func TestDeliverSharesBudgetAndChargesProviderRetries(t *testing.T) {
	sender := &fakeSender{err: errors.New("network failure")}
	plugin := newTestPlugin(t, sender, &fakeInvalidator{})
	source, err := ratelimit.New("push.send", 1, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	plugin.tokens = source
	population := populate(t, plugin)
	request := firstDelivery(t, population)
	failed := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Retry)
	applyWork(t, plugin.db, failed.ApplicationWork)
	for _, cell := range population.NewCells[:2] {
		var request deliveryRequest
		if err := decode(cell.Steps[0].Payload, &request); err != nil {
			t.Fatal(err)
		}
		limited := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Retry)
		if len(limited.ApplicationWork) != 0 {
			t.Fatal("rate limit counted as failure")
		}
	}
	if sender.sent != 1 {
		t.Fatalf("sent = %d; want only first attempt", sender.sent)
	}
}

func TestNewRequiresPushTokenSource(t *testing.T) {
	plugin := newTestPlugin(t, &fakeSender{}, &fakeInvalidator{})
	if _, err := New(plugin.db, &fakeInvalidator{}, Options{Client: ClientDummy}); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestDeliverOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		err         error
		wantState   string
		wantRetry   bool
		invalidated bool
	}{
		{name: "accepted", status: http.StatusCreated, wantState: StateAccepted},
		{name: "gone", status: http.StatusGone, wantState: StateRejected, invalidated: true},
		{name: "forbidden", status: http.StatusForbidden, wantState: StateRejected, invalidated: true},
		{name: "rate limited", status: http.StatusTooManyRequests, wantState: StatePending, wantRetry: true},
		{name: "network", err: errors.New("dial failed"), wantState: StatePending, wantRetry: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeSender{status: test.status, err: test.err}
			invalidator := &fakeInvalidator{}
			plugin := newTestPlugin(t, sender, invalidator)
			request := firstDelivery(t, populate(t, plugin))

			result := deliverHandler{plugin: plugin}.Handle(context.Background(), request)
			switch typed := result.(type) {
			case cellar.Complete:
				if test.wantRetry {
					t.Fatal("completed, want retry")
				}
				applyWork(t, plugin.db, typed.ApplicationWork)
			case cellar.Retry:
				if !test.wantRetry {
					t.Fatal("retried, want completion")
				}
				if typed.NotBefore == nil || !typed.NotBefore.After(time.Now()) || typed.NotBefore.After(request.ExpiresAt) {
					t.Errorf("retry at %v, want a future time no later than expiry", typed.NotBefore)
				}
				applyWork(t, plugin.db, typed.ApplicationWork)
			default:
				t.Fatalf("result = %#v", result)
			}

			if got := deliveryState(t, plugin, "alice"); got != test.wantState {
				t.Errorf("state = %q, want %q", got, test.wantState)
			}
			if (len(invalidator.invalidated) == 1) != test.invalidated {
				t.Errorf("invalidated = %v, want %t", invalidator.invalidated, test.invalidated)
			}
		})
	}
}

func TestDeliverExpiresWithoutSending(t *testing.T) {
	sender := &fakeSender{status: http.StatusCreated}
	plugin := newTestPlugin(t, sender, &fakeInvalidator{})
	request := firstDelivery(t, populate(t, plugin))
	request.ExpiresAt = time.Now().Add(-time.Second)

	result, ok := deliverHandler{plugin: plugin}.Handle(context.Background(), request).(cellar.Complete)
	if !ok {
		t.Fatal("expired delivery did not complete")
	}
	applyWork(t, plugin.db, result.ApplicationWork)
	if sender.sent != 0 || deliveryState(t, plugin, "alice") != StateExpired {
		t.Fatalf("sent = %d, state = %q", sender.sent, deliveryState(t, plugin, "alice"))
	}
}

func TestDeliverSkipsTerminalDelivery(t *testing.T) {
	sender := &fakeSender{status: http.StatusCreated}
	plugin := newTestPlugin(t, sender, &fakeInvalidator{})
	request := firstDelivery(t, populate(t, plugin))
	applyWork(t, plugin.db, []cellar.ApplicationWork{finishWork(request, StateAccepted)})

	if _, ok := (deliverHandler{plugin: plugin}).Handle(context.Background(), request).(cellar.Complete); !ok || sender.sent != 0 {
		t.Fatalf("sent = %d, want an already accepted delivery skipped", sender.sent)
	}
}

func TestWaitRetriesUntilEveryDeliveryIsTerminal(t *testing.T) {
	plugin := newTestPlugin(t, &fakeSender{}, &fakeInvalidator{})
	result := populate(t, plugin)
	wait := waitHandler{plugin: plugin}

	if _, ok := wait.Handle(context.Background(), waitRequest{NotificationID: "n1"}).(cellar.Retry); !ok {
		t.Fatal("waiter completed while deliveries were pending")
	}
	for _, cell := range result.NewCells[:2] {
		var request deliveryRequest
		if err := decode(cell.Steps[0].Payload, &request); err != nil {
			t.Fatal(err)
		}
		applyWork(t, plugin.db, []cellar.ApplicationWork{finishWork(request, StateAccepted)})
	}
	if _, ok := wait.Handle(context.Background(), waitRequest{NotificationID: "n1"}).(cellar.Complete); !ok {
		t.Fatal("waiter did not complete after every delivery finished")
	}
}

func TestTTLMatchesPythonPolicy(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tests := map[string]time.Duration{
		"2026-10-02": 12 * time.Hour,
		"2026-10-01": time.Hour,
		"2026-12-01": 5 * 24 * time.Hour,
		"not-a-date": time.Hour,
	}
	for date, want := range tests {
		if got := TTL(date, now); got != want {
			t.Errorf("TTL(%q) = %v, want %v", date, got, want)
		}
	}
}

func TestTopicMatchesPythonPolicy(t *testing.T) {
	if got := Topic("poll_1-A"); got != "poll_1-A" {
		t.Errorf("short topic = %q", got)
	}
	if got := Topic("poll/1"); got != "poll-1" {
		t.Errorf("sanitised topic = %q", got)
	}
	long := Topic(strings.Repeat("a", 40))
	if len(long) != 32 || !strings.HasPrefix(long, strings.Repeat("a", 19)+"-") {
		t.Errorf("long topic = %q", long)
	}
}

func TestWebPushSenderDerivesPublicKeyAndSubject(t *testing.T) {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	sender, err := newWebPushSender(privateKey, "mailto:ops@example.com")
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if sender.publicKey != strings.TrimRight(publicKey, "=") || sender.subscriber != "ops@example.com" {
		t.Fatalf("sender = %+v, want derived public key %q", sender, publicKey)
	}
	for _, subject := range []string{"", "not-an-email"} {
		if _, err := newWebPushSender(privateKey, subject); err == nil {
			t.Errorf("subject %q accepted", subject)
		}
	}
	if _, err := newWebPushSender("", "ops@example.com"); err == nil {
		t.Error("missing private key accepted")
	}
}
