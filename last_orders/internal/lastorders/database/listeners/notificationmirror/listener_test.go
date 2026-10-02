package notificationmirror_test

import (
	"context"
	"testing"

	"last_orders/internal/lastorders/database/listeners/notificationmirror"
	"last_orders/internal/lastorders/database/listeners/notificationmirror/notificationmirrortest"
)

func TestListenerMirrorsRequestsAndSkipsPushTests(t *testing.T) {
	source := notificationmirrortest.New()
	source.Requests = []notificationmirror.Document{
		{ID: "diagnostics", Data: map[string]any{"manual": int64(123)}},
		{ID: "push_test", Data: map[string]any{"alice": "test"}},
	}
	listener, err := notificationmirror.New(notificationmirror.Config{Source: source})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener.Start(ctx)
	defer listener.Close()

	for source.Ack("diagnostics")["manual"] != int64(123) {
		select {
		case <-ctx.Done():
			t.Fatal("listener stopped before mirroring")
		default:
		}
	}
	if _, ok := source.Ack("push_test")["alice"]; ok {
		t.Fatal("notification mirror must leave push_test to the push-test listener")
	}
}
