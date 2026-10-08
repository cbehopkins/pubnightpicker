package diagnosticsconfig

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	for _, test := range []struct {
		data    map[string]any
		want    bool
		invalid bool
	}{
		{nil, false, false}, {map[string]any{}, false, false},
		{map[string]any{SilenceField: true}, true, false},
		{map[string]any{SilenceField: false}, false, false},
		{map[string]any{SilenceField: "true"}, false, true},
		{map[string]any{SilenceField: nil}, false, true},
	} {
		got, err := Decode(test.data)
		if got.SilenceNotifications != test.want || (err != nil) != test.invalid {
			t.Fatalf("Decode(%v) = %t, %v", test.data, got, err)
		}
	}
	for _, field := range []string{SilenceField, "NotifyPollActorWhenSilenced", "KeepChatNotificationsWhenSilenced"} {
		if _, err := Decode(map[string]any{field: "true"}); err == nil {
			t.Fatalf("invalid %s accepted", field)
		}
	}
}

func TestExceptionMatrix(t *testing.T) {
	for _, silence := range []bool{false, true} {
		for _, actor := range []bool{false, true} {
			for _, chat := range []bool{false, true} {
				t.Run(fmt.Sprintf("%t/%t/%t", silence, actor, chat), func(t *testing.T) {
					settings := Settings{SilenceNotifications: silence, NotifyPollActorWhenSilenced: actor, KeepChatNotificationsWhenSilenced: chat}
					for _, test := range []struct {
						purpose, actorUID, recipientUID string
						actorAllowed, chatAllowed       bool
					}{
						{PurposePollOpened, "alice", "alice", true, false},
						{PurposePollCompleted, "bob", "bob", true, false},
						{PurposePollOpened, "alice", "bob", false, false},
						{PurposePollCompleted, "", "", false, false},
						{PurposePollRescheduled, "alice", "alice", false, false},
						{PurposeGlobalChat, "", "bob", false, true},
						{PurposeEventChat, "", "bob", false, true},
						{"diagnostic", "alice", "alice", false, false},
						{"", "alice", "alice", false, false},
					} {
						want := !silence || actor && test.actorAllowed || chat && test.chatAllowed
						if got := settings.AllowsLive(test.purpose, test.actorUID, test.recipientUID); got != want {
							t.Fatalf("%+v: live=%t want=%t", test, got, want)
						}
					}
				})
			}
		}
	}
}

func TestReadinessRetentionAndCancellation(t *testing.T) {
	updates := make(chan bool)
	failed := make(chan struct{})
	service := New(func(ctx context.Context, update func(Settings)) error {
		select {
		case value := <-updates:
			update(Settings{SilenceNotifications: value})
			close(failed)
			return errors.New("disconnected")
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil)
	if _, known := service.Snapshot(); known {
		t.Fatal("known before initial snapshot")
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	updates <- true
	select {
	case <-service.Ready():
	case <-time.After(time.Second):
		t.Fatal("not ready")
	}
	<-failed
	if value, known := service.Snapshot(); !known || !value.SilenceNotifications {
		t.Fatal("lost last known value")
	}
}

func TestRuntimeUpdates(t *testing.T) {
	updates := make(chan bool)
	applied := make(chan struct{})
	service := New(func(ctx context.Context, update func(Settings)) error {
		for {
			select {
			case value := <-updates:
				update(Settings{SilenceNotifications: value})
				applied <- struct{}{}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}, nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, value := range []bool{false, true, false} {
		updates <- value
		<-applied
		if got, known := service.Snapshot(); got.SilenceNotifications != value || !known {
			t.Fatalf("snapshot = %t, %t", got, known)
		}
	}
}

func TestInitialFailureDoesNotEstablishReadiness(t *testing.T) {
	called := make(chan struct{})
	service := New(func(ctx context.Context, update func(Settings)) error {
		close(called)
		return errors.New("unavailable")
	}, nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-called
	if _, known := service.Snapshot(); known {
		t.Fatal("initial failure defaulted to live")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}
